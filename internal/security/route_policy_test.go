package security

import (
	"reflect"
	"strings"
	"testing"

	"admin/internal/routealias"
)

// TestPolicyByRouteForUserMine 验证个人中心资料接口对手机号与 MFA 绑定地址启用响应字段级加密。
func TestPolicyByRouteForUserMine(t *testing.T) {
	policy := PolicyByRoute(string(routealias.ProfileMine))

	if len(policy.ResponseCipher) != 2 ||
		policy.ResponseCipher[0] != "phone" ||
		policy.ResponseCipher[1] != "buildMFAURL" {
		t.Fatalf("PolicyByRoute(profile.mine) response cipher = %#v, want [phone buildMFAURL]", policy.ResponseCipher)
	}
}

// TestPolicyByRouteForMFAURLs 验证所有会返回 MFA 绑定地址的接口都执行字段级响应加密。
func TestPolicyByRouteForMFAURLs(t *testing.T) {
	aliases := []routealias.Alias{
		routealias.ProfileCheckMFA,
		routealias.ProfileRefreshMFASecret,
		routealias.AdminBuildMFAURL,
	}
	for _, alias := range aliases {
		policy := PolicyByRoute(string(alias))
		if len(policy.ResponseCipher) != 1 || policy.ResponseCipher[0] != "buildMFAURL" {
			t.Fatalf("PolicyByRoute(%s) response cipher = %#v, want [buildMFAURL]", alias, policy.ResponseCipher)
		}
	}
}

// TestPolicyByRouteForLoginAfterInfo 验证登录后初始化接口对 token 与手机号启用响应保护。
func TestPolicyByRouteForLoginAfterInfo(t *testing.T) {
	policy := PolicyByRoute(string(routealias.AuthProfile))

	if len(policy.ResponseCipher) != 2 || policy.ResponseCipher[0] != "token" || policy.ResponseCipher[1] != "phone" {
		t.Fatalf("PolicyByRoute(auth.profile) response cipher = %#v, want [token phone]", policy.ResponseCipher)
	}
	if !reflect.DeepEqual(policy.ResponseSign, []string{"token", "phone"}) {
		t.Fatalf("PolicyByRoute(auth.profile) response sign = %#v, want [token phone]", policy.ResponseSign)
	}
}

// TestResponseCipherFieldsAreSigned 校验所有加密响应字段都先以明文参与回签。
func TestResponseCipherFieldsAreSigned(t *testing.T) {
	for alias, policy := range RouteSecurityPolicies {
		signed := make(map[string]struct{}, len(policy.ResponseSign))
		for _, field := range policy.ResponseSign {
			signed[strings.TrimSpace(field)] = struct{}{}
		}
		for _, cipherField := range policy.ResponseCipher {
			field := strings.TrimPrefix(strings.TrimSpace(cipherField), CipherJSONPrefix)
			if _, ok := signed[field]; !ok {
				t.Fatalf("route %s response cipher field %q is not signed: %+v", alias, field, policy)
			}
		}
	}
}

// TestPolicyByRouteForSecretKeyChecks 验证秘钥预检和自检接口的安全策略完整生效。
func TestPolicyByRouteForSecretKeyChecks(t *testing.T) {
	wantResponseSign := []string{"uuid", "title", "keyVersion", "mode", "status", "allPassed", "canSave", "canEnable", "runtimeChecked", "cacheRefreshed", "checkedAt", "durationMs"}

	validatePolicy := PolicyByRoute(string(routealias.SecretKeyValidate))
	if !reflect.DeepEqual(validatePolicy.ResponseSign, wantResponseSign) {
		t.Fatalf("PolicyByRoute(secretKey.validate) response sign = %#v, want %#v", validatePolicy.ResponseSign, wantResponseSign)
	}

	selfCheckPolicy := PolicyByRoute(string(routealias.SecretKeySelfCheck))
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

// TestPolicyByRouteForAdminRoleUpdate 验证管理员角色分配接口不把角色数组放入轻量签名字段。
func TestPolicyByRouteForAdminRoleUpdate(t *testing.T) {
	policy := PolicyByRoute(string(routealias.AdminRoleUpdate))
	wantRequestSign := []string{"twoStepKey", "twoStepValue"}

	if !reflect.DeepEqual(policy.RequestSign, wantRequestSign) {
		t.Fatalf("PolicyByRoute(admin.role.update) request sign = %#v, want %#v", policy.RequestSign, wantRequestSign)
	}
}

// TestPolicyByRouteForAdminWriteRequests 验证管理员新增和编辑分别使用各自的真实请求字段。
func TestPolicyByRouteForAdminWriteRequests(t *testing.T) {
	addPolicy := PolicyByRoute(string(routealias.AdminAdd))
	if !reflect.DeepEqual(addPolicy.RequestSign, []string{"username", "realName", "email", "phone", "password", "mfaSecureKey", "avatar", "description", "twoStepKey", "twoStepValue"}) {
		t.Fatalf("PolicyByRoute(admin.add) request sign = %#v", addPolicy.RequestSign)
	}
	if !reflect.DeepEqual(addPolicy.RequestCipher, []string{"password", "mfaSecureKey", "twoStepKey", "twoStepValue"}) {
		t.Fatalf("PolicyByRoute(admin.add) request cipher = %#v", addPolicy.RequestCipher)
	}

	updatePolicy := PolicyByRoute(string(routealias.AdminUpdate))
	if !reflect.DeepEqual(updatePolicy.RequestSign, []string{"realName", "email", "phone", "avatar", "description", "password", "isUpdateRoles", "twoStepKey", "twoStepValue"}) {
		t.Fatalf("PolicyByRoute(admin.update) request sign = %#v", updatePolicy.RequestSign)
	}
	if !reflect.DeepEqual(updatePolicy.RequestCipher, []string{"password", "twoStepKey", "twoStepValue"}) {
		t.Fatalf("PolicyByRoute(admin.update) request cipher = %#v", updatePolicy.RequestCipher)
	}
}

// TestPolicyByRouteForUnknownAliasIsEmpty 验证未知路由别名不会触发隐式全字段签名。
func TestPolicyByRouteForUnknownAliasIsEmpty(t *testing.T) {
	policy := PolicyByRoute("unknown.route")
	if len(policy.RequestSign) != 0 || len(policy.ResponseSign) != 0 || len(policy.RequestCipher) != 0 || len(policy.ResponseCipher) != 0 {
		t.Fatalf("PolicyByRoute(unknown.route) = %#v, want empty policy", policy)
	}
}
