package configload

import (
	"testing"

	"admin/internal/config"
)

// TestValidateBootstrapConfigRejectsWeakJWTSecret 确保明显弱 JWT 密钥不能通过启动校验。
func TestValidateBootstrapConfigRejectsWeakJWTSecret(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.JwtSecret = "short"
	if err := Validate(cfg); err == nil {
		t.Fatal("期望弱 jwt_secret 返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsMissingRedis 确保 Redis 地址缺失时启动失败。
func TestValidateBootstrapConfigRejectsMissingRedis(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Redis.Addrs = nil
	if err := Validate(cfg); err == nil {
		t.Fatal("期望 redis.addrs 缺失返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsMissingAppID 确保 app_id 缺失时不会落到共享 Redis 默认命名空间。
func TestValidateBootstrapConfigRejectsMissingAppID(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.AppID = ""
	if err := Validate(cfg); err == nil {
		t.Fatal("期望 app_id 缺失返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsMissingSnowflakeWorkerID 确保雪花 worker_id 缺失时启动失败。
func TestValidateBootstrapConfigRejectsMissingSnowflakeWorkerID(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.WorkerID = nil
	t.Setenv("SNOWFLAKE_WORKER_ID", "")
	if err := Validate(cfg); err == nil {
		t.Fatal("期望 snowflake.worker_id 缺失返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsInvalidSnowflakeWorkerID 确保雪花 worker_id 越界时启动失败。
func TestValidateBootstrapConfigRejectsInvalidSnowflakeWorkerID(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.WorkerID = int64Ptr(1024)
	if err := Validate(cfg); err == nil {
		t.Fatal("期望非法 snowflake.worker_id 返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsInvalidUserRouteShardCount 确保业务用户写入路由只允许平滑拆分档位。
func TestValidateBootstrapConfigRejectsInvalidUserRouteShardCount(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.User.RouteShardCount = 3
	if err := Validate(cfg); err == nil {
		t.Fatal("期望非法 user.route_shard_count 返回错误，实际为 nil")
	}
}

// TestValidateTaskRedisConfigRejectsPartial 确保 task.redis 半配置不会静默回退到主 Redis。
func TestValidateTaskRedisConfigRejectsPartial(t *testing.T) {
	if err := ValidateTaskRedisConfig(config.RedisConfig{DB: 3}); err == nil {
		t.Fatal("期望 task.redis 只配置 db 时返回错误，实际为 nil")
	}
	if err := ValidateTaskRedisConfig(config.RedisConfig{Addrs: []string{"127.0.0.1:6379"}, DB: 3}); err != nil {
		t.Fatalf("期望完整 task.redis 配置通过，实际错误为 %v", err)
	}
}

// TestNormalizeBootstrapConfigDefaultsUserRouteShardCount 确保业务用户写入路由缺省时稳定回落单表。
func TestNormalizeBootstrapConfigDefaultsUserRouteShardCount(t *testing.T) {
	cfg := config.Config{}
	Normalize(&cfg)
	if cfg.User.RouteShardCount != defaultUserRouteShardCount {
		t.Fatalf("route_shard_count = %d, want %d", cfg.User.RouteShardCount, defaultUserRouteShardCount)
	}
}

// TestValidateBootstrapConfigRejectsLarkWithoutWebhook 确保启用告警时必须配置发送端点。
func TestValidateBootstrapConfigRejectsLarkWithoutWebhook(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Alert.Lark.Enabled = true
	if err := Validate(cfg); err == nil {
		t.Fatal("期望 Lark webhook 缺失返回错误，实际为 nil")
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
