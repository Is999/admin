package configload

import (
	"testing"

	"admin/common/idgen"
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

// TestValidateBootstrapConfigRejectsMissingSnowflakeWorkerID 确保未启用 Redis 租约时雪花 worker_id 缺失会启动失败。
func TestValidateBootstrapConfigRejectsMissingSnowflakeWorkerID(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.WorkerID = nil
	t.Setenv("SNOWFLAKE_WORKER_ID", "")
	if err := Validate(cfg); err == nil {
		t.Fatal("期望 snowflake.worker_id 缺失返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigAcceptsRedisSnowflakeLease 确保 Redis 租约模式不要求静态 worker_id。
func TestValidateBootstrapConfigAcceptsRedisSnowflakeLease(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.WorkerID = nil
	cfg.Snowflake.Redis = config.SnowflakeRedisConfig{
		Enabled: true,
		Namespaces: map[string]config.SnowflakeRedisNamespaceConfig{
			"user": {NodeCount: 10},
		},
	}
	t.Setenv("SNOWFLAKE_WORKER_ID", "")
	if err := Validate(cfg); err != nil {
		t.Fatalf("期望 Redis 雪花租约配置通过，实际错误为 %v", err)
	}
}

// TestValidateBootstrapConfigRejectsInvalidSnowflakeNamespaceNodeCount 确保 namespace node_id 池大小受雪花位宽约束。
func TestValidateBootstrapConfigRejectsInvalidSnowflakeNamespaceNodeCount(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.WorkerID = nil
	cfg.Snowflake.Redis = config.SnowflakeRedisConfig{
		Enabled: true,
		Namespaces: map[string]config.SnowflakeRedisNamespaceConfig{
			"user": {NodeCount: int(idgen.SnowflakeMaxWorkerID + 2)},
		},
	}
	t.Setenv("SNOWFLAKE_WORKER_ID", "")
	if err := Validate(cfg); err == nil {
		t.Fatal("期望过大的 snowflake.redis namespace node_count 返回错误，实际为 nil")
	}

	cfg = validAdminBootstrapConfig()
	cfg.Snowflake.WorkerID = nil
	cfg.Snowflake.Redis = config.SnowflakeRedisConfig{
		Enabled: true,
		Namespaces: map[string]config.SnowflakeRedisNamespaceConfig{
			"user": {NodeCount: -1},
		},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("期望负数 snowflake.redis namespace node_count 返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigAcceptsIDSegmentNamespace 确保高吞吐业务可单独启用 Redis Segment 号段。
func TestValidateBootstrapConfigAcceptsIDSegmentNamespace(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.Segment = config.IDSegmentConfig{
		Enabled:                true,
		Scope:                  "main",
		AllocateTimeoutSeconds: 2,
		Namespaces: map[string]config.IDSegmentNamespaceConfig{
			"recharge.order": {
				Enabled:           true,
				Step:              10000,
				PrefetchThreshold: 2000,
			},
		},
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("期望合法 ID Segment 配置通过，实际错误为 %v", err)
	}
}

// TestValidateBootstrapConfigRejectsIDSegmentWithoutNamespace 确保开启 Segment 时不能没有启用的业务 namespace。
func TestValidateBootstrapConfigRejectsIDSegmentWithoutNamespace(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.Segment = config.IDSegmentConfig{Enabled: true}
	if err := Validate(cfg); err == nil {
		t.Fatal("期望 ID Segment 未配置 namespace 时返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsInvalidIDSegmentOptions 确保 Segment 号段参数保持在可控范围内。
func TestValidateBootstrapConfigRejectsInvalidIDSegmentOptions(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.Segment = config.IDSegmentConfig{
		Enabled: true,
		Scope:   "main scope",
		Namespaces: map[string]config.IDSegmentNamespaceConfig{
			"recharge.order": {Enabled: true, Step: 1000},
		},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("期望 ID Segment scope 包含空白时返回错误，实际为 nil")
	}

	cfg = validAdminBootstrapConfig()
	cfg.Snowflake.Segment = config.IDSegmentConfig{
		Enabled: true,
		Scope:   "main",
		Namespaces: map[string]config.IDSegmentNamespaceConfig{
			"recharge.order": {Enabled: true, Step: 1000, PrefetchThreshold: 1000},
		},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("期望 ID Segment 预取阈值非法时返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsSnowflakeScopeWhitespace 确保部署级 scope 不能包含空白字符。
func TestValidateBootstrapConfigRejectsSnowflakeScopeWhitespace(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.WorkerID = nil
	cfg.Snowflake.Redis = config.SnowflakeRedisConfig{
		Enabled: true,
		Scope:   "main scope",
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("期望 snowflake.redis.scope 包含空白时返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsMixedSnowflakeModes 确保 Redis 租约模式不会被静态 worker_id 覆盖。
func TestValidateBootstrapConfigRejectsMixedSnowflakeModes(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.Redis.Enabled = true
	if err := Validate(cfg); err == nil {
		t.Fatal("期望 snowflake.worker_id 与 Redis 租约混用返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsUnsafeSnowflakeLeaseInterval 确保续约间隔不能贴近租约 TTL。
func TestValidateBootstrapConfigRejectsUnsafeSnowflakeLeaseInterval(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.WorkerID = nil
	cfg.Snowflake.Redis = config.SnowflakeRedisConfig{
		Enabled:              true,
		LeaseSeconds:         20,
		RenewIntervalSeconds: 10,
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("期望不安全的雪花 Redis 续约间隔返回错误，实际为 nil")
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

// TestValidateBootstrapConfigRejectsInvalidUserExportSplitRows 确保用户导出拆分阈值不能为负。
func TestValidateBootstrapConfigRejectsInvalidUserExportSplitRows(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.User.ExportSplitRows = -1
	if err := Validate(cfg); err == nil {
		t.Fatal("期望非法 user.export_split_rows 返回错误，实际为 nil")
	}
	cfg.User.ExportSplitRows = maxUserExportSplitRows + 1
	if err := Validate(cfg); err == nil {
		t.Fatal("期望过大的 user.export_split_rows 返回错误，实际为 nil")
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
	if cfg.User.ExportSplitRows != defaultUserExportSplitRows {
		t.Fatalf("export_split_rows = %d, want %d", cfg.User.ExportSplitRows, defaultUserExportSplitRows)
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
