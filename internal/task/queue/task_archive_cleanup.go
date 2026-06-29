package taskqueue

import (
	"context"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	keys "admin/common/rediskeys"
	"admin/internal/infra/loggerx"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	taskArchivedRetentionDefaultSeconds = 7 * 86400       // 归档失败任务默认保留 7 天
	taskArchivedCleanupInterval         = 5 * time.Minute // 归档失败任务过期清理间隔
	taskArchivedCleanupBatchSize        = 500             // 单队列单轮最多清理数量，避免 Redis Lua 长时间阻塞
	taskArchivedCleanupMaxBatches       = 5               // 单队列单轮最多清理批次数，兼顾历史积压追赶和 Redis 压力
)

// startArchivedCleanerLocked 启动归档失败任务过期清理协程。
// 调用方需持有 lifecycleMu，避免 Worker 重复启动时创建多个清理循环。
func (m *Manager) startArchivedCleanerLocked() {
	if m == nil || m.redis == nil || m.archivedCleanStop != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.archivedCleanStop = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.cleanupExpiredArchivedTasks(ctx)
		ticker := time.NewTicker(taskArchivedCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.cleanupExpiredArchivedTasks(ctx)
			}
		}
	}()
}

// cleanupExpiredArchivedTasks 清理超过保留期的 archived 任务。
func (m *Manager) cleanupExpiredArchivedTasks(ctx context.Context) {
	if m == nil || m.redis == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	retention := m.archivedRetention()
	if retention <= 0 {
		return
	}
	cleanupCtx := loggerx.BindContext(ctx)
	cutoff := time.Now().Add(-retention).Unix()
	for _, queue := range m.archivedCleanupQueueNames() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		internalQueue := m.namespacedQueueName(queue)
		deleted, err := m.cleanupExpiredArchivedQueue(cleanupCtx, internalQueue, cutoff, taskArchivedCleanupBatchSize, taskArchivedCleanupMaxBatches)
		if err != nil {
			loggerx.Errorw(cleanupCtx, "归档失败任务过期清理失败", err, logx.Field("queue", queue))
			m.notifyTaskRuntimeAlert(cleanupCtx, TaskRuntimeAlert{
				Kind:      taskRuntimeAlertKindArchiveCleanupFailed,
				Title:     "【P1 归档失败任务清理异常】",
				Status:    "本轮过期 archived 任务清理失败，清理协程继续运行",
				Component: "archive_cleaner",
				Operation: "cleanup_expired_archived_tasks",
				TaskQueue: queue,
				Reason:    err.Error(),
			})
			continue
		}
		if deleted > 0 {
			loggerx.Infow(cleanupCtx, "归档失败任务过期清理完成",
				logx.Field("queue", queue),
				logx.Field("deleted", deleted),
				logx.Field("retention_seconds", int64(retention.Seconds())),
			)
		}
	}
}

// archivedCleanupQueueNames 返回需要执行 archived 清理的逻辑队列名列表。
func (m *Manager) archivedCleanupQueueNames() []string {
	if m == nil || m.inspector == nil {
		return nil
	}
	rawQueueNames, err := m.inspector.Queues()
	if err != nil || len(rawQueueNames) == 0 {
		return m.configuredQueueNames()
	}
	return m.visibleQueueNames(rawQueueNames)
}

// cleanupExpiredArchivedQueue 分批清理单个队列中过期的 archived 任务。
func (m *Manager) cleanupExpiredArchivedQueue(ctx context.Context, internalQueue string, cutoffUnix int64, batchSize int, maxBatches int) (int64, error) {
	if batchSize <= 0 {
		batchSize = taskArchivedCleanupBatchSize
	}
	if maxBatches <= 0 {
		maxBatches = 1
	}
	var total int64
	for i := 0; i < maxBatches; i++ {
		deleted, err := m.deleteExpiredArchivedTasks(ctx, internalQueue, cutoffUnix, batchSize)
		if err != nil {
			return total, errors.Wrapf(err, "清理过期 archived 任务失败 queue=%s", internalQueue)
		}
		total += deleted
		if deleted < int64(batchSize) {
			return total, nil
		}
	}
	return total, nil
}

// deleteExpiredArchivedTasks 删除单个队列中过期的 archived 任务。
func (m *Manager) deleteExpiredArchivedTasks(ctx context.Context, internalQueue string, cutoffUnix int64, batchSize int) (int64, error) {
	internalQueue = strings.TrimSpace(internalQueue)
	if internalQueue == "" {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = taskArchivedCleanupBatchSize
	}
	archivedKey, err := keys.TaskAsynqStateZSetKey(internalQueue, "archived")
	if err != nil {
		return 0, errors.Tag(err)
	}
	result, err := deleteExpiredArchivedTasksScript.Run(ctx, m.redis, []string{archivedKey},
		cutoffUnix,
		keys.TaskAsynqTaskHashKeyPrefix(internalQueue),
		batchSize,
	).Result()
	if err != nil {
		return 0, errors.Tag(err)
	}
	return toInt64(result), nil
}
