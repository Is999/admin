package bootstrap

import (
	"testing"

	"admin/internal/config"
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

// TestValidateBootstrapConfigRejectsMissingAppID 确保 app_id 缺失时不会落到共享 Redis 默认命名空间。
func TestValidateBootstrapConfigRejectsMissingAppID(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.AppID = ""
	if err := validateBootstrapConfig(cfg); err == nil {
		t.Fatal("期望 app_id 缺失返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsMissingSnowflakeWorkerID 确保雪花 worker_id 缺失时启动失败。
func TestValidateBootstrapConfigRejectsMissingSnowflakeWorkerID(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.WorkerID = nil
	t.Setenv("SNOWFLAKE_WORKER_ID", "")
	if err := validateBootstrapConfig(cfg); err == nil {
		t.Fatal("期望 snowflake.worker_id 缺失返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsInvalidSnowflakeWorkerID 确保雪花 worker_id 越界时启动失败。
func TestValidateBootstrapConfigRejectsInvalidSnowflakeWorkerID(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.WorkerID = int64Ptr(1024)
	if err := validateBootstrapConfig(cfg); err == nil {
		t.Fatal("期望非法 snowflake.worker_id 返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsInvalidUserRouteShardCount 确保业务用户写入路由只允许平滑拆分档位。
func TestValidateBootstrapConfigRejectsInvalidUserRouteShardCount(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.User.RouteShardCount = 64
	if err := validateBootstrapConfig(cfg); err == nil {
		t.Fatal("期望非法 user.route_shard_count 返回错误，实际为 nil")
	}
}

// TestNormalizeBootstrapConfigDefaultsUserRouteShardCount 确保业务用户写入路由缺省时稳定回落单表。
func TestNormalizeBootstrapConfigDefaultsUserRouteShardCount(t *testing.T) {
	cfg := config.Config{}
	normalizeBootstrapConfig(&cfg)
	if cfg.User.RouteShardCount != defaultUserRouteShardCount {
		t.Fatalf("route_shard_count = %d, want %d", cfg.User.RouteShardCount, defaultUserRouteShardCount)
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

// TestValidateBootstrapConfigRejectsForeignCollectorStream 确保 Collector 不会误用其它站点 Redis Stream。
func TestValidateBootstrapConfigRejectsForeignCollectorStream(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.AppID = "site-2"
	cfg.Collector.Redis.Stream = "app:site-1:collector:events"
	if err := validateBootstrapConfig(cfg); err == nil {
		t.Fatal("期望其它 app_id 的 collector.redis.stream 返回错误，实际为 nil")
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

// validAdminBootstrapConfig 返回满足默认启动校验的管理端测试配置。
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
