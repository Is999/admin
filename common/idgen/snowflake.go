package idgen

import (
	"hash/crc32"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Is999/go-utils/errors"
	bwsnowflake "github.com/bwmarrin/snowflake"
)

const (
	// ShardMod 表示用户 ID 哈希分片数量，固定为 2 的幂便于平滑拆表。
	ShardMod = 1024
	// SnowflakeWorkerIDUnset 表示未显式配置雪花 worker_id。
	SnowflakeWorkerIDUnset int64 = -1

	snowflakeEpochMillis  int64 = 1704067200000                     // snowflakeEpochMillis 表示业务雪花纪元毫秒，缩短时间戳位宽并保持 ID 可排序。
	snowflakeWorkerBits         = 10                                // snowflakeWorkerBits 表示 worker_id 位数，支持 0-1023 个分布式实例编号。
	snowflakeSequenceBits       = 12                                // snowflakeSequenceBits 表示同一毫秒内的序列位数，单实例每毫秒最多 4096 个 ID。
	snowflakeMaxWorkerID        = int64(1<<snowflakeWorkerBits - 1) // snowflakeMaxWorkerID 表示 worker_id 可配置上限。
	// SnowflakeMaxWorkerID 表示雪花 worker_id 最大值。
	SnowflakeMaxWorkerID = snowflakeMaxWorkerID
	snowflakeWorkerIDEnv = "SNOWFLAKE_WORKER_ID" // snowflakeWorkerIDEnv 表示未配置 snowflake.worker_id 时读取的环境变量名。
)

var (
	staticWorkerID       atomic.Int64          // staticWorkerID 保存静态 worker_id，主要用于手工强管控部署
	workerResolverSeq    atomic.Uint64         // workerResolverSeq 生成动态 worker_id 解析器绑定 token
	workerResolverMu     sync.RWMutex          // workerResolverMu 保护当前动态 worker_id 解析器
	activeWorkerResolver workerResolverBinding // activeWorkerResolver 保存当前动态 worker_id 解析器
	snowflakeFormatOnce  sync.Once             // snowflakeFormatOnce 确保 bwmarrin/snowflake 位宽只初始化一次
	namespaceWorkers     sync.Map              // namespaceWorkers 按业务命名空间缓存当前进程持有的 worker_id
	workerGenerators     sync.Map              // workerGenerators 按业务命名空间和 worker_id 缓存进程内 ID 生成器
)

func init() {
	staticWorkerID.Store(SnowflakeWorkerIDUnset)
}

// WorkerResolver 按业务命名空间分配雪花 worker_id。
type WorkerResolver interface {
	SnowflakeWorkerID(namespace string) (int64, error)
}

// WorkerResolverFunc 适配函数式 worker_id 解析器。
type WorkerResolverFunc func(namespace string) (int64, error)

// SnowflakeWorkerID 返回指定业务命名空间当前可用的 worker_id。
func (f WorkerResolverFunc) SnowflakeWorkerID(namespace string) (int64, error) {
	if f == nil {
		return 0, errors.New("雪花 worker_id 解析器未初始化")
	}
	return f(namespace)
}

// Snowflake 封装成熟 bwmarrin/snowflake 节点，生成本进程内单调递增的雪花 ID。
type Snowflake struct {
	workerID int64             // workerID 是部署分配的分布式实例编号，必须跨实例唯一。
	node     *bwsnowflake.Node // node 内部通过互斥锁保护同一 worker 的并发序列。
}

// snowflakeGeneratorKey 标识单个业务命名空间内的本地生成器。
type snowflakeGeneratorKey struct {
	namespace string // namespace 是调用方业务唯一域，如 user、recharge.order。
	workerID  int64  // workerID 是该业务命名空间下当前实例持有的 node_id。
}

// workerResolverBinding 保存当前生效的动态 worker_id 解析器。
type workerResolverBinding struct {
	resolver WorkerResolver // resolver 负责按 namespace 懒分配 worker_id。
	token    uint64         // token 用于关闭旧解析器时避免误清新解析器。
}

