package security

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"admin_cron/helper"

	utils "github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
)

const (
	// SignFieldAll 表示签名时对当前请求或响应中的所有首层字段参与排序签名。
	SignFieldAll = "*"
	// CipherWholeBody 表示整包请求体或整包响应体执行加解密。
	CipherWholeBody = "cipher"
	// CipherJSONPrefix 表示字段值在加解密前需要按 JSON 编解码。
	CipherJSONPrefix = "json:"
)

// SignRule 描述单个路由请求与响应参与签名的首层字段。
type SignRule struct {
	Request  []string // Request 表示请求验签字段列表
	Response []string // Response 表示响应回签字段列表
}

// RouteSecurityPolicy 定义单个路由的请求验签、响应回签与响应加密策略。
// 请求加密字段不再由后端静态配置，统一由前端通过 X-Cipher 动态声明。
type RouteSecurityPolicy struct {
	RequestSign    []string // RequestSign 表示请求验签字段；为空时默认走全字段验签
	ResponseSign   []string // ResponseSign 表示响应回签字段；为空表示该路由不做响应回签
	ResponseCipher []string // ResponseCipher 表示响应需要加密的字段；cipher 代表整包加密
}

// sensitiveIdentityFields 收口登录/身份校验链路的敏感请求字段。
// 这组字段统一用于账号登录、兼容登录预校验等身份入口，避免每条路由重复维护相同签名参数。
var sensitiveIdentityFields = []string{"username", "password", "secureCode", "key", "captcha"}

// sensitiveProfileFields 收口管理员资料变更链路的敏感请求字段。
// 这组字段覆盖账号资料、密码、MFA 秘钥和二次确认票据，供管理员新增与编辑共用。
var sensitiveProfileFields = []string{"username", "realName", "email", "phone", "password", "mfaSecureKey", "twoStepKey", "twoStepValue"}

// sensitiveSecretKeyFields 收口秘钥管理写操作的敏感请求字段。
// 这组字段包含主配置开关、版本选路、AES/RSA 文件引用和二次确认票据，供新增和编辑秘钥接口共用。
var sensitiveSecretKeyFields = []string{"uuid", "title", "keyVersion", "aesKeyRef", "aesIvRef", "rsaPublicKeyUserRef", "rsaPublicKeyServerRef", "rsaPrivateKeyServerRef", "status", "signStatus", "cryptoStatus", "versionStatus", "stableVersion", "grayVersion", "grayPercent", "remark", "twoStepKey", "twoStepValue"}

// sensitiveSecretKeyResponseFields 收口秘钥详情返回时需要加密的敏感响应字段。
// 秘钥详情读取场景只加密文件引用字段，避免把完整绝对路径直接暴露在前端网络面板中。
var sensitiveSecretKeyResponseFields = []string{"aesKeyRef", "aesIvRef", "rsaPublicKeyUserRef", "rsaPublicKeyServerRef", "rsaPrivateKeyServerRef"}

