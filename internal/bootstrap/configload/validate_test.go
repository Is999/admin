package configload

import (
	"strings"
	"testing"

	"admin/common/idgen"
	"admin/internal/config"
	tasklimits "admin/internal/task/limits"
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

// TestValidateBootstrapConfigRejectsUnsafeAppID 确保 Redis 命名空间不会被分隔符、空白或超长值破坏。
func TestValidateBootstrapConfigRejectsUnsafeAppID(t *testing.T) {
	for _, appID := range []string{"site:other", "site name", "site/{slot}", strings.Repeat("a", maxConfigKeySegmentBytes+1)} {
		cfg := validAdminBootstrapConfig()
		cfg.AppID = appID
		if err := Validate(cfg); err == nil {
			t.Fatalf("期望非安全 app_id=%q 返回错误，实际为 nil", appID)
		}
	}
}

// TestValidateBootstrapConfigRejectsInvalidTrustedProxy 确保错误代理网段不会静默退化为错误客户端 IP。
func TestValidateBootstrapConfigRejectsInvalidTrustedProxy(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.TrustedProxies = []string{"not-a-cidr"}
	if err := Validate(cfg); err == nil {
		t.Fatal("期望非法 trusted_proxies 返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsInvalidTraceSampleRatio 确保链路采样率越界时启动失败。
func TestValidateBootstrapConfigRejectsInvalidTraceSampleRatio(t *testing.T) {
	for _, ratio := range []float64{-0.01, 1.01} {
		cfg := validAdminBootstrapConfig()
		cfg.Observability.SampleRatio = ratio
		if err := Validate(cfg); err == nil {
			t.Fatalf("期望 sample_ratio=%v 返回错误，实际为 nil", ratio)
		}
	}
}

// TestValidateBootstrapConfigRejectsInvalidHotReloadInterval 确保配置轮询间隔不会产生负时长或过长 ticker。
func TestValidateBootstrapConfigRejectsInvalidHotReloadInterval(t *testing.T) {
	for _, interval := range []int{-1, maxHotReloadIntervalSeconds + 1} {
		cfg := validAdminBootstrapConfig()
		cfg.HotReload.CheckIntervalSeconds = interval
		if err := Validate(cfg); err == nil {
			t.Fatalf("期望 check_interval_seconds=%d 返回错误，实际为 nil", interval)
		}
	}
	for _, interval := range []int{0, 1, maxHotReloadIntervalSeconds} {
		cfg := validAdminBootstrapConfig()
		cfg.HotReload.CheckIntervalSeconds = interval
		if err := Validate(cfg); err != nil {
			t.Fatalf("期望 check_interval_seconds=%d 通过，实际错误为 %v", interval, err)
		}
	}
}

// TestValidateBootstrapConfigRejectsInvalidIPRegionPaths 验证离线归属地启用时只接受非空绝对路径。
func TestValidateBootstrapConfigRejectsInvalidIPRegionPaths(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.IPRegion.Enabled = true
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "ip_region.enabled") {
		t.Fatalf("期望缺少 XDB 路径被拒绝，实际错误为 %v", err)
	}

	cfg.IPRegion.IPv4XDBPath = "./data/ip2region/ip2region_v4.xdb"
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "绝对路径") {
		t.Fatalf("期望相对 IPv4 XDB 路径被拒绝，实际错误为 %v", err)
	}

	cfg.IPRegion.IPv4XDBPath = "/data/admin/ip2region/ip2region_v4.xdb"
	if err := Validate(cfg); err != nil {
		t.Fatalf("期望绝对 IPv4 XDB 路径通过，实际错误为 %v", err)
	}

	cfg.IPRegion.IPv4XDBPath = ""
	cfg.IPRegion.IPv6XDBPath = "data/ip2region/ip2region_v6.xdb"
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "绝对路径") {
		t.Fatalf("期望相对 IPv6 XDB 路径被拒绝，实际错误为 %v", err)
	}
}

// TestValidateBootstrapConfigRejectsInvalidMainRedisTopology 确保主 Redis 模式、地址和 DB 配置不会被运行时静默改写。
func TestValidateBootstrapConfigRejectsInvalidMainRedisTopology(t *testing.T) {
	tests := []struct {
		name string             // 用例名称
		cfg  config.RedisConfig // 待校验配置
		want string             // 期望错误片段
	}{
		{name: "模式拼写错误", cfg: config.RedisConfig{Type: "cluser", Addrs: []string{"127.0.0.1:6379"}, PoolSize: 1}, want: "redis.type"},
		{name: "模式非小写", cfg: config.RedisConfig{Type: "Cluster", Addrs: []string{"127.0.0.1:6379"}, PoolSize: 1}, want: "小写规范值"},
		{name: "单机多地址", cfg: config.RedisConfig{Type: "single", Addrs: []string{"127.0.0.1:6379", "127.0.0.1:6380"}, PoolSize: 1}, want: "只能配置一个地址"},
		{name: "单机地址改写", cfg: config.RedisConfig{Type: "single", Addrs: []string{"127.0.0.1:6379"}, AddrMap: map[string]string{"redis": "127.0.0.1"}, PoolSize: 1}, want: "addr_map"},
		{name: "推断集群非零 DB", cfg: config.RedisConfig{Addrs: []string{"127.0.0.1:6379", "127.0.0.1:6380"}, DB: 1, PoolSize: 1}, want: "db 必须为 0"},
		{name: "集群非零 DB", cfg: config.RedisConfig{Type: "cluster", Addrs: []string{"127.0.0.1:6379"}, DB: 1, PoolSize: 1}, want: "db 必须为 0"},
		{name: "集群改写 key 空白", cfg: config.RedisConfig{Type: "cluster", Addrs: []string{"127.0.0.1:6379"}, AddrMap: map[string]string{" redis": "127.0.0.1"}, PoolSize: 1}, want: "addr_map"},
		{name: "集群改写 value 为空", cfg: config.RedisConfig{Type: "cluster", Addrs: []string{"127.0.0.1:6379"}, AddrMap: map[string]string{"redis": " "}, PoolSize: 1}, want: "addr_map"},
		{name: "负数 DB", cfg: config.RedisConfig{Type: "single", Addrs: []string{"127.0.0.1:6379"}, DB: -1, PoolSize: 1}, want: "db 不能小于 0"},
		{name: "地址首尾空白", cfg: config.RedisConfig{Type: "single", Addrs: []string{" 127.0.0.1:6379"}, PoolSize: 1}, want: "首尾空白"},
		{name: "地址数组含空项", cfg: config.RedisConfig{Type: "cluster", Addrs: []string{"127.0.0.1:6379", ""}, PoolSize: 1}, want: "空地址"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validAdminBootstrapConfig()
			cfg.Redis = tt.cfg
			err := Validate(cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("期望 Redis 配置返回包含 %q 的错误，实际为 %v", tt.want, err)
			}
		})
	}
}

