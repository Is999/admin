package bootstrap

import (
	"context"

	"admin/internal/bootstrap/components"
	bootstrapresources "admin/internal/bootstrap/resources"
	"admin/internal/config"
	"admin/internal/svc"

	"github.com/Is999/go-utils/errors"
)

// BuildServiceContext 统一完成基础设施初始化，并发布当前进程运行配置快照。
func BuildServiceContext(ctx context.Context, c config.Config) (*svc.ServiceContext, func(context.Context) error, error) {
	svcCtx, shutdown, err := bootstrapresources.BuildServiceContext(ctx, c)
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	publishRuntimeConfig(c)
	return svcCtx, shutdown, nil
}

// cleanupComponentState 在启动装配失败时释放已经创建的组件资源。
func cleanupComponentState(ctx context.Context, state *components.State) {
	if state == nil {
		return
	}
	// 注册失败时沿用 App 停机同一套资源释放顺序，避免不同失败阶段遗漏 DB/Kafka 连接池。
	_ = bootstrapresources.CloseServiceContextResources(state.ServiceContext, state.TaskRedis, state.TaskRedisOwned)
	if state.Shutdown != nil {
		_ = state.Shutdown(ctx)
	}
}
