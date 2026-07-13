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

// TestSignPoliciesExcludeLargeDisplayFields 校验描述和备注等展示性长文本不进入轻量签名。
func TestSignPoliciesExcludeLargeDisplayFields(t *testing.T) {
	for alias, policy := range RouteSecurityPolicies {
		for _, fields := range [][]string{policy.RequestSign, policy.ResponseSign} {
			for _, field := range fields {
				switch strings.ToLower(strings.TrimSpace(field)) {
				case "description", "reason", "remark":
					t.Fatalf("route %s signs large display field %q", alias, field)
				}
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

// TestPolicyByRouteForAdminRoleUpdate 验证管理员角色分配接口保护角色数组和二次确认票据。
func TestPolicyByRouteForAdminRoleUpdate(t *testing.T) {
	policy := PolicyByRoute(string(routealias.AdminRoleUpdate))
	wantRequestSign := []string{"roleIDs", "twoStepKey", "twoStepValue"}

	if !reflect.DeepEqual(policy.RequestSign, wantRequestSign) {
		t.Fatalf("PolicyByRoute(admin.role.update) request sign = %#v, want %#v", policy.RequestSign, wantRequestSign)
	}
}

// TestPolicyByRouteForSysConfigImport 验证字典备份和导入链路保护上传与备份标识。
func TestPolicyByRouteForSysConfigImport(t *testing.T) {
	policy := PolicyByRoute(string(routealias.SysConfigImport))
	wantRequestSign := []string{"uploadId", "backupId"}

	if !reflect.DeepEqual(policy.RequestSign, wantRequestSign) {
		t.Fatalf("PolicyByRoute(system.config.import) request sign = %#v, want %#v", policy.RequestSign, wantRequestSign)
	}
}

// TestPolicyByRouteForLogoutUsesHeaderOnlySign 验证无业务字段的退出接口仍启用基础头验签。
func TestPolicyByRouteForLogoutUsesHeaderOnlySign(t *testing.T) {
	policy := PolicyByRoute(string(routealias.AuthLogout))
	if policy.RequestSign == nil || len(policy.RequestSign) != 0 {
		t.Fatalf("PolicyByRoute(auth.logout) request sign = %#v, want enabled empty fields", policy.RequestSign)
	}
}

// TestPolicyByRouteForRBACWrites 验证角色与权限写接口保护真实业务字段。
func TestPolicyByRouteForRBACWrites(t *testing.T) {
	tests := map[string][]string{
		"role.add":                     {"title", "pid", "status"},
		"role.update":                  {"title", "pid"},
		"role.status.update":           {"status"},
		"role.permission.update":       {"routePermissionIds", "docPermissionIds"},
		"permission.add":               {"uuid", "title", "module", "pid", "type", "status"},
		"permission.update":            {"uuid", "title", "module", "pid", "type"},
		"permission.status.update":     {"status"},
		"doc_permission.status.update": {"status"},
	}
	for alias, want := range tests {
		if got := PolicyByRoute(alias).RequestSign; !reflect.DeepEqual(got, want) {
			t.Fatalf("PolicyByRoute(%s) request sign = %#v, want %#v", alias, got, want)
		}
	}
}

// TestPolicyByRouteForAdminWriteRequests 验证管理员新增和编辑分别使用各自的真实请求字段。
func TestPolicyByRouteForAdminWriteRequests(t *testing.T) {
	addPolicy := PolicyByRoute(string(routealias.AdminAdd))
	if !reflect.DeepEqual(addPolicy.RequestSign, []string{"username", "realName", "email", "phone", "password", "mfaSecureKey", "avatar", "twoStepKey", "twoStepValue"}) {
		t.Fatalf("PolicyByRoute(admin.add) request sign = %#v", addPolicy.RequestSign)
	}
	if !reflect.DeepEqual(addPolicy.RequestCipher, []string{"password", "mfaSecureKey", "twoStepKey", "twoStepValue"}) {
		t.Fatalf("PolicyByRoute(admin.add) request cipher = %#v", addPolicy.RequestCipher)
	}

	updatePolicy := PolicyByRoute(string(routealias.AdminUpdate))
	if !reflect.DeepEqual(updatePolicy.RequestSign, []string{"realName", "email", "phone", "avatar", "password", "twoStepKey", "twoStepValue"}) {
		t.Fatalf("PolicyByRoute(admin.update) request sign = %#v", updatePolicy.RequestSign)
	}
	if !reflect.DeepEqual(updatePolicy.RequestCipher, []string{"password", "twoStepKey", "twoStepValue"}) {
		t.Fatalf("PolicyByRoute(admin.update) request cipher = %#v", updatePolicy.RequestCipher)
	}
}

// TestPolicyByRouteForUnknownAliasIsEmpty 验证未知路由别名不会触发隐式全字段签名。
func TestPolicyByRouteForUnknownAliasIsEmpty(t *testing.T) {
	policy := PolicyByRoute("unknown.route")
	if policy.RequestSign != nil || policy.ResponseSign != nil || len(policy.RequestCipher) != 0 || len(policy.ResponseCipher) != 0 {
		t.Fatalf("PolicyByRoute(unknown.route) = %#v, want empty policy", policy)
	}
}
