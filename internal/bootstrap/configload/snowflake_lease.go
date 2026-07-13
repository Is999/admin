package configload

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"os"
	"sync"
	"time"

	"admin/common/embedasset"
	"admin/common/idgen"
	keys "admin/common/rediskeys"
	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	defaultSnowflakeRedisScope                = "default" // 默认部署级 node_id 池作用域
	defaultSnowflakeRedisLeaseSeconds         = 120       // 默认 node_id 租约 TTL
	defaultSnowflakeRedisRenewIntervalSeconds = 30        // 默认 node_id 续约间隔
	minSnowflakeRedisLeaseSeconds             = 10        // 最小租约 TTL，避免续约抖动导致频繁失效
	maxSnowflakeRedisLeaseSeconds             = 86400     // 最大租约 TTL，避免极端整数转换溢出并限制故障恢复窗口
	snowflakeLeaseOwnerRandomBytes            = 8         // 租约 owner 随机后缀字节数
)

var (
	// snowflakeLeaseRenewScript 在 Redis 内原子比较 owner 并刷新 node_id 租约 TTL。
	snowflakeLeaseRenewScript = redis.NewScript(embedasset.StripLeadingLineComments(snowflakeLeaseRenewScriptText, "--"))
	// snowflakeLeaseReleaseScript 在 Redis 内原子比较 owner 并释放 node_id 租约。
	snowflakeLeaseReleaseScript = redis.NewScript(embedasset.StripLeadingLineComments(snowflakeLeaseReleaseScriptText, "--"))
)

// snowflakeLeaseRenewScriptText 保存雪花 node_id 租约续期 Lua 脚本源码。
// 脚本只操作单个 node_id 租约 key，兼容 Redis Cluster 单 key 执行约束。
//
//go:embed assets/snowflake_node_lease_renew.lua
var snowflakeLeaseRenewScriptText string

// snowflakeLeaseReleaseScriptText 保存雪花 node_id 租约释放 Lua 脚本源码。
// 脚本只在 owner 匹配时删除租约 key，避免误删其它实例新租约。
//
//go:embed assets/snowflake_node_lease_release.lua
var snowflakeLeaseReleaseScriptText string

// SnowflakeLease 表示 ID 生成器持有的运行期资源。
type SnowflakeLease interface {
	Ready(context.Context) error
	Close(context.Context) error
}

// idGeneratorRuntimeGroup 聚合雪花租约和 Segment 号段等 ID 生成运行期资源。
type idGeneratorRuntimeGroup struct {
	resources []SnowflakeLease // resources 按创建顺序保存，关闭时也按该顺序释放
}

// Ready 检查当前 ID 生成运行资源是否仍可用。
func (g idGeneratorRuntimeGroup) Ready(ctx context.Context) error {
	for _, resource := range g.resources {
		if resource == nil {
			continue
		}
		if err := resource.Ready(ctx); err != nil {
			return errors.Tag(err)
		}
	}
	return nil
}

// snowflakeRedisLeaseManager 管理当前实例按业务命名空间持有的 Redis node_id 租约。
type snowflakeRedisLeaseManager struct {
	client        redis.UniversalClient           // Redis 客户端，兼容单机和集群
	cfg           config.SnowflakeRedisConfig     // 已补齐默认值的 Redis 租约配置
	owner         string                          // 当前实例统一租约 owner
	resolverToken uint64                          // idgen 动态解析器绑定 token
	mu            sync.Mutex                      // 保护 leases 和 closed
	leases        map[string]*snowflakeRedisLease // 按业务 namespace 保存当前实例持有的 node_id 租约
	closed        bool                            // closed 表示管理器已关闭，不再分配新 namespace
}

// snowflakeRedisLease 保存当前实例在单个业务命名空间抢到的 Redis node_id 租约。
type snowflakeRedisLease struct {
	client        redis.UniversalClient // Redis 客户端，兼容单机和集群
	namespace     string                // 业务命名空间，如 user、recharge.order
	key           string                // 当前 node_id 租约 key
	owner         string                // 当前实例租约 owner
	workerID      int64                 // 当前租约对应的雪花 worker_id
	ttl           time.Duration         // Redis 租约 TTL
	renewInterval time.Duration         // 租约续约间隔
	stop          chan struct{}         // 关闭续约循环信号
	done          chan struct{}         // 续约循环退出信号
	closeDone     chan struct{}         // 租约关闭流程完成信号
	closeErr      error                 // 首次关闭流程结果
	closeOnce     sync.Once             // 确保租约只释放一次
	releaseOnce   sync.Once             // 确保本地 worker 状态只释放一次
	onRelease     func(string, int64)   // 本地租约丢失后的管理器回调
}

