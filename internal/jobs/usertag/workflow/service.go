package workflow

import (
	"context"

	"admin/internal/config"
	"admin/internal/jobs/usertag/options"
	"admin/internal/jobs/usertag/repository"
	"admin/internal/jobs/usertag/route"
	"admin/internal/jobs/usertag/runtimectx"
	"admin/internal/jobs/usertag/stage"
	"admin/internal/jobs/usertag/types"
	"admin/internal/svc"
	"admin/internal/task/stats"

	"github.com/Is999/go-utils/errors"
)

const (
	// traceUserTag 表示用户标签处理量明细前缀。
	traceUserTag = "user_tag"
	// traceUserTagScanned 表示用户标签阶段扫描数量明细。
	traceUserTagScanned = "scanned"
	// traceUserTagUpdated 表示用户标签阶段更新数量明细。
	traceUserTagUpdated = "updated"
)

// Service 是 usertag 工作流编排服务。
// 当前只保留通用骨架节点，业务方可按需替换或补充业务阶段。
type Service struct {
	ctx      context.Context        // 当前请求上下文
	svcCtx   *svc.ServiceContext    // 全局依赖上下文
	defaults options.Defaults       // 默认运行参数
	repos    repository.RuntimeDeps // 外部依赖集合
	tagRepo  *repository.TagRepository
	stages   map[string]stage.Stage // 已注册阶段，key 为阶段名称
}

// NewService 创建 usertag 工作流服务。
// 初始化时会从配置解析默认运行参数，构建统一分片计划和仓储运行依赖，
// 并注册 prepare、business_hook、finalize、sync_kafka 等骨架阶段。
func NewService(ctx context.Context, svcCtx *svc.ServiceContext) *Service {
	// 允许调用方省略 ctx，任务入口未透传时使用后台上下文兜底。
	if ctx == nil {
		ctx = context.Background()
	}

	// 从当前服务配置解析用户标签默认参数；测试或离线构造时用空配置兜底。
	var defaults options.Defaults
	if svcCtx != nil {
		cfg := svcCtx.CurrentConfig()
		defaults = options.NewDefaults(cfg.Workflows.UserTag)
	} else {
		defaults = options.NewDefaults(config.UserTagConfig{})
	}

	// 分片计划和 RuntimeDeps 必须在所有仓储之间共享，保证骨架阶段使用同一套运行期分片口径。
	shardPlan := route.NewShardPlan(defaults.ShardTotal, defaults.RuntimeShardTotal)
	service := &Service{
		ctx:      ctx,
		svcCtx:   svcCtx,
		defaults: defaults,
		repos:    repository.NewRuntimeDeps(svcCtx, shardPlan),
		stages:   make(map[string]stage.Stage),
	}

	// TagRepository 承担工作流状态、结果表、Kafka outbox 等共享能力，多个阶段复用同一个实例。
	tagRepo := repository.NewTagRepository(service.repos)
	service.tagRepo = tagRepo

	// 固定骨架阶段在构造时集中注册，后续 RunStage 只按 payload.Node 查表执行。
	// prepare：准备本轮工作流运行环境，清理运行期状态，并在 full 模式处理临时结果表。
	_ = service.RegisterStage(stage.NewPrepareStage(tagRepo))
	// business_hook：业务扩展占位阶段，业务项目可替换或追加具体清洗、计算阶段。
	_ = service.RegisterStage(stage.NewBusinessHookStage())
	// finalize：完成 full 模式结果表原子切换和必要状态收尾。
	_ = service.RegisterStage(stage.NewFinalizeStage(tagRepo))
	// sync_kafka：重建 full 同步快照，节点名保留兼容历史 DAG。
	_ = service.RegisterStage(stage.NewSyncKafkaStage(tagRepo))
	return service
}

// RegisterStage 注册一个 阶段。
func (s *Service) RegisterStage(item stage.Stage) error {
	if item == nil {
		return errors.Errorf("用户标签阶段不能为空")
	}
	name := item.Name()
	if name == "" {
		return errors.Errorf("用户标签阶段名称不能为空")
	}
	s.stages[name] = item
	return nil
}