// TestValidateBootstrapConfigAcceptsSupportedRedisTopologies 确保单地址缺省模式和显式集群保持原有可用语义。
func TestValidateBootstrapConfigAcceptsSupportedRedisTopologies(t *testing.T) {
	tests := []config.RedisConfig{
		{Addrs: []string{"127.0.0.1:6379"}, PoolSize: 1},
		{Type: "standalone", Addrs: []string{"127.0.0.1:6379"}, DB: 2, PoolSize: 1},
		{
			Type:     "cluster",
			Addrs:    []string{"redis-1:6379", "redis-2:6379"},
			AddrMap:  map[string]string{"redis-1": "127.0.0.1"},
			PoolSize: 1,
		},
		{
			Addrs:    []string{"redis-1:6379", "redis-2:6379"},
			AddrMap:  map[string]string{"redis-1": "127.0.0.1"},
			PoolSize: 1,
		},
	}
	for _, redisCfg := range tests {
		cfg := validAdminBootstrapConfig()
		cfg.Redis = redisCfg
		if err := Validate(cfg); err != nil {
			t.Fatalf("期望 Redis 配置通过，实际错误为 %v: %+v", err, redisCfg)
		}
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

// TestValidateBootstrapConfigRejectsNormalizedSnowflakeNamespaceCollision 确保租约 namespace 不会在 trim 后无序覆盖。
func TestValidateBootstrapConfigRejectsNormalizedSnowflakeNamespaceCollision(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.WorkerID = nil
	cfg.Snowflake.Redis = config.SnowflakeRedisConfig{
		Enabled: true,
		Namespaces: map[string]config.SnowflakeRedisNamespaceConfig{
			"user":   {NodeCount: 10},
			" user ": {NodeCount: 20},
		},
	}
	err := Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "规范化后重复") {
		t.Fatalf("期望 snowflake namespace 规范化重复返回精确错误，实际为 %v", err)
	}
}

// TestValidateBootstrapConfigRejectsNonCanonicalSnowflakeNamespace 确保租约 namespace 使用小写安全 key 字符。
func TestValidateBootstrapConfigRejectsNonCanonicalSnowflakeNamespace(t *testing.T) {
	for _, namespace := range []string{"User", "user:{slot}"} {
		cfg := validAdminBootstrapConfig()
		cfg.Snowflake.WorkerID = nil
		cfg.Snowflake.Redis = config.SnowflakeRedisConfig{
			Enabled: true,
			Namespaces: map[string]config.SnowflakeRedisNamespaceConfig{
				namespace: {NodeCount: 10},
			},
		}
		if err := Validate(cfg); err == nil {
			t.Fatalf("期望非规范 snowflake namespace=%q 返回错误，实际为 nil", namespace)
		}
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

// TestValidateFileStorageConfigRejectsInvalidDirectUpload 确保直传不会静默回退到服务端中转。
func TestValidateFileStorageConfigRejectsInvalidDirectUpload(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.FileStorage = config.FileStorageConfig{Type: "local", UploadMode: "direct"}
	if err := Validate(cfg); err == nil {
		t.Fatal("期望 local 存储配置 direct 上传返回错误，实际为 nil")
	}
}

// TestValidateFileStorageConfigRejectsIncompleteS3 确保 S3 必填配置在启动阶段完成校验。
func TestValidateFileStorageConfigRejectsIncompleteS3(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.FileStorage = config.FileStorageConfig{
		Type: "s3",
		S3:   config.FileStorageS3Config{Enabled: true, Region: "ap-southeast-1"},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("期望缺少 S3 bucket 返回错误，实际为 nil")
	}
}

// TestValidateFileStorageConfigRejectsInvalidVirusScanner 确保扫描器名称和超时使用受支持边界。
func TestValidateFileStorageConfigRejectsInvalidVirusScanner(t *testing.T) {
	tests := []config.FileStorageVirusScannerConfig{
		{Name: "custom"},
		{Name: config.VirusScannerClamAV, TimeoutSeconds: -1},
		{Name: config.VirusScannerClamAV, TimeoutSeconds: maxVirusScanTimeoutSeconds + 1},
	}
	for _, scanner := range tests {
		cfg := validAdminBootstrapConfig()
		cfg.FileStorage.VirusScanner = scanner
		if err := Validate(cfg); err == nil {
			t.Fatalf("期望非法病毒扫描配置返回错误，实际为 %+v", scanner)
		}
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

// TestValidateBootstrapConfigRejectsNormalizedSegmentNamespaceCollision 确保号段 namespace 不会在 trim 后无序覆盖。
func TestValidateBootstrapConfigRejectsNormalizedSegmentNamespaceCollision(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.Segment = config.IDSegmentConfig{
		Enabled:                true,
		Scope:                  "main",
		AllocateTimeoutSeconds: 2,
		Namespaces: map[string]config.IDSegmentNamespaceConfig{
			"recharge.order":   {Enabled: true, Step: 1000},
			" recharge.order ": {Enabled: true, Step: 2000},
		},
	}
	err := Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "规范化后重复") {
		t.Fatalf("期望 Segment namespace 规范化重复返回精确错误，实际为 %v", err)
	}
}

// TestValidateBootstrapConfigRejectsNonCanonicalSegmentNamespace 确保号段 namespace 使用小写安全 key 字符。
func TestValidateBootstrapConfigRejectsNonCanonicalSegmentNamespace(t *testing.T) {
	for _, namespace := range []string{"Recharge.Order", "recharge:{slot}"} {
		cfg := validAdminBootstrapConfig()
		cfg.Snowflake.Segment = config.IDSegmentConfig{
			Enabled:                true,
			Scope:                  "main",
			AllocateTimeoutSeconds: 2,
			Namespaces: map[string]config.IDSegmentNamespaceConfig{
				namespace: {Enabled: true, Step: 1000},
			},
		}
		if err := Validate(cfg); err == nil {
			t.Fatalf("期望非规范 Segment namespace=%q 返回错误，实际为 nil", namespace)
		}
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

// TestValidateBootstrapConfigRejectsUnsafeSnowflakeScope 确保部署级 scope 不会破坏 Redis key 或 Cluster slot。
func TestValidateBootstrapConfigRejectsUnsafeSnowflakeScope(t *testing.T) {
	for _, scope := range []string{"main scope", "main:{slot}"} {
		cfg := validAdminBootstrapConfig()
		cfg.Snowflake.WorkerID = nil
		cfg.Snowflake.Redis = config.SnowflakeRedisConfig{
			Enabled: true,
			Scope:   scope,
		}
		if err := Validate(cfg); err == nil {
			t.Fatalf("期望非安全 snowflake.redis.scope=%q 返回错误，实际为 nil", scope)
		}
	}
}

// TestValidateBootstrapConfigRejectsUnsafeSegmentScope 确保号段 scope 不会破坏 Redis key 或 Cluster slot。
func TestValidateBootstrapConfigRejectsUnsafeSegmentScope(t *testing.T) {
	for _, scope := range []string{"main scope", "main:segment"} {
		cfg := validAdminBootstrapConfig()
		cfg.Snowflake.Segment = config.IDSegmentConfig{
			Enabled:                true,
			Scope:                  scope,
			AllocateTimeoutSeconds: 2,
			Namespaces: map[string]config.IDSegmentNamespaceConfig{
				"recharge.order": {Enabled: true, Step: 1000},
			},
		}
		if err := Validate(cfg); err == nil {
			t.Fatalf("期望非安全 snowflake.segment.scope=%q 返回错误，实际为 nil", scope)
		}
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

// TestValidateBootstrapConfigRejectsOversizedSnowflakeLease 确保极端租约值不会溢出 time.Duration 并触发 ticker panic。
func TestValidateBootstrapConfigRejectsOversizedSnowflakeLease(t *testing.T) {
	cfg := validAdminBootstrapConfig()
	cfg.Snowflake.WorkerID = nil
	cfg.Snowflake.Redis = config.SnowflakeRedisConfig{
		Enabled:              true,
		LeaseSeconds:         maxSnowflakeRedisLeaseSeconds + 1,
		RenewIntervalSeconds: 1,
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("期望超大雪花 Redis 租约返回错误，实际为 nil")
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

// TestValidateTaskRedisConfigRejectsInvalidTopology 确保 task.redis 半配置和错误拓扑不会静默回退或改写。
func TestValidateTaskRedisConfigRejectsInvalidTopology(t *testing.T) {
	tests := []struct {
		name string             // 用例名称
		cfg  config.RedisConfig // 待校验配置
		want string             // 期望错误片段
	}{
		{name: "只配置 DB", cfg: config.RedisConfig{DB: 3}, want: "必须同时配置 addrs"},
		{name: "模式拼写错误", cfg: config.RedisConfig{Type: "cluser", Addrs: []string{"127.0.0.1:6379"}}, want: "task.redis.type"},
		{name: "单机多地址", cfg: config.RedisConfig{Type: "single", Addrs: []string{"127.0.0.1:6379", "127.0.0.1:6380"}}, want: "只能配置一个地址"},
		{name: "单机地址改写", cfg: config.RedisConfig{Type: "single", Addrs: []string{"127.0.0.1:6379"}, AddrMap: map[string]string{"redis": "127.0.0.1"}}, want: "addr_map"},
		{name: "集群非零 DB", cfg: config.RedisConfig{Type: "cluster", Addrs: []string{"127.0.0.1:6379"}, DB: 1}, want: "db 必须为 0"},
		{name: "集群改写 key 为空", cfg: config.RedisConfig{Type: "cluster", Addrs: []string{"127.0.0.1:6379"}, AddrMap: map[string]string{"": "127.0.0.1"}}, want: "addr_map"},
		{name: "集群改写 value 空白", cfg: config.RedisConfig{Type: "cluster", Addrs: []string{"127.0.0.1:6379"}, AddrMap: map[string]string{"redis": "127.0.0.1 "}}, want: "addr_map"},
		{name: "负数 DB", cfg: config.RedisConfig{Type: "single", Addrs: []string{"127.0.0.1:6379"}, DB: -1}, want: "db 不能小于 0"},
		{name: "地址含空项", cfg: config.RedisConfig{Type: "cluster", Addrs: []string{"127.0.0.1:6379", " "}}, want: "空地址"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTaskRedisConfig(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("期望 task.redis 返回包含 %q 的错误，实际为 %v", tt.want, err)
			}
		})
	}
}

// TestValidateTaskRedisConfigAcceptsSupportedTopologies 确保 task.redis 保留缺省连接池和单机 DB 语义。
func TestValidateTaskRedisConfigAcceptsSupportedTopologies(t *testing.T) {
	tests := []config.RedisConfig{
		{},
		{Addrs: []string{"127.0.0.1:6379"}, DB: 3},
		{
			Type:    "cluster",
			Addrs:   []string{"redis-1:6379", "redis-2:6379"},
			AddrMap: map[string]string{"redis-1": "127.0.0.1"},
		},
		{
			Addrs:   []string{"redis-1:6379", "redis-2:6379"},
			AddrMap: map[string]string{"redis-1": "127.0.0.1"},
		},
	}
	for _, redisCfg := range tests {
		if err := ValidateTaskRedisConfig(redisCfg); err != nil {
			t.Fatalf("期望 task.redis 配置通过，实际错误为 %v: %+v", err, redisCfg)
		}
	}
}

// TestValidateTaskLimitsRejectsUnboundedRetryAndTimeout 校验默认值和周期任务不能绕过任务资源硬上限。
func TestValidateTaskLimitsRejectsUnboundedRetryAndTimeout(t *testing.T) {
	tests := []config.TaskQueueConfig{
		{DefaultRetry: tasklimits.MaxRetry + 1},
		{DefaultTimeoutSeconds: tasklimits.MaxTimeoutSeconds + 1},
		{Periodic: []config.TaskPeriodicConfig{{Retry: tasklimits.MaxRetry + 1}}},
		{Periodic: []config.TaskPeriodicConfig{{TimeoutSeconds: tasklimits.MaxTimeoutSeconds + 1}}},
	}
	for index, taskCfg := range tests {
		if err := validateTaskLimits(taskCfg); err == nil {
			t.Fatalf("场景 %d 期望超过任务资源硬上限时返回错误", index)
		}
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

// int64Ptr 返回 int64 指针，便于构造可选配置。
func int64Ptr(value int64) *int64 {
	return &value
}