// ConfigureSnowflakeWorker 配置当前进程默认雪花 worker，并按需启用高吞吐 Segment 号段。
func ConfigureSnowflakeWorker(ctx context.Context, cfg config.SnowflakeConfig, client redis.UniversalClient) (SnowflakeLease, error) {
	resources := make([]SnowflakeLease, 0, 2)
	if cfg.Redis.Enabled {
		lease, err := newSnowflakeRedisLeaseManager(ctx, cfg.Redis, client)
		if err != nil {
			return nil, errors.Tag(err)
		}
		resources = append(resources, lease)
	} else if err := ConfigureSnowflakeWorkerID(cfg); err != nil {
		return nil, errors.Tag(err)
	}
	if cfg.Segment.Enabled {
		segmentManager, err := newRedisSegmentManager(ctx, cfg.Segment, cfg.Redis, client)
		if err != nil {
			_ = closeIDGeneratorResources(ctx, resources)
			return nil, errors.Tag(err)
		}
		if segmentManager != nil {
			resources = append(resources, segmentManager)
		}
	}
	if len(resources) == 0 {
		return nil, nil
	}
	if len(resources) == 1 {
		return resources[0], nil
	}
	return idGeneratorRuntimeGroup{resources: resources}, nil
}

// Close 释放当前进程 ID 生成器持有的所有运行期资源。
func (g idGeneratorRuntimeGroup) Close(ctx context.Context) error {
	return closeIDGeneratorResources(ctx, g.resources)
}

