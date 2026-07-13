//go:build integration

package redislimit

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	keys "admin/common/rediskeys"
	"admin/common/runtimecfg"
	"admin/internal/config"

	"github.com/redis/go-redis/v9"
)

const (
	// integrationRedisAddrsEnv 指定真实 Redis 单机地址。
	integrationRedisAddrsEnv = "INTEGRATION_REDIS_ADDRS"
	// integrationRedisClusterAddrsEnv 指定真实 Redis Cluster 种子节点地址。
	integrationRedisClusterAddrsEnv = "INTEGRATION_REDIS_CLUSTER_ADDRS"
	// integrationRedisClusterAddrMapEnv 把 Cluster 公告地址映射为测试进程可访问地址。
	integrationRedisClusterAddrMapEnv = "INTEGRATION_REDIS_CLUSTER_ADDR_MAP"
	// integrationRedisUsernameEnv 指定真实 Redis ACL 用户名。
	integrationRedisUsernameEnv = "INTEGRATION_REDIS_USERNAME"
	// integrationRedisPasswordEnv 指定真实 Redis 密码。
	integrationRedisPasswordEnv = "INTEGRATION_REDIS_PASSWORD"
)

// TestLimiterOnRealRedis 验证真实 Redis 上的多客户端共享、key 隔离和状态 TTL。
func TestLimiterOnRealRedis(t *testing.T) {
	runLimiterIntegration(t, false)
}

// TestLimiterOnRealRedisCluster 验证真实 Redis Cluster 的跨节点脚本和共享额度。
func TestLimiterOnRealRedisCluster(t *testing.T) {
	runLimiterIntegration(t, true)
}

