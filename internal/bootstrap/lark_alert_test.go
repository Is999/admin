package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"admin/common/runtimecfg"
	"admin/internal/config"
	"admin/internal/infra/collectorx"
	"admin/internal/svc"
	"admin/internal/task/queue"
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
	if err := registerTaskFailureLarkAlert(config.Config{
		AppID: "site-1",
		Observability: config.ObservabilityConfig{
			ServiceName: "admin",
			Environment: "test",
		},
		Alert: config.AlertConfig{Lark: config.LarkAlertConfig{
			Enabled:        true,
			WebhookURL:     server.URL,
			TimeoutSeconds: 1,
			MaxErrorBytes:  120,
		}},
	}, manager); err != nil {
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
		if payload.MsgType != "text" {
			t.Fatalf("Lark 消息类型 = %s，期望 text", payload.MsgType)
		}
		for _, want := range []string{
			"【P1 后台任务终态失败】",
			"站点：site-1",
			"任务\n- 名称：demo:lark-final-failure",
			"- 类型：demo:lark-final-failure",
			"任务ID：" + resp.TaskID,
			"工作流ID：wf-lark",
			"节点：root",
			"模式：delta",
			"状态：已归档失败，不会继续自动重试",
		} {
			if !strings.Contains(payload.Content.Text, want) {
				t.Fatalf("Lark 告警缺少 %q:\n%s", want, payload.Content.Text)
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
			enabledBootstrapPeriodic(config.TaskPeriodicConfig{
				Name:      "monitorfeishu-feishu-report-2m",
				Cron:      "*/2 * * * *",
				Workflow:  "monitorfeishu.feishu_report",
				Queue:     taskqueue.QueueDefault,
				UniqueKey: "periodic:monitorfeishu.feishu_report",
			}),
		},
	}, client)
	if manager == nil {
		t.Fatal("期望成功创建任务管理器")
	}

	if err := registerTaskFailureLarkAlert(config.Config{
		AppID: "site-1",
		Observability: config.ObservabilityConfig{
			ServiceName: "admin",
			Environment: "test",
		},
		Alert: config.AlertConfig{Lark: config.LarkAlertConfig{
			Enabled:        true,
			WebhookURL:     server.URL,
			TimeoutSeconds: 1,
			MaxErrorBytes:  120,
		}},
	}, manager); err != nil {
		t.Fatalf("注册 Lark 告警失败: %v", err)
	}
	if err := manager.ValidatePeriodicTaskDefinitions(); err != nil {
		t.Fatalf("周期任务定义校验不应阻断启动: %v", err)
	}

	select {
	case payload := <-payloadCh:
		if payload.MsgType != "text" {
			t.Fatalf("Lark 消息类型 = %s，期望 text", payload.MsgType)
		}
		for _, want := range []string{
			"【P1 周期任务配置异常】",
			"站点：site-1",
			"名称：monitorfeishu-feishu-report-2m",
			"工作流：monitorfeishu.feishu_report",
			"Cron：*/2 * * * *",
			"去重键：periodic:monitorfeishu.feishu_report",
			"错误摘要\n工作流定义不存在",
			"该周期任务不会注册到调度器",
		} {
			if !strings.Contains(payload.Content.Text, want) {
				t.Fatalf("Lark 告警缺少 %q:\n%s", want, payload.Content.Text)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("超时前未收到 Lark webhook 请求")
	}
}

// TestBuildTaskRuntimeAlertMapsFields 验证任务运行异常告警字段不会在 bootstrap 装配时丢失。
func TestBuildTaskRuntimeAlertMapsFields(t *testing.T) {
	got := buildTaskRuntimeAlert(config.Config{
		AppID: "site-1",
		Observability: config.ObservabilityConfig{
			ServiceName: "admin",
			Environment: "test",
		},
	}, taskqueue.TaskRuntimeAlert{
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

// TestBuildCollectorRuntimeAlertMapsFields 验证 Collector 告警会转换为任务系统统一运行异常。
func TestBuildCollectorRuntimeAlertMapsFields(t *testing.T) {
	got := buildCollectorRuntimeAlert(collectorx.RuntimeAlert{
		Kind:       collectorx.RuntimeAlertKindDeadEvent,
		Title:      "【P1 Collector 事件进入死信】",
		Status:     "事件已达到最大重试次数并进入死信",
		Component:  "collector",
		Operation:  "mark_dead",
		BizType:    "cdc.admin_log.audit",
		Transport:  "db",
		UniqueKey:  "",
		Reason:     "processor failed",
		Advice:     "人工重试",
		Count:      3,
		OccurredAt: time.Date(2026, 6, 29, 16, 0, 0, 0, time.UTC),
	})
	if got.Kind != svc.TaskRuntimeAlertKindCollectorDeadEvent || got.Component != "collector" || got.Operation != "mark_dead" {
		t.Fatalf("Collector 告警基础字段不符合预期: %+v", got)
	}
	if got.TaskName != "cdc.admin_log.audit" || got.TaskType != "collector:db" || got.UniqueKey != "cdc.admin_log.audit" {
		t.Fatalf("Collector 告警任务字段不符合预期: %+v", got)
	}
	if !strings.Contains(got.Reason, "processor failed") || !strings.Contains(got.Reason, "影响数量=3") {
		t.Fatalf("Collector 告警原因不符合预期: %+v", got)
	}
	if got.Advice != "人工重试" {
		t.Fatalf("Collector 告警处理建议不符合预期: %+v", got)
	}
}

// enabledBootstrapPeriodic 构造测试中显式启用的周期任务配置。
func enabledBootstrapPeriodic(item config.TaskPeriodicConfig) config.TaskPeriodicConfig {
	enabled := true
	item.Enabled = &enabled
	return item
}
