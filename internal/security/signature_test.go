package security

import "testing"

// TestPolicyByRouteForBuildSecretVerifyAccount 验证兼容登录预校验接口的响应签名与加密字段路径和真实结构保持一致。
func TestPolicyByRouteForBuildSecretVerifyAccount(t *testing.T) {
	policy := PolicyByRoute("user.build_secret_verify_account")

	if len(policy.ResponseSign) != 1 || policy.ResponseSign[0] != "token" {
		t.Fatalf("PolicyByRoute() response sign = %#v, want [token]", policy.ResponseSign)
	}
	if len(policy.ResponseCipher) != 3 ||
		policy.ResponseCipher[0] != "token" ||
		policy.ResponseCipher[1] != "user.phone" ||
		policy.ResponseCipher[2] != "user.buildMFAURL" {
		t.Fatalf("PolicyByRoute() response cipher = %#v, want [token user.phone user.buildMFAURL]", policy.ResponseCipher)
	}
}

// TestPolicyByRouteForUserMine 验证个人中心资料接口对手机号与 MFA 绑定地址启用响应字段级加密。
func TestPolicyByRouteForUserMine(t *testing.T) {
	policy := PolicyByRoute("user.mine")

	if len(policy.ResponseCipher) != 2 ||
		policy.ResponseCipher[0] != "phone" ||
		policy.ResponseCipher[1] != "buildMFAURL" {
		t.Fatalf("PolicyByRoute(user.mine) response cipher = %#v, want [phone buildMFAURL]", policy.ResponseCipher)
	}
}

// TestPolicyByRouteForLoginAfterInfo 验证登录后初始化接口对 token 启用响应加密与签名保护。
func TestPolicyByRouteForLoginAfterInfo(t *testing.T) {
	policy := PolicyByRoute("auth.login_after_info")

	if len(policy.ResponseCipher) != 1 || policy.ResponseCipher[0] != "token" {
		t.Fatalf("PolicyByRoute(auth.login_after_info) response cipher = %#v, want [token]", policy.ResponseCipher)
	}
	if len(policy.ResponseSign) != 1 || policy.ResponseSign[0] != "token" {
		t.Fatalf("PolicyByRoute(auth.login_after_info) response sign = %#v, want [token]", policy.ResponseSign)
	}
}

// TestPolicyByRouteForSecretKeyChecks 验证秘钥预检和自检接口的安全策略完整生效。
func TestPolicyByRouteForSecretKeyChecks(t *testing.T) {
	validatePolicy := PolicyByRoute("secretKey.validate")
	if len(validatePolicy.ResponseSign) != 1 || validatePolicy.ResponseSign[0] != SignFieldAll {
		t.Fatalf("PolicyByRoute(secretKey.validate) response sign = %#v, want [*]", validatePolicy.ResponseSign)
	}

	selfCheckPolicy := PolicyByRoute("secretKey.self_check")
	if len(selfCheckPolicy.RequestSign) != 3 ||
		selfCheckPolicy.RequestSign[0] != "keyVersion" ||
		selfCheckPolicy.RequestSign[1] != "twoStepKey" ||
		selfCheckPolicy.RequestSign[2] != "twoStepValue" {
		t.Fatalf("PolicyByRoute(secretKey.self_check) request sign = %#v, want [keyVersion twoStepKey twoStepValue]", selfCheckPolicy.RequestSign)
	}
	if len(selfCheckPolicy.ResponseSign) != 1 || selfCheckPolicy.ResponseSign[0] != SignFieldAll {
		t.Fatalf("PolicyByRoute(secretKey.self_check) response sign = %#v, want [*]", selfCheckPolicy.ResponseSign)
	}
}
