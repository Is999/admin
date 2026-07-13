//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"sort"
	"strconv"
	"strings"

	"admin/internal/jobs/usertag/options"
	tasklimits "admin/internal/task/limits"

	"github.com/Is999/go-utils/errors"
)

// TriggerUserTagWorkflowReq 触发用户标签计算工作流请求。
type TriggerUserTagWorkflowReq struct {
	Mode             string   `json:"mode"`                      // 执行模式：full/delta/targeted/recalculate
	TagTypes         []int    `json:"tagTypes,optional"`         // 指定标签类型
	UIDs             []string `json:"uids,optional"`             // 指定用户 UID，JSON 使用十进制字符串避免前端精度丢失
	Queue            string   `json:"queue,optional"`            // 指定任务队列
	ShardTotal       int      `json:"shardTotal,optional"`       // 分片数
	BatchSize        int      `json:"batchSize,optional"`        // 游标批次大小
	WorkerCount      int      `json:"workerCount,optional"`      // 节点内部 worker 数
	DryRun           bool     `json:"dryRun,optional"`           // 只计算不落库
	UniqueKey        string   `json:"uniqueKey,optional"`        // 去重键
	UniqueTTLSeconds *int     `json:"uniqueTTLSeconds,optional"` // 去重 TTL 秒
	Retry            *int     `json:"retry,optional"`            // 覆盖默认重试次数
	TimeoutSeconds   *int     `json:"timeoutSeconds,optional"`   // 触发任务超时时间
}

// ReleaseUserTagWorkflowLeaseReq 手动释放用户标签工作流互斥租约请求。
type ReleaseUserTagWorkflowLeaseReq struct {
	WorkflowID   string `json:"workflowId"`            // 待释放租约所属工作流实例 ID，必须与 Redis owner 完整匹配
	Mode         string `json:"mode,optional"`         // 工作流运行模式；留空默认 full，释放时参与 owner 精确匹配
	Reason       string `json:"reason"`                // 人工释放原因，用于审计日志确认已排除运行中切表或事件派发风险
	TwoStepKey   string `json:"twoStepKey,optional"`   // MFA 二次校验票据 key，系统强制 MFA 时必填
	TwoStepValue string `json:"twoStepValue,optional"` // MFA 二次校验票据 value，系统强制 MFA 时必填
}

// RecalculateUserTagReq 表示 admin 指定标签重算请求。
type RecalculateUserTagReq struct {
	TagTypes         []int  `json:"tag_types"`                   // 指定重算的标签类型
	Queue            string `json:"queue,optional"`              // 指定任务队列
	ShardTotal       int    `json:"shard_total,optional"`        // 分片数
	BatchSize        int    `json:"batch_size,optional"`         // 游标批次大小
	WorkerCount      int    `json:"worker_count,optional"`       // 节点内部 worker 数
	DryRun           bool   `json:"dry_run,optional"`            // 只计算不落库
	UniqueTTLSeconds *int   `json:"unique_ttl_seconds,optional"` // 去重 TTL 秒
	Retry            *int   `json:"retry,optional"`              // 覆盖默认重试次数
	TimeoutSeconds   *int   `json:"timeout_seconds,optional"`    // 触发任务超时时间
}