// runLimiterIntegration 在指定 Redis 拓扑复用同一组限流行为断言。
func runLimiterIntegration(t *testing.T, clusterMode bool) {
	t.Helper()
	firstClient := openIntegrationRedis(t, clusterMode)
	secondClient := openIntegrationRedis(t, clusterMode)
	topology := "single"
	if clusterMode {
		topology = "cluster"
	}
	appID := fmt.Sprintf("redis-limit-%s-%d", topology, time.Now().UnixNano())
	previous := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: appID})
	t.Cleanup(func() { runtimecfg.Restore(previous) })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first := New(firstClient)
	second := New(secondClient)
	mainKey := keys.RateLimitRedisKey("integration", "main")
	otherKey := keys.RateLimitRedisKey("integration", "other")
	for _, key := range []string{mainKey, otherKey} {
		resetIntegrationKey(t, ctx, firstClient, key)
	}
	fixedMainKey := keys.RateLimitFixedIntervalRedisKey("integration", "fixed-main")
	fixedOtherKey := keys.RateLimitFixedIntervalRedisKey("integration", "fixed-other")
	fixedRetryKey := keys.RateLimitFixedIntervalRedisKey("integration", "fixed-retry")
	for _, key := range []string{fixedMainKey, fixedOtherKey, fixedRetryKey} {
		resetIntegrationPhysicalKey(t, ctx, firstClient, key)
	}

	limit := Limit{Rate: 1, Burst: 1, Period: time.Second}
	if result, err := first.Allow(ctx, "integration", "main", limit); err != nil || !result.Allowed {
		t.Fatalf("首个客户端 Allow() result=%+v error=%v", result, err)
	}
	if result, err := second.Allow(ctx, "integration", "main", limit); err != nil || result.Allowed {
		t.Fatalf("第二个客户端应共享额度 result=%+v error=%v", result, err)
	}
	if result, err := second.Allow(ctx, "integration", "other", limit); err != nil || !result.Allowed {
		t.Fatalf("不同资源应独立 result=%+v error=%v", result, err)
	}

	concurrentLimit := Limit{Rate: 1, Burst: 7, Period: time.Hour}
	concurrentKey := keys.RateLimitRedisKey("integration", "concurrent")
	resetIntegrationKey(t, ctx, firstClient, concurrentKey)
	if got := countConcurrentAllowed(t, ctx, []*Limiter{first, second}, "integration", "concurrent", concurrentLimit, 64); got != int64(concurrentLimit.Burst) {
		t.Fatalf("真实 Redis 并发放行数=%d, want=%d", got, concurrentLimit.Burst)
	}

	physicalMainKey := physicalRateLimitKey(mainKey)
	if keyType := firstClient.Type(ctx, physicalMainKey).Val(); keyType != "string" {
		t.Fatalf("真实 Redis 限流 key 类型=%s, want string", keyType)
	}
	if ttl := firstClient.TTL(ctx, physicalMainKey).Val(); ttl <= 0 {
		t.Fatalf("真实 Redis 限流 key TTL=%s, want positive", ttl)
	}

	const fixedInterval = 2 * time.Second
	if err := first.WaitFixedInterval(ctx, "integration", "fixed-main", fixedInterval); err != nil {
		t.Fatalf("真实 Redis 首次固定间隔许可失败: %v", err)
	}
	if err := first.WaitFixedInterval(ctx, "integration", "fixed-main", 3*time.Second); err == nil || !strings.Contains(err.Error(), "不同间隔或规则复用") {
		t.Fatalf("真实 Redis 固定间隔规则冲突未失败关闭: %v", err)
	}
	if keyType := firstClient.Type(ctx, fixedMainKey).Val(); keyType != "string" {
		t.Fatalf("真实 Redis 固定间隔 key 类型=%s, want string", keyType)
	}
	if ttl := firstClient.PTTL(ctx, fixedMainKey).Val(); ttl < fixedInterval/2 || ttl > fixedInterval {
		t.Fatalf("真实 Redis 固定间隔 key TTL=%s, want [%s,%s]", ttl, fixedInterval/2, fixedInterval)
	}
	blockedCtx, blockedCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	err := second.WaitFixedInterval(blockedCtx, "integration", "fixed-main", fixedInterval)
	blockedCancel()
	if err == nil || !stderrors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("真实 Redis 跨客户端应共享固定间隔 error=%v", err)
	}
	if err := first.WaitFixedInterval(ctx, "integration", "fixed-other", fixedInterval); err != nil {
		t.Fatalf("真实 Redis 不同固定间隔资源应独立: %v", err)
	}

	const retryInterval = 250 * time.Millisecond
	if err := first.WaitFixedInterval(ctx, "integration", "fixed-retry", retryInterval); err != nil {
		t.Fatalf("真实 Redis 自动重试场景首次许可失败: %v", err)
	}
	retryStartedAt := time.Now()
	retryTTL := firstClient.PTTL(ctx, fixedRetryKey).Val()
	if retryTTL <= 0 || retryTTL > retryInterval {
		t.Fatalf("真实 Redis 自动重试场景初始 TTL=%s, want (0,%s]", retryTTL, retryInterval)
	}
	retryCtx, retryCancel := context.WithTimeout(ctx, 2*time.Second)
	if err := second.WaitFixedInterval(retryCtx, "integration", "fixed-retry", retryInterval); err != nil {
		retryCancel()
		t.Fatalf("真实 Redis 令牌到期后自动重试失败: %v", err)
	}
	retryCancel()
	if elapsed := time.Since(retryStartedAt); elapsed+30*time.Millisecond < retryTTL {
		t.Fatalf("真实 Redis 自动重试提前放行 elapsed=%s initial_ttl=%s", elapsed, retryTTL)
	}
	if clusterMode {
		cluster, ok := firstClient.(*redis.ClusterClient)
		if !ok {
			t.Fatalf("Cluster 集成测试客户端类型=%T, want *redis.ClusterClient", firstClient)
		}
		assertLimiterCoversClusterMasters(t, ctx, cluster, first, second, limit)
		assertFixedIntervalCoversClusterMasters(t, ctx, cluster, first, second)
	}
}

