package taskreport

import (
	"context"
	"strings"
	"time"

	"admin/internal/task/taskwire"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
)

const (
	// reportQueueLimit 限制单份日报读取队列实时计数的数量。
	reportQueueLimit = 32
)

// QueueManager 描述日报读取当前队列积压所需的最小能力。
type QueueManager interface {
	ListReportQueues(ctx context.Context, limit int) (*types.TaskQueueListResp, bool, error) // ListReportQueues 返回有界队列概览和指标是否降级。
}

// HistoryReportBuilder 描述从 DB 短期汇总生成日报的能力。
// 日报只允许使用该入口，禁止回退扫描 Asynq completed/archived 明细。
type HistoryReportBuilder interface {
	BuildTaskReport(ctx context.Context, req ReportRequest, queues []QueueSummary) (Report, error)
}

// ReportRequest 描述一次任务运行日报统计请求。
type ReportRequest struct {
	WindowStart       time.Time // 统计窗口开始时间，默认取生成基准时间往前 24 小时
	WindowEnd         time.Time // 统计窗口结束时间，默认取生成基准时间
	GeneratedAt       time.Time // 报告生成时间；为空时使用当前时间
	ExcludeWorkflowID string    // 排除当前日报工作流，避免报告把自身计入本窗口
}

// Report 汇总一个统计窗口内周期任务和工作流执行情况。
type Report struct {
	WindowStart           time.Time           // 统计窗口开始时间
	WindowEnd             time.Time           // 统计窗口结束时间
	GeneratedAt           time.Time           // 报告生成时间
	TotalTaskExecutions   int                 // 周期来源任务执行总数，包含触发任务与节点任务
	SuccessTaskExecutions int                 // 成功任务数
	FailedTaskExecutions  int                 // 失败任务数
	PeriodicTriggerTotal  int                 // 周期触发入口任务总数
	PeriodicTriggerOK     int                 // 周期触发入口成功数
	PeriodicTriggerFailed int                 // 周期触发入口失败数
	NodeTaskTotal         int                 // 周期工作流节点任务总数
	WorkflowTotal         int                 // 当前窗口任务涉及的去重工作流实例数
	WorkflowSuccess       int                 // 成功工作流实例数
	WorkflowFailed        int                 // 失败工作流实例数
	WorkflowRunning       int                 // 仍在运行的工作流实例数
	WorkflowUnknown       int                 // 状态已过期或无法确认的工作流实例数
	TraceTotalCount       int64               // 任务执行统计中累计处理总量
	TraceReadCount        int64               // 读取数量
	TraceWriteCount       int64               // 写入数量，包含 insert/update/upsert
	TraceDeleteCount      int64               // 删除数量
	TraceErrorCount       int64               // 隔离错误数量
	AverageDurationMS     int64               // 周期来源任务平均耗时
	MaxDurationMS         int64               // 周期来源任务最大耗时
	QueueSummaries        []QueueSummary      // 队列维度摘要
	PeriodicTasks         []PeriodicSummary   // 周期任务维度摘要
	Workflows             []WorkflowSummary   // 工作流维度摘要
	TimeBuckets           []TimeBucketSummary // 小时时间段分布摘要
	FailureTasks          []TaskSummary       // 失败任务明细
	SlowTasks             []TaskSummary       // 慢任务明细
	TraceErrorTasks       []TaskSummary       // 处理量错误任务明细
	IntegrityWarnings     []string            // 数据源截断或安全上限导致的数据完整性提示
}

// QueueSummary 描述队列在日报窗口内和当前时刻的摘要。
type QueueSummary struct {
	Name           string // 队列名称
	TaskExecutions int    // 当前窗口周期来源任务数
	Success        int    // 当前窗口成功任务数
	Failed         int    // 当前窗口失败任务数
	Triggers       int    // 当前窗口周期触发任务数
	NodeTasks      int    // 当前窗口节点任务数
	Pending        int    // 当前 pending 数
	Active         int    // 当前 active 数
	Scheduled      int    // 当前 scheduled 数
	Retry          int    // 当前 retry 数
	Archived       int    // 当前 archived 数
	Completed      int    // 当前 completed 数
	Aggregating    int    // 当前 aggregating 数
}

// PeriodicSummary 描述单个周期配置的执行摘要。
type PeriodicSummary struct {
	Name           string // 周期任务名称
	WorkflowName   string // 工作流名称
	Queue          string // 主要队列
	TaskExecutions int    // 任务执行数
	Success        int    // 成功任务数
	Failed         int    // 失败任务数
	Triggers       int    // 周期触发次数
	NodeTasks      int    // 工作流节点任务数
	AverageMS      int64  // 平均耗时
	MaxMS          int64  // 最大耗时
	LastAt         string // 最近活动时间
}

// WorkflowSummary 描述单个工作流名称下的实例摘要。
type WorkflowSummary struct {
	Name      string // 工作流名称
	Periodic  string // 主要周期任务名称
	Queue     string // 主要队列
	Total     int    // 工作流实例数
	Success   int    // 成功实例数
	Failed    int    // 失败实例数
	Running   int    // 运行中实例数
	Unknown   int    // 未知实例数
	NodeTasks int    // 节点任务数
	AverageMS int64  // 节点平均耗时
	MaxMS     int64  // 节点最大耗时
	LastAt    string // 最近活动时间
}