// Validate 校验并归一化 admin 指定标签重算请求。
func (r *RecalculateUserTagReq) Validate() error {
	r.TagTypes = normalizeUserTagTypes(r.TagTypes)
	if len(r.TagTypes) == 0 {
		return errors.Errorf("tag_types 不能为空")
	}
	if r.ShardTotal < 0 || r.BatchSize < 0 || r.WorkerCount < 0 {
		return errors.Errorf("shard_total、batch_size、worker_count 不能小于 0")
	}
	if len(r.TagTypes) > options.MaxTagTypes {
		return errors.Errorf("tagTypes 数量不能超过 %d", options.MaxTagTypes)
	}
	if r.ShardTotal > options.MaxShardTotal {
		return errors.Errorf("shard_total 不能超过 %d", options.MaxShardTotal)
	}
	if r.BatchSize > options.MaxBatchSize {
		return errors.Errorf("batch_size 不能超过 %d", options.MaxBatchSize)
	}
	if r.WorkerCount > options.MaxWorkerCount {
		return errors.Errorf("worker_count 不能超过 %d", options.MaxWorkerCount)
	}
	if r.UniqueTTLSeconds != nil && *r.UniqueTTLSeconds <= 0 {
		return errors.Errorf("unique_ttl_seconds 必须大于 0")
	}
	if r.Retry != nil && *r.Retry < 0 {
		return errors.Errorf("retry 不能小于 0")
	}
	if r.Retry != nil && *r.Retry > tasklimits.MaxRetry {
		return errors.Errorf("retry 不能超过 %d", tasklimits.MaxRetry)
	}
	if r.TimeoutSeconds != nil && *r.TimeoutSeconds <= 0 {
		return errors.Errorf("timeout_seconds 必须大于 0")
	}
	if r.TimeoutSeconds != nil && *r.TimeoutSeconds > tasklimits.MaxTimeoutSeconds {
		return errors.Errorf("timeout_seconds 不能超过 %d", tasklimits.MaxTimeoutSeconds)
	}
	if !r.DryRun {
		return errors.Errorf("用户标签业务计算阶段尚未实现，仅允许 dry_run 骨架验证")
	}
	return nil
}

// Validate 校验并归一化用户标签工作流租约释放请求。
func (r *ReleaseUserTagWorkflowLeaseReq) Validate() error {
	r.WorkflowID = strings.TrimSpace(r.WorkflowID)
	r.Mode = strings.ToLower(strings.TrimSpace(r.Mode))
	r.Reason = strings.TrimSpace(r.Reason)
	if r.Mode == "" {
		r.Mode = "full"
	}
	if r.WorkflowID == "" {
		return errors.Errorf("workflowId 不能为空")
	}
	switch r.Mode {
	case "full", "delta", "targeted", "recalculate":
	default:
		return errors.Errorf("mode 必须为 full、delta、targeted 或 recalculate")
	}
	if r.Reason == "" {
		return errors.Errorf("reason 不能为空")
	}
	if len([]rune(r.Reason)) > 500 {
		return errors.Errorf("reason 不能超过 500 个字符")
	}
	return nil
}

// ReleaseUserTagWorkflowLeaseResp 是手动释放用户标签工作流互斥租约的回执。
type ReleaseUserTagWorkflowLeaseResp struct {
	WorkflowID   string `json:"workflowId"`             // 已请求释放的工作流实例 ID
	Mode         string `json:"mode"`                   // 已请求释放的工作流运行模式
	Owner        string `json:"owner"`                  // 请求期望匹配的 Redis owner
	CurrentOwner string `json:"currentOwner,omitempty"` // 释放前 Redis 中实际 owner，owner 不匹配时用于排障
	LeaseKey     string `json:"leaseKey"`               // 本次操作精确访问的 Redis 租约 key
	TTLSeconds   int64  `json:"ttlSeconds"`             // 释放前租约剩余 TTL 秒数
	Released     bool   `json:"released"`               // 是否成功释放
	ReleasedAt   string `json:"releasedAt"`             // 后端确认释放的时间，RFC3339 格式
	Reason       string `json:"reason"`                 // 人工释放原因，回显便于前端与审计核对
}

// RecalculateUserTagResp 是 admin 指定标签重算的回执。
type RecalculateUserTagResp struct {
	Message      string `json:"message"`      // 提示信息
	TagCount     int    `json:"tag_count"`    // 本次重算标签数量
	TaskID       string `json:"taskId"`       // 触发任务 ID
	WorkflowID   string `json:"workflowId"`   // 工作流实例 ID
	WorkflowName string `json:"workflowName"` // 工作流名称
	Queue        string `json:"queue"`        // 投递队列
}

// normalizeUserTagTypes 对标签类型去重、过滤非法值并保持升序，便于生成稳定 unique_key。
func normalizeUserTagTypes(items []int) []int {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(items))
	out := make([]int, 0, len(items))
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

