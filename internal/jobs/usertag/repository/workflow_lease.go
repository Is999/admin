package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	keys "admin/common/rediskeys"
	"admin/internal/jobs/usertag/types"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
)

const (
	userTagWorkflowLeaseTTL = 24 * time.Hour
	// userTagWorkflowLeaseOwnerSeparator 仅用于提升 owner 可读性。
	userTagWorkflowLeaseOwnerSeparator = "|"
)

const (
	userTagWorkflowTerminalSuccess   = "success"   // 工作流成功终态，可安全清理遗留非 full 租约
	userTagWorkflowTerminalFailed    = "failed"    // 工作流失败终态，可安全清理遗留非 full 租约
	userTagWorkflowTerminalCompleted = "completed" // 兼容任务中心已完成展示状态，避免 UI 状态映射差异导致锁无法自愈
	userTagWorkflowTerminalArchived  = "archived"  // 兼容任务中心已归档展示状态，避免旧任务归档后仍残留非 full 租约
)

var (
	// ErrWorkflowLeaseNotFound 表示待释放的用户标签工作流互斥租约不存在，通常说明租约已过期或已被正确释放。
	ErrWorkflowLeaseNotFound = errors.New("用户标签工作流互斥租约不存在")
	// ErrWorkflowLeaseOwnerMismatch 表示请求 owner 与 Redis 当前 owner 不一致。
	ErrWorkflowLeaseOwnerMismatch = errors.New("用户标签工作流互斥租约 owner 不匹配")
)

// WorkflowLeaseReleaseResult 表示一次用户标签工作流互斥租约释放尝试的诊断结果。
type WorkflowLeaseReleaseResult struct {
	WorkflowID   string // 请求释放的工作流实例 ID
	Mode         string // 请求释放的工作流运行模式
	Owner        string // 请求期望匹配的 Redis owner，格式为 `workflowID|mode`
	CurrentOwner string // Redis 中实际读取到的 owner，便于人工确认是否误选工作流
	LeaseKey     string // Redis 中精确释放的互斥租约 key
	TTLSeconds   int64  // 释放前 Redis 返回的剩余 TTL 秒数；-1 表示无过期时间，-2 表示 key 不存在
	Released     bool   // 是否完成了 owner 精确匹配后的释放动作
}

// AcquireWorkflowLease 获取用户标签工作流租约。
// full 持有全局租约；非 full 只检查是否有运行中的 full。
func (r *TagRepository) AcquireWorkflowLease(ctx context.Context, opts types.RuntimeOptions) error {
	return r.ensureWorkflowLease(ctx, opts)
}

// RenewWorkflowLease 校验并续期用户标签工作流租约。
// full 续期全局租约；非 full 只感知 full 是否运行中。
func (r *TagRepository) RenewWorkflowLease(ctx context.Context, opts types.RuntimeOptions) error {
	return r.ensureWorkflowLease(ctx, opts)
}

// ensureWorkflowLease 确认当前用户标签工作流满足租约约束。
// full 获取或续期全局租约；非 full 只检查 full 租约。
func (r *TagRepository) ensureWorkflowLease(ctx context.Context, opts types.RuntimeOptions) error {
	if opts.DryRun {
		return nil
	}
	if !workflowRequiresExclusiveLease(opts) {
		return r.ensureNoFullWorkflowLease(ctx)
	}
	return r.ensureExclusiveWorkflowLease(ctx, opts)
}