// closeIDGeneratorResources 按创建顺序释放 ID 运行期资源。
func closeIDGeneratorResources(ctx context.Context, resources []SnowflakeLease) error {
	var closeErr error
	for _, resource := range resources {
		if resource == nil {
			continue
		}
		if err := resource.Close(ctx); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return errors.Tag(closeErr)
}

// normalizeSnowflakeRedisConfig 补齐 Redis 租约 node_id 分配默认值。
func normalizeSnowflakeRedisConfig(cfg config.SnowflakeRedisConfig) config.SnowflakeRedisConfig {
	if cfg.Scope == "" {
		cfg.Scope = defaultSnowflakeRedisScope
	}
	if cfg.LeaseSeconds == 0 {
		cfg.LeaseSeconds = defaultSnowflakeRedisLeaseSeconds
	}
	if cfg.RenewIntervalSeconds == 0 {
		cfg.RenewIntervalSeconds = defaultSnowflakeRedisRenewIntervalSeconds
	}
	cfg.Namespaces = normalizeSnowflakeRedisNamespaces(cfg.Namespaces)
	return cfg
}

// normalizeSnowflakeRedisNamespaces 规范化 namespace 级 node_id 池配置。
func normalizeSnowflakeRedisNamespaces(items map[string]config.SnowflakeRedisNamespaceConfig) map[string]config.SnowflakeRedisNamespaceConfig {
	if items == nil {
		return map[string]config.SnowflakeRedisNamespaceConfig{}
	}
	normalized := make(map[string]config.SnowflakeRedisNamespaceConfig, len(items))
	for namespace, item := range items {
		namespace = idgen.NormalizeNamespace(namespace)
		if namespace == "" {
			continue
		}
		normalized[namespace] = item
	}
	return normalized
}

// newSnowflakeRedisLeaseManager 创建按业务 namespace 分配 node_id 的 Redis 租约管理器。
func newSnowflakeRedisLeaseManager(ctx context.Context, cfg config.SnowflakeRedisConfig, client redis.UniversalClient) (*snowflakeRedisLeaseManager, error) {
	if client == nil {
		return nil, errors.New("snowflake.redis.enabled=true 时 Redis 客户端不能为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cfg = normalizeSnowflakeRedisConfig(cfg)
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, errors.Wrap(err, "检查 snowflake Redis 租约客户端失败")
	}
	manager := &snowflakeRedisLeaseManager{
		client: client,
		cfg:    cfg,
		owner:  snowflakeLeaseOwner(),
		leases: make(map[string]*snowflakeRedisLease),
	}
	manager.resolverToken = idgen.ConfigureWorkerResolver(manager)
	return manager, nil
}

// SnowflakeWorkerID 返回指定业务 namespace 当前实例持有的 worker_id。
func (m *snowflakeRedisLeaseManager) SnowflakeWorkerID(namespace string) (int64, error) {
	namespace = idgen.NormalizeNamespace(namespace)
	if namespace == "" {
		return 0, errors.New("雪花 ID namespace 不能为空")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, errors.Errorf("雪花 Redis 租约管理器已关闭 namespace=%s", namespace)
	}
	if lease, ok := m.leases[namespace]; ok {
		return lease.workerID, nil
	}
	lease, err := acquireSnowflakeRedisLease(context.Background(), m.cfg, m.client, m.owner, namespace, m.releaseNamespace)
	if err != nil {
		return 0, errors.Tag(err)
	}
	m.leases[namespace] = lease
	return lease.workerID, nil
}

// Ready 检查租约管理器状态和 Redis 连接。
func (m *snowflakeRedisLeaseManager) Ready(ctx context.Context) error {
	if m == nil {
		return errors.New("雪花 Redis 租约管理器未初始化")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return errors.New("雪花 Redis 租约管理器已关闭")
	}
	if err := m.client.Ping(ctx).Err(); err != nil {
		return errors.Wrap(err, "检查雪花 Redis 租约连接失败")
	}
	return nil
}

// Close 停止并释放当前实例持有的所有业务 namespace node_id 租约。
func (m *snowflakeRedisLeaseManager) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	leases := make([]*snowflakeRedisLease, 0, len(m.leases))
	for namespace, lease := range m.leases {
		leases = append(leases, lease)
		delete(m.leases, namespace)
	}
	token := m.resolverToken
	m.mu.Unlock()

	var closeErr error
	for _, lease := range leases {
		if err := lease.Close(ctx); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	idgen.ClearWorkerResolver(token)
	return errors.Tag(closeErr)
}

// releaseNamespace 清理单个业务 namespace 的本地 worker 状态。
func (m *snowflakeRedisLeaseManager) releaseNamespace(namespace string, workerID int64) {
	m.mu.Lock()
	if lease, ok := m.leases[namespace]; ok && lease.workerID == workerID {
		delete(m.leases, namespace)
	}
	m.mu.Unlock()
	idgen.ReleaseWorkerIDForNamespace(namespace, workerID)
	idgen.RecordSnowflakeLeaseEvent(namespace, "released")
}

// acquireSnowflakeRedisLease 从 Redis 指定业务 namespace node_id 池申请当前实例独占的 worker_id。
func acquireSnowflakeRedisLease(ctx context.Context, cfg config.SnowflakeRedisConfig, client redis.UniversalClient, owner string, namespace string, onRelease func(string, int64)) (*snowflakeRedisLease, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg = normalizeSnowflakeRedisConfig(cfg)
	ttl := time.Duration(cfg.LeaseSeconds) * time.Second
	nodeCount := snowflakeRedisNodeCount(cfg, namespace)
	startSeed := fmt.Sprintf("%s:%s:%s", owner, cfg.Scope, namespace)
	start := int64(crc32.ChecksumIEEE([]byte(startSeed)) % uint32(nodeCount))
	for offset := int64(0); offset < nodeCount; offset++ {
		workerID := (start + offset) % nodeCount
		key := keys.SnowflakeNodeLeaseKey(cfg.Scope, namespace, workerID)
		ok, err := client.SetNX(ctx, key, owner, ttl).Result()
		if err != nil {
			return nil, errors.Wrapf(err, "申请雪花 node_id 失败 namespace=%s node_id=%d", namespace, workerID)
		}
		if !ok {
			continue
		}
		lease, err := activateSnowflakeRedisLease(ctx, client, key, owner, namespace, workerID, cfg, onRelease)
		if err != nil {
			return nil, errors.Tag(err)
		}
		return lease, nil
	}
	return nil, errors.Errorf("雪花 node_id 池已耗尽 scope=%s namespace=%s range=0-%d", cfg.Scope, namespace, nodeCount-1)
}

// snowflakeRedisNodeCount 返回 namespace 可竞争的 node_id 池大小。
func snowflakeRedisNodeCount(cfg config.SnowflakeRedisConfig, namespace string) int64 {
	namespace = idgen.NormalizeNamespace(namespace)
	if item, ok := cfg.Namespaces[namespace]; ok && item.NodeCount > 0 {
		return int64(item.NodeCount)
	}
	return idgen.SnowflakeMaxWorkerID + 1
}

// activateSnowflakeRedisLease 发布单个业务 namespace 的 worker_id 并启动后台续约。
func activateSnowflakeRedisLease(ctx context.Context, client redis.UniversalClient, key string, owner string, namespace string, workerID int64, cfg config.SnowflakeRedisConfig, onRelease func(string, int64)) (*snowflakeRedisLease, error) {
	if err := idgen.ConfigureWorkerIDForNamespace(namespace, workerID); err != nil {
		_ = releaseSnowflakeRedisLease(ctx, client, key, owner)
		return nil, errors.Tag(err)
	}
	lease := &snowflakeRedisLease{
		client:        client,
		namespace:     namespace,
		key:           key,
		owner:         owner,
		workerID:      workerID,
		ttl:           time.Duration(cfg.LeaseSeconds) * time.Second,
		renewInterval: time.Duration(cfg.RenewIntervalSeconds) * time.Second,
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
		closeDone:     make(chan struct{}),
		onRelease:     onRelease,
	}
	lease.start()
	idgen.RecordSnowflakeLeaseEvent(namespace, "acquired")
	return lease, nil
}

// start 启动 node_id 租约续约循环。
func (l *snowflakeRedisLease) start() {
	go l.renewLoop()
}

// renewLoop 定期续约租约；确认租约丢失时立即停止本进程该业务发号。
func (l *snowflakeRedisLease) renewLoop() {
	defer close(l.done)
	ticker := time.NewTicker(l.renewInterval)
	defer ticker.Stop()
	lastRenewAt := time.Now()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			ok, err := l.renew(context.Background())
			if err != nil {
				idgen.RecordSnowflakeLeaseEvent(l.namespace, "renew_failed")
				logx.Errorf("雪花 node_id 租约续约失败: namespace=%s worker_id=%d key=%s err=%v", l.namespace, l.workerID, l.key, err)
				if time.Since(lastRenewAt) >= l.maxRenewSilence() {
					idgen.RecordSnowflakeLeaseEvent(l.namespace, "renew_timeout")
					l.releaseLocal()
					logx.Errorf("雪花 node_id 租约续约超时，已停止本进程该业务发号: namespace=%s worker_id=%d key=%s", l.namespace, l.workerID, l.key)
					return
				}
				continue
			}
			if !ok {
				idgen.RecordSnowflakeLeaseEvent(l.namespace, "lost")
				l.releaseLocal()
				logx.Errorf("雪花 node_id 租约已丢失，已停止本进程该业务发号: namespace=%s worker_id=%d key=%s", l.namespace, l.workerID, l.key)
				return
			}
			lastRenewAt = time.Now()
		}
	}
}

