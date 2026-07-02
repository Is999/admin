package builtin

import (
	"context"

	keys "admin/common/rediskeys"
	core "admin/internal/bootstrap/components"
	"admin/internal/bootstrap/runmode"
	"admin/internal/config"
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
		// collector 使用主库写连接处理 DB outbox，Redis Stream 在装配期统一追加 app_id 前缀。
		manager, err := collectorx.New(CollectorConfigWithAppID(state.Config), state.ServiceContext.WriteDB(svc.DatabaseMain), state.ServiceContext.Rds)
		if err != nil {
			return errors.Tag(err)
		}
		state.ServiceContext.Collector = manager
		manager.SetAlertHook(func(ctx context.Context, alert collectorx.RuntimeAlert) {
			svc.NotifyTaskRuntimeAlert(ctx, state.ServiceContext.Task, taskalert.CollectorRuntimeAlert(alert))
		})
		// 通用收集器的 DB/Kafka/Redis 消费循环属于 Worker 职责，API-only 不启动后台消费。
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

// CollectorConfigWithAppID 把顶层 app_id 注入 Collector Redis Stream，避免多站点共用 Redis 时串流。
func CollectorConfigWithAppID(c config.Config) config.CollectorConfig {
	cfg := c.Collector
	cfg.Redis.Stream = keys.WithPrefix(cfg.Redis.Stream)
	return cfg
}
