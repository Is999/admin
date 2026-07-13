package taskalert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"admin/common/runtimecfg"
	"admin/internal/config"
	"admin/internal/infra/collectorx"
	"admin/internal/svc"
	taskqueue "admin/internal/task/queue"
	"admin/internal/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// larkAlertPayload 表示测试使用的辅助结构。
type larkAlertPayload struct {
	MsgType string `json:"msg_type"` // MsgType 表示测试字段。
	// Content 表示 Lark 文本消息内容。
	Content struct {
		Text string `json:"text"` // Text 表示测试字段。
	} `json:"content"`
	// Card 表示 Lark 卡片消息内容。
	Card struct {
		// Header 表示卡片标题区域。
		Header struct {
			Template string `json:"template"` // Template 表示卡片颜色模板。
			// Title 表示卡片标题内容。
			Title struct {
				Content string `json:"content"` // Content 表示卡片标题。
			} `json:"title"`
		} `json:"header"`
		// Elements 表示卡片正文元素列表。
		Elements []struct {
			// Text 表示可选的正文文本。
			Text *struct {
				Content string `json:"content"` // Content 表示卡片文本。
			} `json:"text"`
			// Fields 表示正文中的字段列表。
			Fields []struct {
				// Text 表示字段文本容器。
				Text struct {
					Content string `json:"content"` // Content 表示字段文本。
				} `json:"text"`
			} `json:"fields"`
		} `json:"elements"`
	} `json:"card"`
}

// larkAlertCardText 按展示顺序拼接告警卡片中的可见文本。
func larkAlertCardText(payload larkAlertPayload) string {
	parts := []string{payload.Card.Header.Title.Content}
	for _, element := range payload.Card.Elements {
		if element.Text != nil {
			parts = append(parts, element.Text.Content)
		}
		for _, field := range element.Fields {
			parts = append(parts, field.Text.Content)
		}
	}
	return strings.Join(parts, "\n")
}

