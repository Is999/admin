package usertag

import (
	corelogic "admin/internal/logic"
	adminlogic "admin/internal/logic/admin"
	securitylogic "admin/internal/logic/security"
	"crypto/sha1"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	keys "admin/common/rediskeys"
	"admin/internal/config"
	usertagoptions "admin/internal/jobs/usertag/options"
	usertagrepo "admin/internal/jobs/usertag/repository"
	usertagroute "admin/internal/jobs/usertag/route"
	usertagtask "admin/internal/jobs/usertag/task"
	usertagtypes "admin/internal/jobs/usertag/types"
	"admin/internal/svc"
	"admin/internal/task/queue"
	"admin/internal/types"

	"github.com/hibiken/asynq"
)

// UserTagLogic 封装用户标签计算工作流接口逻辑。
type UserTagLogic struct {
	*corelogic.BaseLogic // 复用上下文、日志和任务依赖访问能力
}

// NewUserTagLogic 创建用户标签逻辑对象。
func NewUserTagLogic(r *http.Request, svcCtx *svc.ServiceContext) *UserTagLogic {
	return &UserTagLogic{BaseLogic: corelogic.NewBaseLogic(r, svcCtx)}
}

// TriggerWorkflow 触发用户标签计算工作流。
func (l *UserTagLogic) TriggerWorkflow(req *types.TriggerUserTagWorkflowReq) *types.BizResult {
	// 先校验模式、标签、UID、分片等入口参数，避免无效请求直接进入任务队列。
	if err := req.Validate(); err != nil {
		return types.NewBizResult(codes.ParamError).WithError(corelogic.WrapLogicError(err, "UserTagLogic.TriggerWorkflow 参数校验失败"))
	}
	return l.enqueueWorkflow(req, nil)
}

// RecalculateByTagTypes 后台指定标签重算接口。
func (l *UserTagLogic) RecalculateByTagTypes(req *types.RecalculateUserTagReq) *types.BizResult {
	// admin 侧只关心“指定标签重算”，这里统一转换成用户标签工作流触发请求，复用同一套 DAG 能力。
	if err := req.Validate(); err != nil {
		return types.NewBizResult(codes.ParamError).WithError(corelogic.WrapLogicError(err, "UserTagLogic.RecalculateByTagTypes 参数校验失败"))
	}

	// 把 admin 请求补齐为内部工作流参数，确保重算模式、幂等键和超时配置都能透传到任务系统。
	triggerReq := &types.TriggerUserTagWorkflowReq{
		Mode:             usertagtask.ModeRecalculate,
		TagTypes:         req.TagTypes,
		Queue:            req.Queue,
		ShardTotal:       req.ShardTotal,
		BatchSize:        req.BatchSize,
		WorkerCount:      req.WorkerCount,
		DryRun:           req.DryRun,
		UniqueTTLSeconds: req.UniqueTTLSeconds,
		Retry:            req.Retry,
		TimeoutSeconds:   req.TimeoutSeconds,
	}

	// 返回结构按 admin 历史接口习惯裁剪，避免把底层任务响应细节直接暴露给上层调用方。
	return l.enqueueWorkflow(triggerReq, func(resp *types.TaskWorkflowTriggerResp) any {
		return types.RecalculateUserTagResp{
			Message:      l.Message(i18n.MsgKeyUserTagRecalculateStarted, len(req.TagTypes)),
			TagCount:     len(req.TagTypes),
			TaskID:       resp.TaskID,
			WorkflowID:   resp.WorkflowID,
			WorkflowName: resp.WorkflowName,
			Queue:        resp.Queue,
		}
	})
}