// NewSnowflake 创建指定 worker 的 bwmarrin/snowflake ID 生成器。
func NewSnowflake(workerID int64) (*Snowflake, error) {
	if err := ValidateWorkerID(workerID); err != nil {
		return nil, errors.Tag(err)
	}
	configureSnowflakeFormat()
	node, err := bwsnowflake.NewNode(workerID)
	if err != nil {
		return nil, errors.Wrapf(err, "创建雪花 ID 节点失败 worker_id=%d", workerID)
	}
	// 等待至少 1ms，降低同一 worker_id 进程极速重启时复用上一进程最后毫秒窗口的风险。
	time.Sleep(time.Millisecond)
	return &Snowflake{workerID: workerID, node: node}, nil
}

// configureSnowflakeFormat 固定成熟库位宽，确保 worker_id 和测试解析规则一致。
func configureSnowflakeFormat() {
	snowflakeFormatOnce.Do(func() {
		bwsnowflake.Epoch = snowflakeEpochMillis
		bwsnowflake.NodeBits = uint8(snowflakeWorkerBits)
		bwsnowflake.StepBits = uint8(snowflakeSequenceBits)
	})
}

// ValidateWorkerID 校验雪花 worker_id 是否落在 10 bit 可表达范围内。
func ValidateWorkerID(workerID int64) error {
	if workerID < 0 || workerID > snowflakeMaxWorkerID {
		return errors.Errorf("雪花 worker_id 必须在 0-%d 之间", snowflakeMaxWorkerID)
	}
	return nil
}

// ResolveWorkerID 解析启动配置或环境变量中的雪花 worker_id。
func ResolveWorkerID(configWorkerID int64) (int64, error) {
	if configWorkerID != SnowflakeWorkerIDUnset {
		if err := ValidateWorkerID(configWorkerID); err != nil {
			return 0, errors.Tag(err)
		}
		return configWorkerID, nil
	}
	raw := strings.TrimSpace(os.Getenv(snowflakeWorkerIDEnv))
	if raw == "" {
		return 0, errors.Errorf("雪花 worker_id 未配置，请设置 snowflake.worker_id 或环境变量 %s", snowflakeWorkerIDEnv)
	}
	workerID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, errors.Wrapf(err, "解析环境变量 %s 失败", snowflakeWorkerIDEnv)
	}
	if err := ValidateWorkerID(workerID); err != nil {
		return 0, errors.Tag(err)
	}
	return workerID, nil
}

// ConfigureWorkerID 设置当前进程所有业务命名空间共享的静态 worker_id。
func ConfigureWorkerID(workerID int64) error {
	if err := ValidateWorkerID(workerID); err != nil {
		return errors.Tag(err)
	}
	workerResolverMu.Lock()
	activeWorkerResolver = workerResolverBinding{}
	workerResolverMu.Unlock()
	staticWorkerID.Store(workerID)
	clearNamespaceWorkers()
	clearWorkerGenerators()
	return nil
}

// ConfigureWorkerResolver 注册按业务命名空间动态分配 worker_id 的解析器。
func ConfigureWorkerResolver(resolver WorkerResolver) uint64 {
	if resolver == nil {
		ClearWorkerResolver(0)
		return 0
	}
	token := workerResolverSeq.Add(1)
	workerResolverMu.Lock()
	activeWorkerResolver = workerResolverBinding{resolver: resolver, token: token}
	workerResolverMu.Unlock()
	staticWorkerID.Store(SnowflakeWorkerIDUnset)
	clearNamespaceWorkers()
	clearWorkerGenerators()
	return token
}

// ClearWorkerResolver 清理指定 token 对应的动态 worker_id 解析器。
func ClearWorkerResolver(token uint64) {
	workerResolverMu.Lock()
	if token == 0 || activeWorkerResolver.token == token {
		activeWorkerResolver = workerResolverBinding{}
		staticWorkerID.Store(SnowflakeWorkerIDUnset)
		clearNamespaceWorkers()
		clearWorkerGenerators()
	}
	workerResolverMu.Unlock()
}

// ConfigureWorkerIDForNamespace 记录单个业务命名空间当前持有的 worker_id。
func ConfigureWorkerIDForNamespace(namespace string, workerID int64) error {
	namespace = NormalizeNamespace(namespace)
	if namespace == "" {
		return errors.New("雪花 ID namespace 不能为空")
	}
	if err := ValidateWorkerID(workerID); err != nil {
		return errors.Tag(err)
	}
	if current, ok := namespaceWorkerID(namespace); ok && current != workerID {
		clearWorkerGeneratorsForNamespace(namespace)
	}
	namespaceWorkers.Store(namespace, workerID)
	return nil
}

