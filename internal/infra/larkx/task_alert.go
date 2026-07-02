package larkx

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// TaskFailureAlert 描述一次后台任务终态失败告警。
type TaskFailureAlert struct {
	ServiceName  string    // 服务名
	Environment  string    // 运行环境
	AppID        string    // 站点/应用 ID
	TaskID       string    // Asynq 任务 ID
	TaskType     string    // 任务类型
	TaskName     string    // 任务展示名
	TaskQueue    string    // 任务队列
	TaskSource   string    // 任务来源
	WorkflowID   string    // 工作流实例 ID
	WorkflowName string    // 工作流名称
	WorkflowNode string    // 工作流节点
	Mode         string    // 业务运行模式
	ShardIndex   int       // 分片下标
	ShardTotal   int       // 分片总数
	TraceID      string    // 链路追踪 ID
	OccurredAt   time.Time // 失败时间
	Err          error     // 原始错误
}

// PeriodicConfigAlert 描述一次周期任务配置异常告警。
type PeriodicConfigAlert struct {
	ServiceName  string    // 服务名
	Environment  string    // 运行环境
	AppID        string    // 站点/应用 ID
	TaskIndex    int       // 周期任务配置序号
	TaskName     string    // 周期任务名称
	WorkflowName string    // 引用的工作流名称
	Cron         string    // Cron 表达式
	EverySeconds int       // 固定间隔秒数
	TaskQueue    string    // 投递队列
	UniqueKey    string    // 幂等键
	OccurredAt   time.Time // 发现时间
	Reason       string    // 配置异常原因
	TriggerCount int       // 当前告警窗口累计触发次数
}

// TaskRuntimeAlert 描述一次任务系统运行异常告警。
type TaskRuntimeAlert struct {
	ServiceName  string    // 服务名
	Environment  string    // 运行环境
	AppID        string    // 站点/应用 ID
	Kind         string    // 异常类型
	Title        string    // 告警标题
	Status       string    // 当前处理状态
	Component    string    // 发生异常的组件
	Operation    string    // 发生异常的操作
	TaskName     string    // 关联任务名称
	TaskType     string    // 关联任务类型
	WorkflowName string    // 关联工作流名称
	Cron         string    // 关联周期表达式
	TaskQueue    string    // 关联队列
	UniqueKey    string    // 关联幂等键
	OccurredAt   time.Time // 发现时间
	Reason       string    // 异常原因
	Advice       string    // 处理建议
	TriggerCount int       // 当前告警窗口累计触发次数
}

// SendTaskFailure 发送后台任务终态失败告警。
func (n *Notifier) SendTaskFailure(ctx context.Context, alert TaskFailureAlert) error {
	if n == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return n.sendCard(ctx, n.formatTaskFailureCard(alert))
}

// SendPeriodicConfigInvalid 发送周期任务配置异常告警。
func (n *Notifier) SendPeriodicConfigInvalid(ctx context.Context, alert PeriodicConfigAlert) error {
	if n == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return n.sendCard(ctx, n.formatPeriodicConfigCard(alert))
}

// SendTaskRuntimeAlert 发送任务系统运行异常告警。
func (n *Notifier) SendTaskRuntimeAlert(ctx context.Context, alert TaskRuntimeAlert) error {
	if n == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return n.sendCard(ctx, n.formatTaskRuntimeCard(alert))
}

// formatTaskFailureCard 构造后台任务终态失败告警卡片。
func (n *Notifier) formatTaskFailureCard(alert TaskFailureAlert) messageCard {
	occurredAt := alert.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = n.now()
	}
	elements := []messageCardElement{
		cardMarkdown("**状态**：已归档失败，不会继续自动重试\n**失败时间**：%s", formatCardTime(occurredAt)),
		cardFieldsCompact([][2]string{
			{"服务", alert.ServiceName},
			{"环境 / 站点", joinNonEmpty(" / ", alert.Environment, alert.AppID)},
			{"任务", alert.TaskName},
			{"任务类型", alert.TaskType},
			{"队列 / 来源", joinNonEmpty(" / ", alert.TaskQueue, alert.TaskSource)},
			{"任务ID", alert.TaskID},
			{"工作流", alert.WorkflowName},
			{"工作流ID", alert.WorkflowID},
			{"节点 / 模式", joinNonEmpty(" / ", alert.WorkflowNode, alert.Mode)},
			{"分片", shardText(alert.ShardIndex, alert.ShardTotal)},
			{"TraceID", alert.TraceID},
		}),
	}
	if errText := n.truncateError(alert.Err); errText != "" {
		elements = append(elements, cardMarkdown("**错误摘要**\n%s", shortCardText(errText, n.maxErrorByte)))
	}
	elements = append(elements, cardMarkdown("**处理建议**\n- 在任务中心按任务ID或工作流ID检索执行日志\n- 先确认依赖状态、数据影响范围和幂等性，再决定是否人工重试"))
	if n.atAll {
		elements = append(elements, cardMarkdown("<at id=all></at>"))
	}
	return messageCard{
		Config: messageCardConfig{WideScreenMode: true},
		Header: &messageCardHeader{
			Template: "red",
			Title: messageCardText{
				Tag:     "plain_text",
				Content: "P1 后台任务终态失败",
			},
		},
		Elements: elements,
	}
}

