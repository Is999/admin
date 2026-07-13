package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	keys "admin/common/rediskeys"
	"admin/common/runtimecfg"
	"admin/internal/config"
	"admin/internal/database"

	"github.com/alicebob/miniredis/v2"
	miniredisserver "github.com/alicebob/miniredis/v2/server"
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

// TestAuthorizationCacheRefreshRequired 验证只有 up 模式下的鉴权数据迁移会触发缓存刷新。
func TestAuthorizationCacheRefreshRequired(t *testing.T) {
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
			name:   "refresh executed document permission baseline",
			action: actionUp,
			results: []database.MigrationRunItem{
				{Name: "bootstrap_admin_doc_permission", Status: database.MigrationStatusExecuted},
			},
			want: true,
		},
		{
			name:   "refresh applied document permission baseline by asset",
			action: actionUp,
			results: []database.MigrationRunItem{
				{Asset: "admin_doc_permission.sql", Status: database.MigrationStatusApplied},
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
			if got := authorizationCacheRefreshRequired(tt.action, tt.results); got != tt.want {
				t.Fatalf("authorizationCacheRefreshRequired() = %v, want %v", got, tt.want)
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

// TestRefreshAuthorizationCacheAfterMigrationPropagatesRoleQueryError 验证角色 ID 查询失败会阻止缓存刷新。
func TestRefreshAuthorizationCacheAfterMigrationPropagatesRoleQueryError(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "root@tcp(127.0.0.1:1)/test?timeout=50ms&charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("创建迁移缓存刷新测试数据库失败: %v", err)
	}

	err = refreshAuthorizationCacheAfterMigration(context.Background(), config.Config{}, db)
	if err == nil || !strings.Contains(err.Error(), "查询角色 ID 失败") {
		t.Fatalf("refreshAuthorizationCacheAfterMigration() error = %v, want role query error", err)
	}
}

// TestRefreshAuthorizationCacheAfterMigrationDeletesSharedAndRoleCaches 验证迁移后清理共享索引和全部角色权限缓存。
func TestRefreshAuthorizationCacheAfterMigrationDeletesSharedAndRoleCaches(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisServer.Server().SetPreHook(func(peer *miniredisserver.Peer, command string, args ...string) bool {
		if !strings.EqualFold(command, "cluster") || len(args) != 1 || !strings.EqualFold(args[0], "info") {
			return false
		}
		peer.WriteError("ERR This instance has cluster support disabled")
		return true
	})
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "root@tcp(127.0.0.1:1)/test?timeout=50ms&charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("创建迁移缓存刷新测试数据库失败: %v", err)
	}
	roleQueryCount := 0
	if err = db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		roleQueryCount++
		roleIDs, ok := tx.Statement.Dest.(*[]int)
		if !ok {
			tx.AddError(fmt.Errorf("角色 ID 查询目标类型错误: %T", tx.Statement.Dest))
			return
		}
		*roleIDs = []int{2, 3}
	}); err != nil {
		t.Fatalf("替换角色 ID 查询回调失败: %v", err)
	}
	cfg := config.Config{
		AppID: "site-a",
		Redis: config.RedisConfig{
			Type:     "single",
			Addrs:    []string{redisServer.Addr()},
			PoolSize: 1,
		},
	}
	cacheKeys := []string{
		keys.PermissionTree,
		keys.RoutePermissionIDs,
		keys.PermissionUUID,
		keys.DocPermissionList,
		keys.DocResourcePermissionID,
		fmt.Sprintf(keys.RolePermission, 2),
		fmt.Sprintf(keys.RolePermission, 3),
		fmt.Sprintf(keys.RoleDocPermission, 2),
		fmt.Sprintf(keys.RoleDocPermission, 3),
	}
	for _, cacheKey := range cacheKeys {
		if err := redisServer.Set("app:site-a:table:"+cacheKey, "value"); err != nil {
			t.Fatalf("写入权限缓存[%s]失败: %v", cacheKey, err)
		}
	}

	if err = refreshAuthorizationCacheAfterMigration(context.Background(), cfg, db); err != nil {
		t.Fatalf("refreshAuthorizationCacheAfterMigration() error = %v", err)
	}
	if roleQueryCount != 1 {
		t.Fatalf("refreshAuthorizationCacheAfterMigration() role query count = %d, want 1", roleQueryCount)
	}
	for _, cacheKey := range cacheKeys {
		physicalKey := "app:site-a:table:" + cacheKey
		if redisServer.Exists(physicalKey) {
			t.Fatalf("refreshAuthorizationCacheAfterMigration() key %s still exists", physicalKey)
		}
	}
}
