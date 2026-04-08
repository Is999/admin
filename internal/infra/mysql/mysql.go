package mysqlx

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"admin_cron/internal/config"
	"admin_cron/internal/infra/loggerx"

	"github.com/Is999/go-utils/errors"
	drivermysql "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// New 创建带统一 GORM 日志器的数据库连接，并在启动阶段完成一次连通性检查。
func New(ctx context.Context, cfg config.MySQLConfig, obs config.ObservabilityConfig) (*gorm.DB, error) {
	// writeDSN 为主库连接，readDSNs 为可选读库连接列表。
	writeDSN, readDSNs, err := resolveDataSources(cfg)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if err := checkMySQLDataSources(ctx, writeDSN, readDSNs); err != nil {
		return nil, errors.Tag(err)
	}

	gormCfg := &gorm.Config{
		Logger: loggerx.NewGormLogger(time.Duration(obs.SlowSQLMs) * time.Millisecond),
	}

	gdb, err := gorm.Open(gormmysql.Open(writeDSN), gormCfg)
	if err != nil {
		return nil, errors.Tag(err)
	}

	if len(readDSNs) > 0 {
		replicas := make([]gorm.Dialector, 0, len(readDSNs))
		for _, dsn := range readDSNs {
			replicas = append(replicas, gormmysql.Open(dsn))
		}

		resolver := dbresolver.Register(dbresolver.Config{
			Sources:           []gorm.Dialector{gormmysql.Open(writeDSN)},
			Replicas:          replicas,
			Policy:            dbresolver.RandomPolicy{},
			TraceResolverMode: true, // 在 SQL 日志中标记 [source]/[replica]，便于排查是否按预期命中读写库。
		})
		if cfg.MaxOpenConns > 0 {
			resolver = resolver.SetMaxOpenConns(cfg.MaxOpenConns)
		}
		if cfg.MaxIdleConns > 0 {
			resolver = resolver.SetMaxIdleConns(cfg.MaxIdleConns)
		}
		if cfg.ConnMaxLifetime > 0 {
			resolver = resolver.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)
		}

		if err := gdb.Use(resolver); err != nil {
			closeGormDB(gdb)
			return nil, errors.Wrap(err, "注册 MySQL 读写分离解析器失败")
		}
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		closeGormDB(gdb)
		return nil, errors.Tag(err)
	}

	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)
	}
	// 启动时先 Ping，尽早暴露配置错误或数据库不可达问题。
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, errors.Tag(err)
	}
	// Debug 模式沿用 GORM 自带 SQL 输出，但仍会保留我们自己的结构化 logger。
	if cfg.Debug {
		gdb = gdb.Debug()
	}

	return gdb, nil
}

// closeGormDB 关闭已经创建但尚未交给调用方托管的 GORM 连接池。
// 该函数只用于启动失败回滚，避免 DBResolver 注册或 Ping 失败时泄漏底层连接。
func closeGormDB(gdb *gorm.DB) {
	if gdb == nil {
		return
	}
	sqlDB, err := gdb.DB()
	if err != nil || sqlDB == nil {
		return
	}
	_ = sqlDB.Close()
}

// resolveDataSources 解析并校验读写库 DSN：写库必填，读库去重且自动剔除空值与重复主库地址。
func resolveDataSources(cfg config.MySQLConfig) (string, []string, error) {
	writeDSN := strings.TrimSpace(cfg.WriteDataSource)
	if writeDSN == "" {
		return "", nil, errors.Errorf("缺少 mysql write_data_source 配置")
	}

	if len(cfg.ReadDataSources) == 0 {
		return writeDSN, nil, nil
	}

	replicas := make([]string, 0, len(cfg.ReadDataSources))
	// seen 用于读库 DSN 去重，避免 resolver 重复注册同一实例。
	seen := map[string]struct{}{}
	for _, dsn := range cfg.ReadDataSources {
		trimmed := strings.TrimSpace(dsn)
		if trimmed == "" || trimmed == writeDSN {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		replicas = append(replicas, trimmed)
	}
	return writeDSN, replicas, nil
}

// checkMySQLDataSources 在创建 GORM 连接前逐一探测写库和读库 DSN。
func checkMySQLDataSources(ctx context.Context, writeDSN string, readDSNs []string) error {
	return checkMySQLDataSourcesWithPing(ctx, writeDSN, readDSNs, pingMySQLDataSource)
}

// checkMySQLDataSourcesWithPing 注入探测函数，便于测试启动阶段是否覆盖所有数据源。
func checkMySQLDataSourcesWithPing(ctx context.Context, writeDSN string, readDSNs []string, ping func(context.Context, string, string) error) error {
	if ping == nil {
		return errors.Errorf("MySQL 启动探测函数不能为空")
	}
	// 写库是所有事务和强一致回源的基础，必须最先验证。
	if err := ping(ctx, "write_data_source", writeDSN); err != nil {
		return errors.Tag(err)
	}
	// 每个读库都显式探测，避免随机 resolver 只命中部分副本导致故障延迟暴露。
	for idx, dsn := range readDSNs {
		if err := ping(ctx, fmt.Sprintf("read_data_sources[%d]", idx), dsn); err != nil {
			return errors.Tag(err)
		}
	}
	return nil
}

// pingMySQLDataSource 使用 database/sql 轻量连接单个 DSN，确认目标数据库存在且可联通。
func pingMySQLDataSource(ctx context.Context, label, dsn string) error {
	if err := validateMySQLDataSourceDatabase(label, dsn); err != nil {
		return errors.Tag(err)
	}

	// 启动探测只需要一条短连接，避免提前占用正式 GORM 连接池容量。
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return errors.Wrapf(err, "打开 MySQL %s 失败", label)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)

	if err := db.PingContext(ctx); err != nil {
		return errors.Wrapf(err, "探测 MySQL %s 失败，数据库不存在或不可达", label)
	}
	return nil
}

// validateMySQLDataSourceDatabase 校验 DSN 语法，并要求显式声明默认数据库。
func validateMySQLDataSourceDatabase(label, dsn string) error {
	parsed, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		return errors.Wrapf(err, "解析 MySQL %s DSN 失败", label)
	}
	if strings.TrimSpace(parsed.DBName) == "" {
		return errors.Errorf("MySQL %s DSN 必须包含数据库名", label)
	}
	return nil
}