// formatPeriodicConfigCard 构造周期任务配置异常告警卡片。
func (n *Notifier) formatPeriodicConfigCard(alert PeriodicConfigAlert) messageCard {
	occurredAt := alert.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = n.now()
	}
	elements := []messageCardElement{
		cardMarkdown("**状态**：已跳过该周期任务，调度器继续运行\n**发现时间**：%s", formatCardTime(occurredAt)),
		cardFieldsCompact([][2]string{
			{"服务", alert.ServiceName},
			{"环境 / 站点", joinNonEmpty(" / ", alert.Environment, alert.AppID)},
			{"序号", taskAlertIndexText(alert.TaskIndex)},
			{"名称", alert.TaskName},
			{"工作流", alert.WorkflowName},
			{"Cron / Every", periodicScheduleText(alert.Cron, alert.EverySeconds)},
			{"队列", alert.TaskQueue},
			{"去重键", alert.UniqueKey},
			{"窗口触发次数", triggerCountText(alert.TriggerCount)},
		}),
	}
	if reason := n.truncateText(alert.Reason); reason != "" {
		elements = append(elements, cardMarkdown("**错误摘要**\n%s", reason))
	}
	elements = append(elements, cardMarkdown("**影响与建议**\n- 该周期任务不会注册到调度器，本轮不会自动投递\n- 检查 active release/草稿表 workflow 与当前镜像注册工作流是否一致\n- 新插件需先发布代码并重启 scheduler/worker；废弃配置请禁用或修正后重新发布"))
	if n.atAll {
		elements = append(elements, cardMarkdown("<at id=all></at>"))
	}
	return messageCard{
		Config: messageCardConfig{WideScreenMode: true},
		Header: &messageCardHeader{
			Template: "red",
			Title: messageCardText{
				Tag:     "plain_text",
				Content: "P1 周期任务配置异常",
			},
		},
		Elements: elements,
	}
}

// formatTaskRuntimeCard 构造任务系统运行异常告警卡片。
func (n *Notifier) formatTaskRuntimeCard(alert TaskRuntimeAlert) messageCard {
	occurredAt := alert.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = n.now()
	}
	status := strings.TrimSpace(alert.Status)
	if status == "" {
		status = "任务系统外层运行操作失败，需要人工确认"
	}
	elements := []messageCardElement{
		cardMarkdown("**状态**：%s\n**发现时间**：%s", status, formatCardTime(occurredAt)),
		cardFieldsCompact([][2]string{
			{"服务", alert.ServiceName},
			{"环境 / 站点", joinNonEmpty(" / ", alert.Environment, alert.AppID)},
			{"类型", alert.Kind},
			{"组件 / 动作", joinNonEmpty(" / ", alert.Component, alert.Operation)},
			{"任务", alert.TaskName},
			{"任务类型", alert.TaskType},
			{"工作流", alert.WorkflowName},
			{"Cron", alert.Cron},
			{"队列", alert.TaskQueue},
			{"去重键", alert.UniqueKey},
			{"窗口触发次数", triggerCountText(alert.TriggerCount)},
		}),
	}
	if reason := n.truncateText(alert.Reason); reason != "" {
		elements = append(elements, cardMarkdown("**错误摘要**\n%s", reason))
	}
	elements = append(elements, cardMarkdown("**处理建议**\n%s", taskRuntimeAdviceText(n, alert.Advice)))
	if n.atAll {
		elements = append(elements, cardMarkdown("<at id=all></at>"))
	}
	return messageCard{
		Config: messageCardConfig{WideScreenMode: true},
		Header: &messageCardHeader{
			Template: "red",
			Title: messageCardText{
				Tag:     "plain_text",
				Content: taskRuntimeCardTitle(alert.Title),
			},
		},
		Elements: elements,
	}
}

// taskRuntimeCardTitle 规范化任务运行异常卡片标题。
func taskRuntimeCardTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "P1 任务系统运行异常"
	}
	title = strings.TrimPrefix(title, "【")
	title = strings.TrimSuffix(title, "】")
	return strings.TrimSpace(title)
}

// taskRuntimeAdviceText 生成任务运行异常处理建议。
func taskRuntimeAdviceText(n *Notifier, advice string) string {
	if advice := n.truncateText(advice); advice != "" {
		return "- " + advice
	}
	return "- 检查任务运行配置 active release、Scheduler leader、task.redis/Asynq 队列状态\n- 核对当前镜像是否已注册对应 workflow/handler\n- 恢复后观察任务队列和 Scheduler 状态是否回到成功"
}

// periodicScheduleText 拼接周期任务调度表达式。
func periodicScheduleText(cron string, everySeconds int) string {
	parts := make([]string, 0, 2)
	if cron = strings.TrimSpace(cron); cron != "" {
		parts = append(parts, "cron "+cron)
	}
	if everySeconds > 0 {
		parts = append(parts, "every "+strconv.Itoa(everySeconds)+"s")
	}
	return strings.Join(parts, " / ")
}

// taskAlertIndexText 规范化周期任务配置序号。
func taskAlertIndexText(index int) string {
	if index < 0 {
		return ""
	}
	return strconv.Itoa(index)
}

// triggerCountText 只在重复触发时展示窗口触发次数。
func triggerCountText(count int) string {
	if count <= 1 {
		return ""
	}
	return strconv.Itoa(count)
}

// shardText 生成任务分片展示文本。
func shardText(index, total int) string {
	if total <= 0 {
		return ""
	}
	return strconv.Itoa(index) + "/" + strconv.Itoa(total)
}