// ReleaseWorkerID 释放当前进程持有的 worker_id，避免租约丢失后继续生成冲突 ID。
func ReleaseWorkerID(workerID int64) {
	if staticWorkerID.CompareAndSwap(workerID, SnowflakeWorkerIDUnset) {
		clearNamespaceWorkersForWorker(workerID)
		clearWorkerGeneratorsForWorker(workerID)
		return
	}
	clearNamespaceWorkersForWorker(workerID)
	clearWorkerGeneratorsForWorker(workerID)
}

// ReleaseWorkerIDForNamespace 释放单个业务命名空间持有的 worker_id。
func ReleaseWorkerIDForNamespace(namespace string, workerID int64) {
	namespace = NormalizeNamespace(namespace)
	if namespace == "" {
		return
	}
	if current, ok := namespaceWorkerID(namespace); ok && current == workerID {
		namespaceWorkers.Delete(namespace)
	}
	workerGenerators.Delete(snowflakeGeneratorKey{namespace: namespace, workerID: workerID})
}

// CurrentWorkerID 返回当前进程指定业务命名空间已配置的 worker_id。
func CurrentWorkerID(namespaces ...string) (int64, bool) {
	if len(namespaces) > 0 {
		namespace := NormalizeNamespace(namespaces[0])
		if namespace == "" {
			return 0, false
		}
		if workerID, ok := namespaceWorkerID(namespace); ok {
			return workerID, true
		}
		workerID := staticWorkerID.Load()
		return workerID, workerID != SnowflakeWorkerIDUnset
	}
	if workerID := staticWorkerID.Load(); workerID != SnowflakeWorkerIDUnset {
		return workerID, true
	}
	var (
		found int64
		count int
	)
	namespaceWorkers.Range(func(_, value any) bool {
		found = value.(int64)
		count++
		return count < 2
	})
	return found, count == 1
}

// NormalizeNamespace 规范化调用方业务命名空间。
func NormalizeNamespace(namespace string) string {
	return strings.TrimSpace(namespace)
}

// NextID 返回指定业务命名空间的下一个雪花 ID。
func NextID(namespace string) (int64, error) {
	namespace = NormalizeNamespace(namespace)
	if namespace == "" {
		return 0, errors.New("雪花 ID namespace 不能为空")
	}
	if resolver, token := currentSegmentResolver(); resolver != nil && resolver.SegmentEnabled(namespace) {
		start := time.Now()
		id, err := nextSegmentID(namespace, resolver, token)
		recordIDGenerate(namespace, IDStrategySegment, err, time.Since(start))
		if err != nil {
			return 0, errors.Tag(err)
		}
		return id, nil
	}
	start := time.Now()
	id, err := nextSnowflakeID(namespace)
	recordIDGenerate(namespace, IDStrategySnowflake, err, time.Since(start))
	if err != nil {
		return 0, errors.Tag(err)
	}
	return id, nil
}

// nextSnowflakeID 返回指定业务命名空间的下一个雪花 ID。
func nextSnowflakeID(namespace string) (int64, error) {
	workerID, err := activeWorkerID(namespace)
	if err != nil {
		return 0, errors.Tag(err)
	}
	key := snowflakeGeneratorKey{namespace: namespace, workerID: workerID}
	if value, ok := workerGenerators.Load(key); ok {
		return value.(*Snowflake).NextID()
	}
	generator, err := NewSnowflake(workerID)
	if err != nil {
		return 0, errors.Tag(err)
	}
	actual, _ := workerGenerators.LoadOrStore(key, generator)
	return actual.(*Snowflake).NextID()
}

