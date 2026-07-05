package types

import (
	"strings"

	"github.com/Is999/go-utils/errors"
)

const (
	// collectorStateMin 表示 Collector 最小任务状态码。
	collectorStateMin = 0
	// collectorStateMax 表示 Collector 最大任务状态码。
	collectorStateMax = 4
	// collectorRunMaxLimit 表示人工触发单轮失败重试的最大领取数量。
	collectorRunMaxLimit = 5000
	// collectorRetryMaxIDs 表示人工批量重试一次最多处理的失败事件数量。
	collectorRetryMaxIDs = 500
)

// ListCollectorFailuresReq 表示 Collector 失败事件列表查询请求。
type ListCollectorFailuresReq struct {
	GetPageReq // 复用分页参数

	BizType string `form:"bizType,optional"` // 业务类型过滤
	State   *int   `form:"state,optional"`   // 失败事件状态过滤：0/1/2/3/4
}

// Validate 校验并归一化 Collector 失败事件列表查询请求。
func (r *ListCollectorFailuresReq) Validate() error {
	r.BizType = strings.TrimSpace(r.BizType)
	if r.State != nil && (*r.State < collectorStateMin || *r.State > collectorStateMax) {
		return errors.Errorf("失败事件状态不合法")
	}
	return r.GetPageReq.Validate()
}

// CollectorOverviewReq 表示 Collector 概览查询请求。
type CollectorOverviewReq struct{}

// Validate 保持 Collector 概览请求与其它 go-zero 请求入口一致。
func (r *CollectorOverviewReq) Validate() error {
	return nil
}

// CollectorWindowStatResp 表示一个时间窗口内的处理统计。
type CollectorWindowStatResp struct {
	WindowMinutes int     `json:"windowMinutes"` // 统计窗口分钟数
	SuccessCount  int64   `json:"successCount"`  // 窗口内重试成功数量
	FailCount     int64   `json:"failCount"`     // 窗口内失败回写数量（retry/dead）
	AvgCostMs     float64 `json:"avgCostMs"`     // 窗口内平均处理耗时（毫秒）
	MaxCostMs     float64 `json:"maxCostMs"`     // 窗口内最大处理耗时（毫秒）
}

// CollectorBizTypeStatResp 表示按业务类型聚合的失败热点排行项。
type CollectorBizTypeStatResp struct {
	BizType            string  `json:"bizType"`            // 业务类型
	ReadyCount         int64   `json:"readyCount"`         // 当前已到重试时间的失败事件数量
	RunningCount       int64   `json:"runningCount"`       // 当前重试中数量
	RetryCount         int64   `json:"retryCount"`         // 当前待重试数量
	DeadCount          int64   `json:"deadCount"`          // 当前死信数量
	RecentSuccessCount int64   `json:"recentSuccessCount"` // 最近窗口重试成功数量
	RecentFailCount    int64   `json:"recentFailCount"`    // 最近窗口失败回写数量
	RecentAvgCostMs    float64 `json:"recentAvgCostMs"`    // 最近窗口平均处理耗时（毫秒）
	RecentMaxCostMs    float64 `json:"recentMaxCostMs"`    // 最近窗口最大处理耗时（毫秒）
}

// CollectorRuntimeTotalsResp 表示 Collector 运行态累计计数。
type CollectorRuntimeTotalsResp struct {
	Published     int64 `json:"published"`     // Kafka 投递成功事件数
	PublishFailed int64 `json:"publishFailed"` // Kafka 投递失败事件数
	Consumed      int64 `json:"consumed"`      // Kafka 已拉取事件数
	Invalid       int64 `json:"invalid"`       // Kafka 坏消息事件数
	Duplicate     int64 `json:"duplicate"`     // Collector 幂等去重跳过事件数
	Processed     int64 `json:"processed"`     // 进入 Processor 的事件数
	Succeeded     int64 `json:"succeeded"`     // Processor 成功事件数
	Failed        int64 `json:"failed"`        // Processor 失败事件数
	Batches       int64 `json:"batches"`       // Processor 批次数
	FailedBatches int64 `json:"failedBatches"` // 存在失败事件的 Processor 批次数
	Dead          int64 `json:"dead"`          // 进入死信的事件数
}

