package larkx

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
)

const (
	// defaultTimeoutSeconds 是 Lark HTTP 请求默认超时时间。
	defaultTimeoutSeconds = 5
	// defaultMaxErrorBytes 是告警错误摘要默认最大字节数。
	defaultMaxErrorBytes = 800
	// maxTimeoutSeconds 限制 Lark HTTP 请求最大超时时间，避免任务收尾阶段长时间阻塞。
	maxTimeoutSeconds = 30
	// taskDailyReportCardTopLimit 控制日报卡片中 Top 列表的展示数量。
	taskDailyReportCardTopLimit = 5
	// taskDailyReportCardDetailLimit 控制日报卡片中异常和慢任务明细数量。
	taskDailyReportCardDetailLimit = 3
)

// Notifier 负责向 Lark 群机器人发送告警。
type Notifier struct {
	webhookURL   string           // Lark webhook URL
	secret       string           // Lark 签名密钥
	atAll        bool             // 是否 @所有人
	maxErrorByte int              // 错误摘要最大字节数
	client       *http.Client     // HTTP 客户端
	now          func() time.Time // 当前时间函数，测试中可替换
}

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

// messagePayload 定义 Lark 群机器人消息请求体。
type messagePayload struct {
	Timestamp string          `json:"timestamp,omitempty"` // 秒级时间戳，启用签名时必填
	Sign      string          `json:"sign,omitempty"`      // Lark 签名值，启用签名时必填
	MsgType   string          `json:"msg_type"`            // 消息类型，支持 text 和 interactive
	Content   *messageContent `json:"content,omitempty"`   // text 消息正文
	Card      *messageCard    `json:"card,omitempty"`      // interactive 消息卡片
}

// messageContent 定义 Lark text 消息内容。
type messageContent struct {
	Text string `json:"text"` // 文本消息内容
}

// messageCard 定义 Lark interactive 卡片内容。
type messageCard struct {
	Config   messageCardConfig    `json:"config,omitempty"`   // 卡片展示配置
	Header   *messageCardHeader   `json:"header,omitempty"`   // 卡片标题
	Elements []messageCardElement `json:"elements,omitempty"` // 卡片正文元素
}

// messageCardConfig 定义 Lark 卡片展示配置。
type messageCardConfig struct {
	WideScreenMode bool `json:"wide_screen_mode"` // 是否启用宽屏模式
}

// messageCardHeader 定义 Lark 卡片头部。
type messageCardHeader struct {
	Template string          `json:"template,omitempty"` // 头部颜色模板
	Title    messageCardText `json:"title"`              // 头部标题
}

// messageCardElement 定义 Lark 卡片正文元素。
type messageCardElement struct {
	Tag      string             `json:"tag"`                // 元素类型
	Text     *messageCardText   `json:"text,omitempty"`     // 文本内容
	Fields   []messageCardField `json:"fields,omitempty"`   // 字段布局
	Elements []messageCardText  `json:"elements,omitempty"` // note 元素内容
}

// messageCardField 定义 Lark 卡片字段布局项。
type messageCardField struct {
	IsShort bool            `json:"is_short"` // 是否按短字段展示
	Text    messageCardText `json:"text"`     // 字段文本
}

// messageCardText 定义 Lark 卡片文本节点。
type messageCardText struct {
	Tag     string `json:"tag"`     // 文本类型，plain_text 或 lark_md
	Content string `json:"content"` // 文本内容
}

// responsePayload 定义 Lark 机器人响应字段。
type responsePayload struct {
	Code          *int   `json:"code,omitempty"`          // 小写响应业务码，0 表示成功
	Msg           string `json:"msg,omitempty"`           // 小写响应消息
	StatusCode    *int   `json:"StatusCode,omitempty"`    // 大写响应业务码，0 表示成功
	StatusMessage string `json:"StatusMessage,omitempty"` // 大写响应消息
}

