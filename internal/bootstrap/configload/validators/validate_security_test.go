package validators

import "testing"

// TestValidateBootstrapConfigRejectsInvalidSecretKeyStatus 确保只配置非法状态值也会触发校验。
func TestValidateBootstrapConfigRejectsInvalidSecretKeyStatus(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.AppID = "1"
	cfg.Security.SecretKey.SignStatus = 2
	if err := ValidateSecurity(cfg); err == nil {
		t.Fatal("期望非法 security.secret_key.sign_status 返回错误，实际为 nil")
	}
}
