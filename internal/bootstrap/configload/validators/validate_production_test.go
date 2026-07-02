package validators

import (
	"testing"

	"admin/internal/config"
)

// TestValidateBootstrapConfigRejectsProductionPlaceholderAppKey 确保生产环境不能使用占位 app_key。
func TestValidateBootstrapConfigRejectsProductionPlaceholderAppKey(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	cfg.AppKey = "replace-with-strong-app-key"
	if err := ValidateProduction(cfg); err == nil {
		t.Fatal("期望生产 app_key 占位值返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsProductionRedisTLSInsecure 确保生产环境不能跳过 Redis TLS 校验。
func TestValidateBootstrapConfigRejectsProductionRedisTLSInsecure(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	cfg.Redis.TLSInsecureSkipVerify = true
	if err := ValidateProduction(cfg); err == nil {
		t.Fatal("期望生产 redis.tls_insecure_skip_verify 返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsSecurityGrayWithoutSaltInProduction 确保生产灰度秘钥需要稳定盐值。
func TestValidateBootstrapConfigRejectsSecurityGrayWithoutSaltInProduction(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	cfg.Security.SecretKey = validAdminSecuritySecretKey()
	cfg.Security.SecretKey.StableVersion = "v1"
	cfg.Security.SecretKey.GrayVersion = "v2"
	cfg.Security.SecretKey.GrayPercent = 10
	cfg.Security.SecretKey.GraySalt = ""
	cfg.Security.SecretKey.Versions = []config.SecuritySecretKeyVersionConfig{
		validAdminSecretKeyVersion("v1"),
		validAdminSecretKeyVersion("v2"),
	}
	if err := ValidateProduction(cfg); err == nil {
		t.Fatal("期望生产 secret_key 灰度缺少 gray_salt 返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigAcceptsProductionSafeConfig 确保生产安全配置可以通过启动校验。
func TestValidateBootstrapConfigAcceptsProductionSafeConfig(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	cfg.Security.SecretKey = validAdminSecuritySecretKey()
	if err := ValidateProduction(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
