package options

import (
	"admin/internal/jobs/usertag/types"

	"github.com/Is999/go-utils/errors"
)

const (
	// MaxShardTotal 限制单个用户标签工作流可拆出的最大节点分片数，避免误触发造成任务风暴。
	MaxShardTotal = 128
	// MaxPhysicalShardTotal 限制运行期和结果物理分片数量，当前 shard_no 固定为 0-1023。
	MaxPhysicalShardTotal = 1024
	// MaxBatchSize 限制单批 SQL 游标、IN 查询和 outbox claim 大小。
	MaxBatchSize = 10000
	// MaxWorkerCount 限制节点内部并发配置，当前主要用于入口防御和未来扩展。
	MaxWorkerCount = 64
	// MaxTargetUIDs 限制定向补算一次允许传入的 UID 数量。
	MaxTargetUIDs = 10000
	// MaxTagTypes 限制指定标签集合大小，避免异常请求放大规则编译和 SQL IN 条件。
	MaxTagTypes = 500
)

// ValidateRuntimeOptions 对最终运行参数做硬上限校验，保护队列、数据库和内存。
func ValidateRuntimeOptions(opts types.RuntimeOptions) error {
	if opts.ShardTotal <= 0 || opts.ShardTotal > MaxShardTotal {
		return errors.Errorf("shard_total 必须在 1-%d 之间", MaxShardTotal)
	}
	if opts.BatchSize <= 0 || opts.BatchSize > MaxBatchSize {
		return errors.Errorf("batch_size 必须在 1-%d 之间", MaxBatchSize)
	}
	if opts.WorkerCount <= 0 || opts.WorkerCount > MaxWorkerCount {
		return errors.Errorf("worker_count 必须在 1-%d 之间", MaxWorkerCount)
	}
	if len(opts.UIDs) > MaxTargetUIDs {
		return errors.Errorf("uids 数量不能超过 %d", MaxTargetUIDs)
	}
	if len(opts.TagTypes) > MaxTagTypes {
		return errors.Errorf("tag_types 数量不能超过 %d", MaxTagTypes)
	}
	return nil
}
