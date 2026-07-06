package resources

import (
	"context"
	"strings"

	"admin/internal/audit"
	"admin/internal/bootstrap/configload"
	"admin/internal/config"
	"admin/internal/infra/kafkax"
	"admin/internal/infra/loggerx"
	mysqlx "admin/internal/infra/mysql"
	"admin/internal/infra/redisx"
	"admin/internal/infra/tracing"
	"admin/internal/svc"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// buildResources 聚合 BuildServiceContext 启动过程中已成功初始化、但尚未交给 App 托管的资源。
// 失败回滚时只扩展该结构体字段，避免 closeBuildResources 参数列表随业务依赖增长而失控。
type buildResources struct {
	svc.Dependencies                             // ServiceContext 可直接复用的依赖集合，包含数据库、Redis、Kafka 与审计资源
	Shutdown         func(context.Context) error // tracing 等基础设施关闭钩子，最后释放
}

// BuildServiceContext 统一完成基础设施初始化，避免 main 和 debug 入口各自拼装依赖导致行为漂移。
func BuildServiceContext(ctx context.Context, c config.Config) (*svc.ServiceContext, func(context.Context) error, error) {
	loggerx.Setup(c)

	shutdown, err := tracing.Setup(ctx, c.Observability)
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	resources := buildResources{Dependencies: svc.Dependencies{}, Shutdown: shutdown}

	siteDBs, err := buildSiteDatabases(ctx, c)
	resources.SiteDBs = siteDBs
	if err != nil {
		_ = closeBuildResources(context.Background(), resources)
		return nil, nil, errors.Tag(err)
	}

	rdb, err := redisx.New(ctx, c.Redis, c.Observability)
	if err != nil {
		_ = closeBuildResources(context.Background(), resources)
		return nil, nil, errors.Tag(err)
	}
	resources.Rds = rdb

	snowflakeLease, err := configload.ConfigureSnowflakeWorker(ctx, c.Snowflake, rdb)
	if err != nil {
		_ = closeBuildResources(context.Background(), resources)
		return nil, nil, errors.Wrap(err, "配置雪花 ID worker 失败")
	}
	resources.SnowflakeLease = snowflakeLease

	kafkaProducer, err := kafkax.NewProducer(c.Kafka)
	if err != nil {
		_ = closeBuildResources(context.Background(), resources)
		return nil, nil, errors.Tag(err)
	}
	resources.Kafka = kafkaProducer

	svcCtx := svc.NewServiceContext(c, resources.Dependencies)
	// 审计日志使用主库写连接。
	resources.Audit = audit.NewRecorder(svcCtx.WriteDB(svc.DatabaseMain), c.Observability.LogBodyMaxBytes)
	svcCtx.Audit = resources.Audit
	return svcCtx, shutdown, nil
}

// closeBuildResources 回收 BuildServiceContext 已经创建但尚未交给 App 托管的资源。
// 启动阶段任一后置依赖失败时都必须释放前置连接池，避免健康检查或容器重启循环中持续泄漏连接。
func closeBuildResources(ctx context.Context, resources buildResources) error {
	var firstErr error
	recordErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = errors.Tag(err)
		}
	}
	if resources.SnowflakeLease != nil {
		recordErr(resources.SnowflakeLease.Close(ctx))
	}
	if resources.Kafka != nil {
		recordErr(resources.Kafka.Close())
	}
	if resources.Rds != nil {
		recordErr(resources.Rds.Close())
	}
	recordErr(closeSiteDatabases(resources.SiteDBs))
	if resources.Shutdown != nil {
		recordErr(resources.Shutdown(ctx))
	}
	return errors.Tag(firstErr)
}