// New 创建 Lark 告警器；未启用时返回 nil。
func New(cfg config.LarkAlertConfig) (*Notifier, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	webhookURL, err := resolveConfigValue(cfg.WebhookURL, cfg.WebhookURLRef)
	if err != nil {
		return nil, errors.Wrap(err, "读取 Lark webhook 配置失败")
	}
	if webhookURL == "" {
		return nil, errors.Errorf("缺少 Lark webhook_url 配置")
	}
	if err := validateWebhookURL(webhookURL); err != nil {
		return nil, errors.Tag(err)
	}
	secret, err := resolveConfigValue(cfg.Secret, cfg.SecretRef)
	if err != nil {
		return nil, errors.Wrap(err, "读取 Lark secret 配置失败")
	}
	timeout := boundedTimeout(cfg.TimeoutSeconds)
	maxErrorBytes := cfg.MaxErrorBytes
	if maxErrorBytes <= 0 {
		maxErrorBytes = defaultMaxErrorBytes
	}
	return &Notifier{
		webhookURL:   webhookURL,
		secret:       secret,
		atAll:        cfg.AtAll,
		maxErrorByte: maxErrorBytes,
		client:       &http.Client{Timeout: timeout},
		now:          time.Now,
	}, nil
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

// SendText 发送一条通用文本消息。
func (n *Notifier) SendText(ctx context.Context, text string) error {
	if n == nil {
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.Errorf("Lark 文本消息为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return n.sendText(ctx, text)
}

// sendText 复用 Lark 文本消息协议发送内容。
func (n *Notifier) sendText(ctx context.Context, text string) error {
	payload := messagePayload{
		MsgType: "text",
		Content: &messageContent{Text: text},
	}
	return n.sendPayload(ctx, payload)
}

// sendCard 复用 Lark interactive 消息协议发送卡片内容。
func (n *Notifier) sendCard(ctx context.Context, card messageCard) error {
	payload := messagePayload{
		MsgType: "interactive",
		Card:    &card,
	}
	return n.sendPayload(ctx, payload)
}

// sendPayload 统一补齐签名并发送 Lark webhook 请求。
func (n *Notifier) sendPayload(ctx context.Context, payload messagePayload) error {
	if n.secret != "" {
		timestamp := strconv.FormatInt(n.now().Unix(), 10)
		payload.Timestamp = timestamp
		payload.Sign = sign(timestamp, n.secret)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.Tag(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(body))
	if err != nil {
		return errors.Tag(err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := n.client.Do(req)
	if err != nil {
		return errors.Tag(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return checkResponse(resp.StatusCode, respBody)
}

// formatTaskFailureCard 构造后台任务终态失败告警卡片。
func (n *Notifier) formatTaskFailureCard(alert TaskFailureAlert) messageCard {
	occurredAt := alert.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = n.now()
	}
	elements := []messageCardElement{
		cardMarkdown("**状态**：已归档失败，不会继续自动重试\n**失败时间**：%s", formatReportCardTime(occurredAt)),
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
		elements = append(elements, cardMarkdown("**错误摘要**\n%s", shortReportText(errText, n.maxErrorByte)))
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
		cardMarkdown("**状态**：已跳过该周期任务，调度器继续运行\n**发现时间**：%s", formatReportCardTime(occurredAt)),
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
		cardMarkdown("**状态**：%s\n**发现时间**：%s", status, formatReportCardTime(occurredAt)),
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

// formatTaskDailyReportCard 构造每日任务运行汇总卡片。
func (n *Notifier) formatTaskDailyReportCard(report TaskDailyReport) messageCard {
	generatedAt := report.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = n.now()
	}
	elements := []messageCardElement{
		cardMarkdown("**状态**：%s\n**窗口**：%s\n**口径**：periodic 来源的 completed/archived 任务；工作流按实例状态汇总",
			taskDailyReportStatus(report),
			formatReportCardWindow(report.WindowStart, report.WindowEnd),
		),
		cardFields([][2]string{
			{"服务", report.ServiceName},
			{"环境 / 站点", joinNonEmpty(" / ", report.Environment, report.AppID)},
			{"生成时间", formatReportCardTime(generatedAt)},
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

func cardMarkdown(format string, args ...any) messageCardElement {
	return messageCardElement{
		Tag: "div",
		Text: &messageCardText{
			Tag:     "lark_md",
			Content: strings.TrimSpace(fmt.Sprintf(format, args...)),
		},
	}
}

func cardFields(items [][2]string) messageCardElement {
	fields := make([]messageCardField, 0, len(items))
	for _, item := range items {
		label := strings.TrimSpace(item[0])
		value := helperTaskDailyReportValue(item[1], "-")
		if label == "" {
			continue
		}
		fields = append(fields, messageCardField{
			IsShort: true,
			Text: messageCardText{
				Tag:     "lark_md",
				Content: "**" + label + "**\n" + value,
			},
		})
	}
	return messageCardElement{Tag: "div", Fields: fields}
}

func cardFieldsCompact(items [][2]string) messageCardElement {
	fields := make([]messageCardField, 0, len(items))
	for _, item := range items {
		label := strings.TrimSpace(item[0])
		value := strings.TrimSpace(item[1])
		if label == "" || value == "" {
			continue
		}
		fields = append(fields, messageCardField{
			IsShort: true,
			Text: messageCardText{
				Tag:     "lark_md",
				Content: "**" + label + "**\n" + value,
			},
		})
	}
	return messageCardElement{Tag: "div", Fields: fields}
}

func cardDivider() messageCardElement {
	return messageCardElement{Tag: "hr"}
}

func taskDailyReportCardTitle(report TaskDailyReport) string {
	if report.FailedTaskExecutions > 0 || report.WorkflowFailed > 0 {
		return "P3 任务运行日报 | 存在失败"
	}
	if report.WorkflowRunning > 0 || report.WorkflowUnknown > 0 || report.Truncated || strings.TrimSpace(report.RetentionWarning) != "" {
		return "P3 任务运行日报 | 需关注"
	}
	return "P3 任务运行日报 | 正常"
}

func taskDailyReportCardTemplate(report TaskDailyReport) string {
	if report.FailedTaskExecutions > 0 || report.WorkflowFailed > 0 {
		return "red"
	}
	if report.WorkflowRunning > 0 || report.WorkflowUnknown > 0 || report.Truncated || strings.TrimSpace(report.RetentionWarning) != "" {
		return "orange"
	}
	return "green"
}

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

func (n *Notifier) taskDailyReportFailureLines(items []TaskDailyReportTask) []string {
	if len(items) == 0 {
		return []string{"- 无终态失败任务"}
	}
	limit := minInt(len(items), taskDailyReportCardDetailLimit)
	lines := make([]string, 0, limit)
	for idx := 0; idx < limit; idx++ {
		item := items[idx]
		name := helperTaskDailyReportValue(item.Name, item.Type)
		parts := []string{
			strconv.Itoa(idx+1) + ". " + shortReportText(name, 64),
		}
		if item.WorkflowName != "" || item.WorkflowNode != "" {
			parts = append(parts, "工作流 "+shortReportText(joinNonEmpty("/", item.WorkflowName, item.WorkflowNode), 72))
		}
		if item.ID != "" {
			parts = append(parts, "任务ID "+shortReportText(item.ID, 32))
		}
		if item.DurationMS > 0 {
			parts = append(parts, "耗时 "+durationMSText(item.DurationMS))
		}
		if errText := n.truncateText(item.Error); errText != "" {
			parts = append(parts, "错误 "+shortReportText(errText, 120))
		}
		lines = append(lines, "- "+strings.Join(parts, "；"))
	}
	if len(items) > limit {
		lines = append(lines, "- 其余 "+strconv.Itoa(len(items)-limit)+" 条请在任务中心按任务ID/工作流ID检索")
	}
	return lines
}

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
		name := helperTaskDailyReportValue(item.Name, "unknown")
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
		line := "- " + strconv.Itoa(idx+1) + ". " + shortReportText(name, 72) + "｜" + strings.Join(metrics, " / ")
		if item.Related != "" {
			line += "\n  关联：" + shortReportText(item.Related, 80)
		}
		lines = append(lines, line)
	}
	if len(items) > limit {
		lines = append(lines, "- 其余 "+strconv.Itoa(len(items)-limit)+" 项已折叠")
	}
	return lines
}

func taskDailyReportSlowLines(items []TaskDailyReportTask) []string {
	if len(items) == 0 {
		return []string{"- 无耗时样本"}
	}
	limit := minInt(len(items), taskDailyReportCardDetailLimit)
	lines := make([]string, 0, limit)
	for idx := 0; idx < limit; idx++ {
		item := items[idx]
		name := helperTaskDailyReportValue(item.Name, item.Type)
		parts := []string{
			strconv.Itoa(idx+1) + ". " + shortReportText(name, 72),
			"耗时 " + durationMSText(item.DurationMS),
		}
		if item.WorkflowName != "" {
			parts = append(parts, "工作流 "+shortReportText(item.WorkflowName, 80))
		}
		lines = append(lines, "- "+strings.Join(parts, "；"))
	}
	if len(items) > limit {
		lines = append(lines, "- 其余 "+strconv.Itoa(len(items)-limit)+" 条已折叠")
	}
	return lines
}

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

func taskRuntimeCardTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "P1 任务系统运行异常"
	}
	title = strings.TrimPrefix(title, "【")
	title = strings.TrimSuffix(title, "】")
	return strings.TrimSpace(title)
}

func taskRuntimeAdviceText(n *Notifier, advice string) string {
	if advice := n.truncateText(advice); advice != "" {
		return "- " + advice
	}
	return "- 检查任务运行配置 active release、Scheduler leader、task.redis/Asynq 队列状态\n- 核对当前镜像是否已注册对应 workflow/handler\n- 恢复后观察任务队列和 Scheduler 状态是否回到成功"
}

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

func taskAlertIndexText(index int) string {
	if index < 0 {
		return ""
	}
	return strconv.Itoa(index)
}

func triggerCountText(count int) string {
	if count <= 1 {
		return ""
	}
	return strconv.Itoa(count)
}

func shardText(index, total int) string {
	if total <= 0 {
		return ""
	}
	return strconv.Itoa(index) + "/" + strconv.Itoa(total)
}

func taskDailyReportStatus(report TaskDailyReport) string {
	if report.FailedTaskExecutions > 0 || report.WorkflowFailed > 0 {
		return "存在终态失败，请优先处理失败任务和异常工作流"
	}
	if report.WorkflowRunning > 0 {
		return "存在跨窗口仍在运行的工作流，请观察是否超时"
	}
	return "周期任务与工作流运行正常"
}

func formatReportCardWindow(start, end time.Time) string {
	startText := formatReportCardTime(start)
	endText := formatReportCardTime(end)
	if startText == "" || endText == "" {
		return "-"
	}
	return startText + " ~ " + endText
}

func formatReportCardTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02 15:04")
}

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

func countText(total, success, failed int) string {
	return strconv.Itoa(total) + " 次，成功 " + strconv.Itoa(success) + "，失败 " + strconv.Itoa(failed)
}

func workflowCountText(report TaskDailyReport) string {
	return strconv.Itoa(report.WorkflowTotal) +
		" 个，成功 " + strconv.Itoa(report.WorkflowSuccess) +
		"，失败 " + strconv.Itoa(report.WorkflowFailed) +
		"，运行中 " + strconv.Itoa(report.WorkflowRunning) +
		"，未知 " + strconv.Itoa(report.WorkflowUnknown)
}

func helperTaskDailyReportValue(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return fallback
}

func joinNonEmpty(sep string, values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, sep)
}

func shortReportText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return truncateByBytes(value, limit)
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

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

func durationMSText(ms int64) string {
	if ms <= 0 {
		return "0ms"
	}
	return (time.Duration(ms) * time.Millisecond).String()
}

func formatInt64(value int64) string {
	return strconv.FormatInt(value, 10)
}

// truncateError 规范化错误摘要并按配置限制长度。
func (n *Notifier) truncateError(err error) string {
	if err == nil {
		return ""
	}
	return n.truncateText(err.Error())
}

// truncateText 规范化文本摘要并按配置限制长度。
func (n *Notifier) truncateText(text string) string {
	msg := strings.Join(strings.Fields(text), " ")
	if msg == "" {
		return ""
	}
	limit := n.maxErrorByte
	if limit <= 0 {
		limit = defaultMaxErrorBytes
	}
	if len(msg) <= limit {
		return msg
	}
	return truncateByBytes(msg, limit)
}

// resolveConfigValue 优先使用明文配置，未配置时读取 *_ref 指向的文件。
func resolveConfigValue(value string, ref string) (string, error) {
	value = strings.TrimSpace(value)
	if value != "" {
		return value, nil
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		return "", errors.Tag(err)
	}
	return strings.TrimSpace(string(data)), nil
}

// boundedTimeout 返回受默认值和最大值保护的 HTTP 超时时间。
func boundedTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = defaultTimeoutSeconds
	}
	if seconds > maxTimeoutSeconds {
		seconds = maxTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

// validateWebhookURL 在启动期校验 Lark webhook URL 的基础格式。
func validateWebhookURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return errors.Wrap(err, "Lark webhook_url 非法")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.Errorf("Lark webhook_url 仅支持 http/https")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return errors.Errorf("Lark webhook_url 缺少 host")
	}
	return nil
}

// sign 按 Lark 自定义机器人规则生成 HMAC-SHA256 签名。
func sign(timestamp string, secret string) string {
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// truncateByBytes 按字节限制截断文本，并尽量保留省略号空间。
func truncateByBytes(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	const ellipsis = "..."
	if limit <= len(ellipsis) {
		return utf8PrefixByBytes(text, limit)
	}
	return utf8PrefixByBytes(text, limit-len(ellipsis)) + ellipsis
}

// utf8PrefixByBytes 返回不破坏 UTF-8 字符边界的前缀。
func utf8PrefixByBytes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	end := 0
	for i, r := range text {
		size := utf8.RuneLen(r)
		if size < 0 {
			size = 1
		}
		if i+size > limit {
			break
		}
		end = i + size
	}
	return text[:end]
}

// checkResponse 同时校验 HTTP 状态码和 Lark 业务响应码。
func checkResponse(statusCode int, body []byte) error {
	bodyText := strings.TrimSpace(string(body))
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return errors.Errorf("Lark 告警发送失败 status=%d body=%s", statusCode, bodyText)
	}
	if bodyText == "" {
		return nil
	}
	var resp responsePayload
	if err := json.Unmarshal(body, &resp); err != nil {
		return errors.Wrapf(err, "解析 Lark 告警响应失败 body=%s", bodyText)
	}
	if resp.Code != nil && *resp.Code != 0 {
		return errors.Errorf("Lark 告警发送失败 code=%d msg=%s", *resp.Code, strings.TrimSpace(resp.Msg))
	}
	if resp.StatusCode != nil && *resp.StatusCode != 0 {
		return errors.Errorf("Lark 告警发送失败 status_code=%d msg=%s", *resp.StatusCode, strings.TrimSpace(resp.StatusMessage))
	}
	return nil
}
