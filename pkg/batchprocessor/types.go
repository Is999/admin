package batchprocessor

import (
	"context"
	"time"

	"github.com/Is999/go-utils/errors"
)

const (
	// maxPolicyBatchSize 表示单次收集/处理批量的安全上限，避免误配置造成内存和下游写入尖峰。
	maxPolicyBatchSize = 5000
	// maxPolicyBufferSize 表示单个 bizType 内存缓冲安全上限。
	maxPolicyBufferSize = 50000
	// maxPolicyProcessConcurrency 表示单个 bizType 后台处理并发安全上限。
	maxPolicyProcessConcurrency = 32
	// minPolicyInterval 表示后台周期调度允许的最小间隔，避免纳秒级配置导致空转。
	minPolicyInterval = 10 * time.Millisecond
)

// Data 表示一次“业务数据收集”的输入单元。
// 该结构只描述“收集器需要什么”，不约束“如何落地/如何处理”，由各业务 Module 决定。
type Data struct {
	BizType string // 业务类型：必须与注册时 Register(bizType, ...) 的 bizType 一致

	Action string // 动作标识：用于 allowlist/denylist 控制，避免误写入危险操作

	IdempotencyKey string // 幂等键：用于业务侧实现幂等；若 policy.RequireIdempotencyKey=true，则必须提供

	Payload []byte // 业务负载：通用字节数组，业务自行定义序列化格式（JSON/Protobuf 等）

	Required bool // 是否必达：必达任务会在 flush 失败时触发 RequiredFallback，并把结果回传给调用方

	CreatedAt time.Time // 创建时间：未指定时由 Registry.Collect 自动填充

	NotBefore time.Time // 最早可处理时间：用于业务侧实现“延迟触发”，收集器本身不强制解释

	AdditionalMeta map[string]string // 扩展元数据：用于透传少量非核心字段，避免侵入业务 payload
}

// RetryPolicy 定义重试策略。
// 该策略用于业务执行失败后计算下一次可执行时间；具体落库/状态机由业务模块自行管理。
type RetryPolicy struct {
	MaxRetryTimes int             // 最大重试次数：达到次数后视为死信
	Backoffs      []time.Duration // 指数级/阶梯退避间隔列表，如 [1m,5m,10m]
}

// NextDelay 根据当前 attempt 计算下一次延迟，并返回是否应进入死信。
// - attempt 从 1 开始计数
// - dead=true 表示已达到最大重试次数，调用方应将任务标记为死信
func (p RetryPolicy) NextDelay(attempt int) (time.Duration, bool) {
	if attempt <= 0 {
		return 0, false
	}
	if p.MaxRetryTimes <= 0 {
		return 0, true
	}
	if attempt >= p.MaxRetryTimes {
		return 0, true
	}
	if len(p.Backoffs) == 0 {
		return 0, false
	}
	if attempt-1 >= len(p.Backoffs) {
		return p.Backoffs[len(p.Backoffs)-1], false
	}
	return p.Backoffs[attempt-1], false
}

// Policy 定义单个 bizType 的收集与处理策略。
// 该结构的默认值会在 Register 时被 normalize() 补齐，避免遗漏导致运行时行为不可控。
type Policy struct {
	BatchSize     int           // 收集批次大小：达到该数量立即 flush
	FlushInterval time.Duration // 收集时间阈值：到达该间隔会触发一次 flush（有 jitter）
	FlushJitter   time.Duration // 收集周期抖动：用于打散多个 bizType 同步 flush 的尖峰

	MaxBufferSize         int           // 单个 bizType 最大内存缓冲数量：用于限制下游异常时的内存占用
	BufferFullWaitTimeout time.Duration // 缓冲满后的最大等待时间：为 0 时快速失败，由调用方决定降级或重试

	ProcessEnabled     bool          // 是否启用后台处理调度
	ProcessInterval    time.Duration // 处理周期：到达该间隔会触发一次处理（有 jitter）
	ProcessJitter      time.Duration // 处理周期抖动：用于打散多个 bizType 同步执行的尖峰
	ProcessBatchSize   int           // 单次处理最大数量：传递给 Module.Process
	ProcessConcurrency int           // 业务并发度：同一 bizType 允许并行跑多少个 Process

	RetryPolicy RetryPolicy // 重试策略：用于业务模块计算 next_run_at 等字段

	AllowActions []string // 动作白名单：非空时仅允许列表内 Action
	DenyActions  []string // 动作黑名单：命中直接拒绝

	RequireIdempotencyKey bool // 是否要求幂等键必填
}