// ensureExclusiveWorkflowLease 确保 full 工作流独占全局写租约。
// full 会切换结果表并推进同步基线，必须独占写窗口。
func (r *TagRepository) ensureExclusiveWorkflowLease(ctx context.Context, opts types.RuntimeOptions) error {
	client, err := r.workflowLeaseClient()
	if err != nil {
		return errors.Tag(err)
	}
	owner, err := workflowLeaseOwner(opts)
	if err != nil {
		return errors.Tag(err)
	}
	if terminal, terminalErr := r.workflowOwnerReachedTerminal(ctx, opts.WorkflowID); terminalErr != nil {
		return errors.Tag(terminalErr)
	} else if terminal {
		// full 已终态时残留分片不再抢回租约。
		_, _ = r.releaseWorkflowLeaseOwner(ctx, client, owner, opts.WorkflowID)
		return errors.Errorf("用户标签 full 工作流已进入终态 workflow_id=%s mode=%s", strings.TrimSpace(opts.WorkflowID), strings.TrimSpace(opts.Mode))
	}
	key, err := r.workflowLeaseKey()
	if err != nil {
		return errors.Tag(err)
	}
	locked, err := client.SetNX(ctx, key, owner, userTagWorkflowLeaseTTL).Result()
	if err != nil {
		return errors.Wrap(err, "获取用户标签工作流互斥租约失败")
	}
	if locked {
		return nil
	}
	renewed, err := renewWorkflowLease(ctx, client, key, owner)
	if err != nil {
		return errors.Tag(err)
	}
	if renewed {
		return nil
	}
	current, err := client.Get(ctx, key).Result()
	if err == redis.Nil {
		locked, err = client.SetNX(ctx, key, owner, userTagWorkflowLeaseTTL).Result()
		if err != nil {
			return errors.Wrap(err, "重试获取用户标签工作流互斥租约失败")
		}
		if locked {
			return nil
		}
		current, _ = client.Get(ctx, key).Result()
	} else if err != nil {
		return errors.Wrap(err, "读取用户标签工作流互斥租约失败")
	}
	if released, releaseErr := r.releaseTerminalNonFullWorkflowLease(ctx, current); releaseErr != nil {
		return errors.Tag(releaseErr)
	} else if released {
		locked, err = client.SetNX(ctx, key, owner, userTagWorkflowLeaseTTL).Result()
		if err != nil {
			return errors.Wrap(err, "清理遗留非 full 租约后获取用户标签工作流互斥租约失败")
		}
		if locked {
			return nil
		}
		current, _ = client.Get(ctx, key).Result()
	}
	if released, releaseErr := r.releaseTerminalFullWorkflowLease(ctx, current); releaseErr != nil {
		return errors.Tag(releaseErr)
	} else if released {
		locked, err = client.SetNX(ctx, key, owner, userTagWorkflowLeaseTTL).Result()
		if err != nil {
			return errors.Wrap(err, "清理遗留 full 租约后获取用户标签工作流互斥租约失败")
		}
		if locked {
			return nil
		}
		current, _ = client.Get(ctx, key).Result()
	}
	return errors.Errorf("已有用户标签工作流正在运行 owner=%s", current)
}

// ensureNoFullWorkflowLease 确认当前没有 full 工作流持有全局租约。
// 非 full 不持有全局写锁，这里仅把 full 作为互斥边界。
func (r *TagRepository) ensureNoFullWorkflowLease(ctx context.Context) error {
	client, err := r.workflowLeaseClient()
	if err != nil {
		return errors.Tag(err)
	}
	key, err := r.workflowLeaseKey()
	if err != nil {
		return errors.Tag(err)
	}
	current, err := client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return errors.Wrap(err, "读取用户标签 full 工作流互斥租约失败")
	}
	if workflowLeaseOwnerMode(current) == types.ModeFull {
		if released, releaseErr := r.releaseTerminalFullWorkflowLease(ctx, current); releaseErr != nil {
			return errors.Tag(releaseErr)
		} else if released {
			return nil
		}
		return errors.Errorf("已有用户标签 full 工作流正在运行 owner=%s", current)
	}
	// 非 full 遗留 owner 已终态时可精确清理。
	_, _ = r.releaseTerminalNonFullWorkflowLease(ctx, current)
	return nil
}

// releaseTerminalNonFullWorkflowLease 清理已终态的非 full 遗留租约。
// 只清理已终态的非 full 旧 owner，删除前必须按完整 owner 比对。
func (r *TagRepository) releaseTerminalNonFullWorkflowLease(ctx context.Context, owner string) (bool, error) {
	workflowID, mode, ok := workflowLeaseOwnerParts(owner)
	if !ok || mode == types.ModeFull {
		return false, nil
	}
	terminal, err := r.workflowOwnerReachedTerminal(ctx, workflowID)
	if err != nil {
		return false, errors.Tag(err)
	}
	if !terminal {
		return false, nil
	}
	client, err := r.workflowLeaseClient()
	if err != nil {
		return false, errors.Tag(err)
	}
	deleted, err := r.releaseWorkflowLeaseOwner(ctx, client, owner, workflowID)
	if err != nil {
		return false, errors.Wrapf(err, "清理已终态非 full 用户标签租约失败 workflow_id=%s mode=%s owner=%s", workflowID, mode, owner)
	}
	return deleted, nil
}