// NewRuntimeContext 根据负载创建带统一日志链路的节点上下文。
func (s *Service) NewRuntimeContext(payload types.WorkflowPayload) (*runtimectx.Context, error) {
	opts, err := options.ParseOptions(payload, s.defaults)
	if err != nil {
		return nil, errors.Tag(err)
	}
	node := payload.Node
	if node == "" {
		node = types.NodePrepare
	}
	return runtimectx.New(s.ctx, s.svcCtx, opts, node), nil
}

// StageNames 返回当前已注册阶段名称，便于测试和诊断。
func (s *Service) StageNames() []string {
	names := make([]string, 0, len(s.stages))
	for name := range s.stages {
		names = append(names, name)
	}
	return names
}

// RunStage 执行单个 阶段。
func (s *Service) RunStage(payload types.WorkflowPayload) (stage.Result, error) {
	runtimeCtx, err := s.NewRuntimeContext(payload)
	if err != nil {
		return stage.Result{}, errors.Tag(err)
	}
	item, ok := s.stages[runtimeCtx.Node]
	if !ok {
		return stage.Result{}, errors.Errorf("用户标签阶段未注册 node=%s", runtimeCtx.Node)
	}
	if err := s.ensureWorkflowLease(runtimeCtx); err != nil {
		return stage.Result{}, runtimeCtx.Wrap(err, "获取工作流互斥租约失败")
	}
	runtimeCtx.Infof("阶段开始")
	result, err := item.Run(runtimeCtx, nil)
	if err != nil {
		recordStageTrace(runtimeCtx, result)
		runtimeCtx.Errorf("阶段失败 err=%v", err)
		return result, errors.Tag(err)
	}
	if runtimeCtx.Node == types.NodeSyncKafka {
		if err := s.releaseWorkflowLease(runtimeCtx); err != nil {
			recordStageTrace(runtimeCtx, result)
			return result, runtimeCtx.Wrap(err, "释放工作流互斥租约失败")
		}
	}
	recordStageTrace(runtimeCtx, result)
	runtimeCtx.Infof("阶段完成 scanned=%d updated=%d skipped=%v", result.Scanned, result.Updated, result.Skipped)
	return result, nil
}

// recordStageTrace 记录用户标签阶段处理量，供任务详情和日志统一展示。
func recordStageTrace(ctx *runtimectx.Context, result stage.Result) {
	if ctx == nil {
		return
	}
	name := taskstats.JoinDetailName(traceUserTag, ctx.Node)
	if result.Scanned > 0 {
		taskstats.RecordRead(ctx.Context, taskstats.JoinDetailName(name, traceUserTagScanned), result.Scanned)
	}
	if result.Updated > 0 {
		taskstats.RecordUpdate(ctx.Context, taskstats.JoinDetailName(name, traceUserTagUpdated), result.Updated)
	}
	if result.Skipped {
		taskstats.RecordSkip(ctx.Context, taskstats.JoinDetailName(name, taskstats.DetailPartSkipped), 1)
	}
}

// ensureWorkflowLease 确保当前节点满足用户标签工作流租约约束。
// prepare 节点负责首次获取或校验租约，后续节点通过续租确认工作流 owner 仍然有效。
func (s *Service) ensureWorkflowLease(ctx *runtimectx.Context) error {
	if s == nil || s.tagRepo == nil || ctx == nil {
		return nil
	}
	if ctx.Node == types.NodePrepare {
		return s.tagRepo.AcquireWorkflowLease(ctx.Context, ctx.Options)
	}
	return s.tagRepo.RenewWorkflowLease(ctx.Context, ctx.Options)
}

// releaseWorkflowLease 在工作流完成同步阶段后释放 full 模式租约。
// 非 full 模式不持有全局租约，直接跳过释放流程。
func (s *Service) releaseWorkflowLease(ctx *runtimectx.Context) error {
	if s == nil || s.tagRepo == nil || ctx == nil {
		return nil
	}
	if ctx.Options.Mode != types.ModeFull {
		// 非 full 工作流不持有全局租约，sync_kafka 跳过后也不需要参与 full 的分片释放屏障。
		return nil
	}
	return s.tagRepo.ReleaseWorkflowLeaseAfterShardDone(ctx.Context, ctx.Options)
}