// normalize 为策略补默认值，避免运行期出现 0 值导致的异常行为。
func (p *Policy) normalize() {
	if p.BatchSize <= 0 {
		p.BatchSize = 200
	} else if p.BatchSize > maxPolicyBatchSize {
		p.BatchSize = maxPolicyBatchSize
	}
	if p.FlushInterval <= 0 {
		p.FlushInterval = 1 * time.Second
	} else if p.FlushInterval < minPolicyInterval {
		p.FlushInterval = minPolicyInterval
	}
	if p.FlushJitter < 0 {
		p.FlushJitter = 0
	}
	if p.MaxBufferSize <= 0 {
		p.MaxBufferSize = p.BatchSize * 10
	} else if p.MaxBufferSize > maxPolicyBufferSize {
		p.MaxBufferSize = maxPolicyBufferSize
	}
	if p.MaxBufferSize < p.BatchSize {
		p.MaxBufferSize = p.BatchSize
	}
	if p.BufferFullWaitTimeout < 0 {
		p.BufferFullWaitTimeout = 0
	}
	if p.ProcessBatchSize <= 0 {
		p.ProcessBatchSize = 200
	} else if p.ProcessBatchSize > maxPolicyBatchSize {
		p.ProcessBatchSize = maxPolicyBatchSize
	}
	if p.ProcessInterval <= 0 {
		p.ProcessInterval = 2 * time.Second
	} else if p.ProcessInterval < minPolicyInterval {
		p.ProcessInterval = minPolicyInterval
	}
	if p.ProcessJitter < 0 {
		p.ProcessJitter = 0
	}
	if p.ProcessConcurrency <= 0 {
		p.ProcessConcurrency = 1
	} else if p.ProcessConcurrency > maxPolicyProcessConcurrency {
		p.ProcessConcurrency = maxPolicyProcessConcurrency
	}
	if p.RetryPolicy.MaxRetryTimes <= 0 {
		p.RetryPolicy.MaxRetryTimes = 3
	}
	if len(p.RetryPolicy.Backoffs) == 0 {
		p.RetryPolicy.Backoffs = []time.Duration{1 * time.Minute, 5 * time.Minute, 10 * time.Minute}
	}
}

// Module 定义业务插件接口。
// 该接口由业务实现，用于把“收集器”与“业务落地/处理逻辑”彻底隔离：
// - Validate：校验数据合法性（例如必填字段、动作白名单、payload 结构等）
// - Flush：批量落地（写表/写库/写 MQ/写文件等）
// - RequiredFallback：必达数据在 flush 失败时的兜底落地逻辑（可同步落库/降级到另一链路）
// - Process：批量处理入口（读取已落地数据并处理，状态机/重试/死信由业务自行管理）
type Module interface {
	Validate(ctx context.Context, data Data) error
	Flush(ctx context.Context, batch []Data) error
	RequiredFallback(ctx context.Context, data Data, flushErr error) error
	Process(ctx context.Context, bizType string, limit int) (int, error)
}

// validateRegisterParams 校验注册参数，避免运行期 nil 指针或空业务类型导致路由与权限无法收口。
func validateRegisterParams(bizType string, module Module) error {
	if bizType == "" {
		return errors.Errorf("batchprocessor.Register bizType 为空")
	}
	if module == nil {
		return errors.Errorf("batchprocessor.Register module 为空")
	}
	return nil
}
