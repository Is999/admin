package larkx

import (
	"context"
	"strconv"
	"strings"
	"time"
)

const (
	// taskDailyReportCardTopLimit 控制日报卡片中 Top 列表的展示数量。
	taskDailyReportCardTopLimit = 5
	// taskDailyReportCardDetailLimit 控制日报卡片中异常和慢任务明细数量。
	taskDailyReportCardDetailLimit = 3
)

// TaskDailyReport 描述任务系统日报通知内容。
type TaskDailyReport struct {
	ServiceName           string                 // 服务名
	Environment           string                 // 运行环境
	AppID                 string                 // 站点/应用 ID
	WindowStart           time.Time              // 统计窗口开始时间
	WindowEnd             time.Time              // 统计窗口结束时间
	GeneratedAt           time.Time              // 报告生成时间
	TotalTaskExecutions   int                    // 周期来源任务执行总数
	SuccessTaskExecutions int                    // 成功任务数
	FailedTaskExecutions  int                    // 失败任务数
	PeriodicTriggerTotal  int                    // 周期触发入口任务总数
	PeriodicTriggerOK     int                    // 周期触发入口成功数
	PeriodicTriggerFailed int                    // 周期触发入口失败数
	NodeTaskTotal         int                    // 周期工作流节点任务总数
	WorkflowTotal         int                    // 工作流实例总数
	WorkflowSuccess       int                    // 成功工作流实例数
	WorkflowFailed        int                    // 失败工作流实例数
	WorkflowRunning       int                    // 运行中工作流实例数
	WorkflowUnknown       int                    // 未知工作流实例数
	TraceTotalCount       int64                  // 处理总量
	TraceReadCount        int64                  // 读取数量
	TraceWriteCount       int64                  // 写入数量
	TraceDeleteCount      int64                  // 删除数量
	TraceErrorCount       int64                  // 错误数量
	AverageDurationMS     int64                  // 平均耗时毫秒
	MaxDurationMS         int64                  // 最大耗时毫秒
	Queues                []TaskDailyReportQueue // 队列摘要
	PeriodicTasks         []TaskDailyReportItem  // 周期任务摘要
	Workflows             []TaskDailyReportItem  // 工作流摘要
	FailureTasks          []TaskDailyReportTask  // 失败任务明细
	SlowTasks             []TaskDailyReportTask  // 慢任务明细
	Truncated             bool                   // 是否截断统计
	RetentionWarning      string                 // 保留时间不足提示
}

// TaskDailyReportQueue 描述队列维度日报摘要。
type TaskDailyReportQueue struct {
	Name           string // 队列名称
	TaskExecutions int    // 任务执行数
	Success        int    // 成功数
	Failed         int    // 失败数
	Triggers       int    // 周期触发任务数
	NodeTasks      int    // 工作流节点任务数
	Pending        int    // 当前 pending 数
	Active         int    // 当前 active 数
	Scheduled      int    // 当前 scheduled 数
	Retry          int    // 当前 retry 数
	Archived       int    // 当前 archived 数
}

// TaskDailyReportItem 描述周期任务或工作流摘要项。
type TaskDailyReportItem struct {
	Name           string // 名称
	Related        string // 关联名称
	Queue          string // 主要队列
	TaskExecutions int    // 任务执行数
	Triggers       int    // 周期触发数
	NodeTasks      int    // 节点任务数
	Success        int    // 成功数
	Failed         int    // 失败数
	Running        int    // 运行中数
	Unknown        int    // 未知数
	AverageMS      int64  // 平均耗时毫秒
	MaxMS          int64  // 最大耗时毫秒
	LastAt         string // 最近活动时间
}

// TaskDailyReportTask 描述日报中的任务明细。
type TaskDailyReportTask struct {
	ID           string // Asynq 任务 ID
	Name         string // 任务展示名
	Type         string // 任务类型
	State        string // 任务状态
	Queue        string // 队列
	PeriodicName string // 周期任务名称
	WorkflowID   string // 工作流实例 ID
	WorkflowName string // 工作流名称
	WorkflowNode string // 工作流节点
	StartedAt    string // 开始时间
	FinishedAt   string // 完成或失败时间
	DurationMS   int64  // 耗时毫秒
	Error        string // 错误摘要
}

// SendTaskDailyReport 发送任务系统运行日报。
func (n *Notifier) SendTaskDailyReport(ctx context.Context, report TaskDailyReport) error {
	if n == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return n.sendCard(ctx, n.formatTaskDailyReportCard(report))
}