// releaseTerminalFullWorkflowLease 清理已经到终态的 full 遗留租约。
// 只有任务系统证明 workflow 已终态时，才允许删除 full owner。
func (r *TagRepository) releaseTerminalFullWorkflowLease(ctx context.Context, owner string) (bool, error) {
	workflowID, mode, ok := workflowLeaseOwnerParts(owner)
	if !ok || mode != types.ModeFull {
		return false, nil
	}
	terminal, err := r.workflowOwnerReachedTerminal(ctx, workflowID)
	if err != nil {
		return false, errors.Tag(err)
	}
	if !terminal {
		return false, nil
	}
	client, err := r.workflowLeaseClient()
	if err != nil {
		return false, errors.Tag(err)
	}
	deleted, err := r.releaseWorkflowLeaseOwner(ctx, client, owner, workflowID)
	if err != nil {
		return false, errors.Wrapf(err, "清理已终态 full 用户标签租约失败 workflow_id=%s mode=%s owner=%s", workflowID, mode, owner)
	}
	return deleted, nil
}

// workflowOwnerReachedTerminal 查询 owner 对应 workflow 是否已到终态。
// 查询失败时不做清理，宁可保留人工确认，也不在无法证明终态时释放仍可能运行的历史 workflow 锁。
func (r *TagRepository) workflowOwnerReachedTerminal(ctx context.Context, workflowID string) (bool, error) {
	if r == nil || r.deps.Service == nil || r.deps.Service.Task == nil {
		return false, nil
	}
	resp, err := r.deps.Service.Task.GetWorkflowStatus(ctx, workflowID)
	if err != nil || resp == nil {
		return false, nil
	}
	return workflowStatusAllowsLeaseCleanup(resp.Status), nil
}

// ReleaseWorkflowLease 在工作流最后节点成功后释放租约；若租约已不属于当前 workflow，则不会误删。
func (r *TagRepository) ReleaseWorkflowLease(ctx context.Context, opts types.RuntimeOptions) error {
	if opts.DryRun || !workflowRequiresExclusiveLease(opts) {
		return nil
	}
	client, err := r.workflowLeaseClient()
	if err != nil {
		return errors.Tag(err)
	}
	owner, err := workflowLeaseOwner(opts)
	if err != nil {
		return errors.Tag(err)
	}
	released, err := r.releaseWorkflowLeaseOwner(ctx, client, owner, opts.WorkflowID)
	if err != nil {
		return errors.Wrap(err, "释放用户标签工作流互斥租约失败")
	}
	if !released {
		key, keyErr := r.workflowLeaseKey()
		if keyErr != nil {
			return errors.Tag(keyErr)
		}
		current, readErr := client.Get(ctx, key).Result()
		if readErr == redis.Nil {
			return nil
		}
		if readErr != nil {
			return errors.Wrap(readErr, "确认用户标签工作流互斥租约释放结果失败")
		}
		return errors.Wrapf(ErrWorkflowLeaseOwnerMismatch, "用户标签互斥租约释放时 owner 不匹配 workflow_id=%s mode=%s expect=%s current=%s", strings.TrimSpace(opts.WorkflowID), strings.TrimSpace(opts.Mode), owner, current)
	}
	return nil
}

// ManualReleaseWorkflowLease 按 workflowID/mode 精确释放用户标签工作流互斥租约。
// 只删除中心化定义的单个租约 key，且必须先比对完整 owner。
func (r *TagRepository) ManualReleaseWorkflowLease(ctx context.Context, opts types.RuntimeOptions) (*WorkflowLeaseReleaseResult, error) {
	client, err := r.workflowLeaseClient()
	if err != nil {
		return nil, errors.Tag(err)
	}
	owner, err := workflowLeaseOwner(opts)
	if err != nil {
		return nil, errors.Tag(err)
	}
	key, err := r.workflowLeaseKey()
	if err != nil {
		return nil, errors.Tag(err)
	}
	result := &WorkflowLeaseReleaseResult{
		WorkflowID: strings.TrimSpace(opts.WorkflowID),
		Mode:       strings.TrimSpace(opts.Mode),
		Owner:      owner,
		LeaseKey:   key,
	}
	ttl, ttlErr := client.TTL(ctx, key).Result()
	if ttlErr == nil {
		// Redis TTL 的负数语义保留给接口返回，便于运维判断 key 是否无过期时间或已不存在。
		result.TTLSeconds = int64(ttl / time.Second)
	}
	current, err := client.Get(ctx, key).Result()
	switch {
	case err == redis.Nil:
		result.TTLSeconds = -2
		return result, errors.Wrapf(ErrWorkflowLeaseNotFound, "用户标签互斥租约不存在 workflow_id=%s mode=%s key=%s", result.WorkflowID, result.Mode, key)
	case err != nil:
		return result, errors.Wrapf(err, "读取用户标签互斥租约失败 workflow_id=%s mode=%s key=%s", result.WorkflowID, result.Mode, key)
	}
	result.CurrentOwner = current
	if current != owner {
		return result, errors.Wrapf(ErrWorkflowLeaseOwnerMismatch, "用户标签互斥租约 owner 不匹配 workflow_id=%s mode=%s expect=%s current=%s", result.WorkflowID, result.Mode, owner, current)
	}
	released, err := r.releaseWorkflowLeaseOwner(ctx, client, owner, opts.WorkflowID)
	if err != nil {
		return result, errors.Wrapf(err, "释放用户标签互斥租约失败 workflow_id=%s mode=%s owner=%s", result.WorkflowID, result.Mode, owner)
	}
	if !released {
		// 释放脚本返回未删除表示读取和删除之间 owner 发生变化，按不匹配处理，避免给出误导性成功。
		latest, _ := client.Get(ctx, key).Result()
		result.CurrentOwner = latest
		return result, errors.Wrapf(ErrWorkflowLeaseOwnerMismatch, "用户标签互斥租约释放时 owner 已变化 workflow_id=%s mode=%s expect=%s current=%s", result.WorkflowID, result.Mode, owner, latest)
	}
	result.Released = true
	return result, nil
}

