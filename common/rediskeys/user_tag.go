package keys

import (
	"fmt"
	"strings"
)

// UserTagWorkflowLeaseRedisKey 返回用户标签写工作流互斥租约 Redis key。
func UserTagWorkflowLeaseRedisKey() string {
	return WithPrefix(UserTagWorkflowLeaseKey)
}

// UserTagWorkflowFinalDoneRedisKey 返回用户标签最终分片完成屏障 Redis key。
func UserTagWorkflowFinalDoneRedisKey(workflowID string) string {
	return WithPrefix(fmt.Sprintf(UserTagWorkflowFinalDoneKey, strings.TrimSpace(workflowID)))
}

// UserTagRuntimeCleanupRedisKey 返回用户标签运行期辅助表清理互斥锁 Redis key。
func UserTagRuntimeCleanupRedisKey() string {
	return WithPrefix(UserTagRuntimeCleanupLock)
}

// UserTagEventOutboxRetryScanRedisKey 返回用户标签事件 outbox 异常扫描互斥锁 Redis key。
func UserTagEventOutboxRetryScanRedisKey() string {
	return WithPrefix(UserTagEventOutboxRetryScanLock)
}
