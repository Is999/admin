package validators

import "admin/internal/config"

// validAdminBootstrapConfig 返回满足默认配置片段的管理端测试配置。
func validAdminBootstrapConfig() config.Config {
	return config.Config{
		AppID:     "1",
		JwtSecret: "test-jwt-secret-0123456789abcdef",
		Snowflake: config.SnowflakeConfig{
			WorkerID: int64Ptr(512),
		},
		Redis: config.RedisConfig{
			Type:     "single",
			Addrs:    []string{"127.0.0.1:6379"},
			PoolSize: 1,
		},
	}
}

// validAdminProductionBootstrapConfig 返回满足生产模式校验的管理端测试配置。
func validAdminProductionBootstrapConfig() config.Config {
	cfg := validAdminBootstrapConfig()
	cfg.Mode = "pro"
	cfg.AppID = "1"
	cfg.AppKey = "prod-app-key-0123456789abcdef"
	cfg.JwtSecret = "prod-jwt-secret-0123456789abcdef"
	return cfg
}

// validAdminSecuritySecretKey 返回生产模式可用的测试密钥配置。
func validAdminSecuritySecretKey() config.SecuritySecretKeyConfig {
	item := validAdminSecretKeyVersion("v1")
	return config.SecuritySecretKeyConfig{
		KeyVersion:          item.KeyVersion,
		AESKey:              item.AESKey,
		AESIV:               item.AESIV,
		RSAPublicKeyUser:    item.RSAPublicKeyUser,
		RSAPrivateKeyServer: item.RSAPrivateKeyServer,
		SignStatus:          1,
		CryptoStatus:        1,
		StableVersion:       "v1",
		GrayVersion:         "v2",
		GrayPercent:         10,
		GraySalt:            "secret-key-gray-salt",
		Versions:            []config.SecuritySecretKeyVersionConfig{item, validAdminSecretKeyVersion("v2")},
	}
}

// validAdminSecretKeyVersion 返回指定版本的测试密钥材料。
func validAdminSecretKeyVersion(version string) config.SecuritySecretKeyVersionConfig {
	return config.SecuritySecretKeyVersionConfig{
		KeyVersion:          version,
		AESKey:              "0123456789abcdef0123456789abcdef",
		AESIV:               "0123456789abcdef",
		RSAPublicKeyUser:    "-----BEGIN PUBLIC KEY-----\ntest\n-----END PUBLIC KEY-----",
		RSAPrivateKeyServer: "-----BEGIN RSA PRIVATE KEY-----\ntest\n-----END RSA PRIVATE KEY-----",
	}
}

// int64Ptr 返回 int64 指针，便于构造可选配置。
func int64Ptr(value int64) *int64 {
	return &value
}