// formatTaskDailyReportCard 构造每日任务运行汇总卡片。
func (n *Notifier) formatTaskDailyReportCard(report TaskDailyReport) messageCard {
	generatedAt := report.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = n.now()
	}
	elements := []messageCardElement{
		cardMarkdown("**状态**：%s\n**窗口**：%s\n**口径**：periodic 来源的 completed/archived 任务；工作流按实例状态汇总",
			taskDailyReportStatus(report),
			formatCardWindow(report.WindowStart, report.WindowEnd),
		),
		cardFields([][2]string{
			{"服务", report.ServiceName},
			{"环境 / 站点", joinNonEmpty(" / ", report.Environment, report.AppID)},
			{"生成时间", formatCardTime(generatedAt)},
			{"队列积压", taskDailyReportBacklogText(report.Queues)},
		}),
		cardDivider(),
		cardMarkdown("**总览**\n%s", strings.Join(taskDailyReportSummaryLines(report), "\n")),
	}
	if focus := taskDailyReportFocusLines(report); len(focus) > 0 {
		elements = append(elements, cardMarkdown("**重点关注**\n%s", strings.Join(focus, "\n")))
	}
	if len(report.FailureTasks) > 0 {
		elements = append(elements, cardMarkdown("**失败明细**\n%s", strings.Join(n.taskDailyReportFailureLines(report.FailureTasks), "\n")))
	}
	elements = append(elements,
		cardMarkdown("**周期任务 Top**\n%s", strings.Join(taskDailyReportItemLines(report.PeriodicTasks, "periodic"), "\n")),
		cardMarkdown("**工作流 Top**\n%s", strings.Join(taskDailyReportItemLines(report.Workflows, "workflow"), "\n")),
	)
	if len(report.SlowTasks) > 0 {
		elements = append(elements, cardMarkdown("**慢任务 Top**\n%s", strings.Join(taskDailyReportSlowLines(report.SlowTasks), "\n")))
	}
	elements = append(elements, cardMarkdown("**处理建议**\n%s", strings.Join(taskDailyReportAdviceLines(report), "\n")))
	if n.atAll && (report.FailedTaskExecutions > 0 || report.WorkflowFailed > 0) {
		elements = append(elements, cardMarkdown("<at id=all></at>"))
	}
	return messageCard{
		Config: messageCardConfig{WideScreenMode: true},
		Header: &messageCardHeader{
			Template: taskDailyReportCardTemplate(report),
			Title: messageCardText{
				Tag:     "plain_text",
				Content: taskDailyReportCardTitle(report),
			},
		},
		Elements: elements,
	}
}

// taskDailyReportCardTitle 根据日报异常级别生成卡片标题。
func taskDailyReportCardTitle(report TaskDailyReport) string {
	if report.FailedTaskExecutions > 0 || report.WorkflowFailed > 0 {
		return "P3 任务运行日报 | 存在失败"
	}
	if report.WorkflowRunning > 0 || report.WorkflowUnknown > 0 || report.Truncated || strings.TrimSpace(report.RetentionWarning) != "" {
		return "P3 任务运行日报 | 需关注"
	}
	return "P3 任务运行日报 | 正常"
}

// taskDailyReportCardTemplate 根据日报异常级别选择卡片颜色。
func taskDailyReportCardTemplate(report TaskDailyReport) string {
	if report.FailedTaskExecutions > 0 || report.WorkflowFailed > 0 {
		return "red"
	}
	if report.WorkflowRunning > 0 || report.WorkflowUnknown > 0 || report.Truncated || strings.TrimSpace(report.RetentionWarning) != "" {
		return "orange"
	}
	return "green"
}

// taskDailyReportSummaryLines 生成日报总览指标行。
func taskDailyReportSummaryLines(report TaskDailyReport) []string {
	lines := []string{
		"- 周期触发：" + countText(report.PeriodicTriggerTotal, report.PeriodicTriggerOK, report.PeriodicTriggerFailed),
		"- 工作流：" + workflowCountText(report),
		"- 任务执行：" + countText(report.TotalTaskExecutions, report.SuccessTaskExecutions, report.FailedTaskExecutions),
		"- 节点任务：" + strconv.Itoa(report.NodeTaskTotal) + " 次",
	}
	if report.AverageDurationMS > 0 || report.MaxDurationMS > 0 {
		lines = append(lines, "- 耗时：平均 "+durationMSText(report.AverageDurationMS)+"，最长 "+durationMSText(report.MaxDurationMS))
	}
	if report.TraceTotalCount > 0 {
		lines = append(lines, "- 处理量：总 "+formatInt64(report.TraceTotalCount)+"，读 "+formatInt64(report.TraceReadCount)+"，写 "+formatInt64(report.TraceWriteCount)+"，删 "+formatInt64(report.TraceDeleteCount)+"，错 "+formatInt64(report.TraceErrorCount))
	}
	return lines
}

