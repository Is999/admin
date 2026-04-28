package keys

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/Is999/go-utils/errors"
)

// 任务队列 Redis key 片段只在本包组合完整规则时使用。
const (
	// taskQueueRedisRoot 表示任务系统自管 key 的二级业务前缀。
	taskQueueRedisRoot = "task"
	// taskWorkflowSegment 表示工作流状态 key 的领域段。
	taskWorkflowSegment = "workflow"
	// taskRuntimeSegment 表示任务运行快照 key 的领域段。
	taskRuntimeSegment = "runtime"
	// taskAsynqRedisRoot 表示 Asynq 框架固定 Redis 根前缀。
	taskAsynqRedisRoot = "asynq"
	// taskAsynqStateRetry 表示 Asynq retry 状态 zset 段。
	taskAsynqStateRetry = "retry"
	// taskAsynqStateArchived 表示 Asynq archived 状态 zset 段。
	taskAsynqStateArchived = "archived"
	// taskAsynqStateCompleted 表示 Asynq completed 状态 zset 段。
	taskAsynqStateCompleted = "completed"
	// taskAsynqStateScheduled 表示 Asynq scheduled 状态 zset 段。
	taskAsynqStateScheduled = "scheduled"
	// taskAsynqTaskHashSegment 表示 Asynq 任务详情 hash 段。
	taskAsynqTaskHashSegment = "t"
	// taskAsynqUniqueSegment 表示 Asynq 唯一锁 key 段。
	taskAsynqUniqueSegment = "unique"
	// taskWorkflowNodeSegment 表示工作流节点状态 key 段。
	taskWorkflowNodeSegment = "node"
	// taskWorkflowUniqueSegment 表示工作流幂等占位 key 段。
	taskWorkflowUniqueSegment = "unique"
)

// TaskQueueName 返回带 app_id 命名空间的任务队列或分组名。
func TaskQueueName(appID string, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if HasAppScopedPrefix(name) {
		return name
	}
	return AppScopedKey(appID, name)
}

// TaskQueueNameScope 返回当前 app_id 的任务队列命名空间。
func TaskQueueNameScope(appID string) string {
	return AppScopedPrefix(appID)
}

// TrimTaskQueueName 去掉任务队列或分组名中的 app_id 命名空间。
func TrimTaskQueueName(name string) string {
	return TrimAppScopedPrefix(name)
}

// TaskQueueRedisKey 返回带 app_id 命名空间的任务系统自管 Redis key。
func TaskQueueRedisKey(appID string, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return key
	}
	if HasAppScopedPrefix(key) {
		return key
	}
	if key == taskQueueRedisRoot {
		return AppScopedKey(appID, taskQueueRedisRoot)
	}
	key = strings.TrimPrefix(key, taskQueueRedisRoot+":")
	if key == "" {
		return AppScopedKey(appID, taskQueueRedisRoot)
	}
	return AppScopedKey(appID, taskQueueRedisRoot+":"+key)
}

// TaskRuntimeKey 返回任务运行耗时快照 Redis key。
func TaskRuntimeKey(appID string, taskID string) string {
	return TaskQueueRedisKey(appID, joinKeyParts(taskRuntimeSegment, taskID))
}

// TaskSchedulerLeaderRedisKey 返回调度器 leader 租约 Redis key。
func TaskSchedulerLeaderRedisKey(appID string, leaseKey string) string {
	leaseKey = strings.TrimSpace(leaseKey)
	if leaseKey == "" {
		leaseKey = TaskQueueSchedulerLeaderKey
	}
	return TaskQueueRedisKey(appID, leaseKey)
}

// TaskWorkflowPrefix 返回工作流状态 Redis key 前缀。
func TaskWorkflowPrefix(appID string) string {
	return TaskQueueRedisKey(appID, taskWorkflowSegment)
}

// TaskWorkflowMetaKey 返回工作流主记录 Redis key。
func TaskWorkflowMetaKey(appID string, workflowID string) string {
	return TaskQueueRedisKey(appID, joinKeyParts(taskWorkflowSegment, workflowID, "meta"))
}

// TaskWorkflowNodesKey 返回工作流节点集合 Redis key。
func TaskWorkflowNodesKey(appID string, workflowID string) string {
	return TaskQueueRedisKey(appID, joinKeyParts(taskWorkflowSegment, workflowID, "nodes"))
}

// TaskWorkflowNodeKey 返回单个工作流节点状态 Redis key。
func TaskWorkflowNodeKey(appID string, workflowID string, nodeName string) string {
	return TaskQueueRedisKey(appID, joinKeyParts(taskWorkflowSegment, workflowID, taskWorkflowNodeSegment, nodeName))
}

