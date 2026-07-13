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
		if err = manager.RegisterProcessor(collectorx.BizTypeAuthSecurity, collectorx.NewAuthSecurityProcessor()); err != nil {
			return errors.Tag(err)
		}
		if state.ServiceContext.Audit != nil {
			state.ServiceContext.Audit.SetEnqueuer(audit.NewCollectorWriter(manager))
		}
		manager.SetAlertHook(func(ctx context.Context, alert collectorx.RuntimeAlert) {
			svc.NotifyTaskRuntimeAlert(ctx, state.ServiceContext.RuntimeAlerter, taskalert.CollectorRuntimeAlert(alert))
		})
		// Kafka 消费和失败重试只由 Worker 启动；所有模式都注册停止钩子以释放 Kafka writer。
		startHook := func(context.Context) error { return nil }
		if runmode.ShouldStartWorker(state.Mode) {
			startHook = func(context.Context) error {
				manager.Start()
				return nil
			}
		}
		if err := state.AddLifecycleHooks(NameCollector, startHook, func(ctx context.Context) error {
			return errors.Tag(manager.Stop(ctx))
		}); err != nil {
			return errors.Tag(err)
		}
		return nil
	})
}