// taskDailyReportFocusLines 生成需要人工关注的异常摘要。
func taskDailyReportFocusLines(report TaskDailyReport) []string {
	lines := make([]string, 0, 4)
	if report.FailedTaskExecutions > 0 || report.WorkflowFailed > 0 {
		lines = append(lines, "- 失败：任务 "+strconv.Itoa(report.FailedTaskExecutions)+"，工作流 "+strconv.Itoa(report.WorkflowFailed))
	}
	if report.WorkflowRunning > 0 || report.WorkflowUnknown > 0 {
		lines = append(lines, "- 工作流状态：运行中 "+strconv.Itoa(report.WorkflowRunning)+"，未知 "+strconv.Itoa(report.WorkflowUnknown))
	}
	if warning := strings.TrimSpace(report.RetentionWarning); warning != "" {
		lines = append(lines, "- 数据完整性："+warning)
	}
	if report.Truncated {
		lines = append(lines, "- 统计已截断：请缩小窗口或提高 completed 保留时间后复核")
	}
	return lines
}

// taskDailyReportFailureLines 生成失败任务明细并限制展示数量。
func (n *Notifier) taskDailyReportFailureLines(items []TaskDailyReportTask) []string {
	if len(items) == 0 {
		return []string{"- 无终态失败任务"}
	}
	limit := minInt(len(items), taskDailyReportCardDetailLimit)
	lines := make([]string, 0, limit)
	for idx := 0; idx < limit; idx++ {
		item := items[idx]
		name := cardValue(item.Name, item.Type)
		parts := []string{
			strconv.Itoa(idx+1) + ". " + shortCardText(name, 64),
		}
		if item.WorkflowName != "" || item.WorkflowNode != "" {
			parts = append(parts, "工作流 "+shortCardText(joinNonEmpty("/", item.WorkflowName, item.WorkflowNode), 72))
		}
		if item.ID != "" {
			parts = append(parts, "任务ID "+shortCardText(item.ID, 32))
		}
		if item.DurationMS > 0 {
			parts = append(parts, "耗时 "+durationMSText(item.DurationMS))
		}
		if errText := n.truncateText(item.Error); errText != "" {
			parts = append(parts, "错误 "+shortCardText(errText, 120))
		}
		lines = append(lines, "- "+strings.Join(parts, "；"))
	}
	if len(items) > limit {
		lines = append(lines, "- 其余 "+strconv.Itoa(len(items)-limit)+" 条请在任务中心按任务ID/工作流ID检索")
	}
	return lines
}

// taskDailyReportItemLines 生成周期任务或工作流 Top 展示行。
func taskDailyReportItemLines(items []TaskDailyReportItem, kind string) []string {
	if len(items) == 0 {
		if kind == "workflow" {
			return []string{"- 窗口内未发现周期来源工作流"}
		}
		return []string{"- 窗口内未发现周期来源任务"}
	}
	limit := minInt(len(items), taskDailyReportCardTopLimit)
	lines := make([]string, 0, limit)
	for idx := 0; idx < limit; idx++ {
		item := items[idx]
		name := cardValue(item.Name, "unknown")
		metrics := []string{}
		if kind == "workflow" {
			metrics = append(metrics,
				"实例 "+strconv.Itoa(item.TaskExecutions),
				"成功 "+strconv.Itoa(item.Success),
				"失败 "+strconv.Itoa(item.Failed),
				"运行 "+strconv.Itoa(item.Running),
			)
		} else {
			metrics = append(metrics,
				"执行 "+strconv.Itoa(item.TaskExecutions),
				"成功 "+strconv.Itoa(item.Success),
				"失败 "+strconv.Itoa(item.Failed),
				"触发 "+strconv.Itoa(item.Triggers),
			)
		}
		if item.AverageMS > 0 || item.MaxMS > 0 {
			metrics = append(metrics, "均 "+durationMSText(item.AverageMS), "最长 "+durationMSText(item.MaxMS))
		}
		line := "- " + strconv.Itoa(idx+1) + ". " + shortCardText(name, 72) + "｜" + strings.Join(metrics, " / ")
		if item.Related != "" {
			line += "\n  关联：" + shortCardText(item.Related, 80)
		}
		lines = append(lines, line)
	}
	if len(items) > limit {
		lines = append(lines, "- 其余 "+strconv.Itoa(len(items)-limit)+" 项已折叠")
	}
	return lines
}

