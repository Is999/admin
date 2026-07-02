package validators

import (
	"fmt"
	"strings"

	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
)

// ValidateSecurity 校验本地 secret_key 配置，避免安全中间件运行后才发现材料缺失。
func ValidateSecurity(c config.Config) error {
	cfg := c.Security.SecretKey
	if !securitySecretKeyTouched(cfg) {
		return nil
	}
	if err := validateBinaryStatus("security.secret_key.sign_status", cfg.SignStatus); err != nil {
		return errors.Tag(err)
	}
	if err := validateBinaryStatus("security.secret_key.crypto_status", cfg.CryptoStatus); err != nil {
		return errors.Tag(err)
	}
	if cfg.GrayPercent < 0 || cfg.GrayPercent > 100 {
		return errors.Errorf("security.secret_key.gray_percent 必须在 0 到 100 之间")
	}
	if strings.TrimSpace(cfg.GrayVersion) != "" && cfg.GrayPercent <= 0 {
		return errors.Errorf("security.secret_key.gray_version 非空时 gray_percent 必须大于 0")
	}

	versionSet := make(map[string]struct{}, len(cfg.Versions))
	for i, item := range cfg.Versions {
		if err := validateSecretKeyVersion("security.secret_key.versions", i, item, cfg.SignStatus, cfg.CryptoStatus); err != nil {
			return errors.Tag(err)
		}
		versionSet[strings.TrimSpace(item.KeyVersion)] = struct{}{}
	}

	if len(cfg.Versions) == 0 {
		if err := validateSecretKeyVersion("security.secret_key", -1, config.SecuritySecretKeyVersionConfig{
			KeyVersion:             cfg.KeyVersion,
			AESKey:                 cfg.AESKey,
			AESKeyRef:              cfg.AESKeyRef,
			AESIV:                  cfg.AESIV,
			AESIVRef:               cfg.AESIVRef,
			RSAPublicKeyUser:       cfg.RSAPublicKeyUser,
			RSAPublicKeyUserRef:    cfg.RSAPublicKeyUserRef,
			RSAPrivateKeyServer:    cfg.RSAPrivateKeyServer,
			RSAPrivateKeyServerRef: cfg.RSAPrivateKeyServerRef,
		}, cfg.SignStatus, cfg.CryptoStatus); err != nil {
			return errors.Tag(err)
		}
		return nil
	}

	if stable := strings.TrimSpace(cfg.StableVersion); stable != "" {
		if _, ok := versionSet[stable]; !ok {
			return errors.Errorf("security.secret_key.stable_version 未在 versions 中定义")
		}
	}
	if gray := strings.TrimSpace(cfg.GrayVersion); gray != "" {
		if _, ok := versionSet[gray]; !ok {
			return errors.Errorf("security.secret_key.gray_version 未在 versions 中定义")
		}
	}
	return nil
}

// validateBinaryStatus 校验配置中的二值开关只能使用 0 或 1。
func validateBinaryStatus(name string, value int) error {
	if value != 0 && value != 1 {
		return errors.Errorf("%s 只能为 0 或 1", name)
	}
	return nil
}

// validateSecretKeyVersion 校验单个秘钥版本在当前签名和加密开关下具备必要材料。
func validateSecretKeyVersion(prefix string, index int, cfg config.SecuritySecretKeyVersionConfig, signStatus, cryptoStatus int) error {
	name := prefix
	if index >= 0 {
		name = fmt.Sprintf("%s[%d]", prefix, index)
	}
	if strings.TrimSpace(cfg.KeyVersion) == "" {
		return errors.Errorf("%s.key_version 不能为空", name)
	}
	if cryptoStatus != 0 {
		if strings.TrimSpace(cfg.AESKey) == "" && strings.TrimSpace(cfg.AESKeyRef) == "" {
			return errors.Errorf("%s 启用加解密时必须配置 aes_key 或 aes_key_ref", name)
		}
		if strings.TrimSpace(cfg.AESIV) == "" && strings.TrimSpace(cfg.AESIVRef) == "" {
			return errors.Errorf("%s 启用加解密时必须配置 aes_iv 或 aes_iv_ref", name)
		}
	}
	if signStatus != 0 {
		if strings.TrimSpace(cfg.RSAPublicKeyUser) == "" && strings.TrimSpace(cfg.RSAPublicKeyUserRef) == "" {
			return errors.Errorf("%s 启用签名时必须配置 rsa_public_key_user 或 rsa_public_key_user_ref", name)
		}
		if strings.TrimSpace(cfg.RSAPrivateKeyServer) == "" && strings.TrimSpace(cfg.RSAPrivateKeyServerRef) == "" {
			return errors.Errorf("%s 启用签名时必须配置 rsa_private_key_server 或 rsa_private_key_server_ref", name)
		}
	}
	return nil
}

// securitySecretKeyTouched 判断 secret_key 是否显式配置过任一字段。
func securitySecretKeyTouched(cfg config.SecuritySecretKeyConfig) bool {
	return strings.TrimSpace(cfg.KeyVersion) != "" ||
		strings.TrimSpace(cfg.AESKey) != "" ||
		strings.TrimSpace(cfg.AESKeyRef) != "" ||
		strings.TrimSpace(cfg.AESIV) != "" ||
		strings.TrimSpace(cfg.AESIVRef) != "" ||
		strings.TrimSpace(cfg.RSAPublicKeyUser) != "" ||
		strings.TrimSpace(cfg.RSAPublicKeyUserRef) != "" ||
		strings.TrimSpace(cfg.RSAPublicKeyServer) != "" ||
		strings.TrimSpace(cfg.RSAPublicKeyServerRef) != "" ||
		strings.TrimSpace(cfg.RSAPrivateKeyServer) != "" ||
		strings.TrimSpace(cfg.RSAPrivateKeyServerRef) != "" ||
		strings.TrimSpace(cfg.StableVersion) != "" ||
		strings.TrimSpace(cfg.GrayVersion) != "" ||
		strings.TrimSpace(cfg.GraySalt) != "" ||
		(cfg.SignStatus != 0 && cfg.SignStatus != 1) ||
		(cfg.CryptoStatus != 0 && cfg.CryptoStatus != 1) ||
		len(cfg.Versions) > 0
}