// TaskSummary 描述日报中的任务明细。
type TaskSummary struct {
	ID           string   // Asynq 任务 ID 或工作流实例 ID
	Name         string   // 任务展示名
	Type         string   // 任务类型
	State        string   // 任务状态
	Queue        string   // 队列
	PeriodicName string   // 周期任务名称
	WorkflowID   string   // 工作流实例 ID
	WorkflowName string   // 工作流名称
	WorkflowNode string   // 工作流节点
	StartedAt    string   // 开始时间
	FinishedAt   string   // 完成或失败时间
	DurationMS   int64    // 耗时毫秒
	Error        string   // 错误摘要
	TraceErrors  int64    // 执行统计中的隔离错误数量
	TraceDetails []string // 执行统计错误明细
}

// TimeBucketSummary 描述一个时间段内的任务与处理量分布。
type TimeBucketSummary struct {
	StartAt           string // 时间段开始，RFC3339
	EndAt             string // 时间段结束，RFC3339
	TaskExecutions    int    // 任务执行数
	Success           int    // 成功任务数
	Failed            int    // 失败任务数
	Triggers          int    // 周期触发任务数
	NodeTasks         int    // 工作流节点任务数
	TraceTotalCount   int64  // 处理总量
	TraceReadCount    int64  // 读取数量
	TraceWriteCount   int64  // 写入数量，包含 insert/update/upsert
	TraceDeleteCount  int64  // 删除数量
	TraceErrorCount   int64  // 隔离错误数量
	AverageDurationMS int64  // 平均耗时毫秒
	MaxDurationMS     int64  // 最大耗时毫秒
}

// Service 构建任务系统运行日报。
type Service struct {
	manager QueueManager         // 任务队列实时计数读取入口
	history HistoryReportBuilder // DB 历史日报构建器
}

// NewService 创建只使用 DB 终态汇总的任务运行日报服务。
func NewService(manager QueueManager, history HistoryReportBuilder) *Service {
	return &Service{manager: manager, history: history}
}

// Window 返回指定基准时间往前 24 小时的统计窗口。
func Window(now time.Time) (time.Time, time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	now = now.Truncate(time.Second)
	return now.Add(-24 * time.Hour), now
}

// Build 校验窗口并从 DB 终态汇总构建日报，同时补充 Redis 当前队列积压。
func (s *Service) Build(ctx context.Context, req ReportRequest) (Report, error) {
	if s == nil || s.manager == nil {
		return Report{}, errors.Errorf("任务运行日报管理器为空")
	}
	if s.history == nil {
		return Report{}, errors.Errorf("任务历史落库未启用，数据库版日报不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if req.WindowStart.IsZero() != req.WindowEnd.IsZero() {
		return Report{}, errors.Errorf("任务运行日报统计窗口必须同时提供开始和结束时间")
	}
	req = normalizeRequest(req)
	if req.WindowStart.Nanosecond() != 0 || req.WindowEnd.Nanosecond() != 0 {
		return Report{}, errors.Errorf("任务运行日报统计窗口必须按整秒对齐 start=%s end=%s", req.WindowStart.Format(time.RFC3339Nano), req.WindowEnd.Format(time.RFC3339Nano))
	}
	if !req.WindowEnd.After(req.WindowStart) {
		return Report{}, errors.Errorf("任务运行日报统计窗口非法 start=%s end=%s", req.WindowStart.Format(time.RFC3339), req.WindowEnd.Format(time.RFC3339))
	}
	queueSummaries, queueMetricsLimited, err := s.queueSummaries(ctx)
	if err != nil {
		return Report{}, errors.Tag(err)
	}
	report, err := s.history.BuildTaskReport(ctx, req, queueSummaries)
	if err != nil {
		return Report{}, errors.Tag(err)
	}
	if queueMetricsLimited {
		report.IntegrityWarnings = append(report.IntegrityWarnings, "任务队列数量或聚合组超过安全读取上限，当前实时队列概览已降级")
	}
	return report, nil
}

// normalizeRequest 补齐日报窗口和生成时间。
func normalizeRequest(req ReportRequest) ReportRequest {
	now := req.GeneratedAt
	if now.IsZero() {
		now = time.Now()
	}
	if req.WindowStart.IsZero() || req.WindowEnd.IsZero() {
		req.WindowStart, req.WindowEnd = Window(now)
	}
	if req.GeneratedAt.IsZero() {
		req.GeneratedAt = now
	}
	return req
}

// queueSummaries 读取队列当前积压概览，空队列时补默认展示队列。
func (s *Service) queueSummaries(ctx context.Context) ([]QueueSummary, bool, error) {
	resp, limited, err := s.manager.ListReportQueues(ctx, reportQueueLimit)
	if err != nil {
		return nil, false, errors.Tag(err)
	}
	if resp == nil {
		return nil, false, errors.Errorf("任务运行日报队列概览为空")
	}
	result := make([]QueueSummary, 0, len(resp.Queues))
	for _, item := range resp.Queues {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		result = append(result, QueueSummary{
			Name: name, Pending: item.Pending, Active: item.Active,
			Scheduled: item.Scheduled, Retry: item.Retry, Archived: item.Archived,
			Completed: item.Completed, Aggregating: item.Aggregating,
		})
	}
	if len(result) == 0 {
		result = append(result,
			QueueSummary{Name: taskwire.QueueCritical},
			QueueSummary{Name: taskwire.QueueDefault},
			QueueSummary{Name: taskwire.QueueMaintenance},
		)
	}
	return result, limited, nil
}
