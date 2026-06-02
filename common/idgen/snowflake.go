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
	// ShardMod 表示用户 ID 哈希分片数量。
	ShardMod = 1000
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
	configuredWorkerID  atomic.Int64 // configuredWorkerID 保存启动期解析通过的 worker_id，全局唯一由部署分配保证
	snowflakeFormatOnce sync.Once    // snowflakeFormatOnce 确保 bwmarrin/snowflake 位宽只初始化一次
	workerGenerators    sync.Map     // workerGenerators 按 worker_id 缓存进程内 ID 生成器，避免不同命名空间独立序列冲突
)

func init() {
	configuredWorkerID.Store(SnowflakeWorkerIDUnset)
}

// Snowflake 封装成熟 bwmarrin/snowflake 节点，生成本进程内单调递增的雪花 ID。
type Snowflake struct {
	workerID int64             // workerID 是部署分配的分布式实例编号，必须跨实例唯一。
	node     *bwsnowflake.Node // node 内部通过互斥锁保护同一 worker 的并发序列。
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

// ConfigureWorkerID 设置当前进程使用的雪花 worker_id。
func ConfigureWorkerID(workerID int64) error {
	if err := ValidateWorkerID(workerID); err != nil {
		return errors.Tag(err)
	}
	generator, err := NewSnowflake(workerID)
	if err != nil {
		return errors.Tag(err)
	}
	previous := configuredWorkerID.Swap(workerID)
	if previous != workerID {
		clearWorkerGenerators()
	}
	workerGenerators.Store(workerID, generator)
	return nil
}

// CurrentWorkerID 返回当前进程已配置的雪花 worker_id。
func CurrentWorkerID() (int64, bool) {
	workerID := configuredWorkerID.Load()
	return workerID, workerID != SnowflakeWorkerIDUnset
}

// NextID 返回当前进程的下一个雪花 ID；namespace 仅保留调用方业务语义，不参与 ID 位段。
func NextID(namespace string) (int64, error) {
	workerID, err := activeWorkerID()
	if err != nil {
		return 0, errors.Tag(err)
	}
	if value, ok := workerGenerators.Load(workerID); ok {
		return value.(*Snowflake).NextID()
	}
	generator, err := NewSnowflake(workerID)
	if err != nil {
		return 0, errors.Tag(err)
	}
	actual, _ := workerGenerators.LoadOrStore(workerID, generator)
	return actual.(*Snowflake).NextID()
}

// activeWorkerID 返回当前可用于生成 ID 的显式 worker_id。
func activeWorkerID() (int64, error) {
	if workerID, ok := CurrentWorkerID(); ok {
		return workerID, nil
	}
	workerID, err := ResolveWorkerID(SnowflakeWorkerIDUnset)
	if err != nil {
		return 0, errors.Tag(err)
	}
	if err := ConfigureWorkerID(workerID); err != nil {
		return 0, errors.Tag(err)
	}
	return workerID, nil
}

// NextID 返回当前生成器的下一个雪花 ID。
func (g *Snowflake) NextID() (int64, error) {
	if g == nil || g.node == nil {
		return 0, errors.New("雪花 ID 生成器未初始化")
	}
	return g.node.Generate().Int64(), nil
}

// ShardNo 返回用户 ID 稳定哈希后的 0-999 分片号。
func ShardNo(id int64) int {
	checksum := crc32.ChecksumIEEE([]byte(strconv.FormatInt(id, 10)))
	return int(checksum % ShardMod)
}

// clearWorkerGenerators 清空进程内 worker 生成器缓存。
func clearWorkerGenerators() {
	workerGenerators.Range(func(key, _ any) bool {
		workerGenerators.Delete(key)
		return true
	})
}

// resetWorkerIDForTest 重置进程级 worker_id，避免测试之间相互影响。
func resetWorkerIDForTest() {
	configuredWorkerID.Store(SnowflakeWorkerIDUnset)
	clearWorkerGenerators()
}
