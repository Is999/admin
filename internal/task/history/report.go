package taskhistory

import (
	"context"
	"sort"
	"time"

	"admin/internal/jobs/taskreport"
	"admin/internal/model"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

const (
	// reportDimensionLimit 限制日报单个维度返回数量，避免配置异常制造大结果集。
	reportDimensionLimit = 200
	// reportTopLimit 与日报展示明细上限保持一致。
	reportTopLimit = 8
	// reportQueryTimeout 限制整份数据库日报占用连接的时间，超时交给任务有限重试。
	reportQueryTimeout = 15 * time.Second
	// workflowReportColumns 只读取日报 Top 列表需要的汇总列，避免加载历史快照 JSON。
	workflowReportColumns = "workflow_id, workflow_name, periodic_name, queue, status, workflow_created_at, finished_at, duration_ms, error_message, trace_error"
)

// BuildTaskReport 从工作流汇总和失败明细生成日报，不扫描 Redis 终态任务。
func (s *Store) BuildTaskReport(ctx context.Context, req taskreport.ReportRequest, queues []taskreport.QueueSummary) (taskreport.Report, error) {
	if s == nil {
		return taskreport.Report{}, errors.Errorf("任务历史日报存储为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	queryCtx, cancel := context.WithTimeout(ctx, reportQueryTimeout)
	defer cancel()
	ctx = queryCtx
	report, err := s.reportTotals(s.periodicReportQuery(ctx, req), req, queues)
	if err != nil {
		return taskreport.Report{}, errors.Tag(err)
	}
	if report.QueueSummaries, err = s.reportQueues(s.periodicReportQuery(ctx, req), queues); err != nil {
		return taskreport.Report{}, errors.Tag(err)
	}
	if report.PeriodicTasks, err = s.reportPeriodic(s.periodicReportQuery(ctx, req)); err != nil {
		return taskreport.Report{}, errors.Tag(err)
	}
	if report.Workflows, err = s.reportWorkflows(s.periodicReportQuery(ctx, req)); err != nil {
		return taskreport.Report{}, errors.Tag(err)
	}
	if report.TimeBuckets, err = s.reportTimeBuckets(s.periodicReportQuery(ctx, req)); err != nil {
		return taskreport.Report{}, errors.Tag(err)
	}
	if report.SlowTasks, err = s.reportSlowWorkflows(s.periodicReportQuery(ctx, req)); err != nil {
		return taskreport.Report{}, errors.Tag(err)
	}
	if report.TraceErrorTasks, err = s.reportTraceErrorWorkflows(s.periodicReportQuery(ctx, req)); err != nil {
		return taskreport.Report{}, errors.Tag(err)
	}
	if report.FailureTasks, err = s.reportFailureTasks(ctx, req); err != nil {
		return taskreport.Report{}, errors.Tag(err)
	}
	if len(report.PeriodicTasks) >= reportDimensionLimit || len(report.Workflows) >= reportDimensionLimit {
		report.IntegrityWarnings = append(report.IntegrityWarnings, "日报维度数量达到安全上限，仅展示前 200 个维度")
	}
	return report, nil
}

// periodicReportQuery 返回日报周期工作流的有界基础查询。
func (s *Store) periodicReportQuery(ctx context.Context, req taskreport.ReportRequest) *gorm.DB {
	query := s.readDB.WithContext(ctx).Model(&model.TaskWorkflowRun{}).
		Where("app_id = ? AND finished_at >= ? AND finished_at < ? AND periodic_name <> ''", s.appID, req.WindowStart, req.WindowEnd)
	if req.ExcludeWorkflowID != "" {
		query = query.Where("workflow_id <> ?", req.ExcludeWorkflowID)
	}
	return query
}

// reportTotals 聚合日报总计。
func (s *Store) reportTotals(query *gorm.DB, req taskreport.ReportRequest, queues []taskreport.QueueSummary) (taskreport.Report, error) {
	type totalRow struct {
		WorkflowTotal   int64   `gorm:"column:workflow_total"`   // 工作流总数
		WorkflowSuccess int64   `gorm:"column:workflow_success"` // 成功工作流数
		WorkflowFailed  int64   `gorm:"column:workflow_failed"`  // 失败工作流数
		NodeTasks       int64   `gorm:"column:node_tasks"`       // 节点任务总数
		NodeSuccess     int64   `gorm:"column:node_success"`     // 成功节点任务数
		NodeFailed      int64   `gorm:"column:node_failed"`      // 失败节点任务数
		TraceTotal      int64   `gorm:"column:trace_total"`      // 处理总量
		TraceRead       int64   `gorm:"column:trace_read"`       // 读取量
		TraceWrite      int64   `gorm:"column:trace_write"`      // 写入量
		TraceDelete     int64   `gorm:"column:trace_delete"`     // 删除量
		TraceError      int64   `gorm:"column:trace_error"`      // 错误量
		AverageMS       float64 `gorm:"column:average_ms"`       // 平均耗时毫秒
		MaxMS           int64   `gorm:"column:max_ms"`           // 最大耗时毫秒
	}
	var row totalRow
	err := query.Select(`COUNT(*) AS workflow_total,
		COALESCE(SUM(status = 'success'), 0) AS workflow_success,
		COALESCE(SUM(status = 'failed'), 0) AS workflow_failed,
		COALESCE(SUM(task_total), 0) AS node_tasks,
		COALESCE(SUM(succeeded), 0) AS node_success,
		COALESCE(SUM(failed), 0) AS node_failed,
		COALESCE(SUM(trace_total), 0) AS trace_total,
		COALESCE(SUM(trace_read), 0) AS trace_read,
		COALESCE(SUM(trace_write), 0) AS trace_write,
		COALESCE(SUM(trace_delete), 0) AS trace_delete,
		COALESCE(SUM(trace_error), 0) AS trace_error,
		COALESCE(AVG(duration_ms), 0) AS average_ms,
		COALESCE(MAX(duration_ms), 0) AS max_ms`).Take(&row).Error
	if err != nil {
		return taskreport.Report{}, errors.Wrap(err, "聚合任务日报总计失败")
	}
	workflowTotal := int(row.WorkflowTotal)
	report := taskreport.Report{
		WindowStart: req.WindowStart, WindowEnd: req.WindowEnd, GeneratedAt: req.GeneratedAt,
		PeriodicTriggerTotal: workflowTotal, PeriodicTriggerOK: int(row.WorkflowSuccess), PeriodicTriggerFailed: int(row.WorkflowFailed),
		NodeTaskTotal: int(row.NodeTasks), WorkflowTotal: workflowTotal, WorkflowSuccess: int(row.WorkflowSuccess), WorkflowFailed: int(row.WorkflowFailed),
		TotalTaskExecutions: workflowTotal + int(row.NodeTasks), SuccessTaskExecutions: int(row.WorkflowSuccess + row.NodeSuccess), FailedTaskExecutions: int(row.WorkflowFailed + row.NodeFailed),
		TraceTotalCount: row.TraceTotal, TraceReadCount: row.TraceRead, TraceWriteCount: row.TraceWrite,
		TraceDeleteCount: row.TraceDelete, TraceErrorCount: row.TraceError,
		AverageDurationMS: int64(row.AverageMS), MaxDurationMS: row.MaxMS,
		QueueSummaries: queues, PeriodicTasks: []taskreport.PeriodicSummary{}, Workflows: []taskreport.WorkflowSummary{},
		TimeBuckets: []taskreport.TimeBucketSummary{}, FailureTasks: []taskreport.TaskSummary{}, SlowTasks: []taskreport.TaskSummary{}, TraceErrorTasks: []taskreport.TaskSummary{},
	}
	return report, nil
}

// reportQueues 合并 DB 窗口统计与 Redis 当前积压。
func (s *Store) reportQueues(query *gorm.DB, current []taskreport.QueueSummary) ([]taskreport.QueueSummary, error) {
	type row struct {
		Queue     string `gorm:"column:queue"`      // 队列名称
		Workflows int64  `gorm:"column:workflows"`  // 工作流数
		NodeTasks int64  `gorm:"column:node_tasks"` // 节点任务数
		Success   int64  `gorm:"column:success"`    // 成功数
		Failed    int64  `gorm:"column:failed"`     // 失败数
	}
	rows := make([]row, 0)
	if err := query.Select("queue, COUNT(*) AS workflows, COALESCE(SUM(task_total), 0) AS node_tasks, COALESCE(SUM(status = 'success') + SUM(succeeded), 0) AS success, COALESCE(SUM(status = 'failed') + SUM(failed), 0) AS failed").
		Group("queue").Limit(reportDimensionLimit).Scan(&rows).Error; err != nil {
		return nil, errors.Wrap(err, "聚合任务日报队列失败")
	}
	byName := make(map[string]taskreport.QueueSummary, len(current)+len(rows))
	for _, item := range current {
		byName[item.Name] = item
	}
	for _, item := range rows {
		summary := byName[item.Queue]
		summary.Name = item.Queue
		summary.Triggers = int(item.Workflows)
		summary.NodeTasks = int(item.NodeTasks)
		summary.TaskExecutions = summary.Triggers + summary.NodeTasks
		summary.Success = int(item.Success)
		summary.Failed = int(item.Failed)
		byName[item.Queue] = summary
	}
	result := make([]taskreport.QueueSummary, 0, len(byName))
	for _, item := range byName {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// reportPeriodic 聚合周期任务维度。
func (s *Store) reportPeriodic(query *gorm.DB) ([]taskreport.PeriodicSummary, error) {
	type row struct {
		Name         string    `gorm:"column:name"`          // 周期任务名称
		WorkflowName string    `gorm:"column:workflow_name"` // 工作流名称
		Queue        string    `gorm:"column:queue"`         // 队列名称
		Workflows    int64     `gorm:"column:workflows"`     // 工作流数
		NodeTasks    int64     `gorm:"column:node_tasks"`    // 节点任务数
		Success      int64     `gorm:"column:success"`       // 成功数
		Failed       int64     `gorm:"column:failed"`        // 失败数
		AverageMS    float64   `gorm:"column:average_ms"`    // 平均耗时毫秒
		MaxMS        int64     `gorm:"column:max_ms"`        // 最大耗时毫秒
		LastAt       time.Time `gorm:"column:last_at"`       // 最近完成时间
	}
	rows := make([]row, 0)
	err := query.Select("periodic_name AS name, workflow_name, queue, COUNT(*) AS workflows, COALESCE(SUM(task_total), 0) AS node_tasks, COALESCE(SUM(status = 'success') + SUM(succeeded), 0) AS success, COALESCE(SUM(status = 'failed') + SUM(failed), 0) AS failed, COALESCE(AVG(duration_ms), 0) AS average_ms, COALESCE(MAX(duration_ms), 0) AS max_ms, MAX(finished_at) AS last_at").
		Group("periodic_name, workflow_name, queue").Order("last_at DESC").Limit(reportDimensionLimit).Scan(&rows).Error
	if err != nil {
		return nil, errors.Wrap(err, "聚合任务日报周期维度失败")
	}
	result := make([]taskreport.PeriodicSummary, 0, len(rows))
	for _, item := range rows {
		result = append(result, taskreport.PeriodicSummary{
			Name: item.Name, WorkflowName: item.WorkflowName, Queue: item.Queue,
			TaskExecutions: int(item.Workflows + item.NodeTasks), Triggers: int(item.Workflows), NodeTasks: int(item.NodeTasks),
			Success: int(item.Success), Failed: int(item.Failed), AverageMS: int64(item.AverageMS), MaxMS: item.MaxMS, LastAt: item.LastAt.Format(time.RFC3339),
		})
	}
	return result, nil
}

// reportWorkflows 聚合工作流名称维度。
func (s *Store) reportWorkflows(query *gorm.DB) ([]taskreport.WorkflowSummary, error) {
	type row struct {
		Name      string    `gorm:"column:name"`       // 工作流名称
		Periodic  string    `gorm:"column:periodic"`   // 周期任务名称
		Queue     string    `gorm:"column:queue"`      // 队列名称
		Total     int64     `gorm:"column:total"`      // 工作流总数
		Success   int64     `gorm:"column:success"`    // 成功数
		Failed    int64     `gorm:"column:failed"`     // 失败数
		NodeTasks int64     `gorm:"column:node_tasks"` // 节点任务数
		AverageMS float64   `gorm:"column:average_ms"` // 平均耗时毫秒
		MaxMS     int64     `gorm:"column:max_ms"`     // 最大耗时毫秒
		LastAt    time.Time `gorm:"column:last_at"`    // 最近完成时间
	}
	rows := make([]row, 0)
	err := query.Select("workflow_name AS name, periodic_name AS periodic, queue, COUNT(*) AS total, COALESCE(SUM(status = 'success'), 0) AS success, COALESCE(SUM(status = 'failed'), 0) AS failed, COALESCE(SUM(task_total), 0) AS node_tasks, COALESCE(AVG(duration_ms), 0) AS average_ms, COALESCE(MAX(duration_ms), 0) AS max_ms, MAX(finished_at) AS last_at").
		Group("workflow_name, periodic_name, queue").Order("last_at DESC").Limit(reportDimensionLimit).Scan(&rows).Error
	if err != nil {
		return nil, errors.Wrap(err, "聚合任务日报工作流维度失败")
	}
	result := make([]taskreport.WorkflowSummary, 0, len(rows))
	for _, item := range rows {
		result = append(result, taskreport.WorkflowSummary{
			Name: item.Name, Periodic: item.Periodic, Queue: item.Queue, Total: int(item.Total),
			Success: int(item.Success), Failed: int(item.Failed), NodeTasks: int(item.NodeTasks),
			AverageMS: int64(item.AverageMS), MaxMS: item.MaxMS, LastAt: item.LastAt.Format(time.RFC3339),
		})
	}
	return result, nil
}

// reportTimeBuckets 聚合小时分布。
func (s *Store) reportTimeBuckets(query *gorm.DB) ([]taskreport.TimeBucketSummary, error) {
	type row struct {
		HourBucket  int64   `gorm:"column:hour_bucket"`  // 小时时间桶
		Workflows   int64   `gorm:"column:workflows"`    // 工作流数
		NodeTasks   int64   `gorm:"column:node_tasks"`   // 节点任务数
		Success     int64   `gorm:"column:success"`      // 成功数
		Failed      int64   `gorm:"column:failed"`       // 失败数
		TraceTotal  int64   `gorm:"column:trace_total"`  // 处理总量
		TraceRead   int64   `gorm:"column:trace_read"`   // 读取量
		TraceWrite  int64   `gorm:"column:trace_write"`  // 写入量
		TraceDelete int64   `gorm:"column:trace_delete"` // 删除量
		TraceError  int64   `gorm:"column:trace_error"`  // 错误量
		AverageMS   float64 `gorm:"column:average_ms"`   // 平均耗时毫秒
		MaxMS       int64   `gorm:"column:max_ms"`       // 最大耗时毫秒
	}
	rows := make([]row, 0, 25)
	err := query.Select("(UNIX_TIMESTAMP(finished_at) DIV 3600) AS hour_bucket, COUNT(*) AS workflows, COALESCE(SUM(task_total), 0) AS node_tasks, COALESCE(SUM(status = 'success') + SUM(succeeded), 0) AS success, COALESCE(SUM(status = 'failed') + SUM(failed), 0) AS failed, COALESCE(SUM(trace_total), 0) AS trace_total, COALESCE(SUM(trace_read), 0) AS trace_read, COALESCE(SUM(trace_write), 0) AS trace_write, COALESCE(SUM(trace_delete), 0) AS trace_delete, COALESCE(SUM(trace_error), 0) AS trace_error, COALESCE(AVG(duration_ms), 0) AS average_ms, COALESCE(MAX(duration_ms), 0) AS max_ms").
		Group("hour_bucket").Order("hour_bucket ASC").Limit(744).Scan(&rows).Error
	if err != nil {
		return nil, errors.Wrap(err, "聚合任务日报小时维度失败")
	}
	result := make([]taskreport.TimeBucketSummary, 0, len(rows))
	for _, item := range rows {
		start := time.Unix(item.HourBucket*3600, 0)
		result = append(result, taskreport.TimeBucketSummary{
			StartAt: start.Format(time.RFC3339), EndAt: start.Add(time.Hour).Format(time.RFC3339),
			TaskExecutions: int(item.Workflows + item.NodeTasks), Success: int(item.Success), Failed: int(item.Failed),
			Triggers: int(item.Workflows), NodeTasks: int(item.NodeTasks), TraceTotalCount: item.TraceTotal,
			TraceReadCount: item.TraceRead, TraceWriteCount: item.TraceWrite, TraceDeleteCount: item.TraceDelete,
			TraceErrorCount: item.TraceError, AverageDurationMS: int64(item.AverageMS), MaxDurationMS: item.MaxMS,
		})
	}
	return result, nil
}

// reportSlowWorkflows 返回最慢工作流汇总，替代逐成功分片明细。
func (s *Store) reportSlowWorkflows(query *gorm.DB) ([]taskreport.TaskSummary, error) {
	rows := make([]model.TaskWorkflowRun, 0, reportTopLimit)
	if err := query.Select(workflowReportColumns).Order("duration_ms DESC").Order("id DESC").Limit(reportTopLimit).Find(&rows).Error; err != nil {
		return nil, errors.Wrap(err, "查询任务日报慢工作流失败")
	}
	return workflowTaskSummaries(rows), nil
}

// reportTraceErrorWorkflows 返回处理量错误最多的工作流汇总。
func (s *Store) reportTraceErrorWorkflows(query *gorm.DB) ([]taskreport.TaskSummary, error) {
	rows := make([]model.TaskWorkflowRun, 0, reportTopLimit)
	if err := query.Select(workflowReportColumns).Where("trace_error > 0").Order("trace_error DESC").Order("id DESC").Limit(reportTopLimit).Find(&rows).Error; err != nil {
		return nil, errors.Wrap(err, "查询任务日报错误工作流失败")
	}
	return workflowTaskSummaries(rows), nil
}

// reportFailureTasks 返回窗口内最终失败任务明细。
func (s *Store) reportFailureTasks(ctx context.Context, req taskreport.ReportRequest) ([]taskreport.TaskSummary, error) {
	query := s.readDB.WithContext(ctx).Model(&model.TaskFailure{}).
		Where("app_id = ? AND source = ? AND failed_at >= ? AND failed_at < ?", s.appID, "periodic", req.WindowStart, req.WindowEnd)
	if req.ExcludeWorkflowID != "" {
		query = query.Where("workflow_id <> ?", req.ExcludeWorkflowID)
	}
	rows := make([]model.TaskFailure, 0, reportTopLimit)
	if err := query.Order("failed_at DESC").Order("id DESC").Limit(reportTopLimit).Find(&rows).Error; err != nil {
		return nil, errors.Wrap(err, "查询任务日报失败明细失败")
	}
	result := make([]taskreport.TaskSummary, 0, len(rows))
	for _, row := range rows {
		result = append(result, taskreport.TaskSummary{
			ID: row.TaskID, Name: row.TaskName, Type: row.TaskType, State: "archived", Queue: row.Queue,
			PeriodicName: row.PeriodicName, WorkflowID: row.WorkflowID, WorkflowName: row.WorkflowName, WorkflowNode: row.WorkflowNode,
			FinishedAt: row.FailedAt.Format(time.RFC3339), Error: row.ErrorMessage,
		})
	}
	return result, nil
}

// workflowTaskSummaries 把工作流汇总行转换为日报复用的明细结构。
func workflowTaskSummaries(rows []model.TaskWorkflowRun) []taskreport.TaskSummary {
	result := make([]taskreport.TaskSummary, 0, len(rows))
	for _, row := range rows {
		result = append(result, taskreport.TaskSummary{
			ID: row.WorkflowID, Name: row.PeriodicName, Type: "workflow.summary", State: row.Status,
			Queue: row.Queue, PeriodicName: row.PeriodicName, WorkflowID: row.WorkflowID,
			WorkflowName: row.WorkflowName, StartedAt: row.WorkflowCreatedAt.Format(time.RFC3339),
			FinishedAt: row.FinishedAt.Format(time.RFC3339), DurationMS: row.DurationMS,
			Error: row.ErrorMessage, TraceErrors: row.TraceError,
		})
	}
	return result
}