// TestLarkRuntimeAlerterWorksWithoutTaskManager 验证任务系统关闭时非任务组件告警仍可发送并限频。
func TestLarkRuntimeAlerterWorksWithoutTaskManager(t *testing.T) {
	payloadCh := make(chan larkAlertPayload, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload larkAlertPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		payloadCh <- payload
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer server.Close()

	alerter, err := NewLarkRuntimeAlerter(config.Config{
		AppID: "site-1",
		Alert: config.AlertConfig{Lark: config.LarkAlertConfig{
			Enabled: true, WebhookURL: server.URL, TimeoutSeconds: 1,
		}},
	})
	if err != nil {
		t.Fatalf("创建独立 Lark 运行告警器失败: %v", err)
	}
	alert := svc.TaskRuntimeAlert{
		Kind: "collector_worker_failed", Component: "collector", Operation: "consume", Reason: "kafka timeout",
	}
	alerter.NotifyRuntimeAlert(context.Background(), alert)
	alert.Reason = "kafka timeout again"
	alerter.NotifyRuntimeAlert(context.Background(), alert)

	select {
	case payload := <-payloadCh:
		if text := larkAlertCardText(payload); !strings.Contains(text, "collector") || !strings.Contains(text, "kafka timeout") {
			t.Fatalf("独立运行告警内容不完整: %s", text)
		}
	case <-time.After(time.Second):
		t.Fatal("任务系统关闭时未收到独立运行告警")
	}
	select {
	case payload := <-payloadCh:
		t.Fatalf("同一指纹告警未限频: %+v", payload)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestLarkRuntimeAlerterRetriesAfterSendFailure 验证发送失败不会误触发限频。
func TestLarkRuntimeAlerterRetriesAfterSendFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer server.Close()

	alerter, err := NewLarkRuntimeAlerter(config.Config{
		Alert: config.AlertConfig{Lark: config.LarkAlertConfig{
			Enabled: true, WebhookURL: server.URL, TimeoutSeconds: 1,
		}},
	})
	if err != nil {
		t.Fatalf("创建独立 Lark 运行告警器失败: %v", err)
	}
	alert := svc.TaskRuntimeAlert{
		Kind: "collector_worker_failed", Component: "collector", Operation: "consume", Reason: "kafka timeout",
	}
	alerter.NotifyRuntimeAlert(context.Background(), alert)
	alerter.NotifyRuntimeAlert(context.Background(), alert)
	if got := requests.Load(); got != 2 {
		t.Fatalf("Lark 请求次数 = %d，期望发送失败后立即重试", got)
	}
}

// TestTaskFailureLarkAlertSendsOnArchivedTask 验证任务进入 archived 终态失败后会真正调用 Lark webhook。
func TestTaskFailureLarkAlertSendsOnArchivedTask(t *testing.T) {
	payloadCh := make(chan larkAlertPayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload larkAlertPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case payloadCh <- payload:
		default:
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer server.Close()

	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		redisServer.Close()
	})
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "site-1"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})

	manager := taskqueue.New(config.TaskQueueConfig{
		Enabled:          true,
		AppID:            "site-1",
		DefaultQueue:     taskqueue.QueueDefault,
		Concurrency:      1,
		TaskCheckSeconds: 1,
	}, client)
	if manager == nil {
		t.Fatal("期望成功创建任务管理器")
	}
	t.Cleanup(func() {
		_ = manager.Stop(context.Background())
	})

	if err := manager.RegisterHandler("demo:lark-final-failure", asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return asynq.SkipRetry
	})); err != nil {
		t.Fatalf("注册 Lark 终态失败测试处理器失败: %v", err)
	}
	larkCfg := config.Config{
		AppID: "site-1",
		Observability: config.ObservabilityConfig{
			ServiceName: "admin",
		},
		Alert: config.AlertConfig{Lark: config.LarkAlertConfig{
			Enabled:        true,
			WebhookURL:     server.URL,
			TimeoutSeconds: 1,
			MaxErrorBytes:  120,
		}},
	}
	larkCfg.Mode = "test"
	if err := RegisterLark(larkCfg, manager); err != nil {
		t.Fatalf("注册 Lark 告警失败: %v", err)
	}
	if err := manager.StartWorker(); err != nil {
		t.Fatalf("启动 worker 失败: %v", err)
	}

	resp, err := manager.EnqueueRegisteredTask(context.Background(), &types.EnqueueTaskReq{
		TaskType: "demo:lark-final-failure",
		Payload: json.RawMessage(`{
			"workflowId":"wf-lark",
			"workflowName":"workflow.lark",
			"workflowNode":"root",
			"mode":"delta",
			"shardTotal":1
		}`),
	})
	if err != nil {
		t.Fatalf("投递 Lark 终态失败测试任务失败: %v", err)
	}

	select {
	case payload := <-payloadCh:
		if payload.MsgType != "interactive" {
			t.Fatalf("Lark 消息类型 = %s，期望 interactive", payload.MsgType)
		}
		if payload.Card.Header.Template != "red" {
			t.Fatalf("Lark 卡片颜色 = %s，期望 red", payload.Card.Header.Template)
		}
		text := larkAlertCardText(payload)
		for _, want := range []string{
			"P1 后台任务终态失败",
			"**环境 / 站点**\ntest / site-1",
			"**任务**\ndemo:lark-final-failure",
			"**任务类型**\ndemo:lark-final-failure",
			"**任务ID**\n" + resp.TaskID,
			"**工作流ID**\nwf-lark",
			"**节点 / 模式**\nroot / delta",
			"**状态**：已归档失败，不会继续自动重试",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("Lark 告警缺少 %q:\n%s", want, text)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("超时前未收到 Lark webhook 请求")
	}
}

// TestPeriodicConfigInvalidLarkAlertSends 验证周期任务配置异常会调用 Lark webhook。
func TestPeriodicConfigInvalidLarkAlertSends(t *testing.T) {
	payloadCh := make(chan larkAlertPayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload larkAlertPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case payloadCh <- payload:
		default:
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer server.Close()

	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		redisServer.Close()
	})
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "site-1"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})

	manager := taskqueue.New(config.TaskQueueConfig{
		Enabled:      true,
		AppID:        "site-1",
		DefaultQueue: taskqueue.QueueDefault,
		Periodic: []config.TaskPeriodicConfig{
			enabledAlertPeriodic(config.TaskPeriodicConfig{
				Name:      "task-report-daily-summary",
				Cron:      "0 10 * * *",
				Workflow:  "task_report.daily_summary",
				Queue:     taskqueue.QueueMaintenance,
				UniqueKey: "periodic:task_report.daily_summary",
			}),
		},
	}, client)
	if manager == nil {
		t.Fatal("期望成功创建任务管理器")
	}

	larkCfg := config.Config{
		AppID: "site-1",
		Observability: config.ObservabilityConfig{
			ServiceName: "admin",
		},
		Alert: config.AlertConfig{Lark: config.LarkAlertConfig{
			Enabled:        true,
			WebhookURL:     server.URL,
			TimeoutSeconds: 1,
			MaxErrorBytes:  120,
		}},
	}
	larkCfg.Mode = "test"
	if err := RegisterLark(larkCfg, manager); err != nil {
		t.Fatalf("注册 Lark 告警失败: %v", err)
	}
	if err := manager.ValidatePeriodicTaskDefinitions(); err != nil {
		t.Fatalf("周期任务定义校验不应阻断启动: %v", err)
	}

	select {
	case payload := <-payloadCh:
		if payload.MsgType != "interactive" {
			t.Fatalf("Lark 消息类型 = %s，期望 interactive", payload.MsgType)
		}
		if payload.Card.Header.Template != "red" {
			t.Fatalf("Lark 卡片颜色 = %s，期望 red", payload.Card.Header.Template)
		}
		text := larkAlertCardText(payload)
		for _, want := range []string{
			"P1 周期任务配置异常",
			"**环境 / 站点**\ntest / site-1",
			"**名称**\ntask-report-daily-summary",
			"**工作流**\ntask_report.daily_summary",
			"**Cron / Every**\ncron 0 10 * * *",
			"**去重键**\nperiodic:task_report.daily_summary",
			"**错误摘要**\n工作流定义不存在",
			"**影响与建议**\n- 该周期任务不会注册到调度器",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("Lark 告警缺少 %q:\n%s", want, text)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("超时前未收到 Lark webhook 请求")
	}
}

