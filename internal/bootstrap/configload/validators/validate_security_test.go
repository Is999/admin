package validators

import (
	"testing"

	"admin/internal/config"
)

// TestValidateBootstrapConfigRejectsInvalidSecretKeyStatus 确保只配置非法状态值也会触发校验。
func TestValidateBootstrapConfigRejectsInvalidSecretKeyStatus(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.AppID = "1"
	cfg.Security.SecretKey.SignStatus = 2
	if err := ValidateSecurity(cfg); err == nil {
		t.Fatal("期望非法 security.secret_key.sign_status 返回错误，实际为 nil")
	}
}

// TestValidateSecurityAcceptsIndependentSwitches 确保签名和加密可以分别启用。
func TestValidateSecurityAcceptsIndependentSwitches(t *testing.T) {
	cases := []config.SecuritySecretKeyConfig{
		{
			KeyVersion:             "v1",
			SignStatus:             1,
			RSAPublicKeyUserRef:    "/run/secrets/user_public.pem",
			RSAPrivateKeyServerRef: "/run/secrets/server_private.pem",
		},
		{
			KeyVersion:             "v1",
			CryptoStatus:           1,
			AESKeyRef:              "/run/secrets/aes_key",
			AESIVRef:               "/run/secrets/aes_iv",
			RSAPublicKeyUserRef:    "/run/secrets/user_public.pem",
			RSAPrivateKeyServerRef: "/run/secrets/server_private.pem",
		},
	}
	for _, secretCfg := range cases {
		cfg := validAdminBootstrapConfig()
		cfg.AppID = "1"
		cfg.Security.SecretKey = secretCfg
		if err := ValidateSecurity(cfg); err != nil {
			t.Fatalf("ValidateSecurity() sign=%d crypto=%d error = %v", secretCfg.SignStatus, secretCfg.CryptoStatus, err)
		}
	}
}

// TestValidateSecurityRejectsCryptoWithoutRSA 确保 RSA 加解密不会在启动后才因材料缺失失败。
func TestValidateSecurityRejectsCryptoWithoutRSA(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Security.SecretKey = config.SecuritySecretKeyConfig{
		KeyVersion:   "v1",
		CryptoStatus: 1,
		AESKeyRef:    "/run/secrets/aes_key",
		AESIVRef:     "/run/secrets/aes_iv",
	}
	if err := ValidateSecurity(cfg); err == nil {
		t.Fatal("期望仅启用加解密但缺少 RSA 材料返回错误")
	}
}

// TestValidateSecurityTreatsEnabledStatusAsConfigured 确保仅开启安全开关也会进入材料完整性校验。
func TestValidateSecurityTreatsEnabledStatusAsConfigured(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Security.SecretKey.SignStatus = 1
	if err := ValidateSecurity(cfg); err == nil {
		t.Fatal("期望仅开启签名但未配置秘钥材料返回错误，实际为 nil")
	}
}