// RouteSecurityPolicies 定义后台接口的推荐安全策略：
// 1. 默认所有接口请求都验签；
// 2. 请求加密字段由前端通过 X-Cipher 动态声明；
// 3. token、秘钥详情等敏感响应字段按路由配置做响应加密和回签。
var RouteSecurityPolicies = map[string]RouteSecurityPolicy{
	// auth.login 表示新版登录接口：响应对 token、手机号与 MFA 绑定地址做字段级加密。
	"auth.login": {
		RequestSign:    sensitiveIdentityFields,
		ResponseSign:   []string{"token"},
		ResponseCipher: []string{"token", "user.phone", "user.buildMFAURL"},
	},
	// auth.refresh 表示刷新令牌接口：响应只保护新 token 与刷新标记。
	"auth.refresh": {
		ResponseSign:   []string{"token", "isRefresh"},
		ResponseCipher: []string{"token"},
	},
	// auth.login_after_info 表示登录后初始化接口：token 走响应字段级加密。
	"auth.login_after_info": {
		ResponseSign:   []string{"token"},
		ResponseCipher: []string{"token"},
	},
	// user.build_secret_verify_account 表示兼容登录预校验接口：保护 token、手机号和二维码。
	"user.build_secret_verify_account": {
		RequestSign:    sensitiveIdentityFields,
		ResponseSign:   []string{"token"},
		ResponseCipher: []string{"token", "user.phone", "user.buildMFAURL"},
	},
	// user.mine 表示个人中心资料接口：手机号与 MFA 绑定地址属于敏感响应字段，需要字段级加密保护。
	"user.mine": {
		ResponseCipher: []string{"phone", "buildMFAURL"},
	},
	// user.check_secure 表示通用安全码校验接口：仅校验安全码字段签名。
	"user.check_secure": {
		RequestSign: []string{"secure"},
	},
	// user.check_mfa_secure 表示 MFA 动态码校验接口：校验动态码、场景和值班绑定秘钥。
	"user.check_mfa_secure": {
		RequestSign: []string{"secure", "scenarios", "mfaSecureKey"},
	},
	// admin.add 表示新增管理员接口：资料与初始安全字段统一参与签名。
	"admin.add": {
		RequestSign: sensitiveProfileFields,
	},
	// admin.update 表示编辑管理员接口：资料与安全字段统一参与签名。
	"admin.update": {
		RequestSign: sensitiveProfileFields,
	},
	// admin.delete 表示删除管理员接口：仅校验二次确认票据，避免敏感删除请求绕过统一签名保护。
	"admin.delete": {
		RequestSign: []string{"twoStepKey", "twoStepValue"},
	},
	// admin.status.update 表示修改管理员状态接口：只校验状态与二次确认票据。
	"admin.status.update": {
		RequestSign: []string{"status", "twoStepKey", "twoStepValue"},
	},
	// user.admin_mfa_status 表示后台修改管理员 MFA 状态接口：只校验状态切换与二次确认票据。
	"user.admin_mfa_status": {
		RequestSign: []string{"mfaStatus", "twoStepKey", "twoStepValue"},
	},
	// admin.password.reset 表示管理员重置密码接口：密码相关字段必须参与签名。
	"admin.password.reset": {
		RequestSign: []string{"password", "twoStepKey", "twoStepValue"},
	},
	// admin.reset.initial_state 表示管理员重置初始状态接口：密码类字段与二次确认票据统一保护。
	"admin.reset.initial_state": {
		RequestSign: []string{"password", "twoStepKey", "twoStepValue"},
	},
	// admin.role.update 表示覆盖保存管理员角色接口：角色集合与二次确认票据统一参与签名。
	"admin.role.update": {
		RequestSign: []string{"roleIDs", "twoStepKey", "twoStepValue"},
	},
	// admin.role.add 表示追加管理员角色接口：角色集合与二次确认票据统一参与签名。
	"admin.role.add": {
		RequestSign: []string{"roleIDs", "twoStepKey", "twoStepValue"},
	},
	// user.update_password 表示个人中心改密接口：旧密码、新密码与确认密码必须参与签名。
	"user.update_password": {
		RequestSign: []string{"passwordOld", "passwordNew", "confirmPassword", "twoStepKey", "twoStepValue"},
	},
	// user.update_mine 表示个人中心资料更新接口：个人资料与二次确认票据统一参与签名。
	"user.update_mine": {
		RequestSign: []string{"realName", "email", "phone", "avatar", "description", "twoStepKey", "twoStepValue"},
	},
	// user.update_mfa_status 表示个人中心启停 MFA 接口：状态、秘钥与二次确认票据统一参与签名。
	"user.update_mfa_status": {
		RequestSign: []string{"mfaStatus", "mfaSecureKey", "twoStepKey", "twoStepValue"},
	},
	// user.update_mfa_secret 表示刷新个人 MFA 秘钥接口：只保护新秘钥和二次确认票据。
	"user.update_mfa_secret": {
		RequestSign: []string{"mfaSecureKey", "twoStepKey", "twoStepValue"},
	},
	// user.update_avatar 表示个人头像更新接口：仅校验头像字段签名。
	"user.update_avatar": {
		RequestSign: []string{"avatar"},
	},
	// secretKey.get 表示秘钥详情接口：仅对返回的真实秘钥材料字段做响应加密。
	"secretKey.get": {
		ResponseCipher: sensitiveSecretKeyResponseFields,
	},
	// security.debug.sign 表示安全调试台签名接口：调试结果整体按字段回签并保护输出文本。
	"security.debug.sign": {
		ResponseCipher: []string{"payloadText", "signText", "sign"},
		ResponseSign:   []string{SignFieldAll},
	},
	// security.debug.verify 表示安全调试台验签接口：调试结果整体按字段回签并保护输出文本。
	"security.debug.verify": {
		ResponseCipher: []string{"payloadText", "signText", "sign"},
		ResponseSign:   []string{SignFieldAll},
	},
	// security.debug.encrypt 表示安全调试台加密接口：保护原文、密文与结果文本，避免调试台泄漏敏感内容。
	"security.debug.encrypt": {
		ResponseCipher: []string{"payloadText", "resultPayloadText", "ciphertext", "plaintext"},
		ResponseSign:   []string{SignFieldAll},
	},
	// security.debug.decrypt 表示安全调试台解密接口：保护原文、密文与结果文本，避免调试台泄漏敏感内容。
	"security.debug.decrypt": {
		ResponseCipher: []string{"payloadText", "resultPayloadText", "ciphertext", "plaintext"},
		ResponseSign:   []string{SignFieldAll},
	},
	// secretKey.add 表示新增秘钥接口：秘钥材料写入前统一参与签名。
	"secretKey.add": {
		RequestSign: sensitiveSecretKeyFields,
	},
	// secretKey.edit 表示编辑秘钥接口：秘钥材料变更统一参与签名。
	"secretKey.edit": {
		RequestSign: sensitiveSecretKeyFields,
	},
	// secretKey.editStatus 表示启停秘钥接口：只校验状态与二次确认票据。
	"secretKey.editStatus": {
		RequestSign: []string{"status", "twoStepKey", "twoStepValue"},
	},
	// secretKey.renew 表示刷新秘钥缓存接口：只校验二次确认票据。
	"secretKey.renew": {
		RequestSign: []string{"twoStepKey", "twoStepValue"},
	},
	// secretKey.validate 表示秘钥路径预检接口：返回结构化校验结果并对明细结果回签。
	"secretKey.validate": {
		RequestSign:  sensitiveSecretKeyFields,
		ResponseSign: []string{SignFieldAll},
	},
	// secretKey.self_check 表示秘钥运行态自检接口：校验版本和二次确认票据。
	"secretKey.self_check": {
		RequestSign:  []string{"keyVersion", "twoStepKey", "twoStepValue"},
		ResponseSign: []string{SignFieldAll},
	},
	// user_tag.workflow_lease.release 表示释放工作流互斥锁接口：保护参数和二次确认票据。
	"user_tag.workflow_lease.release": {
		RequestSign: []string{"workflowId", "mode", "reason", "twoStepKey", "twoStepValue"},
	},
}

