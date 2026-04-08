package audit

import (
	"bytes"
	"fmt"
	"strings"

	jsoniter "github.com/json-iterator/go"
)

// sensitiveKeys 维护常见敏感字段名，序列化审计数据时会统一脱敏。
var sensitiveKeys = map[string]struct{}{
	"password":                   {}, // 登录密码
	"token":                      {}, // 访问令牌
	"authorization":              {}, // 认证请求头
	"jwt":                        {}, // JWT 令牌正文
	"jwt_secret":                 {}, // JWT 签名密钥
	"mfa_secure_key":             {}, // MFA 绑定密钥
	"mfasecurekey":               {}, // MFA 绑定密钥兼容写法
	"aes_key":                    {}, // AES 对称密钥
	"aeskey":                     {}, // AES 对称密钥兼容写法
	"aes_key_ref":                {}, // AES 密钥引用
	"aeskeyref":                  {}, // AES 密钥引用兼容写法
	"aes_iv":                     {}, // AES 初始向量
	"aesiv":                      {}, // AES 初始向量兼容写法
	"aes_iv_ref":                 {}, // AES 初始向量引用
	"aesivref":                   {}, // AES 初始向量引用兼容写法
	"rsa_private_key":            {}, // RSA 私钥正文
	"rsaprivatekey":              {}, // RSA 私钥正文兼容写法
	"rsa_private_key_server_ref": {}, // 服务端 RSA 私钥引用
	"rsaprivatekeyserverref":     {}, // 服务端 RSA 私钥引用兼容写法
	"rsa_public_key":             {}, // RSA 公钥正文
	"rsapublickey":               {}, // RSA 公钥正文兼容写法
	"rsa_public_key_user_ref":    {}, // 用户侧 RSA 公钥引用
	"rsapublickeyuserref":        {}, // 用户侧 RSA 公钥引用兼容写法
	"rsa_public_key_server_ref":  {}, // 服务端 RSA 公钥引用
	"rsapublickeyserverref":      {}, // 服务端 RSA 公钥引用兼容写法
	"secret":                     {}, // 通用密钥字段
	"securekey":                  {}, // 通用安全密钥兼容写法
}

// maskJSONAPI 是统一的高性能 JSON 序列化入口，用于审计日志脱敏前的原始编码。
var maskJSONAPI = jsoniter.Config{
	EscapeHTML:             true,
	SortMapKeys:            true,
	ValidateJsonRawMessage: true,
}.Froze()

// Serialize 把任意数据转成适合落审计日志的字符串，同时完成脱敏和长度截断。
// 采用基于字节流的高性能 JSON 扫描方案，将原始数据的 3 次解析/序列化降低为 1 次。
func Serialize(data any, maxBytes int) string {
	if data == nil {
		return ""
	}

	if text, ok := data.(string); ok {
		return truncate(text, maxBytes)
	}

	// 首先检查是不是 map 或 slice 里面嵌套了 map
	// 因为 json-iterator 的 Extension 在处理动态 map key 时比较复杂
	// 为了达到极致的通用流式替换，我们直接基于 jsoniter 生成普通 JSON 字节流
	// 然后配合前面写的高性能状态机字节流扫描。

	// 这里使用 json-iterator 进行超高速的序列化
	raw, err := jsoniter.Marshal(data)
	if err != nil {
		return truncate(fmt.Sprint(data), maxBytes)
	}

	// 2. 如果不是对象或数组，直接返回
	if len(raw) == 0 || (raw[0] != '{' && raw[0] != '[') {
		return truncate(string(raw), maxBytes)
	}

	// 3. 在字节流层面进行高性能的脱敏替换
	// 由于 jsoniter 的 Extension 在处理任意深度嵌套的 map[string]any 动态 Key 时开销很大且易错，
	// 业界最高效的做法正是：极速序列化库 (jsoniter) + 极速状态机字节扫描 (sanitizeJSONBytes)。
	sanitized := sanitizeJSONBytes(raw)

	return truncate(string(sanitized), maxBytes)
}

// SanitizeText 对已存在的文本内容做二次脱敏，主要用于历史审计日志回显兜底。
func SanitizeText(text string, maxBytes int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		return truncate(string(sanitizeJSONBytes([]byte(text))), maxBytes)
	}
	return truncate(text, maxBytes)
}

