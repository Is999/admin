package security

import (
	"strings"

	"admin/internal/routealias"
)

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

// adminAddSignFields 收口新增管理员时真实存在的签名字段；角色数组由角色范围校验负责。
var adminAddSignFields = []string{"username", "realName", "email", "phone", "password", "mfaSecureKey", "avatar", "description", "twoStepKey", "twoStepValue"}

// adminAddCipherFields 收口新增管理员时需要解密的安全字段。
var adminAddCipherFields = []string{"password", "mfaSecureKey", "twoStepKey", "twoStepValue"}

// adminUpdateSignFields 收口编辑管理员时真实存在的签名字段；角色数组由角色范围校验负责。
var adminUpdateSignFields = []string{"realName", "email", "phone", "avatar", "description", "password", "isUpdateRoles", "twoStepKey", "twoStepValue"}

// adminUpdateCipherFields 收口编辑管理员时需要解密的安全字段。
var adminUpdateCipherFields = []string{"password", "twoStepKey", "twoStepValue"}

// sensitiveUserProfileFields 收口前台用户后台管理链路的敏感请求字段。
var sensitiveUserProfileFields = []string{"username", "password", "nickname", "email", "phone", "avatar", "status", "twoStepKey", "twoStepValue"}

// sensitiveUserProfileCipherFields 收口前台用户后台管理链路的请求解密字段。
var sensitiveUserProfileCipherFields = []string{"password", "email", "phone", "twoStepKey", "twoStepValue"}

// sensitiveSecretKeyFields 收口秘钥管理写操作的敏感请求字段。
// 这组字段包含主配置开关、版本选路、AES/RSA 文件引用和二次确认票据，供新增和编辑秘钥接口共用。
var sensitiveSecretKeyFields = []string{"uuid", "title", "keyVersion", "aesKeyRef", "aesIvRef", "rsaPublicKeyUserRef", "rsaPublicKeyServerRef", "rsaPrivateKeyServerRef", "status", "signStatus", "cryptoStatus", "versionStatus", "stableVersion", "grayVersion", "grayPercent", "remark", "twoStepKey", "twoStepValue"}

// sensitiveSecretKeyCipherFields 收口秘钥管理写操作的请求解密字段。
var sensitiveSecretKeyCipherFields = []string{"aesKeyRef", "aesIvRef", "rsaPublicKeyUserRef", "rsaPublicKeyServerRef", "rsaPrivateKeyServerRef", "twoStepKey", "twoStepValue"}

// sensitiveSecretKeyResponseFields 收口秘钥详情返回时需要加密的敏感响应字段。
// 秘钥详情读取场景只加密文件引用字段，避免把完整绝对路径直接暴露在前端网络面板中。
var sensitiveSecretKeyResponseFields = []string{"aesKeyRef", "aesIvRef", "rsaPublicKeyUserRef", "rsaPublicKeyServerRef", "rsaPrivateKeyServerRef", CipherJSONPrefix + "versionList"}

