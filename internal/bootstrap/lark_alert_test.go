package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"admin/internal/config"
	"admin/internal/task/queue"
	"admin/internal/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

type larkAlertPayload struct {
	MsgType string `json:"msg_type"`
	Content struct {
		Text string `json:"text"`
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