// ReleaseWorkflowLease 手动释放用户标签工作流互斥租约。
func (l *UserTagLogic) ReleaseWorkflowLease(req *types.ReleaseUserTagWorkflowLeaseReq) *types.BizResult {
	// 人工释放属于生产兜底操作，入口必须先校验 workflowID、mode 和原因，避免误删仍在运行的 full 租约。
	if err := req.Validate(); err != nil {
		return types.NewBizResult(codes.ParamError).WithError(corelogic.WrapLogicError(err, "UserTagLogic.ReleaseWorkflowLease 参数校验失败"))
	}
	adminLogic := &adminlogic.AdminLogic{BaseLogic: l.BaseLogic}
	if err := adminLogic.RequireOperateMFATwoStep(securitylogic.MFAScenarioUserTagLeaseRelease, req.TwoStepKey, req.TwoStepValue); err != nil {
		return adminLogic.MFABizResult(err)
	}
	if l.Svc == nil || l.Svc.Rds == nil {
		return types.NewBizResult(codes.RedisUnavailable).
			SetI18nMessage(i18n.MsgKeyRedisUnavailable).
			WithError(corelogic.WrapLogicError(errors.Errorf("用户标签工作流互斥租约需要 Redis，但 Redis 未初始化 workflow_id=%s mode=%s", req.WorkflowID, req.Mode), "UserTagLogic.ReleaseWorkflowLease Redis不可用"))
	}
	defaults := usertagoptions.NewDefaults(l.Svc.CurrentConfig().Workflows.UserTag)
	repo := usertagrepo.NewTagRepository(usertagrepo.NewRuntimeDeps(l.Svc, usertagroute.NewShardPlanWithResult(defaults.ShardTotal, defaults.RuntimeShardTotal, defaults.ResultShardTotal)))
	releaseResult, err := repo.ManualReleaseWorkflowLease(l.Ctx, usertagtypes.RuntimeOptions{
		WorkflowID: req.WorkflowID,
		Mode:       req.Mode,
	})
	if err == nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyUserTagLeaseReleaseSuccess).
			WithData(buildReleaseWorkflowLeaseResp(releaseResult, req.Reason))
	}
	switch {
	case errors.Is(err, usertagrepo.ErrWorkflowLeaseNotFound):
		return types.NewBizResult(codes.UserTagWorkflowLeaseNotFound).
			SetI18nMessage(i18n.MsgKeyUserTagLeaseNotFound).
			WithError(corelogic.WrapLogicError(err, "UserTagLogic.ReleaseWorkflowLease 租约不存在 workflow_id=%s mode=%s", req.WorkflowID, req.Mode))
	case errors.Is(err, usertagrepo.ErrWorkflowLeaseOwnerMismatch):
		return types.NewBizResult(codes.UserTagWorkflowLeaseOwnerMismatch).
			SetI18nMessage(i18n.MsgKeyUserTagLeaseOwnerMismatch).
			WithError(corelogic.WrapLogicError(err, "UserTagLogic.ReleaseWorkflowLease owner不匹配 workflow_id=%s mode=%s", req.WorkflowID, req.Mode))
	default:
		return types.NewBizResult(codes.UserTagWorkflowLeaseReleaseFailed).
			SetI18nMessage(i18n.MsgKeyUserTagLeaseReleaseFail).
			WithError(corelogic.WrapLogicError(err, "UserTagLogic.ReleaseWorkflowLease 释放失败 workflow_id=%s mode=%s", req.WorkflowID, req.Mode))
	}
}

// buildReleaseWorkflowLeaseResp 将仓储释放结果转换为接口回执。
func buildReleaseWorkflowLeaseResp(result *usertagrepo.WorkflowLeaseReleaseResult, reason string) types.ReleaseUserTagWorkflowLeaseResp {
	if result == nil {
		return types.ReleaseUserTagWorkflowLeaseResp{Reason: strings.TrimSpace(reason), ReleasedAt: time.Now().Format(time.RFC3339)}
	}
	return types.ReleaseUserTagWorkflowLeaseResp{
		WorkflowID:   result.WorkflowID,
		Mode:         result.Mode,
		Owner:        result.Owner,
		CurrentOwner: result.CurrentOwner,
		LeaseKey:     result.LeaseKey,
		TTLSeconds:   result.TTLSeconds,
		Released:     result.Released,
		ReleasedAt:   time.Now().Format(time.RFC3339),
		Reason:       strings.TrimSpace(reason),
	}
}

// enqueueWorkflow 统一封装用户标签工作流入队和错误码映射，避免不同入口重复维护一套任务系统适配逻辑。
func (l *UserTagLogic) enqueueWorkflow(req *types.TriggerUserTagWorkflowReq, dataBuilder func(*types.TaskWorkflowTriggerResp) any) *types.BizResult {
	// 服务未启用任务系统时直接返回忙碌态，避免接口表面成功但任务实际无法执行。
	if l.Svc == nil || l.Svc.Task == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(taskqueue.ErrTaskQueueDisabled, "UserTagLogic.enqueueWorkflow 任务系统未启用"))
	}

	// 先把用户标签领域请求转换成通用任务工作流请求，再交给任务系统统一调度。
	workflowReq := buildUserTagWorkflowReq(req, l.Svc.CurrentConfig().Workflows.UserTag)
	resp, err := l.Svc.Task.EnqueueWorkflowTrigger(l.Ctx, workflowReq)
	if err == nil {
		data := any(resp)
		if dataBuilder != nil {
			// 某些接口需要把底层工作流响应重塑成定制结构，这里统一交由回调处理。
			data = dataBuilder(resp)
		}
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyTaskTriggerSuccess).
			WithData(data)
	}

	// 任务系统错误在这里集中翻译成接口层统一业务码，便于前端和 admin 按稳定口径处理。
	switch {
	case errors.Is(err, taskqueue.ErrTaskQueueDisabled):
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(corelogic.WrapLogicError(err, "UserTagLogic.enqueueWorkflow 任务系统未启用"))
	case errors.Is(err, taskqueue.ErrWorkflowNotFound):
		return types.NotFound(i18n.MsgKeyTaskWorkflowNotFound, err).ToBizResult()
	case errors.Is(err, taskqueue.ErrWorkflowAlreadyExists), errors.Is(err, asynq.ErrDuplicateTask), errors.Is(err, asynq.ErrTaskIDConflict):
		return types.NewBizResult(codes.Fail).
			SetI18nMessage(i18n.MsgKeyTaskDuplicate).
			WithError(corelogic.WrapLogicError(err, "UserTagLogic.enqueueWorkflow 工作流任务重复"))
	default:
		return types.ServerError(i18n.MsgKeyTaskTriggerFail, err, "UserTagLogic.TriggerWorkflow").ToBizResult()
	}
}

