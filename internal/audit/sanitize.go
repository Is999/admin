package audit

import (
	"bytes"
	"fmt"
	"strings"

	jsoniter "github.com/json-iterator/go"
)

// exactSensitiveKeys 只列出无法通过敏感关键词识别的真实契约字段，不维护推测别名。
var exactSensitiveKeys = map[string]struct{}{
	"mfaSecureKey":   {}, // API 的 MFA 绑定秘钥
	"mfa_secure_key": {}, // 数据模型的 MFA 绑定秘钥
	"secure":         {}, // 登录密码或 MFA 动态码
	"secureCode":     {}, // 登录安全验证码
	"captcha":        {}, // 登录图形验证码
	"twoStepKey":     {}, // MFA 二次票据 key
	"twoStepValue":   {}, // MFA 二次票据 value
}

// Serialize 把任意数据转成适合落审计日志的字符串，同时完成脱敏和长度截断。
// 采用 JSON 字节流扫描方案，避免为动态 map key 做多次解析和重建。
func Serialize(data any, maxBytes int) string {
	if data == nil {
		return ""
	}

	if text, ok := data.(string); ok {
		return truncate(text, maxBytes)
	}

	// 先统一编码为 JSON 字节流，再用轻量状态机按 key 脱敏动态对象。
	raw, err := jsoniter.Marshal(data)
	if err != nil {
		return truncate(fmt.Sprint(data), maxBytes)
	}

	if len(raw) == 0 || (raw[0] != '{' && raw[0] != '[') {
		return truncate(string(raw), maxBytes)
	}

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
	var buf bytes.Buffer
	buf.Grow(len(data))

	i := 0
	for i < len(data) {
		if data[i] != '"' {
			buf.WriteByte(data[i])
			i++
			continue
		}

		start := i
		i++
		for i < len(data) {
			if data[i] == '"' && !isEscapedJSONQuote(data, i) {
				break
			}
			i++
		}
		if i >= len(data) {
			buf.Write(data[start:])
			break
		}
		end := i // end 指向闭合引号

		strContent := string(data[start+1 : end])

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

		buf.Write(data[start : end+1])
		i = end + 1

		if isKey && isSensitiveKey(strContent) {
			buf.Write(data[i:j])
			i = j
			buf.WriteString(`"***"`)
			i = skipJSONValue(data, i)
		}
	}
	return buf.Bytes()
}

// skipJSONValue 跳过完整 JSON value，支持字符串、数字、布尔、null、对象和数组。
func skipJSONValue(data []byte, start int) int {
	i := start
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
			if data[i] == '"' && !isEscapedJSONQuote(data, i) {
				return i + 1
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
			if data[i] == '"' && !isEscapedJSONQuote(data, i) {
				inString = !inString
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

// isEscapedJSONQuote 判断当前位置的双引号是否被奇数个反斜杠转义。
func isEscapedJSONQuote(data []byte, quoteIndex int) bool {
	slashCount := 0
	for i := quoteIndex - 1; i >= 0 && data[i] == '\\'; i-- {
		slashCount++
	}
	return slashCount%2 == 1
}

// isSensitiveKey 对真实契约字段和携带敏感材料关键词的字段统一脱敏。
func isSensitiveKey(key string) bool {
	if _, ok := exactSensitiveKeys[key]; ok {
		return true
	}
	// 这里只压缩命名分隔符以识别敏感材料关键词，不扩展接口契约。
	compact := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
	return strings.Contains(compact, "password") ||
		strings.Contains(compact, "token") ||
		strings.Contains(compact, "secret") ||
		strings.Contains(compact, "privatekey") ||
		strings.Contains(compact, "publickey") ||
		strings.Contains(compact, "aeskey") ||
		strings.Contains(compact, "aesiv")
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