// assertLimiterCoversClusterMasters 为每个 Cluster master 选择一个 slot，验证跨节点执行和跨实例共享。
func assertLimiterCoversClusterMasters(t *testing.T, ctx context.Context, cluster *redis.ClusterClient, first, second *Limiter, limit Limit) {
	t.Helper()
	masterRanges := integrationClusterMasterRanges(t, ctx, cluster)

	candidate := 0
	for masterID, slotRange := range masterRanges {
		resource := ""
		logicalKey := ""
		for attempts := 0; attempts < 1024; attempts++ {
			candidate++
			currentResource := fmt.Sprintf("master-slot-%d", candidate)
			currentKey := keys.RateLimitRedisKey("integration", currentResource)
			physicalKey := physicalRateLimitKey(currentKey)
			slot, slotErr := cluster.Do(ctx, "CLUSTER", "KEYSLOT", physicalKey).Int()
			if slotErr != nil {
				t.Fatalf("计算真实 Redis Cluster slot 失败: %v", slotErr)
			}
			if slot >= slotRange.Start && slot <= slotRange.End {
				resource = currentResource
				logicalKey = currentKey
				break
			}
		}
		if resource == "" {
			t.Fatalf("未能为 Redis Cluster master=%s 选择 slot=%d-%d 的测试 key", masterID, slotRange.Start, slotRange.End)
		}
		physicalKey := physicalRateLimitKey(logicalKey)
		resetIntegrationKey(t, ctx, cluster, logicalKey)
		if result, allowErr := first.Allow(ctx, "integration", resource, limit); allowErr != nil || !result.Allowed {
			t.Fatalf("Redis Cluster master=%s 首次申请 result=%+v error=%v", masterID, result, allowErr)
		}
		if result, allowErr := second.Allow(ctx, "integration", resource, limit); allowErr != nil || result.Allowed {
			t.Fatalf("Redis Cluster master=%s 跨实例应共享额度 result=%+v error=%v", masterID, result, allowErr)
		}
		if keyType := cluster.Type(ctx, physicalKey).Val(); keyType != "string" {
			t.Fatalf("Redis Cluster master=%s 限流 key 类型=%s, want string", masterID, keyType)
		}
		if ttl := cluster.TTL(ctx, physicalKey).Val(); ttl <= 0 {
			t.Fatalf("Redis Cluster master=%s 限流 key TTL=%s, want positive", masterID, ttl)
		}
	}
}

// assertFixedIntervalCoversClusterMasters 为每个 Cluster master 选择固定间隔 key，验证跨实例共享和 TTL。
func assertFixedIntervalCoversClusterMasters(t *testing.T, ctx context.Context, cluster *redis.ClusterClient, first, second *Limiter) {
	t.Helper()
	masterRanges := integrationClusterMasterRanges(t, ctx, cluster)
	const interval = 2 * time.Second
	candidate := 0
	for masterID, slotRange := range masterRanges {
		resource := ""
		physicalKey := ""
		for attempts := 0; attempts < 1024; attempts++ {
			candidate++
			currentResource := fmt.Sprintf("fixed-master-slot-%d", candidate)
			currentKey := keys.RateLimitFixedIntervalRedisKey("integration", currentResource)
			slot, slotErr := cluster.Do(ctx, "CLUSTER", "KEYSLOT", currentKey).Int()
			if slotErr != nil {
				t.Fatalf("计算固定间隔 Redis Cluster slot 失败: %v", slotErr)
			}
			if slot >= slotRange.Start && slot <= slotRange.End {
				resource = currentResource
				physicalKey = currentKey
				break
			}
		}
		if resource == "" {
			t.Fatalf("未能为固定间隔 Redis Cluster master=%s 选择 slot=%d-%d 的测试 key", masterID, slotRange.Start, slotRange.End)
		}
		resetIntegrationPhysicalKey(t, ctx, cluster, physicalKey)
		if err := first.WaitFixedInterval(ctx, "integration", resource, interval); err != nil {
			t.Fatalf("Redis Cluster master=%s 首次固定间隔许可失败: %v", masterID, err)
		}
		blockedCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		err := second.WaitFixedInterval(blockedCtx, "integration", resource, interval)
		cancel()
		if err == nil || !stderrors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Redis Cluster master=%s 跨实例应共享固定间隔 error=%v", masterID, err)
		}
		if keyType := cluster.Type(ctx, physicalKey).Val(); keyType != "string" {
			t.Fatalf("Redis Cluster master=%s 固定间隔 key 类型=%s, want string", masterID, keyType)
		}
		if ttl := cluster.PTTL(ctx, physicalKey).Val(); ttl < interval/2 || ttl > interval {
			t.Fatalf("Redis Cluster master=%s 固定间隔 key TTL=%s, want [%s,%s]", masterID, ttl, interval/2, interval)
		}
	}
}