// maxRenewSilence 返回允许连续续约失败的最大时长，必须早于 Redis TTL 到期。
func (l *snowflakeRedisLease) maxRenewSilence() time.Duration {
	window := l.ttl - l.renewInterval
	if half := l.ttl / 2; window < half {
		window = half
	}
	if window < time.Second {
		return time.Second
	}
	return window
}

// renew 仅在 owner 匹配时延长当前 node_id 租约。
func (l *snowflakeRedisLease) renew(ctx context.Context) (bool, error) {
	result, err := snowflakeLeaseRenewScript.Run(ctx, l.client, []string{l.key}, l.owner, int(l.ttl/time.Second)).Int()
	if err != nil {
		return false, errors.Tag(err)
	}
	return result == 1, nil
}

// Close 停止续约并释放当前实例持有的单业务 node_id 租约。
func (l *snowflakeRedisLease) Close(ctx context.Context) error {
	if l == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	l.closeOnce.Do(func() {
		close(l.stop)
		l.releaseLocal()
		go func() {
			select {
			case <-l.done:
				l.closeErr = releaseSnowflakeRedisLease(ctx, l.client, l.key, l.owner)
			case <-ctx.Done():
				l.closeErr = errors.Wrap(ctx.Err(), "等待雪花租约续约协程退出超时")
			}
			close(l.closeDone)
		}()
	})
	select {
	case <-l.closeDone:
		return errors.Tag(l.closeErr)
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "关闭雪花 Redis 租约超时")
	}
}

// releaseLocal 释放本地单个业务 namespace 的 worker 状态。
func (l *snowflakeRedisLease) releaseLocal() {
	l.releaseOnce.Do(func() {
		if l.onRelease != nil {
			l.onRelease(l.namespace, l.workerID)
			return
		}
		idgen.ReleaseWorkerIDForNamespace(l.namespace, l.workerID)
		idgen.RecordSnowflakeLeaseEvent(l.namespace, "released")
	})
}

// releaseSnowflakeRedisLease 仅在 owner 匹配时删除租约 key。
func releaseSnowflakeRedisLease(ctx context.Context, client redis.UniversalClient, key string, owner string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := snowflakeLeaseReleaseScript.Run(ctx, client, []string{key}, owner).Int(); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// snowflakeLeaseOwner 生成可读且进程唯一的租约 owner。
func snowflakeLeaseOwner() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s:%d:%s", host, os.Getpid(), randomHex(snowflakeLeaseOwnerRandomBytes))
}

// randomHex 返回指定字节长度的随机十六进制字符串。
func randomHex(size int) string {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
