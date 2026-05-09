package keys

import (
	"fmt"
	"strings"
)

// ArchiveJobPlanRedisKey 返回归档任务区间规划互斥锁 Redis key。
func ArchiveJobPlanRedisKey(jobName string) string {
	return WithPrefix(fmt.Sprintf(ArchiveJobPlanLock, strings.TrimSpace(jobName)))
}

// ArchiveJobWatermarkRedisKey 返回归档任务水位推进互斥锁 Redis key。
func ArchiveJobWatermarkRedisKey(jobName string) string {
	return WithPrefix(fmt.Sprintf(ArchiveJobWatermarkLock, strings.TrimSpace(jobName)))
}

// ArchiveJobCleanupRedisKey 返回归档历史表清理互斥锁 Redis key。
func ArchiveJobCleanupRedisKey(jobName string) string {
	return WithPrefix(fmt.Sprintf(ArchiveJobCleanupLock, strings.TrimSpace(jobName)))
}