// Validate 校验用户标签工作流触发请求。
func (r *TriggerUserTagWorkflowReq) Validate() error {
	r.Mode = strings.ToLower(strings.TrimSpace(r.Mode))
	if r.Mode == "" {
		r.Mode = "full"
	}
	r.TagTypes = normalizeUserTagTypes(r.TagTypes)
	var err error
	r.UIDs, err = normalizeUserTagUIDs(r.UIDs)
	if err != nil {
		return errors.Tag(err)
	}
	switch r.Mode {
	case "full", "delta", "targeted", "recalculate":
	default:
		return errors.Errorf("mode 必须为 full、delta、targeted 或 recalculate")
	}
	if r.Mode == "full" && (len(r.UIDs) > 0 || len(r.TagTypes) > 0) {
		return errors.Errorf("full 模式是全站切表重建，不能提供 uids 或 tagTypes；指定 UID 请使用 targeted，指定标签请使用 recalculate")
	}
	if r.Mode == "targeted" {
		if len(r.UIDs) == 0 {
			return errors.Errorf("targeted 模式必须提供 uids，不能使用多维筛选 hash 作为用户标签计算来源")
		}
		if len(r.TagTypes) == 0 {
			return errors.Errorf("targeted 模式必须提供 tagTypes")
		}
	}
	if r.Mode == "recalculate" && len(r.TagTypes) == 0 {
		return errors.Errorf("recalculate 模式必须提供 tagTypes")
	}
	if r.ShardTotal < 0 || r.BatchSize < 0 || r.WorkerCount < 0 {
		return errors.Errorf("shardTotal、batchSize、workerCount 不能小于 0")
	}
	if len(r.UIDs) > options.MaxTargetUIDs {
		return errors.Errorf("uids 数量不能超过 %d", options.MaxTargetUIDs)
	}
	if len(r.TagTypes) > options.MaxTagTypes {
		return errors.Errorf("tagTypes 数量不能超过 %d", options.MaxTagTypes)
	}
	if r.ShardTotal > options.MaxShardTotal {
		return errors.Errorf("shardTotal 不能超过 %d", options.MaxShardTotal)
	}
	if r.BatchSize > options.MaxBatchSize {
		return errors.Errorf("batchSize 不能超过 %d", options.MaxBatchSize)
	}
	if r.WorkerCount > options.MaxWorkerCount {
		return errors.Errorf("workerCount 不能超过 %d", options.MaxWorkerCount)
	}
	if r.UniqueTTLSeconds != nil && *r.UniqueTTLSeconds <= 0 {
		return errors.Errorf("uniqueTTLSeconds 必须大于 0")
	}
	if r.Retry != nil && *r.Retry < 0 {
		return errors.Errorf("retry 不能小于 0")
	}
	if r.Retry != nil && *r.Retry > tasklimits.MaxRetry {
		return errors.Errorf("retry 不能超过 %d", tasklimits.MaxRetry)
	}
	if r.TimeoutSeconds != nil && *r.TimeoutSeconds <= 0 {
		return errors.Errorf("timeoutSeconds 必须大于 0")
	}
	if r.TimeoutSeconds != nil && *r.TimeoutSeconds > tasklimits.MaxTimeoutSeconds {
		return errors.Errorf("timeoutSeconds 不能超过 %d", tasklimits.MaxTimeoutSeconds)
	}
	if !r.DryRun {
		return errors.Errorf("用户标签业务计算阶段尚未实现，仅允许 dryRun 骨架验证")
	}
	return nil
}

// normalizeUserTagUIDs 校验十进制字符串 UID，并按数值去重升序，避免 JavaScript 大整数精度丢失。
func normalizeUserTagUIDs(items []string) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	seen := make(map[int64]struct{}, len(items))
	values := make([]int64, 0, len(items)) // values 保存已校验 UID，排序后再转回规范十进制字符串
	for _, item := range items {
		item = strings.TrimSpace(item)
		uid, err := strconv.ParseInt(item, 10, 64)
		if err != nil || uid <= 0 {
			return nil, errors.Errorf("uid 必须为正 int64 十进制字符串: %s", item)
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		values = append(values, uid)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	out := make([]string, 0, len(values))
	for _, uid := range values {
		out = append(out, strconv.FormatInt(uid, 10))
	}
	return out, nil
}