// joinInts 把标签类型列表拼成稳定字符串，用于生成幂等键等场景。
func joinInts(items []int) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, strconv.Itoa(item))
	}
	return strings.Join(parts, "_")
}

// buildUserTagWorkflowReq 把用户标签领域参数映射成任务系统通用工作流触发请求。
func buildUserTagWorkflowReq(req *types.TriggerUserTagWorkflowReq, cfg config.UserTagConfig) *types.TriggerTaskWorkflowReq {
	// 不同运行模式对应不同 DAG 定义名，最终由任务系统按名称装载工作流。
	name := usertagtask.WorkflowNameUserTagFull
	switch strings.ToLower(req.Mode) {
	case usertagtask.ModeDelta:
		name = usertagtask.WorkflowNameUserTagDelta
	case usertagtask.ModeTargeted:
		name = usertagtask.WorkflowNameUserTagTargeted
	case usertagtask.ModeRecalculate:
		name = usertagtask.WorkflowNameUserTagRecalculate
	}

	// defaults 负责补齐标签系统在配置中的默认分片、批次等运行参数。
	defaults := usertagoptions.NewDefaults(cfg)

	// targets 承载工作流运行参数，后续由任务插件解析回领域选项。
	targets := make([]string, 0, len(req.TagTypes)+len(req.UIDs)+6)
	targets = append(targets, "mode="+req.Mode)
	for _, tagType := range req.TagTypes {
		if tagType > 0 {
			targets = append(targets, "tag="+strconv.Itoa(tagType))
		}
	}
	for _, uid := range req.UIDs {
		targets = append(targets, "uid="+uid)
	}
	if req.BatchSize > 0 {
		targets = append(targets, "batch_size="+strconv.Itoa(req.BatchSize))
	}
	if req.WorkerCount > 0 {
		targets = append(targets, "worker_count="+strconv.Itoa(req.WorkerCount))
	}
	if req.DryRun {
		targets = append(targets, "dry_run=1")
	}

	// 未显式传分片数时，优先复用 worker_count，其次回退到用户标签模块默认配置。
	shardTotal := req.ShardTotal
	if shardTotal <= 0 && req.WorkerCount > 0 {
		shardTotal = req.WorkerCount
	}
	if shardTotal <= 0 {
		shardTotal = defaults.ShardTotal
	}

	// 幂等键允许外部覆盖；默认按规范化计算参数生成指纹，做到同参去重、异参并发。
	uniqueKey := strings.TrimSpace(req.UniqueKey)
	if uniqueKey == "" {
		uniqueKey = defaultUserTagUniqueKey(req)
	}
	return &types.TriggerTaskWorkflowReq{
		Name:             name,
		Targets:          targets,
		Queue:            strings.TrimSpace(req.Queue),
		ShardTotal:       shardTotal,
		GrayPercent:      100,
		UniqueKey:        uniqueKey,
		UniqueTTLSeconds: req.UniqueTTLSeconds,
		Retry:            req.Retry,
		TimeoutSeconds:   req.TimeoutSeconds,
	}
}

// defaultUserTagUniqueKey 基于会影响计算结果的规范化参数生成默认去重键。
// 该键只阻止完全相同请求在去重 TTL 内重复入队。
func defaultUserTagUniqueKey(req *types.TriggerUserTagWorkflowReq) string {
	if req == nil {
		return ""
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = usertagtask.ModeFull
	}
	canonical := strings.Join([]string{
		"mode=" + mode,
		"tags=" + joinInts(req.TagTypes),
		"uids=" + strings.Join(req.UIDs, "_"),
		"shard_total=" + strconv.Itoa(req.ShardTotal),
		"batch_size=" + strconv.Itoa(req.BatchSize),
		"worker_count=" + strconv.Itoa(req.WorkerCount),
		"dry_run=" + strconv.FormatBool(req.DryRun),
	}, "|")
	sum := sha1.Sum([]byte(canonical))
	return fmt.Sprintf(keys.UserTagWorkflowUniqueSegment, mode, sum[:8])
}