// taskDailyReportSlowLines 生成慢任务 Top 展示行。
func taskDailyReportSlowLines(items []TaskDailyReportTask) []string {
	if len(items) == 0 {
		return []string{"- 无耗时样本"}
	}
	limit := minInt(len(items), taskDailyReportCardDetailLimit)
	lines := make([]string, 0, limit)
	for idx := 0; idx < limit; idx++ {
		item := items[idx]
		name := cardValue(item.Name, item.Type)
		parts := []string{
			strconv.Itoa(idx+1) + ". " + shortCardText(name, 72),
			"耗时 " + durationMSText(item.DurationMS),
		}
		if item.WorkflowName != "" {
			parts = append(parts, "工作流 "+shortCardText(item.WorkflowName, 80))
		}
		lines = append(lines, "- "+strings.Join(parts, "；"))
	}
	if len(items) > limit {
		lines = append(lines, "- 其余 "+strconv.Itoa(len(items)-limit)+" 条已折叠")
	}
	return lines
}

// taskDailyReportAdviceLines 生成日报处理建议列表。
func taskDailyReportAdviceLines(report TaskDailyReport) []string {
	rawItems := taskDailyReportAdvice(report)
	if len(rawItems) == 0 {
		return []string{"- 继续观察队列积压和慢任务变化"}
	}
	items := make([]string, 0, len(rawItems))
	for _, item := range rawItems {
		items = append(items, "- "+item)
	}
	return items
}

// taskDailyReportStatus 生成日报状态摘要。
func taskDailyReportStatus(report TaskDailyReport) string {
	if report.FailedTaskExecutions > 0 || report.WorkflowFailed > 0 {
		return "存在终态失败，请优先处理失败任务和异常工作流"
	}
	if report.WorkflowRunning > 0 {
		return "存在跨窗口仍在运行的工作流，请观察是否超时"
	}
	return "周期任务与工作流运行正常"
}

// taskDailyReportBacklogText 汇总各队列当前积压数据。
func taskDailyReportBacklogText(items []TaskDailyReportQueue) string {
	if len(items) == 0 {
		return "无队列数据"
	}
	var pending, active, retry, archived int
	for _, item := range items {
		pending += item.Pending
		active += item.Active
		retry += item.Retry
		archived += item.Archived
	}
	return "pending " + strconv.Itoa(pending) +
		" / active " + strconv.Itoa(active) +
		" / retry " + strconv.Itoa(retry) +
		" / archived " + strconv.Itoa(archived)
}

// countText 生成总数、成功数和失败数展示文本。
func countText(total, success, failed int) string {
	return strconv.Itoa(total) + " 次，成功 " + strconv.Itoa(success) + "，失败 " + strconv.Itoa(failed)
}

// workflowCountText 生成工作流实例状态汇总文本。
func workflowCountText(report TaskDailyReport) string {
	return strconv.Itoa(report.WorkflowTotal) +
		" 个，成功 " + strconv.Itoa(report.WorkflowSuccess) +
		"，失败 " + strconv.Itoa(report.WorkflowFailed) +
		"，运行中 " + strconv.Itoa(report.WorkflowRunning) +
		"，未知 " + strconv.Itoa(report.WorkflowUnknown)
}

// taskDailyReportAdvice 根据日报异常状态生成处理建议。
func taskDailyReportAdvice(report TaskDailyReport) []string {
	advice := make([]string, 0, 4)
	if warning := strings.TrimSpace(report.RetentionWarning); warning != "" {
		advice = append(advice, "数据完整性："+warning)
	}
	if report.Truncated {
		advice = append(advice, "统计结果已达到聚合上限，请缩小窗口或提高任务 completed 保留时间后复核。")
	}
	if report.FailedTaskExecutions > 0 || report.WorkflowFailed > 0 {
		advice = append(advice, "按失败明细中的任务ID或工作流ID进入任务中心检索执行日志，确认依赖、数据影响范围和是否需要人工重试。")
	} else {
		advice = append(advice, "当前窗口无终态失败；继续观察慢任务 Top 和队列积压变化即可。")
	}
	if report.WorkflowUnknown > 0 {
		advice = append(advice, "存在未知工作流状态，通常表示工作流元数据已过期；可结合节点任务明细和队列保留时间确认是否需要补查。")
	}
	return advice
}

// durationMSText 将毫秒数格式化为 Go duration 文本。
func durationMSText(ms int64) string {
	if ms <= 0 {
		return "0ms"
	}
	return (time.Duration(ms) * time.Millisecond).String()
}

// formatInt64 统一格式化 int64 指标值。
func formatInt64(value int64) string {
	return strconv.FormatInt(value, 10)
}

// minInt 返回两个整数中的较小值。
func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
