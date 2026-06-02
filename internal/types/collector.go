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
	// collectorRunMaxLimit 表示人工触发单轮执行的最大领取数量。
	collectorRunMaxLimit = 5000
	// collectorRetryMaxIDs 表示人工批量重试一次最多处理的任务数量。
	collectorRetryMaxIDs = 500
)

// ListCollectorTasksReq 表示 Collector 任务列表查询请求。
type ListCollectorTasksReq struct {
	GetPageReq // 复用分页参数

	BizType   string `form:"bizType,optional"`   // 业务类型过滤
	Transport string `form:"transport,optional"` // 传输通道过滤
	State     *int   `form:"state,optional"`     // 任务状态过滤：0/1/2/3/4
}

// Validate 校验并归一化 Collector 任务列表查询请求。
func (r *ListCollectorTasksReq) Validate() error {
	r.BizType = strings.TrimSpace(r.BizType)
	r.Transport = strings.ToLower(strings.TrimSpace(r.Transport))
	if r.State != nil && (*r.State < collectorStateMin || *r.State > collectorStateMax) {
		return errors.Errorf("任务状态不合法")
	}
	if r.Transport != "" && !isCollectorTransport(r.Transport) {
		return errors.Errorf("传输通道不合法")
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
	SuccessCount  int64   `json:"successCount"`  // 窗口内成功完成数量
	FailCount     int64   `json:"failCount"`     // 窗口内失败回写数量（retry/dead）
	AvgCostMs     float64 `json:"avgCostMs"`     // 窗口内平均处理耗时（毫秒）
	MaxCostMs     float64 `json:"maxCostMs"`     // 窗口内最大处理耗时（毫秒）
}

// CollectorBizTypeStatResp 表示按业务类型聚合的性能排行项。
type CollectorBizTypeStatResp struct {
	BizType            string  `json:"bizType"`            // 业务类型
	ReadyCount         int64   `json:"readyCount"`         // 当前已到执行时间的待处理数量
	RunningCount       int64   `json:"runningCount"`       // 当前执行中数量
	RetryCount         int64   `json:"retryCount"`         // 当前待重试数量
	DeadCount          int64   `json:"deadCount"`          // 当前死信数量
	RecentSuccessCount int64   `json:"recentSuccessCount"` // 最近窗口成功完成数量
	RecentFailCount    int64   `json:"recentFailCount"`    // 最近窗口失败回写数量
	RecentAvgCostMs    float64 `json:"recentAvgCostMs"`    // 最近窗口平均处理耗时（毫秒）
	RecentMaxCostMs    float64 `json:"recentMaxCostMs"`    // 最近窗口最大处理耗时（毫秒）
}

// CollectorTransportStatResp 表示按入队来源聚合的分布项。
type CollectorTransportStatResp struct {
	Transport    string `json:"transport"`    // 来源通道
	TotalCount   int64  `json:"totalCount"`   // 总任务量
	ReadyCount   int64  `json:"readyCount"`   // 已到执行时间的待处理数量
	RunningCount int64  `json:"runningCount"` // 执行中数量
	RetryCount   int64  `json:"retryCount"`   // 待重试数量
	DeadCount    int64  `json:"deadCount"`    // 死信数量
	DoneCount    int64  `json:"doneCount"`    // 已完成数量
}

// CollectorOverviewResp 表示 Collector 运维概览信息。
type CollectorOverviewResp struct {
	PendingCount          int64 `json:"pendingCount"`          // 待执行数量
	RunningCount          int64 `json:"runningCount"`          // 执行中数量
	DoneCount             int64 `json:"doneCount"`             // 已完成数量
	RetryCount            int64 `json:"retryCount"`            // 待重试数量
	DeadCount             int64 `json:"deadCount"`             // 死信数量
	ReadyCount            int64 `json:"readyCount"`            // 当前已到执行时间的待处理数量
	RunningTimeoutCount   int64 `json:"runningTimeoutCount"`   // 疑似超时运行中的数量
	OldestReadyAgeSeconds int64 `json:"oldestReadyAgeSeconds"` // 最早待处理任务已积压秒数

	KafkaBatchSize          int    `json:"kafkaBatchSize"`          // Kafka 批次配置
	RedisReadCount          int64  `json:"redisReadCount"`          // Redis 单批读取配置
	DBRunnerBatchSize       int    `json:"dbRunnerBatchSize"`       // DB Worker 单批处理配置
	DBRunnerIntervalSecs    int    `json:"dbRunnerIntervalSecs"`    // DB Worker 轮询间隔秒数
	RunningLeaseSeconds     int    `json:"runningLeaseSeconds"`     // running 租约秒数
	MaxRetryTimes           int    `json:"maxRetryTimes"`           // 最大重试次数
	OverviewGeneratedAt     string `json:"overviewGeneratedAt"`     // 当前概览生成时间
	OverviewCacheTTLSeconds int    `json:"overviewCacheTTLSeconds"` // 当前概览缓存 TTL（秒）
	OverviewCacheAgeSeconds int64  `json:"overviewCacheAgeSeconds"` // 当前概览缓存已存在秒数
	OverviewCacheHit        bool   `json:"overviewCacheHit"`        // 当前响应是否命中缓存

	Recent1m  CollectorWindowStatResp `json:"recent1m"`  // 最近 1 分钟处理统计
	Recent5m  CollectorWindowStatResp `json:"recent5m"`  // 最近 5 分钟处理统计
	Recent15m CollectorWindowStatResp `json:"recent15m"` // 最近 15 分钟处理统计

	BizTypeTop15m  []CollectorBizTypeStatResp   `json:"bizTypeTop15m"`  // 最近 15 分钟重点业务排行
	TransportStats []CollectorTransportStatResp `json:"transportStats"` // 当前 transport 分布
}

// CollectorTaskResp 表示 Collector 任务对外返回结构。
type CollectorTaskResp struct {
	ID           int64  `json:"id"`           // 主键
	EventID      string `json:"eventId"`      // 事件 ID
	BizType      string `json:"bizType"`      // 业务类型
	PartitionKey string `json:"partitionKey"` // 聚合分区键
	Payload      string `json:"payload"`      // 结构化事件负载
	Transport    string `json:"transport"`    // 传输通道
	State        int    `json:"state"`        // 任务状态
	Attempt      int    `json:"attempt"`      // 已失败重试次数
	NextRunAt    string `json:"nextRunAt"`    // 下次可执行时间
	StartedAt    string `json:"startedAt"`    // 开始执行时间
	FinishedAt   string `json:"finishedAt"`   // 结束时间
	LastError    string `json:"lastError"`    // 最近一次失败原因
	CreatedAt    string `json:"createdAt"`    // 创建时间
	UpdatedAt    string `json:"updatedAt"`    // 修改时间
}

// RunCollectorReq 表示手动触发执行 Collector 任务的请求。
type RunCollectorReq struct {
	Limit int `json:"limit,optional"` // 单次执行上限；为空时使用后端默认值
}

// Validate 校验手动触发 Collector 任务的请求。
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

// RetryCollectorTasksReq 表示对指定任务做“手动重试/手动延迟重试”的请求。
type RetryCollectorTasksReq struct {
	IDs          []int64 `json:"ids"`                   // 任务 ID 列表
	DelaySeconds int     `json:"delaySeconds,optional"` // 相对当前时间延迟执行，单位秒
	ResetAttempt *bool   `json:"resetAttempt,optional"` // 是否重置 attempt，未传时默认重置
}

// Validate 校验并归一化人工重试 Collector 任务的请求。
func (r *RetryCollectorTasksReq) Validate() error {
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
			return errors.Errorf("任务ID不合法")
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

// RetryCollectorTasksResp 表示手动重试响应。
type RetryCollectorTasksResp struct {
	Updated int `json:"updated"` // 成功更新的任务数量
}

// isCollectorTransport 判断任务来源过滤值是否受支持。
func isCollectorTransport(value string) bool {
	switch value {
	case "db", "kafka", "redis":
		return true
	default:
		return false
	}
}