// activeWorkerID 返回当前 namespace 可用于生成 ID 的显式 worker_id。
func activeWorkerID(namespace string) (int64, error) {
	if workerID, ok := namespaceWorkerID(namespace); ok {
		return workerID, nil
	}
	if resolver, token := currentWorkerResolver(); resolver != nil {
		workerID, err := resolver.SnowflakeWorkerID(namespace)
		if err != nil {
			return 0, errors.Tag(err)
		}
		if !workerResolverActive(token) {
			return 0, errors.Errorf("雪花 worker_id 解析器已关闭 namespace=%s", namespace)
		}
		if err = ConfigureWorkerIDForNamespace(namespace, workerID); err != nil {
			return 0, errors.Tag(err)
		}
		return workerID, nil
	}
	if workerID := staticWorkerID.Load(); workerID != SnowflakeWorkerIDUnset {
		if err := ConfigureWorkerIDForNamespace(namespace, workerID); err != nil {
			return 0, errors.Tag(err)
		}
		return workerID, nil
	}
	return 0, errors.Errorf("雪花 worker_id 未配置或 Redis node_id 租约已释放 namespace=%s", namespace)
}

// NextID 返回当前生成器的下一个雪花 ID。
func (g *Snowflake) NextID() (int64, error) {
	if g == nil || g.node == nil {
		return 0, errors.New("雪花 ID 生成器未初始化")
	}
	return g.node.Generate().Int64(), nil
}

// ShardNo 返回用户 ID 稳定哈希后的 0-1023 分片号。
func ShardNo(id int64) int {
	checksum := crc32.ChecksumIEEE([]byte(strconv.FormatInt(id, 10)))
	return int(checksum % ShardMod)
}

// currentWorkerResolver 返回当前动态 worker_id 解析器和绑定 token。
func currentWorkerResolver() (WorkerResolver, uint64) {
	workerResolverMu.RLock()
	defer workerResolverMu.RUnlock()
	return activeWorkerResolver.resolver, activeWorkerResolver.token
}

// workerResolverActive 判断指定 token 的动态解析器是否仍然有效。
func workerResolverActive(token uint64) bool {
	workerResolverMu.RLock()
	defer workerResolverMu.RUnlock()
	return token != 0 && activeWorkerResolver.token == token && activeWorkerResolver.resolver != nil
}

// namespaceWorkerID 返回业务命名空间当前缓存的 worker_id。
func namespaceWorkerID(namespace string) (int64, bool) {
	value, ok := namespaceWorkers.Load(namespace)
	if !ok {
		return 0, false
	}
	return value.(int64), true
}

// clearNamespaceWorkers 清空业务命名空间到 worker_id 的映射。
func clearNamespaceWorkers() {
	namespaceWorkers.Range(func(key, _ any) bool {
		namespaceWorkers.Delete(key)
		return true
	})
}

// clearNamespaceWorkersForWorker 清空指定 worker_id 关联的业务命名空间映射。
func clearNamespaceWorkersForWorker(workerID int64) {
	namespaceWorkers.Range(func(key, value any) bool {
		if value.(int64) == workerID {
			namespaceWorkers.Delete(key)
		}
		return true
	})
}

// clearWorkerGenerators 清空进程内 worker 生成器缓存。
func clearWorkerGenerators() {
	workerGenerators.Range(func(key, _ any) bool {
		workerGenerators.Delete(key)
		return true
	})
}

// clearWorkerGeneratorsForNamespace 清空指定业务命名空间的本地生成器。
func clearWorkerGeneratorsForNamespace(namespace string) {
	workerGenerators.Range(func(key, _ any) bool {
		if item, ok := key.(snowflakeGeneratorKey); ok && item.namespace == namespace {
			workerGenerators.Delete(key)
		}
		return true
	})
}

// clearWorkerGeneratorsForWorker 清空指定 worker_id 关联的本地生成器。
func clearWorkerGeneratorsForWorker(workerID int64) {
	workerGenerators.Range(func(key, _ any) bool {
		if item, ok := key.(snowflakeGeneratorKey); ok && item.workerID == workerID {
			workerGenerators.Delete(key)
		}
		return true
	})
}

// resetWorkerIDForTest 重置进程级 worker_id，避免测试之间相互影响。
func resetWorkerIDForTest() {
	ClearWorkerResolver(0)
	ClearSegmentResolver(0)
	staticWorkerID.Store(SnowflakeWorkerIDUnset)
	clearNamespaceWorkers()
	clearWorkerGenerators()
}