// CollectorRuntimeWindowResp 表示 Collector 运行态近窗口统计。
type CollectorRuntimeWindowResp struct {
	WindowMinutes              int     `json:"windowMinutes"` // 统计窗口分钟数
	CollectorRuntimeTotalsResp         // 窗口内累计计数
	AvgBatchSize               float64 `json:"avgBatchSize"` // 平均批次大小
	AvgCostMs                  float64 `json:"avgCostMs"`    // 平均 Processor 批处理耗时（毫秒）
	MaxCostMs                  float64 `json:"maxCostMs"`    // 最大 Processor 批处理耗时（毫秒）
	LastEventAt                string  `json:"lastEventAt"`  // 窗口内最后一次指标更新时间
}

// CollectorRuntimeBizTypeResp 表示单个 bizType 的运行态热点统计。
type CollectorRuntimeBizTypeResp struct {
	BizType                    string  `json:"bizType"` // 业务类型
	CollectorRuntimeTotalsResp         // 当前业务类型累计计数
	AvgBatchSize               float64 `json:"avgBatchSize"` // 平均批次大小
	AvgCostMs                  float64 `json:"avgCostMs"`    // 平均 Processor 批处理耗时（毫秒）
	MaxCostMs                  float64 `json:"maxCostMs"`    // 最大 Processor 批处理耗时（毫秒）
	LastEventAt                string  `json:"lastEventAt"`  // 最近一次指标更新时间
}

// CollectorRuntimeMetricsResp 表示 Collector 当前进程运行态指标快照。
type CollectorRuntimeMetricsResp struct {
	Enabled       bool                          `json:"enabled"`       // Collector 是否启用
	Scope         string                        `json:"scope"`         // 指标作用域
	StartedAt     string                        `json:"startedAt"`     // 统计器创建时间
	GeneratedAt   string                        `json:"generatedAt"`   // 快照生成时间
	Totals        CollectorRuntimeTotalsResp    `json:"totals"`        // 当前进程累计计数
	Recent1m      CollectorRuntimeWindowResp    `json:"recent1m"`      // 最近 1 分钟窗口
	Recent5m      CollectorRuntimeWindowResp    `json:"recent5m"`      // 最近 5 分钟窗口
	Recent15m     CollectorRuntimeWindowResp    `json:"recent15m"`     // 最近 15 分钟窗口
	BizTypeTop15m []CollectorRuntimeBizTypeResp `json:"bizTypeTop15m"` // 最近 15 分钟任务热点排行
}

// CollectorOverviewResp 表示 Collector 运维概览信息。
type CollectorOverviewResp struct {
	PendingCount          int64 `json:"pendingCount"`          // 待重试数量
	RunningCount          int64 `json:"runningCount"`          // 重试中数量
	DoneCount             int64 `json:"doneCount"`             // 重试成功数量
	RetryCount            int64 `json:"retryCount"`            // 待重试数量
	DeadCount             int64 `json:"deadCount"`             // 死信数量
	ReadyCount            int64 `json:"readyCount"`            // 当前已到重试时间的失败事件数量
	RunningTimeoutCount   int64 `json:"runningTimeoutCount"`   // 疑似超时重试中的数量
	OldestReadyAgeSeconds int64 `json:"oldestReadyAgeSeconds"` // 最早待重试事件已积压秒数

	KafkaBatchSize            int    `json:"kafkaBatchSize"`            // Kafka 批次配置
	FailureRunnerBatchSize    int    `json:"failureRunnerBatchSize"`    // 失败账本重试单批处理配置
	FailureRunnerIntervalSecs int    `json:"failureRunnerIntervalSecs"` // 失败账本重试轮询间隔秒数
	RunningLeaseSeconds       int    `json:"runningLeaseSeconds"`       // running 租约秒数
	MaxRetryTimes             int    `json:"maxRetryTimes"`             // 最大重试次数
	OverviewGeneratedAt       string `json:"overviewGeneratedAt"`       // 当前概览生成时间
	OverviewCacheTTLSeconds   int    `json:"overviewCacheTTLSeconds"`   // 当前概览缓存 TTL（秒）
	OverviewCacheAgeSeconds   int64  `json:"overviewCacheAgeSeconds"`   // 当前概览缓存已存在秒数
	OverviewCacheHit          bool   `json:"overviewCacheHit"`          // 当前响应是否命中缓存

	Recent1m  CollectorWindowStatResp `json:"recent1m"`  // 最近 1 分钟处理统计
	Recent5m  CollectorWindowStatResp `json:"recent5m"`  // 最近 5 分钟处理统计
	Recent15m CollectorWindowStatResp `json:"recent15m"` // 最近 15 分钟处理统计

	BizTypeTop15m []CollectorBizTypeStatResp `json:"bizTypeTop15m"` // 最近 15 分钟失败热点排行

	RuntimeMetrics CollectorRuntimeMetricsResp `json:"runtimeMetrics"` // 当前进程运行态指标
}