// ReleaseWorkflowLeaseAfterFinalShardDone 记录最终分片完成，并在所有分片完成后释放全局写租约。
// full 必须等待 dispatch_hooks 全部分片完成后才能释放租约。
func (r *TagRepository) ReleaseWorkflowLeaseAfterFinalShardDone(ctx context.Context, opts types.RuntimeOptions) error {
	if opts.DryRun || !workflowRequiresExclusiveLease(opts) {
		return nil
	}
	if opts.ShardTotal <= 1 {
		return r.ReleaseWorkflowLease(ctx, opts)
	}
	if opts.ShardIndex < 0 || opts.ShardIndex >= opts.ShardTotal {
		return errors.Errorf("用户标签工作流释放租约分片参数异常 shard=%d/%d", opts.ShardIndex, opts.ShardTotal)
	}
	client, err := r.workflowLeaseClient()
	if err != nil {
		return errors.Tag(err)
	}
	key, err := r.workflowFinalDoneKey(opts.WorkflowID)
	if err != nil {
		return errors.Tag(err)
	}
	count, err := userTagWorkflowShardDoneScript.Run(ctx, client, []string{key}, opts.ShardIndex, int64(userTagWorkflowLeaseTTL/time.Second)).Int64()
	if err != nil {
		return errors.Wrap(err, "记录用户标签工作流最终分片完成失败")
	}
	if count < int64(opts.ShardTotal) {
		return nil
	}
	return r.ReleaseWorkflowLease(ctx, opts)
}

// releaseWorkflowLeaseOwner 按完整 owner 原子释放全局租约，并用单 key 命令清理当前 workflow 的分片屏障。
// write_lock 用 Lua 比对 owner；final_done 释放后独立清理。
func (r *TagRepository) releaseWorkflowLeaseOwner(ctx context.Context, client redis.UniversalClient, owner string, workflowID string) (bool, error) {
	key, err := r.workflowLeaseKey()
	if err != nil {
		return false, errors.Tag(err)
	}
	deleted, err := userTagWorkflowLeaseReleaseScript.Run(ctx, client, []string{key}, owner).Int64()
	if err != nil {
		return false, errors.Tag(err)
	}
	if deleted > 0 && strings.TrimSpace(workflowID) != "" {
		finalDoneKey, keyErr := r.workflowFinalDoneKey(workflowID)
		if keyErr != nil {
			return false, errors.Tag(keyErr)
		}
		// final_done 带 TTL，清理失败不影响租约释放结果。
		_ = client.Del(ctx, finalDoneKey).Err()
	}
	return deleted > 0, nil
}

// workflowLeaseClient 返回工作流租约使用的 Redis 客户端，缺失依赖时按错误返回。
func (r *TagRepository) workflowLeaseClient() (redis.UniversalClient, error) {
	if r == nil {
		return nil, errors.Errorf("用户标签仓储未初始化，无法获取 Redis 租约客户端")
	}
	if r.deps.Redis == nil {
		return nil, errors.Errorf("用户标签工作流互斥租约需要 Redis，但 Redis 未初始化")
	}
	return r.deps.Redis, nil
}

