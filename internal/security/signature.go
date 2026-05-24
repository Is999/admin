package security

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"admin/helper"
	"admin/internal/routealias"

	utils "github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
)

const (
	// SignFieldAll 表示签名时对当前请求或响应中的所有首层字段参与排序签名。
	SignFieldAll = "*"
	// CipherWholeBody 表示已废弃的整包加密标记，仅用于识别并拒绝非法输入。
	CipherWholeBody = "cipher"
	// CipherJSONPrefix 表示字段值在加解密前需要按 JSON 编解码。
	CipherJSONPrefix = "json:"
)

// SignRule 描述单个路由请求与响应参与签名的首层字段。
type SignRule struct {
	Request  []string // Request 表示请求验签字段列表
	Response []string // Response 表示响应回签字段列表
}

// RouteSecurityPolicy 定义单个路由的请求验签、请求解密、响应回签与响应加密策略。
type RouteSecurityPolicy struct {
	RequestSign    []string // RequestSign 表示请求验签关键字段；禁止默认使用 *
	RequestCipher  []string // RequestCipher 表示请求允许解密的字段；禁止使用 cipher 整包加密
	ResponseSign   []string // ResponseSign 表示响应回签关键字段；禁止默认使用 *
	ResponseCipher []string // ResponseCipher 表示响应需要加密的字段路径
}

// sensitiveIdentityFields 收口登录/身份校验链路的敏感请求字段。
// 这组字段统一用于账号登录、登录预校验等身份入口，避免每条路由重复维护相同签名参数。
var sensitiveIdentityFields = []string{"username", "password", "secureCode", "key", "captcha"}

// sensitiveProfileFields 收口管理员资料变更链路的敏感请求字段。
// 这组字段覆盖账号资料、密码、MFA 秘钥和二次确认票据，供管理员新增与编辑共用。
var sensitiveProfileFields = []string{"username", "realName", "email", "phone", "password", "mfaSecureKey", "twoStepKey", "twoStepValue"}

// sensitiveProfileCipherFields 收口管理员资料变更链路的请求解密字段。
var sensitiveProfileCipherFields = []string{"password", "mfaSecureKey", "twoStepKey", "twoStepValue"}

// sensitiveSecretKeyFields 收口秘钥管理写操作的敏感请求字段。
// 这组字段包含主配置开关、版本选路、AES/RSA 文件引用和二次确认票据，供新增和编辑秘钥接口共用。
var sensitiveSecretKeyFields = []string{"uuid", "title", "keyVersion", "aesKeyRef", "aesIvRef", "rsaPublicKeyUserRef", "rsaPublicKeyServerRef", "rsaPrivateKeyServerRef", "status", "signStatus", "cryptoStatus", "versionStatus", "stableVersion", "grayVersion", "grayPercent", "remark", "twoStepKey", "twoStepValue"}

// sensitiveSecretKeyCipherFields 收口秘钥管理写操作的请求解密字段。
var sensitiveSecretKeyCipherFields = []string{"aesKeyRef", "aesIvRef", "rsaPublicKeyUserRef", "rsaPublicKeyServerRef", "rsaPrivateKeyServerRef", "twoStepKey", "twoStepValue"}

// sensitiveSecretKeyResponseFields 收口秘钥详情返回时需要加密的敏感响应字段。
// 秘钥详情读取场景只加密文件引用字段，避免把完整绝对路径直接暴露在前端网络面板中。
var sensitiveSecretKeyResponseFields = []string{"aesKeyRef", "aesIvRef", "rsaPublicKeyUserRef", "rsaPublicKeyServerRef", "rsaPrivateKeyServerRef", CipherJSONPrefix + "versionList"}

// securityDebugSignResponseFields 收口安全调试台签名/验签响应中需要回签的轻量字段。
var securityDebugSignResponseFields = []string{"appId", "requestId", "traceId", "timestamp", "signatureType", "payloadText", "signText", "sign"}

