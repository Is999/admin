package builtin

import (
	"context"

	core "admin/internal/bootstrap/components"
	"admin/internal/bootstrap/runmode"
	"admin/internal/infra/cdcx"
	cdcjobs "admin/internal/jobs/cdc"

	"github.com/Is999/go-utils/errors"
)

// newCDCConsumer 创建 Debezium CDC 消费启动组件。
func newCDCConsumer() core.Component {
	return core.NewFunc(NameCDCConsumer, func(ctx context.Context, state *core.State) error {
		_ = ctx
		if state == nil || state.ServiceContext == nil {
			return errors.Errorf("CDC 消费组件缺少服务上下文")
		}
		consumer, err := cdcx.New(state.Config.CDC)
		if err != nil {
			return errors.Tag(err)
		}
		if err = cdcjobs.RegisterProcessors(state.ServiceContext, consumer); err != nil {
			return errors.Tag(err)
		}
		if err = consumer.ValidateProcessors(); err != nil {
			return errors.Tag(err)
		}
		state.ServiceContext.CDC = consumer
		// CDC 消费循环属于 Worker 职责，API-only 不启动后台消费。
		if state.Config.CDC.Enabled && runmode.ShouldStartWorker(state.Mode) {
			if err := state.AddLifecycleHooks(NameCDCConsumer, func(context.Context) error {
				consumer.Start()
				return nil
			}, func(ctx context.Context) error {
				return errors.Tag(consumer.Stop(ctx))
			}); err != nil {
				return errors.Tag(err)
			}
		}
		return nil
	})
}
