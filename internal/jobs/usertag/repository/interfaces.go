package repository

import (
	"context"

	"admin/internal/jobs/usertag/queryplan"
	"admin/internal/jobs/usertag/route"
	"admin/internal/svc"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Databases 保存 usertag 使用的数据库连接。
type Databases struct {
	MainDB *gorm.DB // 主库连接
}

// RuntimeDeps 保存 usertag 运行所需外部依赖。
type RuntimeDeps struct {
	Service   *svc.ServiceContext   // 原始服务上下文，供需要库路由方法的仓储使用
	DBs       Databases             // 站点主库连接
	Redis     redis.UniversalClient // Redis 客户端
	ShardPlan route.ShardPlan       // 分片路由计划
}

// NewRuntimeDeps 从 ServiceContext 构造 依赖集合。
func NewRuntimeDeps(svcCtx *svc.ServiceContext, plan route.ShardPlan) RuntimeDeps {
	if svcCtx == nil {
		return RuntimeDeps{ShardPlan: plan}
	}
	return RuntimeDeps{
		Service: svcCtx,
		DBs: Databases{
			MainDB: svcCtx.SiteDBs.MainDB,
		},
		Redis:     svcCtx.Rds,
		ShardPlan: plan,
	}
}

// QueryExecutor 定义按查询计划执行数据访问的最小接口。
// 具体仓储需要先 Validate 计划，再执行字段最小化与条件下推。
type QueryExecutor interface {
	Execute(ctx context.Context, plan queryplan.Plan, dest any) error
}

// OutboxRepository 定义标签得失事件 outbox 的最小访问接口。
type OutboxRepository interface {
	Append(ctx context.Context, workflowID string, shard int, rows any) error            // 追加最终标签差异事件
	Drain(ctx context.Context, workflowID string, shard int, batchSize int) (int, error) // 派发并标记 outbox 事件
}