// securityDebugVerifyResponseFields 收口安全调试台验签响应中需要回签的轻量字段。
var securityDebugVerifyResponseFields = []string{"appId", "requestId", "traceId", "timestamp", "signatureType", "payloadText", "signText", "sign", "verified"}

// securityDebugCipherResponseFields 收口安全调试台加解密响应中需要回签的轻量字段。
var securityDebugCipherResponseFields = []string{"appId", "cryptoType", "cipherHeader", "payloadText", "resultPayloadText"}

// secretKeyCheckResponseFields 收口秘钥预检和自检响应中需要回签的轻量字段。
var secretKeyCheckResponseFields = []string{"uuid", "title", "keyVersion", "mode", "status", "allPassed", "canSave", "canEnable", "runtimeChecked", "cacheRefreshed", "checkedAt", "durationMs"}

// RouteSecurityPolicies 定义后台接口的显式安全策略，key 来自统一路由别名常量。
var RouteSecurityPolicies = map[routealias.Alias]RouteSecurityPolicy{
	// auth.login 表示新版登录接口：响应对 token、手机号与 MFA 绑定地址做字段级加密。
	routealias.AuthLogin: {
		RequestSign:    sensitiveIdentityFields,
		RequestCipher:  []string{"password", "secureCode"},
		ResponseSign:   []string{"token"},
		ResponseCipher: []string{"token", "user.phone", "user.buildMFAURL"},
	},
	// auth.refresh 表示刷新令牌接口：响应只保护新 token 与刷新标记。
	routealias.AuthRefresh: {
		ResponseSign:   []string{"token", "isRefresh"},
		ResponseCipher: []string{"token"},
	},
	// auth.profile 表示登录后初始化接口：token 走响应字段级加密。
	routealias.AuthProfile: {
		ResponseSign:   []string{"token"},
		ResponseCipher: []string{"token"},
	},
	// auth.verify_account 表示登录预校验接口：保护 token、手机号和二维码。
	routealias.AuthVerifyAccount: {
		RequestSign:    sensitiveIdentityFields,
		RequestCipher:  []string{"password", "secureCode"},
		ResponseSign:   []string{"token"},
		ResponseCipher: []string{"token", "user.phone", "user.buildMFAURL"},
	},
	// profile.mine 表示个人中心资料接口：手机号与 MFA 绑定地址属于敏感响应字段，需要字段级加密保护。
	routealias.ProfileMine: {
		ResponseCipher: []string{"phone", "buildMFAURL"},
	},
	// profile.check_secure 表示通用安全码校验接口：仅校验安全码字段签名。
	routealias.ProfileCheckSecure: {
		RequestSign:   []string{"secure"},
		RequestCipher: []string{"secure"},
	},
	// profile.check_mfa 表示 MFA 动态码校验接口：校验动态码、场景和值班绑定秘钥。
	routealias.ProfileCheckMFA: {
		RequestSign:   []string{"secure", "scenarios", "mfaSecureKey"},
		RequestCipher: []string{"secure", "mfaSecureKey"},
	},
	// admin.add 表示新增管理员接口：资料与初始安全字段统一参与签名。
	routealias.AdminAdd: {
		RequestSign:   sensitiveProfileFields,
		RequestCipher: sensitiveProfileCipherFields,
	},
	// admin.update 表示编辑管理员接口：资料与安全字段统一参与签名。
	routealias.AdminUpdate: {
		RequestSign:   sensitiveProfileFields,
		RequestCipher: sensitiveProfileCipherFields,
	},
	// admin.delete 表示删除管理员接口：仅校验二次确认票据，避免敏感删除请求绕过统一签名保护。
	routealias.AdminDelete: {
		RequestSign: []string{"twoStepKey", "twoStepValue"},
	},
	// admin.status.update 表示修改管理员状态接口：只校验状态与二次确认票据。
	routealias.AdminStatusUpdate: {
		RequestSign: []string{"status", "twoStepKey", "twoStepValue"},
	},
	// admin.mfa_status.update 表示后台修改管理员 MFA 状态接口：只校验状态切换与二次确认票据。
	routealias.AdminMFAStatus: {
		RequestSign: []string{"mfaStatus", "twoStepKey", "twoStepValue"},
	},
	// admin.password.reset 表示管理员重置密码接口：密码相关字段必须参与签名。
	routealias.AdminPasswordReset: {
		RequestSign:   []string{"password", "twoStepKey", "twoStepValue"},
		RequestCipher: []string{"password", "twoStepKey", "twoStepValue"},
	},
	// admin.reset.initial_state 表示管理员重置初始状态接口：密码类字段与二次确认票据统一保护。
	routealias.AdminResetInitialState: {
		RequestSign:   []string{"password", "twoStepKey", "twoStepValue"},
		RequestCipher: []string{"password", "twoStepKey", "twoStepValue"},
	},
	// admin.role.update 表示覆盖保存管理员角色接口：角色集合与二次确认票据统一参与签名。
	routealias.AdminRoleUpdate: {
		RequestSign: []string{"roleIDs", "twoStepKey", "twoStepValue"},
	},
	// profile.update_password 表示个人中心改密接口：旧密码、新密码与确认密码必须参与签名。
	routealias.ProfileUpdatePassword: {
		RequestSign:   []string{"passwordOld", "passwordNew", "confirmPassword", "twoStepKey", "twoStepValue"},
		RequestCipher: []string{"passwordOld", "passwordNew", "confirmPassword", "twoStepKey", "twoStepValue"},
	},
	// profile.update_mine 表示个人中心资料更新接口：个人资料与二次确认票据统一参与签名。
	routealias.ProfileUpdateMine: {
		RequestSign:   []string{"realName", "email", "phone", "avatar", "description", "twoStepKey", "twoStepValue"},
		RequestCipher: []string{"email", "phone", "twoStepKey", "twoStepValue"},
	},
	// profile.update_mfa_status 表示个人中心启停 MFA 接口：状态、秘钥与二次确认票据统一参与签名。
	routealias.ProfileUpdateMFAStatus: {
		RequestSign:   []string{"mfaStatus", "mfaSecureKey", "twoStepKey", "twoStepValue"},
		RequestCipher: []string{"mfaSecureKey", "twoStepKey", "twoStepValue"},
	},
	// profile.update_mfa_secret 表示刷新个人 MFA 秘钥接口：只保护新秘钥和二次确认票据。
	routealias.ProfileUpdateMFASecret: {
		RequestSign:   []string{"mfaSecureKey", "twoStepKey", "twoStepValue"},
		RequestCipher: []string{"mfaSecureKey", "twoStepKey", "twoStepValue"},
	},
	// profile.update_avatar 表示个人头像更新接口：仅校验头像字段签名。
	routealias.ProfileUpdateAvatar: {
		RequestSign: []string{"avatar"},
	},
	// secretKey.get 表示秘钥详情接口：仅对返回的真实秘钥材料字段做响应加密。
	routealias.SecretKeyGet: {
		ResponseCipher: sensitiveSecretKeyResponseFields,
	},
	// security.debug.sign 表示安全调试台签名接口：调试结果整体按字段回签并保护输出文本。
	routealias.SecurityDebugSign: {
		RequestCipher:  []string{"payloadText"},
		ResponseCipher: []string{"payloadText", "signText", "sign"},
		ResponseSign:   securityDebugSignResponseFields,
	},
	// security.debug.verify 表示安全调试台验签接口：调试结果整体按字段回签并保护输出文本。
	routealias.SecurityDebugVerify: {
		RequestCipher:  []string{"payloadText", "sign"},
		ResponseCipher: []string{"payloadText", "signText", "sign"},
		ResponseSign:   securityDebugVerifyResponseFields,
	},
	// security.debug.encrypt 表示安全调试台加密接口：保护输入和输出 JSON 文本，避免调试台泄漏敏感内容。
	routealias.SecurityDebugEncrypt: {
		RequestCipher:  []string{"payloadText"},
		ResponseCipher: []string{"payloadText", "resultPayloadText"},
		ResponseSign:   securityDebugCipherResponseFields,
	},
	// security.debug.decrypt 表示安全调试台解密接口：保护输入和输出 JSON 文本，避免调试台泄漏敏感内容。
	routealias.SecurityDebugDecrypt: {
		RequestCipher:  []string{"payloadText"},
		ResponseCipher: []string{"payloadText", "resultPayloadText"},
		ResponseSign:   securityDebugCipherResponseFields,
	},
	// secretKey.add 表示新增秘钥接口：秘钥材料写入前统一参与签名。
	routealias.SecretKeyAdd: {
		RequestSign:   sensitiveSecretKeyFields,
		RequestCipher: sensitiveSecretKeyCipherFields,
	},
	// secretKey.edit 表示编辑秘钥接口：秘钥材料变更统一参与签名。
	routealias.SecretKeyUpdate: {
		RequestSign:   sensitiveSecretKeyFields,
		RequestCipher: sensitiveSecretKeyCipherFields,
	},
	// secretKey.editStatus 表示启停秘钥接口：只校验状态与二次确认票据。
	routealias.SecretKeyStatus: {
		RequestSign: []string{"status", "twoStepKey", "twoStepValue"},
	},
	// secretKey.renew 表示刷新秘钥缓存接口：只校验二次确认票据。
	routealias.SecretKeyRenew: {
		RequestSign: []string{"twoStepKey", "twoStepValue"},
	},
	// secretKey.validate 表示秘钥路径预检接口：返回结构化校验结果并对明细结果回签。
	routealias.SecretKeyValidate: {
		RequestSign:   sensitiveSecretKeyFields,
		RequestCipher: sensitiveSecretKeyCipherFields,
		ResponseSign:  secretKeyCheckResponseFields,
	},
	// secretKey.self_check 表示秘钥运行态自检接口：校验版本和二次确认票据。
	routealias.SecretKeySelfCheck: {
		RequestSign:  []string{"keyVersion", "twoStepKey", "twoStepValue"},
		ResponseSign: secretKeyCheckResponseFields,
	},
	// user_tag.workflow_lease.release 表示释放工作流互斥锁接口：保护参数和二次确认票据。
	routealias.UserTagWorkflowLeaseRelease: {
		RequestSign: []string{"workflowId", "mode", "reason", "twoStepKey", "twoStepValue"},
	},
}

// PolicyByRoute 根据路由别名读取统一安全策略。
func PolicyByRoute(route string) RouteSecurityPolicy {
	alias := routealias.Alias(strings.TrimSpace(route))
	if alias == "" || strings.EqualFold(string(alias), string(routealias.Ignore)) {
		return RouteSecurityPolicy{}
	}
	if policy, ok := RouteSecurityPolicies[alias]; ok {
		return policy
	}
	return RouteSecurityPolicy{}
}

// BuildSignString 生成待签名字符串，按字段排序后拼接时间绑定的请求盐值。
// traceID 对应 X-Trace-Id，timestamp 对应 X-Timestamp，二者共同参与签名和防重放。
func BuildSignString(data map[string]any, signParams []string, traceID, timestamp, appID string) string {
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
	builder.WriteString(utils.Md5(appID + "-" + traceID + "-" + timestamp))
	return builder.String()
}

// EncodeCipherParams 把字段级加密配置编码成请求头值；整包加密标记不再生成请求头。
func EncodeCipherParams(params []string) string {
	params = helper.UniqueNonEmptyStrings(params)
	if len(params) == 0 {
		return ""
	}
	for _, param := range params {
		if strings.EqualFold(param, CipherWholeBody) {
			return ""
		}
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