// workflowAppID 返回用户标签工作流 Redis key 使用的 app_id。
func (r *TagRepository) workflowAppID() (string, error) {
	if r == nil || r.deps.Service == nil {
		return "", errors.Errorf("用户标签仓储上下文未初始化，无法获取 app_id")
	}
	appID := keys.NormalizeAppID(r.deps.Service.CurrentConfig().AppID)
	if appID == "" {
		return "", errors.Errorf("用户标签工作流缺少 app_id 配置")
	}
	return appID, nil
}

// workflowLeaseKey 返回当前 app_id 作用域下的用户标签写工作流租约 key。
func (r *TagRepository) workflowLeaseKey() (string, error) {
	appID, err := r.workflowAppID()
	if err != nil {
		return "", errors.Tag(err)
	}
	return keys.UserTagWorkflowLeaseRedisKey(appID), nil
}

// workflowFinalDoneKey 返回当前 workflow 的最终分片完成屏障 key。
// key 中包含 app_id 和 workflow_id，避免多站点共用 Redis 时互相影响。
func (r *TagRepository) workflowFinalDoneKey(workflowID string) (string, error) {
	appID, err := r.workflowAppID()
	if err != nil {
		return "", errors.Tag(err)
	}
	return keys.UserTagWorkflowFinalDoneRedisKey(appID, workflowID), nil
}

func workflowLeaseOwner(opts types.RuntimeOptions) (string, error) {
	workflowID := strings.TrimSpace(opts.WorkflowID)
	if workflowID == "" {
		return "", errors.Errorf("用户标签工作流互斥租约缺少 workflow_id")
	}
	return fmt.Sprintf("%s%s%s", workflowID, userTagWorkflowLeaseOwnerSeparator, strings.TrimSpace(opts.Mode)), nil
}

// workflowRequiresExclusiveLease 判断当前模式是否需要持有全局写租约。
// 只有 full 会切表并刷新只读快照；非 full 保持可并发补算，避免手动任务失败后阻塞所有用户标签入口。
func workflowRequiresExclusiveLease(opts types.RuntimeOptions) bool {
	return strings.TrimSpace(opts.Mode) == types.ModeFull
}

// workflowLeaseOwnerMode 从 Redis owner 中提取模式。
// workflow_id 可能包含分隔符，因此只识别最后一个分隔符后的模式。
func workflowLeaseOwnerMode(owner string) string {
	_, mode, ok := workflowLeaseOwnerParts(owner)
	if !ok {
		return ""
	}
	return mode
}

// workflowLeaseOwnerParts 拆分 Redis owner 中的 workflow_id 与 mode。
// owner 只用于诊断和互斥判断，真正释放锁仍然使用完整 owner 原子比较，避免误删其它 workflow 的租约。
func workflowLeaseOwnerParts(owner string) (string, string, bool) {
	idx := strings.LastIndex(owner, userTagWorkflowLeaseOwnerSeparator)
	if idx <= 0 || idx >= len(owner)-1 {
		return "", "", false
	}
	workflowID := strings.TrimSpace(owner[:idx])
	mode := strings.TrimSpace(owner[idx+len(userTagWorkflowLeaseOwnerSeparator):])
	switch mode {
	case types.ModeFull, types.ModeDelta, types.ModeTargeted, types.ModeRecalculate:
		return workflowID, mode, workflowID != ""
	default:
		return "", "", false
	}
}

// workflowStatusAllowsLeaseCleanup 判断工作流状态是否允许自动清理遗留非 full 租约。
// 同时兼容工作流状态和任务中心展示状态。
func workflowStatusAllowsLeaseCleanup(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case userTagWorkflowTerminalSuccess,
		userTagWorkflowTerminalFailed,
		userTagWorkflowTerminalCompleted,
		userTagWorkflowTerminalArchived:
		return true
	default:
		return false
	}
}

// renewWorkflowLease 在 Redis 内原子校验 owner 并刷新 TTL。
// 返回 false 表示租约不存在或已属于其他 workflow，调用方再决定是否重试获取或返回互斥错误。
func renewWorkflowLease(ctx context.Context, client redis.UniversalClient, key string, owner string) (bool, error) {
	ttlSeconds := int64(userTagWorkflowLeaseTTL / time.Second)
	if ttlSeconds <= 0 {
		return false, errors.Errorf("用户标签工作流互斥租约 TTL 配置无效")
	}
	res, err := userTagWorkflowLeaseRenewScript.Run(ctx, client, []string{key}, owner, ttlSeconds).Int64()
	if err != nil {
		return false, errors.Wrap(err, "续期用户标签工作流互斥租约失败")
	}
	return res == 1, nil
}
