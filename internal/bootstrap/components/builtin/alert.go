package builtin

import (
	"context"

	core "admin/internal/bootstrap/components"
	"admin/internal/jobs/taskalert"

	"github.com/Is999/go-utils/errors"
)

// newRuntimeAlert 创建独立运行告警组件，不受任务系统开关影响。
func newRuntimeAlert() core.Component {
	return core.NewFunc(NameRuntimeAlert, func(_ context.Context, state *core.State) error {
		if state == nil || state.ServiceContext == nil {
			return errors.Errorf("运行告警组件缺少服务上下文")
		}
		alerter, err := taskalert.NewLarkRuntimeAlerter(state.Config)
		if err != nil {
			return errors.Tag(err)
		}
		state.ServiceContext.RuntimeAlerter = alerter
		return nil
	})
}
