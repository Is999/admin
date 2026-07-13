package keys

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/Is999/go-utils/errors"
)

// TaskQueueName 返回带当前 app_id 命名空间的任务队列或分组名。
func TaskQueueName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return WithPrefix(name)
}

// TaskQueueNameScope 返回当前 app_id 的任务队列命名空间。
func TaskQueueNameScope() string {
	return Prefix()
}

// TrimTaskQueueName 去掉任务队列或分组名中的 app_id 命名空间。
func TrimTaskQueueName(name string) string {
	return TrimPrefix(name)
}

// TaskQueueRedisKey 返回带当前 app_id 命名空间的任务系统自管 Redis key。
func TaskQueueRedisKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return key
	}
	if HasPrefix(key) {
		return WithPrefix(key)
	}
	if key == taskQueueRedisRoot {
		return WithPrefix(taskQueueRedisRoot)
	}
	key = strings.TrimPrefix(key, taskQueueRedisRoot+":")
	if key == "" {
		return WithPrefix(taskQueueRedisRoot)
	}
	return WithPrefix(taskQueueRedisRoot + ":" + key)
}

// TaskRuntimeKey 返回按逻辑队列和任务 ID 隔离的运行耗时快照 Redis key。
func TaskRuntimeKey(queue string, taskID string) string {
	queue = strings.TrimSpace(queue)
	taskID = strings.TrimSpace(taskID)
	if queue == "" || taskID == "" {
		return ""
	}
	// 字节长度前缀保证含冒号的队列名和任务 ID 仍能唯一分段。
	return TaskQueueRedisKey(fmt.Sprintf("%s:%d:%s:%d:%s", taskRuntimeSegment, len(queue), queue, len(taskID), taskID))
}

// TaskSchedulerLeaderRedisKey 返回调度器 leader 租约 Redis key。
func TaskSchedulerLeaderRedisKey(leaseKey string) string {
	leaseKey = strings.TrimSpace(leaseKey)
	if leaseKey == "" {
		leaseKey = TaskQueueSchedulerLeaderKey
	}
	return TaskQueueRedisKey(leaseKey)
}

// TaskWorkflowPrefix 返回工作流状态 Redis key 前缀。
func TaskWorkflowPrefix() string {
	return TaskQueueRedisKey(taskWorkflowSegment)
}

// TaskWorkflowMetaKey 返回工作流主记录 Redis key。
func TaskWorkflowMetaKey(workflowID string) string {
	return TaskQueueRedisKey(joinKeyParts(taskWorkflowSegment, workflowID, taskWorkflowMetaSegment))
}

// TaskWorkflowNodesKey 返回工作流节点集合 Redis key。
func TaskWorkflowNodesKey(workflowID string) string {
	return TaskQueueRedisKey(joinKeyParts(taskWorkflowSegment, workflowID, taskWorkflowNodesSegment))
}

// TaskWorkflowNodeKey 返回单个工作流节点状态 Redis key。
func TaskWorkflowNodeKey(workflowID string, nodeName string) string {
	return TaskQueueRedisKey(joinKeyParts(taskWorkflowSegment, workflowID, taskWorkflowNodeSegment, nodeName))
}

// TaskWorkflowUniqueKey 返回工作流幂等占位 Redis key。
func TaskWorkflowUniqueKey(name string, key string) string {
	return TaskQueueRedisKey(joinKeyParts(taskWorkflowSegment, taskWorkflowUniqueSegment, name, key))
}

// TaskWorkflowUniqueLockKey 返回工作流幂等预占短锁 Redis key。
func TaskWorkflowUniqueLockKey(name string, key string) string {
	return TaskQueueRedisKey(joinKeyParts(taskWorkflowSegment, taskWorkflowUniqueLockSegment, name, key))
}

// TaskAsynqPendingKey 返回 Asynq pending list Redis key。
func TaskAsynqPendingKey(queue string) string {
	return taskAsynqKey(queue, taskAsynqStatePending)
}

// TaskAsynqActiveKey 返回 Asynq active list Redis key。
func TaskAsynqActiveKey(queue string) string {
	return taskAsynqKey(queue, taskAsynqStateActive)
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
