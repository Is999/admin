package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"admin/common/runtimecfg"
	"admin/internal/bootstrap"
	"admin/internal/config"
	"admin/internal/database"
	mysqlx "admin/internal/infra/mysql"
	"admin/internal/infra/redisx"
	corelogic "admin/internal/logic"
	rbaclogic "admin/internal/logic/rbac"
	"admin/internal/svc"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

const (
	// actionStatus 只查看当前迁移状态，不执行 SQL。
	actionStatus = "status"
	// actionDryRun 预览待执行迁移和安全拦截原因。
	actionDryRun = "dry-run"
	// actionUp 执行未完成迁移。
	actionUp = "up"
)

// migrationCommandOptions 表示迁移命令行参数。
type migrationCommandOptions struct {
	ConfigFile       string // 配置文件路径
	Action           string // 迁移动作：status/dry-run/up
	AllowBootstrap   bool   // 是否允许执行 bootstrap-only 基线迁移
	AllowDestructive bool   // 是否允许执行 destructive 迁移
}

// buildVersion 由构建阶段通过 -ldflags 注入，用于发布排查。
var buildVersion = "dev"

// configFile 支持通过 -f 指定配置文件，便于区分本地、测试和线上环境。
var configFile = flag.String("f", "./etc/config.yaml", "配置文件路径")

// action 控制迁移命令的执行模式。
var action = flag.String("action", actionStatus, "迁移动作：status/dry-run/up")

// allowBootstrap 控制是否允许执行仅用于新库初始化的基线迁移。
var allowBootstrap = flag.Bool("allow-bootstrap", false, "允许执行 bootstrap-only 基线迁移")

// allowDestructive 控制是否允许执行 destructive 迁移。
var allowDestructive = flag.Bool("allow-destructive", false, "允许执行 destructive 迁移")

// showVersion 控制是否只输出二进制版本并退出。
var showVersion = flag.Bool("version", false, "输出构建版本并退出")

// main 解析命令行参数并执行数据库迁移命令。
func main() {
	flag.Parse()
	if *showVersion {
		fmt.Println(buildVersion)
		return
	}

	options := migrationCommandOptions{
		ConfigFile:       *configFile,
		Action:           *action,
		AllowBootstrap:   *allowBootstrap,
		AllowDestructive: *allowDestructive,
	}
	if err := run(context.Background(), options, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run 执行迁移命令，返回错误供 main 统一退出。
func run(ctx context.Context, options migrationCommandOptions, output io.Writer) (err error) {
	resolvedAction, err := resolveMigrationAction(options.Action)
	if err != nil {
		return errors.Tag(err)
	}
	cfg, err := bootstrap.LoadConfig(options.ConfigFile)
	if err != nil {
		return errors.Wrap(err, "加载配置失败")
	}
	db, err := mysqlx.New(ctx, cfg.MySQL, cfg.Observability)
	if err != nil {
		return errors.Wrap(err, "连接 MySQL 失败")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return errors.Wrap(err, "获取 MySQL 底层连接失败")
	}
	defer func() {
		if closeErr := sqlDB.Close(); closeErr != nil && err == nil {
			err = errors.Wrap(closeErr, "关闭 MySQL 连接失败")
		}
	}()

	results, err := database.RunMigrations(ctx, database.NewGormMigrationStore(db), database.DefaultMigrations(), database.MigrationRunOptions{
		DryRun:           resolvedAction != actionUp,
		AllowBootstrap:   options.AllowBootstrap,
		AllowDestructive: options.AllowDestructive,
	})
	if printErr := printResults(output, results); printErr != nil {
		return errors.Wrap(printErr, "输出迁移结果失败")
	}
	if err != nil {
		return errors.Tag(err)
	}
	if permissionCacheRefreshRequired(resolvedAction, results) {
		if err := refreshPermissionCacheAfterMigration(ctx, cfg, db); err != nil {
			return errors.Wrap(err, "刷新权限缓存失败")
		}
		if _, err := fmt.Fprintln(output, "权限缓存已刷新：permission_related"); err != nil {
			return errors.Wrap(err, "输出权限缓存刷新结果失败")
		}
	}
	return nil
}

// resolveMigrationAction 规范化命令行传入的迁移动作。
func resolveMigrationAction(action string) (string, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		action = actionStatus
	}
	if action != actionStatus && action != actionDryRun && action != actionUp {
		return "", errors.Errorf("不支持的迁移动作: %s", action)
	}
	return action, nil
}

// printResults 输出迁移计划或执行结果。
func printResults(output io.Writer, results []database.MigrationRunItem) error {
	if output == nil {
		return errors.Errorf("迁移结果输出目标不能为空")
	}
	if _, err := fmt.Fprintf(output, "%-10s %-14s %-36s %s\n", "STATUS", "VERSION", "NAME", "ASSET"); err != nil {
		return errors.Tag(err)
	}
	for _, item := range results {
		line := fmt.Sprintf("%-10s %-14s %-36s %s", item.Status, item.Version, item.Name, item.Asset)
		if item.Reason != "" {
			line += " # " + item.Reason
		}
		if _, err := fmt.Fprintln(output, line); err != nil {
			return errors.Tag(err)
		}
	}
	return nil
}

// permissionCacheRefreshRequired 判断本轮迁移是否需要刷新权限缓存。
func permissionCacheRefreshRequired(action string, results []database.MigrationRunItem) bool {
	if action != actionUp {
		return false
	}
	for _, item := range results {
		if item.Status != database.MigrationStatusExecuted && item.Status != database.MigrationStatusApplied {
			continue
		}
		if isPermissionDataMigration(item) {
			return true
		}
	}
	return false
}

// isPermissionDataMigration 判断迁移是否会影响权限定义或角色授权关系。
func isPermissionDataMigration(item database.MigrationRunItem) bool {
	switch strings.TrimSpace(item.Name) {
	case "sync_document_permissions":
		return true
	}
	switch strings.TrimSpace(item.Asset) {
	case "document_permission_seed.sql":
		return true
	}
	return false
}

// refreshPermissionCacheAfterMigration 复用权限领域刷新逻辑，避免迁移补权后继续命中旧缓存。
func refreshPermissionCacheAfterMigration(ctx context.Context, cfg config.Config, db *gorm.DB) (err error) {
	if db == nil {
		return errors.Errorf("MySQL 连接不能为空")
	}
	restoreRuntimeConfig := publishMigrationRuntimeConfig(cfg)
	defer restoreRuntimeConfig()

	rdb, err := redisx.New(ctx, cfg.Redis, cfg.Observability)
	if err != nil {
		return errors.Wrap(err, "连接 Redis 失败")
	}
	defer func() {
		if closeErr := rdb.Close(); closeErr != nil && err == nil {
			err = errors.Wrap(closeErr, "关闭 Redis 连接失败")
		}
	}()

	svcCtx := svc.NewServiceContext(cfg, svc.Dependencies{
		SiteDBs: svc.SiteDatabases{MainDB: db},
		Rds:     rdb,
	})
	logicObj := &rbaclogic.AdminPermissionLogic{
		BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx),
	}
	logicObj.RefreshPermissionRelatedCache()
	return nil
}

// publishMigrationRuntimeConfig 发布迁移命令所需的轻量运行配置，并返回恢复函数。
func publishMigrationRuntimeConfig(cfg config.Config) func() {
	previous := runtimecfg.Get()
	runtimecfg.Set(cfg)
	return func() {
		runtimecfg.Restore(previous)
	}
}