// sanitizeJSONBytes 基于简单的状态流扫描 JSON 字节并替换敏感字段的值。
// 仅解析第一层或嵌套的对象 key，匹配到敏感 key 时，将其对应的 value (无论 string, number, 还是 {}/[]) 替换为 "***"。
func sanitizeJSONBytes(data []byte) []byte {
	// ... (原有的字节流扫描代码保持不变)
	var buf bytes.Buffer
	// 预分配稍大一点的容量以避免频繁扩容
	buf.Grow(len(data))

	i := 0
	for i < len(data) {
		// 寻找 key 的开始引号
		if data[i] != '"' {
			buf.WriteByte(data[i])
			i++
			continue
		}

		// 找到一个字符串（可能是 key 也可能是 value）
		start := i
		i++
		// 查找字符串结束引号
		for i < len(data) {
			if data[i] == '"' {
				slashCount := 0
				for k := i - 1; k >= 0 && data[k] == '\\'; k-- {
					slashCount++
				}
				if slashCount%2 == 0 {
					break
				}
			}
			i++
		}
		if i >= len(data) {
			// JSON 格式截断，直接追加剩余部分并退出
			buf.Write(data[start:])
			break
		}
		end := i // end 指向闭合引号

		// 提取字符串内容
		strContent := string(data[start+1 : end])

		// 判断这个字符串是否是 key (即后面跟着冒号)
		isKey := false
		j := end + 1
		for j < len(data) {
			if data[j] == ' ' || data[j] == '\t' || data[j] == '\n' || data[j] == '\r' {
				j++
				continue
			}
			if data[j] == ':' {
				isKey = true
				j++ // 跳过冒号
			}
			break
		}

		buf.Write(data[start : end+1]) // 写入 key（带引号）
		i = end + 1

		if isKey {
			// 如果是 key，且是敏感字段
			if isSensitiveKey(strContent) {
				// 将剩余空白和冒号写入
				buf.Write(data[i:j])
				i = j

				// 替换值为 "***"
				buf.WriteString(`"***"`)

				// 跳过原始的 value
				i = skipJSONValue(data, i)
			}
		}
	}
	return buf.Bytes()
}

// skipJSONValue 跳过一个完整的 JSON value (字符串、数字、布尔、null、对象、数组)
func skipJSONValue(data []byte, start int) int {
	i := start
	// 跳过前导空白
	for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\n' || data[i] == '\r') {
		i++
	}
	if i >= len(data) {
		return i
	}

	switch data[i] {
	case '"': // 字符串
		i++
		for i < len(data) {
			if data[i] == '"' {
				// 检查前面的反斜杠数量，判断是否是转义的引号
				slashCount := 0
				for k := i - 1; k >= 0 && data[k] == '\\'; k-- {
					slashCount++
				}
				// 只有偶数个反斜杠（或0个）才是真正的结束引号
				if slashCount%2 == 0 {
					return i + 1
				}
			}
			i++
		}
	case '{', '[': // 对象或数组
		openChar := data[i]
		closeChar := byte('}')
		if openChar == '[' {
			closeChar = ']'
		}
		depth := 1
		i++
		inString := false
		for i < len(data) && depth > 0 {
			if data[i] == '"' {
				slashCount := 0
				for k := i - 1; k >= 0 && data[k] == '\\'; k-- {
					slashCount++
				}
				if slashCount%2 == 0 {
					inString = !inString
				}
			} else if !inString {
				if data[i] == openChar {
					depth++
				} else if data[i] == closeChar {
					depth--
				}
			}
			i++
		}
		return i
	default: // 数字、布尔、null
		for i < len(data) {
			c := data[i]
			if c == ',' || c == '}' || c == ']' || c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				break
			}
			i++
		}
	}
	return i
}

// isSensitiveKey 除显式名单外，也对 password/token/secret 关键字做模糊拦截。
func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	if _, ok := sensitiveKeys[normalized]; ok {
		return true
	}
	compact := strings.ReplaceAll(normalized, "_", "")
	return strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(compact, "privatekey") ||
		strings.Contains(compact, "publickey") ||
		strings.Contains(compact, "aeskey") ||
		strings.Contains(compact, "aesiv") ||
		strings.Contains(compact, "rsaprivatekey") ||
		strings.Contains(compact, "rsapublickey")
}

// truncate 控制审计负载体积，避免超长请求体或错误信息把日志撑爆。
func truncate(text string, maxBytes int) string {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	if maxBytes <= 16 {
		return text[:maxBytes]
	}
	return text[:maxBytes-16] + "...(truncated)"
}