// PolicyByRoute 根据路由别名读取统一安全策略。
// 未显式配置的接口默认启用“请求全字段验签”，满足“每个接口都需要验签”的推荐要求。
func PolicyByRoute(route string) RouteSecurityPolicy {
	route = strings.TrimSpace(route)
	if route == "" || strings.EqualFold(route, "ignore") {
		return RouteSecurityPolicy{}
	}
	if policy, ok := RouteSecurityPolicies[route]; ok {
		return policy
	}
	return RouteSecurityPolicy{
		RequestSign: []string{SignFieldAll},
	}
}

// BuildSignString 生成待签名字符串，保持 laravel-admin getSignStr 的字段排序与 key 拼接规则。
// traceID 统一对应请求头 X-Trace-Id，作为签名盐值和防重放标识参与计算。
func BuildSignString(data map[string]any, signParams []string, traceID, appID string) string {
	params := resolveSignParams(data, signParams)
	sort.Strings(params)

	var builder strings.Builder
	for _, key := range params {
		value, ok := data[key]
		if !ok || isEmptySignValue(value) {
			continue
		}
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(SignValueString(value))
		builder.WriteString("&")
	}
	builder.WriteString("key=")
	builder.WriteString(utils.Md5(appID + "-" + traceID))
	return builder.String()
}