// TestBuildTaskRuntimeAlertMapsFields 验证任务运行异常告警字段不会在适配层丢失。
func TestBuildTaskRuntimeAlertMapsFields(t *testing.T) {
	cfg := config.Config{
		AppID: "site-1",
		Observability: config.ObservabilityConfig{
			ServiceName: "admin",
			Environment: "prod",
		},
	}
	cfg.Mode = "test"
	got := buildTaskRuntimeAlert(cfg, taskqueue.TaskRuntimeAlert{
		Kind:         "periodic_enqueue_failed",
		Title:        "【P1 周期任务入队失败】",
		Status:       "本轮周期任务未成功投递到队列，调度器继续运行",
		Component:    "scheduler",
		Operation:    "enqueue_periodic_task",
		TaskName:     "周期任务触发:demo",
		TaskType:     taskqueue.TypeWorkflowTrigger,
		WorkflowName: "demo.workflow",
		Cron:         "*/2 * * * *",
		TaskQueue:    taskqueue.QueueDefault,
		UniqueKey:    "periodic:demo.workflow",
		OccurredAt:   time.Date(2026, 6, 29, 15, 45, 0, 0, time.UTC),
		Reason:       "redis timeout",
		Advice:       "检查队列",
		TriggerCount: 2,
	})
	if got.ServiceName != "admin" || got.Environment != "test" || got.AppID != "site-1" {
		t.Fatalf("基础字段不符合预期: %+v", got)
	}
	if got.Kind != "periodic_enqueue_failed" || got.Operation != "enqueue_periodic_task" || got.TriggerCount != 2 {
		t.Fatalf("运行异常字段不符合预期: %+v", got)
	}
	if got.TaskName != "周期任务触发:demo" || got.WorkflowName != "demo.workflow" || got.Reason != "redis timeout" {
		t.Fatalf("任务字段不符合预期: %+v", got)
	}
	if got.Advice != "检查队列" {
		t.Fatalf("处理建议字段不符合预期: %+v", got)
	}
}

// TestCollectorRuntimeAlertMapsFields 验证 Collector 告警会转换为任务系统统一运行异常。
func TestCollectorRuntimeAlertMapsFields(t *testing.T) {
	got := CollectorRuntimeAlert(collectorx.RuntimeAlert{
		Kind:       collectorx.RuntimeAlertKindDeadEvent,
		Title:      "【P1 Collector 事件进入死信】",
		Status:     "事件已达到最大重试次数并进入死信",
		Component:  "collector",
		Operation:  "mark_dead",
		BizType:    "cdc.admin_log.audit",
		Channel:    "kafka",
		UniqueKey:  "",
		Reason:     "processor failed",
		Advice:     "人工重试",
		Count:      3,
		OccurredAt: time.Date(2026, 6, 29, 16, 0, 0, 0, time.UTC),
	})
	if got.Kind != svc.TaskRuntimeAlertKindCollectorDeadEvent || got.Component != "collector" || got.Operation != "mark_dead" {
		t.Fatalf("Collector 告警基础字段不符合预期: %+v", got)
	}
	if got.TaskName != "cdc.admin_log.audit" || got.TaskType != "collector:kafka" || got.UniqueKey != "cdc.admin_log.audit" {
		t.Fatalf("Collector 告警任务字段不符合预期: %+v", got)
	}
	if !strings.Contains(got.Reason, "processor failed") || !strings.Contains(got.Reason, "影响数量=3") {
		t.Fatalf("Collector 告警原因不符合预期: %+v", got)
	}
	if got.Advice != "人工重试" {
		t.Fatalf("Collector 告警处理建议不符合预期: %+v", got)
	}
}

// enabledAlertPeriodic 构造测试中显式启用的周期任务配置。
func enabledAlertPeriodic(item config.TaskPeriodicConfig) config.TaskPeriodicConfig {
	enabled := true
	item.Enabled = &enabled
	return item
}