// sensitiveSecretKeyResponseSignFields 收口秘钥详情响应回签字段，json: 加密前缀不属于真实字段路径。
var sensitiveSecretKeyResponseSignFields = []string{"aesKeyRef", "aesIvRef", "rsaPublicKeyUserRef", "rsaPublicKeyServerRef", "rsaPrivateKeyServerRef", "versionList"}

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
		ResponseSign:   []string{"token", "user.phone", "user.buildMFAURL"},
		ResponseCipher: []string{"token", "user.phone", "user.buildMFAURL"},
	},
	// auth.refresh 表示刷新令牌接口：响应只保护新 token 与刷新标记。
	routealias.AuthRefresh: {
		ResponseSign:   []string{"token", "isRefresh"},
		ResponseCipher: []string{"token"},
	},
	// auth.profile 表示登录后初始化接口：token 与手机号走响应字段级加密。
	routealias.AuthProfile: {
		ResponseSign:   []string{"token", "phone"},
		ResponseCipher: []string{"token", "phone"},
	},
	// profile.mine 表示个人中心资料接口：手机号与 MFA 绑定地址属于敏感响应字段，需要字段级加密保护。
	routealias.ProfileMine: {
		ResponseSign:   []string{"phone", "buildMFAURL"},
		ResponseCipher: []string{"phone", "buildMFAURL"},
	},
	// profile.check_secure 表示通用安全码校验接口：仅校验安全码字段签名。
	routealias.ProfileCheckSecure: {
		RequestSign:   []string{"secure"},
		RequestCipher: []string{"secure"},
	},
	// profile.check_mfa 表示 MFA 动态码校验接口：校验动态码、场景和值班绑定秘钥。
	routealias.ProfileCheckMFA: {
		RequestSign:    []string{"secure", "scenarios", "mfaSecureKey"},
		RequestCipher:  []string{"secure", "mfaSecureKey"},
		ResponseSign:   []string{"buildMFAURL"},
		ResponseCipher: []string{"buildMFAURL"},
	},
	// profile.refresh_mfa_secret 表示重新生成个人 MFA 绑定材料：绑定地址必须按敏感字段加密返回。
	routealias.ProfileRefreshMFASecret: {
		ResponseSign:   []string{"buildMFAURL"},
		ResponseCipher: []string{"buildMFAURL"},
	},
	// admin.mfa_secret_url 表示为指定管理员生成绑定材料：绑定地址必须按敏感字段加密返回。
	routealias.AdminBuildMFAURL: {
		ResponseSign:   []string{"buildMFAURL"},
		ResponseCipher: []string{"buildMFAURL"},
	},
	// admin.add 表示新增管理员接口：资料与初始安全字段统一参与签名。
	routealias.AdminAdd: {
		RequestSign:   adminAddSignFields,
		RequestCipher: adminAddCipherFields,
	},
	// admin.update 表示编辑管理员接口：只保护当前编辑请求真实承载的资料、安全字段和角色更新开关。
	routealias.AdminUpdate: {
		RequestSign:   adminUpdateSignFields,
		RequestCipher: adminUpdateCipherFields,
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
	// admin.role.update 表示覆盖保存管理员角色接口：只签名二次确认票据，角色数组由请求校验和角色范围校验收口。
	routealias.AdminRoleUpdate: {
		RequestSign: []string{"twoStepKey", "twoStepValue"},
	},
	// user.add 表示后台新增前台用户接口：账号资料、密码与二次确认票据统一保护。
	routealias.UserAdd: {
		RequestSign:   sensitiveUserProfileFields,
		RequestCipher: sensitiveUserProfileCipherFields,
	},
	// user.update 表示后台编辑前台用户资料接口：资料字段与二次确认票据统一保护。
	routealias.UserUpdate: {
		RequestSign:   []string{"nickname", "email", "phone", "avatar", "twoStepKey", "twoStepValue"},
		RequestCipher: []string{"email", "phone", "twoStepKey", "twoStepValue"},
	},
	// user.status.update 表示后台修改前台用户状态接口：状态与二次确认票据必须参与签名。
	routealias.UserStatusUpdate: {
		RequestSign: []string{"status", "twoStepKey", "twoStepValue"},
	},
	// user.password.reset 表示后台重置前台用户密码接口：密码与二次确认票据统一保护。
	routealias.UserPasswordReset: {
		RequestSign:   []string{"password", "twoStepKey", "twoStepValue"},
		RequestCipher: []string{"password", "twoStepKey", "twoStepValue"},
	},
	// user.runtime.sync 表示手动同步前台用户运行态接口：同步范围与二次确认票据统一保护。
	routealias.UserRuntimeSync: {
		RequestSign: []string{"profile", "sessions", "twoStepKey", "twoStepValue"},
	},
	// user.export 表示前台用户列表导出接口：查询条件参与签名，邮箱和手机号条件字段级加密。
	routealias.UserExport: {
		RequestSign:   []string{"id", "shardNo", "username", "email", "phone", "status"},
		RequestCipher: []string{"email", "phone"},
	},
	// api_runtime.config_reload.run 表示触发 API 配置热加载接口：二次确认票据必须参与签名。
	routealias.APIRuntimeConfigReloadRun: {
		RequestSign: []string{"twoStepKey", "twoStepValue"},
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
		ResponseSign:   sensitiveSecretKeyResponseSignFields,
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
