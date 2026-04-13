package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"admin_cron/internal/bootstrap"
	"admin_cron/internal/database"
	mysqlx "admin_cron/internal/infra/mysql"

	"github.com/Is999/go-utils/errors"
)

const (
	actionStatus = "status"
	actionDryRun = "dry-run"
	actionUp     = "up"
)

// buildVersion 由构建阶段通过 -ldflags 注入，用于发布排查。
var buildVersion = "dev"

func main() {
	configFile := flag.String("f", "./etc/config.yaml", "配置文件路径")
	action := flag.String("action", actionStatus, "迁移动作：status/dry-run/up")
	allowBootstrap := flag.Bool("allow-bootstrap", false, "允许执行 bootstrap-only 基线迁移")
	allowDestructive := flag.Bool("allow-destructive", false, "允许执行 destructive 迁移")
	showVersion := flag.Bool("version", false, "输出构建版本并退出")
	flag.Parse()
	if *showVersion {
		fmt.Println(buildVersion)
		return
	}

	if err := run(*configFile, *action, *allowBootstrap, *allowDestructive); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(configFile string, action string, allowBootstrap bool, allowDestructive bool) error {
	action = strings.ToLower(strings.TrimSpace(action))
	if action != actionStatus && action != actionDryRun && action != actionUp {
		return errors.Errorf("不支持的迁移动作: %s", action)
	}
	ctx := context.Background()
	cfg, err := bootstrap.LoadConfig(configFile)
	if err != nil {
		return errors.Wrap(err, "加载配置失败")
	}
	db, err := mysqlx.New(ctx, cfg.MySQL, cfg.Observability)
	if err != nil {
		return errors.Wrap(err, "连接 MySQL 失败")
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer sqlDB.Close()
	}

	results, err := database.RunMigrations(ctx, database.NewGormMigrationStore(db), database.DefaultMigrations(), database.MigrationRunOptions{
		DryRun:           action != actionUp,
		AllowBootstrap:   allowBootstrap,
		AllowDestructive: allowDestructive,
	})
	printResults(results)
	return err
}

func printResults(results []database.MigrationRunItem) {
	fmt.Printf("%-10s %-14s %-36s %s\n", "STATUS", "VERSION", "NAME", "ASSET")
	for _, item := range results {
		line := fmt.Sprintf("%-10s %-14s %-36s %s", item.Status, item.Version, item.Name, item.Asset)
		if item.Reason != "" {
			line += " # " + item.Reason
		}
		fmt.Println(line)
	}
}