// TaskWorkflowNodeScheduledKey 返回节点调度去重 Redis key。
func TaskWorkflowNodeScheduledKey(appID string, workflowID string, nodeName string) string {
	return TaskQueueRedisKey(appID, joinKeyParts(taskWorkflowSegment, workflowID, taskWorkflowNodeSegment, nodeName, "scheduled"))
}

// TaskWorkflowNodeFinalizedKey 返回节点终态收口 Redis key。
func TaskWorkflowNodeFinalizedKey(appID string, workflowID string, nodeName string) string {
	return TaskQueueRedisKey(appID, joinKeyParts(taskWorkflowSegment, workflowID, taskWorkflowNodeSegment, nodeName, "finalized"))
}

// TaskWorkflowNodeInstanceKey 返回单个分片实例终态巡检 Redis key。
func TaskWorkflowNodeInstanceKey(appID string, workflowID string, nodeName string, shardIndex int) string {
	return TaskQueueRedisKey(appID, joinKeyParts(taskWorkflowSegment, workflowID, taskWorkflowNodeSegment, nodeName, "instance", strconv.Itoa(shardIndex)))
}

// TaskWorkflowCompletedKey 返回工作流完成标记 Redis key。
func TaskWorkflowCompletedKey(appID string, workflowID string) string {
	return TaskQueueRedisKey(appID, joinKeyParts(taskWorkflowSegment, workflowID, "completed"))
}

// TaskWorkflowFailedKey 返回工作流失败标记 Redis key。
func TaskWorkflowFailedKey(appID string, workflowID string) string {
	return TaskQueueRedisKey(appID, joinKeyParts(taskWorkflowSegment, workflowID, "failed"))
}

// TaskWorkflowUniqueKey 返回工作流幂等占位 Redis key。
func TaskWorkflowUniqueKey(appID string, name string, key string) string {
	return TaskQueueRedisKey(appID, joinKeyParts(taskWorkflowSegment, taskWorkflowUniqueSegment, name, key))
}

// TaskWorkflowUniqueLockKey 返回工作流幂等预占短锁 Redis key。
func TaskWorkflowUniqueLockKey(appID string, name string, key string) string {
	return TaskQueueRedisKey(appID, joinKeyParts(taskWorkflowSegment, "unique-lock", name, key))
}

// TaskAsynqScheduledKey 返回 Asynq scheduled zset Redis key。
func TaskAsynqScheduledKey(queue string) string {
	return taskAsynqKey(queue, taskAsynqStateScheduled)
}

// TaskAsynqTaskHashKeyPrefix 返回 Asynq 任务详情 hash Redis key 前缀。
func TaskAsynqTaskHashKeyPrefix(queue string) string {
	return taskAsynqKey(queue, taskAsynqTaskHashSegment) + ":"
}

// TaskAsynqTaskHashKey 返回 Asynq 单个任务详情 hash Redis key。
func TaskAsynqTaskHashKey(queue string, taskID string) string {
	return TaskAsynqTaskHashKeyPrefix(queue) + strings.TrimSpace(taskID)
}

// TaskAsynqUniqueKey 返回 Asynq 任务唯一锁 Redis key。
func TaskAsynqUniqueKey(queue string, taskType string, payload []byte) string {
	prefix := taskAsynqKey(queue, taskAsynqUniqueSegment) + ":" + strings.TrimSpace(taskType) + ":"
	if payload == nil {
		return prefix
	}
	// Asynq 使用 payload MD5 作为唯一锁摘要。
	checksum := md5.Sum(payload)
	return prefix + hex.EncodeToString(checksum[:])
}

// TaskAsynqStateZSetKey 返回 Asynq 状态 zset Redis key。
func TaskAsynqStateZSetKey(queue string, state string) (string, error) {
	state = strings.TrimSpace(state)
	switch state {
	case taskAsynqStateRetry, taskAsynqStateArchived, taskAsynqStateCompleted:
		return taskAsynqKey(queue, state), nil
	default:
		return "", errors.Errorf("不支持的 zset 任务状态: %s", state)
	}
}

// taskAsynqKey 返回 Asynq 框架 key，队列名已经包含 app_id 命名空间。
func taskAsynqKey(queue string, segment string) string {
	return fmt.Sprintf("%s:{%s}:%s", taskAsynqRedisRoot, strings.TrimSpace(queue), strings.TrimSpace(segment))
}

// joinKeyParts 拼接 Redis key 业务段，自动跳过空段。
func joinKeyParts(parts ...string) string {
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		items = append(items, part)
	}
	return strings.Join(items, ":")
}