// integrationClusterMasterRanges 返回真实 Cluster 中每个 master 的一个 slot 区间。
func integrationClusterMasterRanges(t *testing.T, ctx context.Context, cluster *redis.ClusterClient) map[string]redis.ClusterSlot {
	t.Helper()
	slots, err := cluster.ClusterSlots(ctx).Result()
	if err != nil {
		t.Fatalf("读取真实 Redis Cluster slot 失败: %v", err)
	}
	masterRanges := make(map[string]redis.ClusterSlot)
	for _, slotRange := range slots {
		if len(slotRange.Nodes) == 0 {
			continue
		}
		master := slotRange.Nodes[0]
		masterID := strings.TrimSpace(master.ID)
		if masterID == "" {
			masterID = strings.TrimSpace(master.Addr)
		}
		if masterID == "" {
			continue
		}
		if _, exists := masterRanges[masterID]; !exists {
			masterRanges[masterID] = slotRange
		}
	}
	if len(masterRanges) < 2 {
		t.Fatalf("真实 Redis Cluster master 数=%d, want at least 2", len(masterRanges))
	}
	return masterRanges
}

// openIntegrationRedis 连接测试声明的真实 Redis 单机或 Cluster。
func openIntegrationRedis(t *testing.T, clusterMode bool) redis.UniversalClient {
	t.Helper()
	addrsEnv := integrationRedisAddrsEnv
	if clusterMode {
		addrsEnv = integrationRedisClusterAddrsEnv
	}
	addrs := integrationRedisAddrs(t, addrsEnv)
	username := strings.TrimSpace(os.Getenv(integrationRedisUsernameEnv))
	password := os.Getenv(integrationRedisPasswordEnv)
	var client redis.UniversalClient
	if !clusterMode {
		if len(addrs) != 1 {
			t.Fatalf("%s 必须只配置一个 Redis 单机地址，实际=%v", addrsEnv, addrs)
		}
		client = redis.NewClient(&redis.Options{Addr: addrs[0], Username: username, Password: password, ContextTimeoutEnabled: true})
	} else {
		options := &redis.ClusterOptions{Addrs: addrs, Username: username, Password: password, ContextTimeoutEnabled: true}
		addrMap := integrationRedisAddrMap(t)
		if len(addrMap) > 0 {
			options.NewClient = func(option *redis.Options) *redis.Client {
				cloned := *option
				if mapped, ok := addrMap[option.Addr]; ok {
					cloned.Addr = mapped
				}
				return redis.NewClient(&cloned)
			}
		}
		client = redis.NewClusterClient(options)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("连接真实 Redis 失败 addrs=%v error=%v", addrs, err)
	}
	return client
}

// integrationRedisAddrs 解析并校验真实 Redis 集成测试地址。
func integrationRedisAddrs(t *testing.T, envName string) []string {
	t.Helper()
	rawAddrs := strings.Split(strings.TrimSpace(os.Getenv(envName)), ",")
	addrs := make([]string, 0, len(rawAddrs))
	for _, rawAddr := range rawAddrs {
		if addr := strings.TrimSpace(rawAddr); addr != "" {
			addrs = append(addrs, addr)
		}
	}
	if len(addrs) == 0 {
		t.Skipf("%s 未配置，跳过对应 Redis 集成测试", envName)
	}
	return addrs
}

// integrationRedisAddrMap 解析 Cluster 公告地址到测试进程地址的精确映射。
func integrationRedisAddrMap(t *testing.T) map[string]string {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(integrationRedisClusterAddrMapEnv))
	if raw == "" {
		return nil
	}
	result := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		source, target, ok := strings.Cut(strings.TrimSpace(pair), "=")
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if !ok || source == "" || target == "" {
			t.Fatalf("%s 映射项非法 item=%q", integrationRedisClusterAddrMapEnv, pair)
		}
		if _, exists := result[source]; exists {
			t.Fatalf("%s 存在重复源地址 source=%s", integrationRedisClusterAddrMapEnv, source)
		}
		result[source] = target
	}
	return result
}

// resetIntegrationKey 清理测试 key，并注册有界的结束清理。
func resetIntegrationKey(t *testing.T, ctx context.Context, client redis.UniversalClient, logicalKey string) {
	t.Helper()
	physicalKey := physicalRateLimitKey(logicalKey)
	resetIntegrationPhysicalKey(t, ctx, client, physicalKey)
}

// resetIntegrationPhysicalKey 清理真实 Redis 物理 key，并注册有界的结束清理。
func resetIntegrationPhysicalKey(t *testing.T, ctx context.Context, client redis.UniversalClient, physicalKey string) {
	t.Helper()
	if err := client.Del(ctx, physicalKey).Err(); err != nil {
		t.Fatalf("清理真实 Redis 限流 key=%s 失败: %v", physicalKey, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = client.Del(cleanupCtx, physicalKey).Err()
	})
}
