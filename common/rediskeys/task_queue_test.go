package keys

import "testing"

// TestTaskQueueName 验证队列名统一追加 app_id，完整跨站点队列名不能被改写。
func TestTaskQueueName(t *testing.T) {
	tests := []struct {
		name  string
		appID string
		queue string
		want  string
	}{
		{
			name:  "逻辑队列追加当前app前缀",
			appID: "215",
			queue: "maintenance",
			want:  "app:215:maintenance",
		},
		{
			name:  "当前app队列保持不变",
			appID: "215",
			queue: "app:215:maintenance",
			want:  "app:215:maintenance",
		},
		{
			name:  "其它app完整队列保持原样",
			appID: "102",
			queue: "app:215:maintenance",
			want:  "app:215:maintenance",
		},
		{
			name:  "不完整app前缀按逻辑队列处理",
			appID: "102",
			queue: "app:215",
			want:  "app:102:app:215",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TaskQueueName(tt.appID, tt.queue); got != tt.want {
				t.Fatalf("TaskQueueName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTaskQueueRedisKey 验证任务系统自管 key 统一收敛到 task 二级前缀。
func TestTaskQueueRedisKey(t *testing.T) {
	tests := []struct {
		name  string
		appID string
		key   string
		want  string
	}{
		{
			name:  "逻辑key追加task前缀",
			appID: "215",
			key:   "workflow:wf-1:meta",
			want:  "app:215:task:workflow:wf-1:meta",
		},
		{
			name:  "已有task前缀不重复追加",
			appID: "215",
			key:   "task:scheduler:leader",
			want:  "app:215:task:scheduler:leader",
		},
		{
			name:  "task根段只保留一次",
			appID: "215",
			key:   "task",
			want:  "app:215:task",
		},
		{
			name:  "当前app完整key保持不变",
			appID: "215",
			key:   "app:215:task:scheduler:leader",
			want:  "app:215:task:scheduler:leader",
		},
		{
			name:  "其它app完整key保持原样",
			appID: "102",
			key:   "app:215:task:scheduler:leader",
			want:  "app:215:task:scheduler:leader",
		},
		{
			name:  "不完整app前缀按逻辑key处理",
			appID: "102",
			key:   "app:215",
			want:  "app:102:task:app:215",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TaskQueueRedisKey(tt.appID, tt.key); got != tt.want {
				t.Fatalf("TaskQueueRedisKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTaskRuntimeAndWorkflowKeys 验证任务运行快照和工作流 key 的完整规则。
func TestTaskRuntimeAndWorkflowKeys(t *testing.T) {
	if got := TaskRuntimeKey("215", "task-1"); got != "app:215:task:runtime:task-1" {
		t.Fatalf("TaskRuntimeKey() = %q", got)
	}
	if got := TaskWorkflowPrefix("215"); got != "app:215:task:workflow" {
		t.Fatalf("TaskWorkflowPrefix() = %q", got)
	}
	if got := TaskWorkflowMetaKey("215", "wf-1"); got != "app:215:task:workflow:wf-1:meta" {
		t.Fatalf("TaskWorkflowMetaKey() = %q", got)
	}
	if got := TaskWorkflowNodesKey("215", "wf-1"); got != "app:215:task:workflow:wf-1:nodes" {
		t.Fatalf("TaskWorkflowNodesKey() = %q", got)
	}
	if got := TaskWorkflowNodeKey("215", "wf-1", "root"); got != "app:215:task:workflow:wf-1:node:root" {
		t.Fatalf("TaskWorkflowNodeKey() = %q", got)
	}
	if got := TaskWorkflowNodeScheduledKey("215", "wf-1", "root"); got != "app:215:task:workflow:wf-1:node:root:scheduled" {
		t.Fatalf("TaskWorkflowNodeScheduledKey() = %q", got)
	}
	if got := TaskWorkflowNodeFinalizedKey("215", "wf-1", "root"); got != "app:215:task:workflow:wf-1:node:root:finalized" {
		t.Fatalf("TaskWorkflowNodeFinalizedKey() = %q", got)
	}
	if got := TaskWorkflowNodeInstanceKey("215", "wf-1", "root", 3); got != "app:215:task:workflow:wf-1:node:root:instance:3" {
		t.Fatalf("TaskWorkflowNodeInstanceKey() = %q", got)
	}
	if got := TaskWorkflowCompletedKey("215", "wf-1"); got != "app:215:task:workflow:wf-1:completed" {
		t.Fatalf("TaskWorkflowCompletedKey() = %q", got)
	}
	if got := TaskWorkflowFailedKey("215", "wf-1"); got != "app:215:task:workflow:wf-1:failed" {
		t.Fatalf("TaskWorkflowFailedKey() = %q", got)
	}
	if got := TaskWorkflowUniqueKey("215", "user_tag.delta.refresh", "daily"); got != "app:215:task:workflow:unique:user_tag.delta.refresh:daily" {
		t.Fatalf("TaskWorkflowUniqueKey() = %q", got)
	}
	if got := TaskWorkflowUniqueLockKey("215", "user_tag.delta.refresh", "daily"); got != "app:215:task:workflow:unique-lock:user_tag.delta.refresh:daily" {
		t.Fatalf("TaskWorkflowUniqueLockKey() = %q", got)
	}
}

// TestTaskSchedulerLeaderRedisKey 验证调度 leader 锁 key 的默认值和归一规则。
func TestTaskSchedulerLeaderRedisKey(t *testing.T) {
	tests := []struct {
		name     string
		appID    string
		leaseKey string
		want     string
	}{
		{name: "空配置使用默认key", appID: "215", want: "app:215:task:scheduler:leader"},
		{name: "自定义逻辑key归入task前缀", appID: "215", leaseKey: "scheduler:new", want: "app:215:task:scheduler:new"},
		{name: "已有task根段不重复追加", appID: "215", leaseKey: "task:scheduler:leader", want: "app:215:task:scheduler:leader"},
		{name: "其它app完整key保持原样", appID: "215", leaseKey: "app:102:task:scheduler:leader", want: "app:102:task:scheduler:leader"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TaskSchedulerLeaderRedisKey(tt.appID, tt.leaseKey); got != tt.want {
				t.Fatalf("TaskSchedulerLeaderRedisKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTaskAsynqKeys 验证 Asynq 框架 key 保持官方存储形态。
func TestTaskAsynqKeys(t *testing.T) {
	queue := TaskQueueName("215", "maintenance")
	if got := TaskAsynqScheduledKey(queue); got != "asynq:{app:215:maintenance}:scheduled" {
		t.Fatalf("TaskAsynqScheduledKey() = %q", got)
	}
	if got := TaskAsynqTaskHashKeyPrefix(queue); got != "asynq:{app:215:maintenance}:t:" {
		t.Fatalf("TaskAsynqTaskHashKeyPrefix() = %q", got)
	}
	if got := TaskAsynqTaskHashKey(queue, "task-1"); got != "asynq:{app:215:maintenance}:t:task-1" {
		t.Fatalf("TaskAsynqTaskHashKey() = %q", got)
	}
	if got := TaskAsynqUniqueKey(queue, "email:send", []byte(`{"uid":1}`)); got != "asynq:{app:215:maintenance}:unique:email:send:0b43657867acc68460ccc015a031a4a8" {
		t.Fatalf("TaskAsynqUniqueKey() = %q", got)
	}
	if got := TaskAsynqUniqueKey(queue, "reindex", nil); got != "asynq:{app:215:maintenance}:unique:reindex:" {
		t.Fatalf("TaskAsynqUniqueKey(nil) = %q", got)
	}
	if got, err := TaskAsynqStateZSetKey(queue, "archived"); err != nil || got != "asynq:{app:215:maintenance}:archived" {
		t.Fatalf("TaskAsynqStateZSetKey() = %q err=%v", got, err)
	}
	if got, err := TaskAsynqStateZSetKey(queue, " retry "); err != nil || got != "asynq:{app:215:maintenance}:retry" {
		t.Fatalf("TaskAsynqStateZSetKey(trim) = %q err=%v", got, err)
	}
	if _, err := TaskAsynqStateZSetKey(queue, "pending"); err == nil {
		t.Fatal("期望不支持的 Asynq zset 状态返回错误")
	}
}
