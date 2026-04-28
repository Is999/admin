package keys

import (
	"fmt"
	"strings"
)

// UserTagWorkflowLeaseRedisKey 返回用户标签写工作流互斥租约 Redis key。
func UserTagWorkflowLeaseRedisKey(appID string) string {
	return AppScopedKey(appID, UserTagWorkflowLeaseKey)
}

// UserTagWorkflowSyncDoneRedisKey 返回用户标签 sync_kafka 分片完成屏障 Redis key。
func UserTagWorkflowSyncDoneRedisKey(appID string, workflowID string) string {
	return AppScopedKey(appID, fmt.Sprintf(UserTagWorkflowSyncDoneKey, strings.TrimSpace(workflowID)))
}

// UserTagRuntimeCleanupRedisKey 返回用户标签运行期辅助表清理互斥锁 Redis key。
func UserTagRuntimeCleanupRedisKey(appID string) string {
	return AppScopedKey(appID, UserTagRuntimeCleanupLock)
}

// UserTagKafkaOutboxRetryScanRedisKey 返回用户标签 Kafka outbox 异常扫描互斥锁 Redis key。
func UserTagKafkaOutboxRetryScanRedisKey(appID string) string {
	return AppScopedKey(appID, UserTagKafkaOutboxRetryScanLock)
}
