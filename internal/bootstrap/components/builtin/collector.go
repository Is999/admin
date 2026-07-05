package builtin

import (
	"context"

	"admin/internal/audit"
	core "admin/internal/bootstrap/components"
	"admin/internal/bootstrap/runmode"
	"admin/internal/infra/collectorx"
	"admin/internal/jobs/taskalert"
	"admin/internal/svc"

	"github.com/Is999/go-utils/errors"
)

// newCollector 创建通用收集器启动组件。
func newCollector() core.Component {
	return core.NewFunc(NameCollector, func(ctx context.Context, state *core.State) error {
		_ = ctx
		if state == nil || state.ServiceContext == nil {
			return errors.Errorf("通用收集器组件缺少服务上下文")
		}
		// collector 使用 Redis 保存 EventID 幂等状态，DB 仅保存失败账本。
		manager, err := collectorx.New(state.Config.Collector, state.ServiceContext.WriteDB(svc.DatabaseMain), state.ServiceContext.Rds)
		if err != nil {
			return errors.Tag(err)
		}
		state.ServiceContext.Collector = manager
		if err = manager.RegisterProcessor(audit.AdminLogCollectorBizType, audit.NewAdminLogBatchProcessor(state.ServiceContext.WriteDB(svc.DatabaseMain))); err != nil {
			return errors.Tag(err)
		}
		if state.ServiceContext.Audit != nil {
			state.ServiceContext.Audit.SetEnqueuer(audit.NewCollectorWriter(manager))
		}
		manager.SetAlertHook(func(ctx context.Context, alert collectorx.RuntimeAlert) {
			svc.NotifyTaskRuntimeAlert(ctx, state.ServiceContext.Task, taskalert.CollectorRuntimeAlert(alert))
		})
		// 通用收集器的 Kafka 消费和失败重试循环属于 Worker 职责，API-only 不启动后台消费。
		if runmode.ShouldStartWorker(state.Mode) {
			if err := state.AddLifecycleHooks(NameCollector, func(context.Context) error {
				manager.Start()
				return nil
			}, func(ctx context.Context) error {
				return errors.Tag(manager.Stop(ctx))
			}); err != nil {
				return errors.Tag(err)
			}
		}
		return nil
	})
}