// CollectorFailureResp 表示 Collector 失败事件对外返回结构。
type CollectorFailureResp struct {
	ID           int64  `json:"id"`           // 主键
	EventID      string `json:"eventId"`      // 事件 ID
	BizType      string `json:"bizType"`      // 业务类型
	PartitionKey string `json:"partitionKey"` // 聚合分区键
	Payload      string `json:"payload"`      // 结构化事件负载
	State        int    `json:"state"`        // 失败事件状态
	Attempt      int    `json:"attempt"`      // 已失败重试次数
	NextRunAt    string `json:"nextRunAt"`    // 下次可执行时间
	StartedAt    string `json:"startedAt"`    // 开始执行时间
	FinishedAt   string `json:"finishedAt"`   // 结束时间
	LastError    string `json:"lastError"`    // 最近一次失败原因
	CreatedAt    string `json:"createdAt"`    // 创建时间
	UpdatedAt    string `json:"updatedAt"`    // 修改时间
}

// RunCollectorReq 表示手动触发执行 Collector 失败重试的请求。
type RunCollectorReq struct {
	Limit int `json:"limit,optional"` // 单次执行上限；为空时使用后端默认值
}

// Validate 校验手动触发 Collector 失败重试的请求。
func (r *RunCollectorReq) Validate() error {
	if r.Limit < 0 {
		return errors.Errorf("执行数量限制不合法")
	}
	if r.Limit > collectorRunMaxLimit {
		return errors.Errorf("执行数量不能超过%d", collectorRunMaxLimit)
	}
	return nil
}

// RunCollectorResp 表示手动触发执行结果。
type RunCollectorResp struct {
	Processed int    `json:"processed"`         // 本次成功处理数量
	Error     string `json:"error,omitempty"`   // 执行失败原因（前端展示用）
	Message   string `json:"message,omitempty"` // 附加说明（前端展示用）
}

// RetryCollectorFailuresReq 表示对指定失败事件做“手动重试/手动延迟重试”的请求。
type RetryCollectorFailuresReq struct {
	IDs          []int64 `json:"ids"`                   // 失败事件 ID 列表
	DelaySeconds int     `json:"delaySeconds,optional"` // 相对当前时间延迟执行，单位秒
	ResetAttempt *bool   `json:"resetAttempt,optional"` // 是否重置 attempt，未传时默认重置
}

// Validate 校验并归一化人工重试 Collector 失败事件的请求。
func (r *RetryCollectorFailuresReq) Validate() error {
	if len(r.IDs) == 0 {
		return errors.Errorf("ids不能为空")
	}
	if len(r.IDs) > collectorRetryMaxIDs {
		return errors.Errorf("ids一次最多处理%d个", collectorRetryMaxIDs)
	}
	if r.DelaySeconds < 0 {
		return errors.Errorf("延迟秒数不合法")
	}
	seen := make(map[int64]struct{}, len(r.IDs))
	ids := make([]int64, 0, len(r.IDs))
	for _, id := range r.IDs {
		if id <= 0 {
			return errors.Errorf("失败事件ID不合法")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	r.IDs = ids
	return nil
}

// RetryCollectorFailuresResp 表示手动重试响应。
type RetryCollectorFailuresResp struct {
	Updated int `json:"updated"` // 成功更新的失败事件数量
}
