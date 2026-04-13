package bootstrap

import (
	"testing"

	"admin_cron/internal/config"
)

// TestValidateBootstrapConfigRejectsWeakJWTSecret 确保明显弱 JWT 密钥不能通过启动校验。
func TestValidateBootstrapConfigRejectsWeakJWTSecret(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.JwtSecret = "short"
	if err := validateBootstrapConfig(cfg); err == nil {
		t.Fatal("期望弱 jwt_secret 返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsMissingRedis 确保 Redis 地址缺失时启动失败。
func TestValidateBootstrapConfigRejectsMissingRedis(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Redis.Addrs = nil
	if err := validateBootstrapConfig(cfg); err == nil {
		t.Fatal("期望 redis.addrs 缺失返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsInvalidCollectorTransport 确保 Collector 载体只能使用受支持枚举。
func TestValidateBootstrapConfigRejectsInvalidCollectorTransport(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Collector.Transport = "amqp"
	if err := validateBootstrapConfig(cfg); err == nil {
		t.Fatal("期望非法 collector.transport 返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsForcedKafkaWithoutTopic 确保强制 Kafka 载体时必须具备可投递配置。
func TestValidateBootstrapConfigRejectsForcedKafkaWithoutTopic(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Collector = config.CollectorConfig{
		Enabled:   true,
		Transport: "kafka",
		Kafka: config.CollectorKafkaConfig{
			Enabled: true,
			Brokers: []string{"127.0.0.1:9092"},
		},
	}
	if err := validateBootstrapConfig(cfg); err == nil {
		t.Fatal("期望 collector.kafka.topic 缺失返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsCollectorKafkaWithoutGroup 确保启用 Collector 后 Kafka 消费组不可缺失。
func TestValidateBootstrapConfigRejectsCollectorKafkaWithoutGroup(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Collector = config.CollectorConfig{
		Enabled:   true,
		Transport: "kafka",
		Kafka: config.CollectorKafkaConfig{
			Enabled: true,
			Brokers: []string{"127.0.0.1:9092"},
			Topic:   "collector_events",
		},
	}
	if err := validateBootstrapConfig(cfg); err == nil {
		t.Fatal("期望 collector.kafka.group_id 缺失返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsLarkWithoutWebhook 确保启用告警时必须配置发送端点。
func TestValidateBootstrapConfigRejectsLarkWithoutWebhook(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Alert.Lark.Enabled = true
	if err := validateBootstrapConfig(cfg); err == nil {
		t.Fatal("期望 Lark webhook 缺失返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsProductionPlaceholderAppKey 确保生产环境不能使用占位 app_key。
func TestValidateBootstrapConfigRejectsProductionPlaceholderAppKey(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	cfg.AppKey = "replace-with-strong-app-key"
	if err := validateBootstrapConfig(cfg); err == nil {
		t.Fatal("期望生产 app_key 占位值返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsProductionRedisTLSInsecure 确保生产环境不能跳过 Redis TLS 校验。
func TestValidateBootstrapConfigRejectsProductionRedisTLSInsecure(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	cfg.Redis.TLSInsecureSkipVerify = true
	if err := validateBootstrapConfig(cfg); err == nil {
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
	if err := validateBootstrapConfig(cfg); err == nil {
		t.Fatal("期望生产 secret_key 灰度缺少 gray_salt 返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsInvalidSecretKeyStatus 确保只配置非法状态值也会触发校验。
func TestValidateBootstrapConfigRejectsInvalidSecretKeyStatus(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.AppID = "1"
	cfg.Security.SecretKey.SignStatus = 2
	if err := validateBootstrapConfig(cfg); err == nil {
		t.Fatal("期望非法 security.secret_key.sign_status 返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigAcceptsProductionSafeConfig 确保生产安全配置可以通过启动校验。
func TestValidateBootstrapConfigAcceptsProductionSafeConfig(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	cfg.Security.SecretKey = validAdminSecuritySecretKey()
	if err := validateBootstrapConfig(cfg); err != nil {
		t.Fatalf("validateBootstrapConfig() error = %v", err)
	}
}

func validAdminBootstrapConfig() config.Config {
	return config.Config{
		JwtSecret: "test-jwt-secret-0123456789abcdef",
		Redis: config.RedisConfig{
			Type:     "single",
			Addrs:    []string{"127.0.0.1:6379"},
			PoolSize: 1,
		},
	}
}

func validAdminProductionBootstrapConfig() config.Config {
	cfg := validAdminBootstrapConfig()
	cfg.Mode = "pro"
	cfg.AppID = "1"
	cfg.AppKey = "prod-app-key-0123456789abcdef"
	cfg.JwtSecret = "prod-jwt-secret-0123456789abcdef"
	return cfg
}

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

func validAdminSecretKeyVersion(version string) config.SecuritySecretKeyVersionConfig {
	return config.SecuritySecretKeyVersionConfig{
		KeyVersion:          version,
		AESKey:              "0123456789abcdef0123456789abcdef",
		AESIV:               "0123456789abcdef",
		RSAPublicKeyUser:    "-----BEGIN PUBLIC KEY-----\ntest\n-----END PUBLIC KEY-----",
		RSAPrivateKeyServer: "-----BEGIN RSA PRIVATE KEY-----\ntest\n-----END RSA PRIVATE KEY-----",
	}
}
