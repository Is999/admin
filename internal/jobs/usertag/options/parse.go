package options

import (
	"sort"
	"strconv"
	"strings"

	"admin/internal/jobs/usertag/types"

	"github.com/Is999/go-utils/errors"
)

// ParseOptions 解析 usertag 工作流负载和 targets 参数。
// 重要约束：filterHash 只属于后台多维筛选，不允许作为用户标签计算来源。
func ParseOptions(payload types.WorkflowPayload, defaults Defaults) (types.RuntimeOptions, error) {
	mode, err := parseMode(payload.Mode)
	if err != nil {
		return types.RuntimeOptions{}, errors.Tag(err)
	}
	opts := types.RuntimeOptions{
		WorkflowID:       strings.TrimSpace(payload.WorkflowID),
		Mode:             mode,
		TagTypes:         normalizeInts(payload.TagTypes),
		UIDs:             normalizeInt64s(payload.UIDs),
		ShardIndex:       payload.ShardIndex,
		ShardTotal:       positiveOr(payload.ShardTotal, defaults.ShardTotal),
		BatchSize:        positiveOr(payload.BatchSize, defaults.BatchSize),
		WorkerCount:      positiveOr(payload.WorkerCount, defaults.WorkerCount),
		DryRun:           payload.DryRun,
		SyncSnapshotOnly: payload.SyncSnapshotOnly,
		EventHookEnabled: defaults.EventHookEnabled,
	}
	for _, target := range payload.Targets {
		key, val, ok := strings.Cut(strings.TrimSpace(target), "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.ToLower(key))
		val = strings.TrimSpace(val)
		switch key {
		case "mode":
			mode, err := parseMode(val)
			if err != nil {
				return opts, errors.Tag(err)
			}
			opts.Mode = mode
		case "tag", "tag_type", "tagtype":
			tagType, err := strconv.Atoi(val)
			if err != nil {
				return opts, errors.Wrapf(err, "解析标签类型失败 value=%s", val)
			}
			opts.TagTypes = append(opts.TagTypes, tagType)
		case "uid":
			uid, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return opts, errors.Wrapf(err, "解析用户 UID 失败 value=%s", val)
			}
			opts.UIDs = append(opts.UIDs, uid)
		case "filter_hash", "filterhash":
			return opts, errors.Errorf("多维筛选 hash 不能作为用户标签计算来源，请显式传入 uids")
		case "batch_size", "batchsize":
			batchSize, err := strconv.Atoi(val)
			if err != nil {
				return opts, errors.Wrapf(err, "解析批次大小失败 value=%s", val)
			}
			opts.BatchSize = positiveOr(batchSize, defaults.BatchSize)
		case "worker_count", "workercount":
			workerCount, err := strconv.Atoi(val)
			if err != nil {
				return opts, errors.Wrapf(err, "解析 worker 数失败 value=%s", val)
			}
			opts.WorkerCount = positiveOr(workerCount, defaults.WorkerCount)
		case "dry_run", "dryrun":
			opts.DryRun = val == "1" || strings.EqualFold(val, "true")
		case "sync_snapshot_only", "syncsnapshotonly":
			opts.SyncSnapshotOnly = val == "1" || strings.EqualFold(val, "true")
		}
	}
	opts.TagTypes = normalizeInts(opts.TagTypes)
	opts.UIDs = normalizeInt64s(opts.UIDs)
	if opts.Mode == types.ModeFull && (len(opts.UIDs) > 0 || len(opts.TagTypes) > 0) {
		return opts, errors.Errorf("full 模式是全站切表重建，不能提供 uids 或 tag_types；指定 UID 请使用 targeted，指定标签请使用 recalculate")
	}
	if opts.Mode == types.ModeTargeted && len(opts.UIDs) == 0 {
		return opts, errors.Errorf("targeted 模式必须显式提供 uids")
	}
	if opts.Mode == types.ModeTargeted && len(opts.TagTypes) == 0 {
		return opts, errors.Errorf("targeted 模式必须提供 tag_types")
	}
	if opts.Mode == types.ModeRecalculate && len(opts.TagTypes) == 0 {
		return opts, errors.Errorf("recalculate 模式必须提供 tag_types")
	}
	if opts.SyncSnapshotOnly && opts.Mode != types.ModeFull {
		return opts, errors.Errorf("sync_snapshot_only 仅支持 full 模式")
	}
	if opts.SyncSnapshotOnly && opts.DryRun {
		return opts, errors.Errorf("sync_snapshot_only 与 dry_run 不能同时启用")
	}
	if err := ValidateRuntimeOptions(opts); err != nil {
		return opts, errors.Tag(err)
	}
	return opts, nil
}

// parseMode 规范化运行模式，空模式按 full 兼容，非法模式必须显式拒绝。
func parseMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", types.ModeFull:
		return types.ModeFull, nil
	case types.ModeDelta:
		return types.ModeDelta, nil
	case types.ModeTargeted:
		return types.ModeTargeted, nil
	case types.ModeRecalculate:
		return types.ModeRecalculate, nil
	default:
		return "", errors.Errorf("mode 必须为 full、delta、targeted 或 recalculate")
	}
}

// normalizeInts 对标签类型集合去重、过滤非法值并升序输出。
func normalizeInts(items []int) []int {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(items)) // seen 记录已出现的标签类型，避免重复计算
	out := make([]int, 0, len(items))          // out 保存最终有序标签类型
	for _, item := range items {
		if item <= 0 {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Ints(out)
	return out
}

// normalizeInt64s 对 UID 集合去重、过滤非法值并升序输出。
func normalizeInt64s(items []int64) []int64 {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(items)) // seen 记录已出现的 UID，避免重复写入运行期集合
	out := make([]int64, 0, len(items))          // out 保存最终有序 UID
	for _, item := range items {
		if item <= 0 {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
