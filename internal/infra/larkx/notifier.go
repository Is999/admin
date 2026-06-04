package larkx

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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

// messagePayload 定义 Lark 群机器人 text 消息请求体。
type messagePayload struct {
	Timestamp string         `json:"timestamp,omitempty"` // 秒级时间戳，启用签名时必填
	Sign      string         `json:"sign,omitempty"`      // Lark 签名值，启用签名时必填
	MsgType   string         `json:"msg_type"`            // 消息类型，当前固定为 text
	Content   messageContent `json:"content"`             // text 消息正文
}

// messageContent 定义 Lark text 消息内容。
type messageContent struct {
	Text string `json:"text"` // 文本消息内容
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
	return n.sendText(ctx, n.formatTaskFailureText(alert))
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
		Content: messageContent{Text: text},
	}
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

// formatTaskFailureText 构造面向生产排障的 Lark 告警文本。
func (n *Notifier) formatTaskFailureText(alert TaskFailureAlert) string {
	occurredAt := alert.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = n.now()
	}
	lines := []string{
		"【P1 后台任务终态失败】",
		"状态：已归档失败，不会继续自动重试",
	}
	// 只输出非空字段，避免告警中出现无意义占位。
	appendLine := func(label string, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			lines = append(lines, "- "+label+"："+value)
		}
	}
	appendSection := func(title string) {
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		lines = append(lines, title)
	}
	appendLine("服务", alert.ServiceName)
	appendLine("环境", alert.Environment)
	appendLine("站点", alert.AppID)
	lines = append(lines, "- 失败时间："+occurredAt.Format(time.RFC3339))

	appendSection("任务")
	appendLine("名称", alert.TaskName)
	appendLine("类型", alert.TaskType)
	appendLine("来源", alert.TaskSource)
	appendLine("队列", alert.TaskQueue)
	appendLine("任务ID", alert.TaskID)

	appendSection("工作流")
	appendLine("名称", alert.WorkflowName)
	appendLine("工作流ID", alert.WorkflowID)
	appendLine("节点", alert.WorkflowNode)
	appendLine("模式", alert.Mode)
	if alert.ShardTotal > 0 {
		lines = append(lines, "- 分片："+strconv.Itoa(alert.ShardIndex)+"/"+strconv.Itoa(alert.ShardTotal))
	}
	appendLine("TraceID", alert.TraceID)
	if errText := n.truncateError(alert.Err); errText != "" {
		appendSection("错误摘要")
		lines = append(lines, errText)
	}
	appendSection("处理建议")
	lines = append(lines, "请在任务中心按任务ID或工作流ID检索执行日志，确认依赖状态、数据影响范围和重试策略后再处理。")
	if n.atAll {
		lines = append(lines, "")
		lines = append(lines, `<at user_id="all">所有人</at>`)
	}
	return strings.Join(lines, "\n")
}

// truncateError 规范化错误摘要并按配置限制长度。
func (n *Notifier) truncateError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.Join(strings.Fields(err.Error()), " ")
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
