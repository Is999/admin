package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"admin/common/runtimecfg"
	"admin/internal/config"
	"admin/internal/database"

	"github.com/alicebob/miniredis/v2"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestResolveMigrationActionDefault 验证空 action 默认只查看迁移状态。
func TestResolveMigrationActionDefault(t *testing.T) {
	got, err := resolveMigrationAction(" ")
	if err != nil {
		t.Fatalf("resolve migration action failed: %v", err)
	}
	if got != actionStatus {
		t.Fatalf("expected default action %q, got %q", actionStatus, got)
	}
}

// TestResolveMigrationActionNormalizes 验证 action 会去空格并统一小写。
func TestResolveMigrationActionNormalizes(t *testing.T) {
	got, err := resolveMigrationAction(" UP ")
	if err != nil {
		t.Fatalf("resolve migration action failed: %v", err)
	}
	if got != actionUp {
		t.Fatalf("expected normalized action %q, got %q", actionUp, got)
	}
}

// TestResolveMigrationActionRejectsUnknown 验证未知 action 会被拒绝，避免误执行。
func TestResolveMigrationActionRejectsUnknown(t *testing.T) {
	if _, err := resolveMigrationAction("rollback"); err == nil {
		t.Fatal("expected unknown action to be rejected")
	}
}

// TestPrintResults 验证迁移结果输出包含状态、资产和拦截原因。
func TestPrintResults(t *testing.T) {
	var output bytes.Buffer
	err := printResults(&output, []database.MigrationRunItem{
		{
			Version: "202606050001",
			Name:    "bootstrap_admin_table",
			Asset:   "admin.sql",
			Status:  database.MigrationStatusBlocked,
			Reason:  "bootstrap-only 迁移需要显式允许",
		},
	})
	if err != nil {
		t.Fatalf("print migration results failed: %v", err)
	}
	text := output.String()
	for _, want := range []string{"STATUS", "202606050001", "admin.sql", "bootstrap-only 迁移需要显式允许"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, text)
		}
	}
}

// TestPrintResultsRejectsNilWriter 验证输出目标为空时返回错误。
func TestPrintResultsRejectsNilWriter(t *testing.T) {
	if err := printResults(nil, nil); err == nil {
		t.Fatal("expected nil output writer to be rejected")
	}
}

// TestPermissionCacheRefreshRequired 验证只有 up 模式下的权限数据迁移会触发权限缓存刷新。
func TestPermissionCacheRefreshRequired(t *testing.T) {
	tests := []struct {
		name    string                      // name 表示测试场景。
		action  string                      // action 表示迁移动作。
		results []database.MigrationRunItem // results 表示迁移执行结果。
		want    bool                        // want 表示是否需要刷新权限缓存。
	}{
		{
			name:   "skip dry run",
			action: actionDryRun,
			results: []database.MigrationRunItem{
				{Name: "bootstrap_admin_permission", Status: database.MigrationStatusExecuted},
			},
		},
		{
			name:   "skip unrelated migration",
			action: actionUp,
			results: []database.MigrationRunItem{
				{Name: "bootstrap_admin_table", Status: database.MigrationStatusExecuted},
			},
		},
		{
			name:   "refresh executed permission baseline",
			action: actionUp,
			results: []database.MigrationRunItem{
				{Name: "bootstrap_admin_permission", Status: database.MigrationStatusExecuted},
			},
			want: true,
		},
		{
			name:   "refresh applied permission baseline by asset",
			action: actionUp,
			results: []database.MigrationRunItem{
				{Asset: "admin_permission.sql", Status: database.MigrationStatusApplied},
			},
			want: true,
		},
		{
			name:   "skip pending permission baseline",
			action: actionUp,
			results: []database.MigrationRunItem{
				{Name: "bootstrap_admin_permission", Status: database.MigrationStatusPending},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := permissionCacheRefreshRequired(tt.action, tt.results); got != tt.want {
				t.Fatalf("permissionCacheRefreshRequired() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPublishMigrationRuntimeConfigRestoresPreviousAppID 确保迁移命令刷新缓存时不会丢失 Redis key 的 app_id 前缀。
func TestPublishMigrationRuntimeConfigRestoresPreviousAppID(t *testing.T) {
	previous := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "before"})
	t.Cleanup(func() {
		runtimecfg.Restore(previous)
	})

	restore := publishMigrationRuntimeConfig(config.Config{AppID: "after"})
	if got := runtimecfg.AppID(); got != "after" {
		t.Fatalf("runtimecfg.AppID() = %q, want after", got)
	}
	restore()
	if got := runtimecfg.AppID(); got != "before" {
		t.Fatalf("runtimecfg.AppID() after restore = %q, want before", got)
	}
}

// TestRefreshPermissionCacheAfterMigrationPropagatesRefreshError 验证迁移后权限缓存刷新失败会返回给命令入口。
func TestRefreshPermissionCacheAfterMigrationPropagatesRefreshError(t *testing.T) {
	// redisServer 提供正常 Redis，使测试精确进入权限缓存刷新阶段。
	redisServer := miniredis.RunT(t)
	// db 使用不可达连接，在全量角色查询时返回可预期错误。
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "root@tcp(127.0.0.1:1)/test?timeout=50ms&charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("创建迁移缓存刷新测试数据库失败: %v", err)
	}
	// cfg 只包含迁移后缓存刷新所需的 Redis 配置。
	cfg := config.Config{
		AppID: "site-a",
		Redis: config.RedisConfig{
			Type:     "single",
			Addrs:    []string{redisServer.Addr()},
			PoolSize: 1,
		},
	}

	err = refreshPermissionCacheAfterMigration(context.Background(), cfg, db)
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("refreshPermissionCacheAfterMigration() error = %v, want refresh failure", err)
	}
}
