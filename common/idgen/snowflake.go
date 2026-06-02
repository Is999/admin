package idgen

import (
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Is999/go-utils/errors"
)

const (
	// ShardMod 表示用户取模分片固定使用的上限。
	ShardMod = 1000

	snowflakeEpochMillis  int64 = 1704067200000
	snowflakeWorkerBits         = 10
	snowflakeSequenceBits       = 12
	snowflakeMaxWorkerID        = int64(1<<snowflakeWorkerBits - 1)
	snowflakeSequenceMask       = int64(1<<snowflakeSequenceBits - 1)
	snowflakeWorkerIDEnv        = "SNOWFLAKE_WORKER_ID"
)

var namespaceGenerators sync.Map // namespaceGenerators 缓存不同业务命名空间的 ID 生成器

// Snowflake 生成本进程内单调递增的雪花 ID。
type Snowflake struct {
	mu         sync.Mutex
	workerID   int64
	lastMillis int64
	sequence   int64
	nowMillis  func() int64
}

// NewSnowflake 创建指定 worker 的雪花 ID 生成器。
func NewSnowflake(workerID int64) (*Snowflake, error) {
	if workerID < 0 || workerID > snowflakeMaxWorkerID {
		return nil, errors.Errorf("雪花 worker_id 必须在 0-%d 之间", snowflakeMaxWorkerID)
	}
	return &Snowflake{
		workerID:  workerID,
		nowMillis: func() int64 { return time.Now().UnixMilli() },
	}, nil
}

// NextID 返回指定业务命名空间下的下一个雪花 ID。
func NextID(namespace string) (int64, error) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = "default"
	}
	if value, ok := namespaceGenerators.Load(namespace); ok {
		return value.(*Snowflake).NextID()
	}
	generator, err := NewSnowflake(AutoWorkerID(namespace))
	if err != nil {
		return 0, errors.Tag(err)
	}
	actual, _ := namespaceGenerators.LoadOrStore(namespace, generator)
	return actual.(*Snowflake).NextID()
}

// NextID 返回当前生成器的下一个雪花 ID。
func (g *Snowflake) NextID() (int64, error) {
	if g == nil {
		return 0, errors.New("雪花 ID 生成器未初始化")
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.nowMillis()
	if now < g.lastMillis {
		return 0, errors.Errorf("系统时钟回拨，无法生成雪花ID last=%d now=%d", g.lastMillis, now)
	}
	if now == g.lastMillis {
		g.sequence = (g.sequence + 1) & snowflakeSequenceMask
		if g.sequence == 0 {
			now = g.waitNextMillis(now)
		}
	} else {
		g.sequence = 0
	}
	g.lastMillis = now
	return ((now - snowflakeEpochMillis) << (snowflakeWorkerBits + snowflakeSequenceBits)) |
		(g.workerID << snowflakeSequenceBits) |
		g.sequence, nil
}

// AutoWorkerID 根据环境变量或进程特征生成稳定 worker ID。
func AutoWorkerID(namespace string) int64 {
	if raw := strings.TrimSpace(os.Getenv(snowflakeWorkerIDEnv)); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil && value >= 0 && value <= snowflakeMaxWorkerID {
			return value
		}
	}
	host, _ := os.Hostname()
	h := fnv.New32a()
	_, _ = h.Write([]byte(host))
	_, _ = h.Write([]byte{':'})
	_, _ = h.Write([]byte(strconv.Itoa(os.Getpid())))
	_, _ = h.Write([]byte{':'})
	_, _ = h.Write([]byte(strings.TrimSpace(namespace)))
	return int64(h.Sum32()) & snowflakeMaxWorkerID
}

// ShardNo 返回雪花 ID 对 1000 取模后的分片号。
func ShardNo(id int64) int {
	shard := int(id % ShardMod)
	if shard < 0 {
		shard += ShardMod
	}
	return shard
}

// waitNextMillis 自旋等待进入下一毫秒。
func (g *Snowflake) waitNextMillis(current int64) int64 {
	next := g.nowMillis()
	for next <= current {
		time.Sleep(time.Millisecond)
		next = g.nowMillis()
	}
	return next
}
