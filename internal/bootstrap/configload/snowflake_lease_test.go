package configload

import (
	"context"
	"strings"
	"testing"

	"admin/common/idgen"
	keys "admin/common/rediskeys"
	"admin/internal/config"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

const (
	snowflakeTestScope             = "unit"           // snowflakeTestScope 表示测试部署级租约池。
	snowflakeTestUserNamespace     = "user"           // snowflakeTestUserNamespace 表示用户业务 ID 命名空间。
	snowflakeTestRechargeNamespace = "recharge.order" // snowflakeTestRechargeNamespace 表示充值订单业务 ID 命名空间。
	snowflakeTestWithdrawNamespace = "withdraw.order" // snowflakeTestWithdrawNamespace 表示提现订单业务 ID 命名空间。
)

// TestConfigureSnowflakeWorkerAcquiresRedisLease 验证 Redis 租约模式按业务 namespace 懒申请 node_id 并在关闭时释放。
func TestConfigureSnowflakeWorkerAcquiresRedisLease(t *testing.T) {
	cleanupSnowflakeWorker(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	lease, err := ConfigureSnowflakeWorker(context.Background(), redisSnowflakeConfig(snowflakeTestScope), client)
	if err != nil {
		t.Fatalf("ConfigureSnowflakeWorker() error = %v", err)
	}
	if _, ok := idgen.CurrentWorkerID(snowflakeTestUserNamespace); ok {
		t.Fatal("expected no namespace worker before first ID")
	}
	if _, err = idgen.NextID(snowflakeTestUserNamespace); err != nil {
		t.Fatalf("NextID(user) error = %v", err)
	}
	workerID, ok := idgen.CurrentWorkerID(snowflakeTestUserNamespace)
	if !ok {
		t.Fatal("expected user worker_id to be configured")
	}
	key := keys.SnowflakeNodeLeaseKey(snowflakeTestScope, snowflakeTestUserNamespace, workerID)
	if !server.Exists(key) {
		t.Fatalf("expected snowflake lease key %s to exist", key)
	}
	if err = lease.Close(context.Background()); err != nil {
		t.Fatalf("lease.Close() error = %v", err)
	}
	if server.Exists(key) {
		t.Fatalf("expected snowflake lease key %s to be released", key)
	}
	if _, ok = idgen.CurrentWorkerID(snowflakeTestUserNamespace); ok {
		t.Fatal("expected user worker_id to be released")
	}
}

// TestConfigureSnowflakeWorkerUsesNamespaceNodeCount 验证 namespace 可限制可竞争的 node_id 池大小。
func TestConfigureSnowflakeWorkerUsesNamespaceNodeCount(t *testing.T) {
	cleanupSnowflakeWorker(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	cfg := redisSnowflakeConfig(snowflakeTestScope)
	cfg.Redis.Namespaces = map[string]config.SnowflakeRedisNamespaceConfig{
		snowflakeTestUserNamespace: {NodeCount: 10},
	}
	lease, err := ConfigureSnowflakeWorker(context.Background(), cfg, client)
	if err != nil {
		t.Fatalf("ConfigureSnowflakeWorker() error = %v", err)
	}
	if _, err = idgen.NextID(snowflakeTestUserNamespace); err != nil {
		t.Fatalf("NextID(user) error = %v", err)
	}
	workerID, ok := idgen.CurrentWorkerID(snowflakeTestUserNamespace)
	if !ok {
		t.Fatal("expected user worker_id to be configured")
	}
	if workerID < 0 || workerID >= 10 {
		t.Fatalf("user worker_id = %d, want 0-9", workerID)
	}
	if err = lease.Close(context.Background()); err != nil {
		t.Fatalf("lease.Close() error = %v", err)
	}
}

// TestConfigureSnowflakeWorkerReportsNamespaceNodePoolExhausted 验证 namespace 小池位耗尽时不会越界抢占。
func TestConfigureSnowflakeWorkerReportsNamespaceNodePoolExhausted(t *testing.T) {
	cleanupSnowflakeWorker(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	cfg := redisSnowflakeConfig(snowflakeTestScope)
	cfg.Redis.Namespaces = map[string]config.SnowflakeRedisNamespaceConfig{
		snowflakeTestUserNamespace: {NodeCount: 10},
	}
	for workerID := int64(0); workerID < 10; workerID++ {
		key := keys.SnowflakeNodeLeaseKey(snowflakeTestScope, snowflakeTestUserNamespace, workerID)
		server.Set(key, "occupied")
	}
	lease, err := ConfigureSnowflakeWorker(context.Background(), cfg, client)
	if err != nil {
		t.Fatalf("ConfigureSnowflakeWorker() error = %v", err)
	}
	if _, err = idgen.NextID(snowflakeTestUserNamespace); err == nil || !strings.Contains(err.Error(), "range=0-9") {
		t.Fatalf("NextID(user) error = %v, want exhausted range=0-9", err)
	}
	if err = lease.Close(context.Background()); err != nil {
		t.Fatalf("lease.Close() error = %v", err)
	}
}

// TestConfigureSnowflakeWorkerSkipsOccupiedRedisNode 验证已有租约不会被第二个实例复用。
func TestConfigureSnowflakeWorkerSkipsOccupiedRedisNode(t *testing.T) {
	cleanupSnowflakeWorker(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	first, err := ConfigureSnowflakeWorker(context.Background(), redisSnowflakeConfig(snowflakeTestScope), client)
	if err != nil {
		t.Fatalf("ConfigureSnowflakeWorker(first) error = %v", err)
	}
	if _, err = idgen.NextID(snowflakeTestUserNamespace); err != nil {
		t.Fatalf("NextID(first user) error = %v", err)
	}
	firstWorkerID, _ := idgen.CurrentWorkerID(snowflakeTestUserNamespace)
	second, err := ConfigureSnowflakeWorker(context.Background(), redisSnowflakeConfig(snowflakeTestScope), client)
	if err != nil {
		t.Fatalf("ConfigureSnowflakeWorker(second) error = %v", err)
	}
	if _, err = idgen.NextID(snowflakeTestUserNamespace); err != nil {
		t.Fatalf("NextID(second user) error = %v", err)
	}
	secondWorkerID, _ := idgen.CurrentWorkerID(snowflakeTestUserNamespace)
	if secondWorkerID == firstWorkerID {
		t.Fatalf("expected second lease to use another node_id, got %d", secondWorkerID)
	}
	if err = first.Close(context.Background()); err != nil {
		t.Fatalf("first.Close() error = %v", err)
	}
	if err = second.Close(context.Background()); err != nil {
		t.Fatalf("second.Close() error = %v", err)
	}
}

// TestConfigureSnowflakeWorkerIsolatesBusinessNamespaces 验证不同业务 namespace 使用相互独立的 Redis 租约 key。
func TestConfigureSnowflakeWorkerIsolatesBusinessNamespaces(t *testing.T) {
	cleanupSnowflakeWorker(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	lease, err := ConfigureSnowflakeWorker(context.Background(), redisSnowflakeConfig(snowflakeTestScope), client)
	if err != nil {
		t.Fatalf("ConfigureSnowflakeWorker() error = %v", err)
	}
	if _, err = idgen.NextID(snowflakeTestRechargeNamespace); err != nil {
		t.Fatalf("NextID(recharge.order) error = %v", err)
	}
	rechargeWorkerID, ok := idgen.CurrentWorkerID(snowflakeTestRechargeNamespace)
	if !ok {
		t.Fatal("expected recharge namespace worker_id")
	}
	if _, err = idgen.NextID(snowflakeTestWithdrawNamespace); err != nil {
		t.Fatalf("NextID(withdraw.order) error = %v", err)
	}
	withdrawWorkerID, ok := idgen.CurrentWorkerID(snowflakeTestWithdrawNamespace)
	if !ok {
		t.Fatal("expected withdraw namespace worker_id")
	}
	rechargeKey := keys.SnowflakeNodeLeaseKey(snowflakeTestScope, snowflakeTestRechargeNamespace, rechargeWorkerID)
	withdrawKey := keys.SnowflakeNodeLeaseKey(snowflakeTestScope, snowflakeTestWithdrawNamespace, withdrawWorkerID)
	if rechargeKey == withdrawKey {
		t.Fatalf("expected different namespace lease keys, got %s", rechargeKey)
	}
	if !server.Exists(rechargeKey) || !server.Exists(withdrawKey) {
		t.Fatalf("expected both namespace lease keys to exist recharge=%s withdraw=%s", rechargeKey, withdrawKey)
	}
	if err = lease.Close(context.Background()); err != nil {
		t.Fatalf("lease.Close() error = %v", err)
	}
	if server.Exists(rechargeKey) || server.Exists(withdrawKey) {
		t.Fatalf("expected namespace lease keys to be released")
	}
}

// TestConfigureSnowflakeWorkerRejectsMissingRedisClient 验证 Redis 模式不会静默回退静态 worker。
func TestConfigureSnowflakeWorkerRejectsMissingRedisClient(t *testing.T) {
	cleanupSnowflakeWorker(t)
	if _, err := ConfigureSnowflakeWorker(context.Background(), redisSnowflakeConfig(snowflakeTestScope), nil); err == nil {
		t.Fatal("expected nil redis client to be rejected")
	}
}

// TestConfigureSnowflakeWorkerUsesSegmentForConfiguredNamespace 验证配置的高吞吐 namespace 使用 Redis Segment 号段。
func TestConfigureSnowflakeWorkerUsesSegmentForConfiguredNamespace(t *testing.T) {
	cleanupSnowflakeWorker(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	cfg := redisSnowflakeConfig(snowflakeTestScope)
	cfg.Segment = testIDSegmentConfig(snowflakeTestScope, snowflakeTestRechargeNamespace, 3)
	lease, err := ConfigureSnowflakeWorker(context.Background(), cfg, client)
	if err != nil {
		t.Fatalf("ConfigureSnowflakeWorker() error = %v", err)
	}
	first, err := idgen.NextID(snowflakeTestRechargeNamespace)
	if err != nil {
		t.Fatalf("NextID(segment first) error = %v", err)
	}
	second, err := idgen.NextID(snowflakeTestRechargeNamespace)
	if err != nil {
		t.Fatalf("NextID(segment second) error = %v", err)
	}
	if first != 1 || second != 2 {
		t.Fatalf("segment ids = %d,%d want 1,2", first, second)
	}
	if _, ok := idgen.CurrentWorkerID(snowflakeTestRechargeNamespace); ok {
		t.Fatal("segment namespace should not acquire snowflake worker")
	}
	key := keys.IDSegmentCounterKey(snowflakeTestScope, snowflakeTestRechargeNamespace)
	if got, _ := server.Get(key); got != "3" {
		t.Fatalf("segment high-water = %q want 3", got)
	}
	if err = lease.Close(context.Background()); err != nil {
		t.Fatalf("lease.Close() error = %v", err)
	}
}

// TestSegmentIDContinuesFromLocalRangeWhenRedisUnavailable 验证 Redis 短暂不可用时可继续消耗本地号段。
func TestSegmentIDContinuesFromLocalRangeWhenRedisUnavailable(t *testing.T) {
	cleanupSnowflakeWorker(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	cfg := config.SnowflakeConfig{
		WorkerID: int64Ptr(12),
		Segment:  testIDSegmentConfig(snowflakeTestScope, snowflakeTestRechargeNamespace, 3),
	}
	cfg.Segment.AllocateTimeoutSeconds = 1
	lease, err := ConfigureSnowflakeWorker(context.Background(), cfg, client)
	if err != nil {
		t.Fatalf("ConfigureSnowflakeWorker() error = %v", err)
	}
	for want := int64(1); want <= 3; want++ {
		id, nextErr := idgen.NextID(snowflakeTestRechargeNamespace)
		if nextErr != nil {
			t.Fatalf("NextID(local segment %d) error = %v", want, nextErr)
		}
		if id != want {
			t.Fatalf("NextID(local segment) = %d want %d", id, want)
		}
		if want == 1 {
			_ = client.Close()
			server.Close()
		}
	}
	if _, err = idgen.NextID(snowflakeTestRechargeNamespace); err == nil {
		t.Fatal("expected exhausted local segment to fail when Redis is unavailable")
	}
	if err = lease.Close(context.Background()); err != nil {
		t.Fatalf("lease.Close() error = %v", err)
	}
}

// TestConfigureSnowflakeWorkerSegmentCloseStopsNamespace 验证关闭运行期资源后 Segment namespace 不会继续发号。
func TestConfigureSnowflakeWorkerSegmentCloseStopsNamespace(t *testing.T) {
	cleanupSnowflakeWorker(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	cfg := redisSnowflakeConfig(snowflakeTestScope)
	cfg.Segment = testIDSegmentConfig(snowflakeTestScope, snowflakeTestRechargeNamespace, 3)
	lease, err := ConfigureSnowflakeWorker(context.Background(), cfg, client)
	if err != nil {
		t.Fatalf("ConfigureSnowflakeWorker() error = %v", err)
	}
	if _, err = idgen.NextID(snowflakeTestRechargeNamespace); err != nil {
		t.Fatalf("NextID(segment) error = %v", err)
	}
	if err = lease.Close(context.Background()); err != nil {
		t.Fatalf("lease.Close() error = %v", err)
	}
	if _, err = idgen.NextID(snowflakeTestRechargeNamespace); err == nil {
		t.Fatal("expected segment namespace to stop after runtime close")
	}
}

// redisSnowflakeConfig 返回测试使用的 Redis 租约配置。
func redisSnowflakeConfig(scope string) config.SnowflakeConfig {
	return config.SnowflakeConfig{
		Redis: config.SnowflakeRedisConfig{
			Enabled:              true,
			Scope:                scope,
			LeaseSeconds:         30,
			RenewIntervalSeconds: 5,
		},
	}
}

// testIDSegmentConfig 返回单 namespace 测试 Segment 配置。
func testIDSegmentConfig(scope string, namespace string, step int64) config.IDSegmentConfig {
	return config.IDSegmentConfig{
		Enabled:                true,
		Scope:                  scope,
		AllocateTimeoutSeconds: 1,
		Namespaces: map[string]config.IDSegmentNamespaceConfig{
			namespace: {
				Enabled:           true,
				Step:              step,
				PrefetchThreshold: 0,
			},
		},
	}
}

// cleanupSnowflakeWorker 清理进程级雪花 worker，避免测试间串扰。
func cleanupSnowflakeWorker(t *testing.T) {
	t.Helper()
	idgen.ClearWorkerResolver(0)
	idgen.ClearSegmentResolver(0)
	t.Cleanup(func() {
		idgen.ClearWorkerResolver(0)
		idgen.ClearSegmentResolver(0)
	})
}