// EncodeCipherParams 把字段级加密配置编码成请求头值，整包加密直接返回 cipher。
func EncodeCipherParams(params []string) string {
	params = helper.UniqueNonEmptyStrings(params)
	if len(params) == 0 {
		return ""
	}
	if len(params) == 1 && strings.EqualFold(params[0], CipherWholeBody) {
		return CipherWholeBody
	}
	body, err := json.Marshal(params)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(body)
}

// resolveSignParams 解析签名字段列表；配置了 * 时，对当前 map 的所有首层字段签名。
func resolveSignParams(data map[string]any, signParams []string) []string {
	params := helper.UniqueNonEmptyStrings(signParams)
	if !utils.IsHas(SignFieldAll, params) {
		return params
	}
	result := make([]string, 0, len(data))
	for key := range data {
		switch strings.TrimSpace(key) {
		case "", "sign", "ciphertext":
			continue
		default:
			result = append(result, key)
		}
	}
	return result
}

// SignValueString 把参与签名的值转换为稳定字符串。
func SignValueString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", v), "0"), ".")
	case float32:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", v), "0"), ".")
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, bool:
		return fmt.Sprint(v)
	default:
		body, err := stableJSONMarshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(body)
	}
}

// stableJSONMarshal 对复杂值执行递归稳定 JSON 序列化，确保对象 key 顺序在前后端完全一致。
func stableJSONMarshal(value any) ([]byte, error) {
	normalized, err := normalizeStableJSONValue(value)
	if err != nil {
		return nil, errors.Tag(err)
	}
	var builder strings.Builder
	if err := writeStableJSON(&builder, normalized); err != nil {
		return nil, errors.Tag(err)
	}
	return []byte(builder.String()), nil
}

// normalizeStableJSONValue 先把任意复杂值收敛成 map/slice/json.Number 基础结构，避免 struct 等类型直接参与排序。
func normalizeStableJSONValue(value any) (any, error) {
	switch v := value.(type) {
	case nil, string, bool, json.Number,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return v, nil
	case map[string]any:
		return v, nil
	case []any:
		return v, nil
	default:
		body, err := json.Marshal(v)
		if err != nil {
			return nil, errors.Tag(err)
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		var normalized any
		if err := decoder.Decode(&normalized); err != nil {
			return nil, errors.Tag(err)
		}
		return normalized, nil
	}
}

// writeStableJSON 递归输出稳定 JSON，map key 统一按字典序排序。
func writeStableJSON(builder *strings.Builder, value any) error {
	switch v := value.(type) {
	case nil:
		builder.WriteString("null")
		return nil
	case string:
		body, err := json.Marshal(v)
		if err != nil {
			return errors.Tag(err)
		}
		builder.Write(body)
		return nil
	case bool:
		if v {
			builder.WriteString("true")
		} else {
			builder.WriteString("false")
		}
		return nil
	case json.Number:
		builder.WriteString(v.String())
		return nil
	case float64:
		builder.WriteString(strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", v), "0"), "."))
		return nil
	case float32:
		builder.WriteString(strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", v), "0"), "."))
		return nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		builder.WriteString(fmt.Sprint(v))
		return nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		builder.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				builder.WriteByte(',')
			}
			keyBody, err := json.Marshal(key)
			if err != nil {
				return errors.Tag(err)
			}
			builder.Write(keyBody)
			builder.WriteByte(':')
			if err := writeStableJSON(builder, v[key]); err != nil {
				return errors.Tag(err)
			}
		}
		builder.WriteByte('}')
		return nil
	case []any:
		builder.WriteByte('[')
		for index, item := range v {
			if index > 0 {
				builder.WriteByte(',')
			}
			if err := writeStableJSON(builder, item); err != nil {
				return errors.Tag(err)
			}
		}
		builder.WriteByte(']')
		return nil
	default:
		normalized, err := normalizeStableJSONValue(v)
		if err != nil {
			return errors.Tag(err)
		}
		return writeStableJSON(builder, normalized)
	}
}

// isEmptySignValue 判断字段是否应跳过签名，空字符串和 nil
func isEmptySignValue(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return text == ""
	}
	return false
}