// CloseServiceContextResources 释放 ServiceContext 托管的外部资源。
// 任务 Redis 可能复用业务主 Redis，关闭时通过 owned 标记避免重复关闭共享连接。
func CloseServiceContextResources(svcCtx *svc.ServiceContext, taskRedis redis.UniversalClient, taskRedisOwned bool) error {
	var firstErr error
	recordErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = errors.Tag(err)
		}
	}
	if svcCtx == nil {
		if taskRedisOwned && taskRedis != nil {
			recordErr(taskRedis.Close())
		}
		return errors.Tag(firstErr)
	}
	if svcCtx.SnowflakeLease != nil {
		recordErr(svcCtx.SnowflakeLease.Close(context.Background()))
	}
	if taskRedisOwned && taskRedis != nil {
		recordErr(taskRedis.Close())
	}
	if svcCtx.Rds != nil && (!taskRedisOwned || svcCtx.Rds != taskRedis) {
		recordErr(svcCtx.Rds.Close())
	}
	if svcCtx.Kafka != nil {
		recordErr(svcCtx.Kafka.Close())
	}
	recordErr(closeSiteDatabases(svcCtx.SiteDBs))
	return errors.Tag(firstErr)
}

// buildSiteDatabases 初始化主库和可选命名扩展库连接。
// 默认只打开顶层 mysql；site_mysql.<name> 仅用于后续拆库扩展。
func buildSiteDatabases(ctx context.Context, c config.Config) (svc.SiteDatabases, error) {
	if !hasMySQLDataSource(c.MySQL) {
		return svc.SiteDatabases{}, errors.Errorf("缺少 mysql.write_data_source 配置")
	}
	mainDB, err := openSiteDatabase(ctx, "mysql", c.MySQL, c.Observability)
	if err != nil {
		return svc.SiteDatabases{}, errors.Tag(err)
	}
	dbs := svc.SiteDatabases{
		MainDB:   mainDB,
		NamedDBs: make(map[svc.DBName]*gorm.DB),
	}
	for name, dbCfg := range c.SiteMySQL {
		if !hasMySQLDataSource(dbCfg) {
			continue
		}
		dbName := svc.DBName(strings.TrimSpace(name))
		db, err := openSiteDatabase(ctx, "site_mysql."+string(dbName), dbCfg, c.Observability)
		if err != nil {
			_ = closeSiteDatabases(dbs)
			return svc.SiteDatabases{}, errors.Tag(err)
		}
		dbs.NamedDBs[dbName] = db
	}
	return dbs, nil
}

// openSiteDatabase 初始化单个 MySQL 连接；缺少写库 DSN 时直接返回启动错误。
func openSiteDatabase(ctx context.Context, name string, cfg config.MySQLConfig, obs config.ObservabilityConfig) (*gorm.DB, error) {
	if strings.TrimSpace(cfg.WriteDataSource) == "" {
		return nil, errors.Errorf("缺少 %s.write_data_source 配置", name)
	}
	db, err := mysqlx.New(ctx, cfg, obs)
	if err != nil {
		return nil, errors.Wrapf(err, "打开 MySQL[%s] 失败", name)
	}
	return db, nil
}

// hasMySQLDataSource 判断 MySQL 配置是否提供写库 DSN。
func hasMySQLDataSource(cfg config.MySQLConfig) bool {
	return strings.TrimSpace(cfg.WriteDataSource) != ""
}

// closeSiteDatabases 关闭站点数据库 GORM 底层连接池。
// 测试或特殊装配可能复用同一个 *gorm.DB 指针，因此这里按指针去重，避免重复 Close 产生噪声错误。
func closeSiteDatabases(siteDBs svc.SiteDatabases) error {
	var firstErr error
	recordErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = errors.Tag(err)
		}
	}
	seen := make(map[*gorm.DB]struct{}, 4)
	closeOne := func(name string, db *gorm.DB) {
		if db == nil {
			return
		}
		if _, ok := seen[db]; ok {
			return
		}
		seen[db] = struct{}{}
		sqlDB, err := db.DB()
		if err != nil {
			recordErr(errors.Wrapf(err, "获取 MySQL[%s]底层连接池失败", name))
			return
		}
		if sqlDB == nil {
			return
		}
		if err = sqlDB.Close(); err != nil {
			recordErr(errors.Wrapf(err, "关闭 MySQL[%s]连接池失败", name))
		}
	}
	closeOne("mysql", siteDBs.MainDB)
	for name, db := range siteDBs.NamedDBs {
		closeOne("site_mysql."+string(name), db)
	}
	return errors.Tag(firstErr)
}
