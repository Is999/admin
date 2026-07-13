package builtin

import (
	"context"

	core "admin/internal/bootstrap/components"
	cachelogic "admin/internal/logic/cache"

	"github.com/Is999/go-utils/errors"
)

// newSecurityCacheSync 创建安全缓存失效补偿启动组件。
func newSecurityCacheSync() core.Component {
	return core.NewFunc(NameSecurityCacheSync, func(ctx context.Context, state *core.State) error {
		_ = ctx
		if state == nil || state.ServiceContext == nil {
			return errors.Errorf("安全缓存失效补偿组件缺少服务上下文")
		}
		worker := cachelogic.NewSecurityCacheSyncWorker(state.ServiceContext)
		return errors.Tag(state.AddLifecycleHooks(NameSecurityCacheSync, worker.Start, worker.Stop))
	})
}
