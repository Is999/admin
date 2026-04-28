package security

import (
	"reflect"
	"testing"
)

// TestPolicyByRouteForBuildSecretVerifyAccount 验证登录预校验接口的响应签名与加密字段路径和真实结构保持一致。
func TestPolicyByRouteForBuildSecretVerifyAccount(t *testing.T) {
	policy := PolicyByRoute("auth.verify_account")

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
	policy := PolicyByRoute("profile.mine")

	if len(policy.ResponseCipher) != 2 ||
		policy.ResponseCipher[0] != "phone" ||
		policy.ResponseCipher[1] != "buildMFAURL" {
		t.Fatalf("PolicyByRoute(profile.mine) response cipher = %#v, want [phone buildMFAURL]", policy.ResponseCipher)
	}
}

// TestPolicyByRouteForLoginAfterInfo 验证登录后初始化接口对 token 启用响应加密与签名保护。
func TestPolicyByRouteForLoginAfterInfo(t *testing.T) {
	policy := PolicyByRoute("auth.profile")

	if len(policy.ResponseCipher) != 1 || policy.ResponseCipher[0] != "token" {
		t.Fatalf("PolicyByRoute(auth.profile) response cipher = %#v, want [token]", policy.ResponseCipher)
	}
	if len(policy.ResponseSign) != 1 || policy.ResponseSign[0] != "token" {
		t.Fatalf("PolicyByRoute(auth.profile) response sign = %#v, want [token]", policy.ResponseSign)
	}
}

// TestPolicyByRouteForSecretKeyChecks 验证秘钥预检和自检接口的安全策略完整生效。
func TestPolicyByRouteForSecretKeyChecks(t *testing.T) {
	wantResponseSign := []string{"uuid", "title", "keyVersion", "mode", "status", "allPassed", "canSave", "canEnable", "runtimeChecked", "cacheRefreshed", "checkedAt", "durationMs"}

	validatePolicy := PolicyByRoute("secretKey.validate")
	if !reflect.DeepEqual(validatePolicy.ResponseSign, wantResponseSign) {
		t.Fatalf("PolicyByRoute(secretKey.validate) response sign = %#v, want %#v", validatePolicy.ResponseSign, wantResponseSign)
	}

	selfCheckPolicy := PolicyByRoute("secretKey.self_check")
	if len(selfCheckPolicy.RequestSign) != 3 ||
		selfCheckPolicy.RequestSign[0] != "keyVersion" ||
		selfCheckPolicy.RequestSign[1] != "twoStepKey" ||
		selfCheckPolicy.RequestSign[2] != "twoStepValue" {
		t.Fatalf("PolicyByRoute(secretKey.self_check) request sign = %#v, want [keyVersion twoStepKey twoStepValue]", selfCheckPolicy.RequestSign)
	}
	if !reflect.DeepEqual(selfCheckPolicy.ResponseSign, wantResponseSign) {
		t.Fatalf("PolicyByRoute(secretKey.self_check) response sign = %#v, want %#v", selfCheckPolicy.ResponseSign, wantResponseSign)
	}
}

// TestPolicyByRouteForUnknownAliasIsEmpty 验证未知路由别名不会触发隐式全字段签名。
func TestPolicyByRouteForUnknownAliasIsEmpty(t *testing.T) {
	policy := PolicyByRoute("unknown.route")
	if len(policy.RequestSign) != 0 || len(policy.ResponseSign) != 0 || len(policy.RequestCipher) != 0 || len(policy.ResponseCipher) != 0 {
		t.Fatalf("PolicyByRoute(unknown.route) = %#v, want empty policy", policy)
	}
}
