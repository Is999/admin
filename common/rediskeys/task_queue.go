package keys

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strconv"
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

// TaskRuntimeKey 返回任务运行耗时快照 Redis key。
func TaskRuntimeKey(taskID string) string {
	return TaskQueueRedisKey(joinKeyParts(taskRuntimeSegment, taskID))
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

// TaskWorkflowNodeScheduledKey 返回节点调度去重 Redis key。
func TaskWorkflowNodeScheduledKey(workflowID string, nodeName string) string {
	return TaskQueueRedisKey(joinKeyParts(taskWorkflowSegment, workflowID, taskWorkflowNodeSegment, nodeName, taskWorkflowScheduledSegment))
}

// TaskWorkflowNodeFinalizedKey 返回节点终态收口 Redis key。
func TaskWorkflowNodeFinalizedKey(workflowID string, nodeName string) string {
	return TaskQueueRedisKey(joinKeyParts(taskWorkflowSegment, workflowID, taskWorkflowNodeSegment, nodeName, taskWorkflowFinalizedSegment))
}

// TaskWorkflowNodeInstanceKey 返回单个分片实例终态巡检 Redis key。
func TaskWorkflowNodeInstanceKey(workflowID string, nodeName string, shardIndex int) string {
	return TaskQueueRedisKey(joinKeyParts(taskWorkflowSegment, workflowID, taskWorkflowNodeSegment, nodeName, taskWorkflowInstanceSegment, strconv.Itoa(shardIndex)))
}

// TaskWorkflowCompletedKey 返回工作流完成标记 Redis key。
func TaskWorkflowCompletedKey(workflowID string) string {
	return TaskQueueRedisKey(joinKeyParts(taskWorkflowSegment, workflowID, taskWorkflowCompletedSegment))
}

// TaskWorkflowFailedKey 返回工作流失败标记 Redis key。
func TaskWorkflowFailedKey(workflowID string) string {
	return TaskQueueRedisKey(joinKeyParts(taskWorkflowSegment, workflowID, taskWorkflowFailedSegment))
}

// TaskWorkflowUniqueKey 返回工作流幂等占位 Redis key。
func TaskWorkflowUniqueKey(name string, key string) string {
	return TaskQueueRedisKey(joinKeyParts(taskWorkflowSegment, taskWorkflowUniqueSegment, name, key))
}

// TaskWorkflowUniqueLockKey 返回工作流幂等预占短锁 Redis key。
func TaskWorkflowUniqueLockKey(name string, key string) string {
	return TaskQueueRedisKey(joinKeyParts(taskWorkflowSegment, taskWorkflowUniqueLockSegment, name, key))
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
