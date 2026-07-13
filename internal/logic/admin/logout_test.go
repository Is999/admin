package admin

import (
	"context"
	"sync"
	"testing"

	keys "admin/common/rediskeys"
	"admin/common/runtimecfg"
	"admin/helper"
	"admin/internal/config"
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	"admin/internal/requestctx"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestLogoutPreservesLaterSession 验证旧请求晚到的登出不会删除同账号后来建立的新会话。
func TestLogoutPreservesLaterSession(t *testing.T) {
	server := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	cfg := config.Config{AppID: "site-a", JwtExpiresIn: 3600}
	previous := runtimecfg.Get()
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Restore(previous) })
	svcCtx := svc.NewServiceContext(cfg, svc.Dependencies{Rds: rds})
	cacheLogic := cachelogic.NewCacheLogic(context.Background(), svcCtx)
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{ID: 7, UserName: "admin", Token: "new-token"}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}

	ctx, _ := requestctx.New(context.Background())
	requestctx.SetAccessToken(ctx, "old-token")
	logicObj := &AdminLogic{BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx)}
	result := logicObj.Logout(&helper.CtxAdmin{ID: 7, Name: "admin"})
	if result == nil || result.IsFailure() {
		t.Fatalf("Logout() = %+v", result)
	}
	if token, err := cacheLogic.GetAdminToken(7); err != nil || token != "new-token" {
		t.Fatalf("旧请求登出后缓存 token=%q error=%v，期望保留 new-token", token, err)
	}
}

// TestLogoutDeletesCurrentSession 验证当前 token 登出后会删除对应管理员会话。
func TestLogoutDeletesCurrentSession(t *testing.T) {
	server := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	cfg := config.Config{AppID: "site-a", JwtExpiresIn: 3600}
	previous := runtimecfg.Get()
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Restore(previous) })
	svcCtx := svc.NewServiceContext(cfg, svc.Dependencies{Rds: rds})
	cacheLogic := cachelogic.NewCacheLogic(context.Background(), svcCtx)
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{ID: 7, UserName: "admin", Token: "current-token"}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}

	ctx, _ := requestctx.New(context.Background())
	requestctx.SetAccessToken(ctx, "current-token")
	logicObj := &AdminLogic{BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx)}
	result := logicObj.Logout(&helper.CtxAdmin{ID: 7, Name: "admin"})
	if result == nil || result.IsFailure() {
		t.Fatalf("Logout() = %+v", result)
	}
	if _, err := cacheLogic.GetAdminToken(7); err == nil {
		t.Fatal("当前 token 登出后管理员会话仍存在")
	}
}

// TestRefreshAndLogoutConcurrentlyEndWithoutSession 验证两个已通过鉴权的刷新与退出请求交错后不会遗留会话。
func TestRefreshAndLogoutConcurrentlyEndWithoutSession(t *testing.T) {
	server := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	cfg := config.Config{
		AppID:        "site-a",
		JwtSecret:    "test-secret-please-change",
		JwtExpiresIn: 3600,
	}
	previous := runtimecfg.Get()
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Restore(previous) })
	svcCtx := svc.NewServiceContext(cfg, svc.Dependencies{Rds: rds})
	cacheLogic := cachelogic.NewCacheLogic(context.Background(), svcCtx)
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{ID: 7, UserName: "admin", Token: "old-token"}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}

	newLogic := func(path string) *AdminLogic {
		ctx, _ := requestctx.New(context.Background())
		requestctx.SetAccessToken(ctx, "old-token")
		requestctx.SetRequest(ctx, "POST", path, "127.0.0.1")
		return &AdminLogic{BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx)}
	}
	refreshLogic := newLogic("/auth/refresh")
	logoutLogic := newLogic("/auth/logout")
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(2)
	var refreshResult, logoutResult *types.BizResult
	go func() {
		defer wait.Done()
		<-start
		refreshResult = refreshLogic.RefreshAccessToken(&helper.CtxAdmin{ID: 7, Name: "admin"})
	}()
	go func() {
		defer wait.Done()
		<-start
		logoutResult = logoutLogic.Logout(&helper.CtxAdmin{ID: 7, Name: "admin"})
	}()
	close(start)
	wait.Wait()

	if refreshResult == nil || logoutResult == nil || logoutResult.IsFailure() {
		t.Fatalf("并发刷新/退出结果异常，refresh=%+v logout=%+v", refreshResult, logoutResult)
	}
	if server.Exists(keys.AdminInfoRedisKey(7)) {
		t.Fatal("并发刷新与退出完成后仍遗留管理员会话")
	}
}
