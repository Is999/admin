package admin

import (
	"context"
	"testing"
	"time"

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

// TestRefreshAccessTokenRenewsSessionTTL 验证临近过期的 Redis 会话刷新后按新 JWT 有效期续期。
func TestRefreshAccessTokenRenewsSessionTTL(t *testing.T) {
	server := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer rds.Close()
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
	if err := cacheLogic.SetAdminInfo(1, &types.AdminInfo{ID: 1, UserName: "admin", Token: "old-token"}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}
	server.FastForward(3599 * time.Second)

	ctx, _ := requestctx.New(context.Background())
	requestctx.SetAccessToken(ctx, "old-token")
	requestctx.SetRequest(ctx, "POST", "/auth/refresh", "127.0.0.1")
	logicObj := &AdminLogic{BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx)}
	result := logicObj.RefreshAccessToken(&helper.CtxAdmin{ID: 1, Name: "admin"})
	if result == nil || result.IsFailure() {
		t.Fatalf("RefreshAccessToken() = %+v", result)
	}
	info, err := cacheLogic.GetAdminInfo(1)
	if err != nil {
		t.Fatalf("GetAdminInfo() error = %v", err)
	}
	if info.Token == "" || info.Token == "old-token" {
		t.Fatalf("刷新后缓存 token 未轮换: %+v", info)
	}
	ttl := rds.TTL(context.Background(), keys.AdminInfoRedisKey(1)).Val()
	if ttl < 3590*time.Second {
		t.Fatalf("刷新后会话 TTL=%s，期望接近 1h", ttl)
	}
}

// TestRefreshAccessTokenRejectsMissingSession 验证 Redis 会话缺失时不从数据库复活刷新会话。
func TestRefreshAccessTokenRejectsMissingSession(t *testing.T) {
	server := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer rds.Close()
	cfg := config.Config{AppID: "site-a", JwtSecret: "test-secret-please-change", JwtExpiresIn: 3600}
	previous := runtimecfg.Get()
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Restore(previous) })
	svcCtx := svc.NewServiceContext(cfg, svc.Dependencies{Rds: rds})
	ctx, _ := requestctx.New(context.Background())
	requestctx.SetAccessToken(ctx, "old-token")
	logicObj := &AdminLogic{BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx)}

	result := logicObj.RefreshAccessToken(&helper.CtxAdmin{ID: 1, Name: "admin"})
	if result == nil || result.Code != 401 {
		t.Fatalf("缺失会话时应返回未授权，实际为 %+v", result)
	}
}

// TestRefreshAccessTokenRejectsStaleRequestToken 验证并发刷新落后的旧请求不能覆盖已轮换的新 token。
func TestRefreshAccessTokenRejectsStaleRequestToken(t *testing.T) {
	server := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer rds.Close()
	cfg := config.Config{AppID: "site-a", JwtSecret: "test-secret-please-change", JwtExpiresIn: 3600}
	previous := runtimecfg.Get()
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Restore(previous) })
	svcCtx := svc.NewServiceContext(cfg, svc.Dependencies{Rds: rds})
	cacheLogic := cachelogic.NewCacheLogic(context.Background(), svcCtx)
	if err := cacheLogic.SetAdminInfo(1, &types.AdminInfo{ID: 1, UserName: "admin", Token: "current-token"}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}

	ctx, _ := requestctx.New(context.Background())
	requestctx.SetAccessToken(ctx, "stale-token")
	logicObj := &AdminLogic{BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx)}
	result := logicObj.RefreshAccessToken(&helper.CtxAdmin{ID: 1, Name: "admin"})
	if result == nil || result.Code != 401 {
		t.Fatalf("旧请求 token 应返回未授权，实际为 %+v", result)
	}
	if token, err := cacheLogic.GetAdminToken(1); err != nil || token != "current-token" {
		t.Fatalf("旧请求失败后缓存 token = %q error = %v", token, err)
	}
}

// TestRefreshAccessTokenIsIdempotentWithinGrace 验证同一旧 token 的多标签页重试返回第一次轮换出的同一 token。
func TestRefreshAccessTokenIsIdempotentWithinGrace(t *testing.T) {
	server := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer rds.Close()
	cfg := config.Config{AppID: "site-a", JwtSecret: "test-secret-please-change", JwtExpiresIn: 3600}
	previous := runtimecfg.Get()
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Restore(previous) })
	svcCtx := svc.NewServiceContext(cfg, svc.Dependencies{Rds: rds})
	cacheLogic := cachelogic.NewCacheLogic(context.Background(), svcCtx)
	if err := cacheLogic.SetAdminInfo(1, &types.AdminInfo{ID: 1, UserName: "admin", Token: "old-token"}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}
	ctx, _ := requestctx.New(context.Background())
	requestctx.SetAccessToken(ctx, "old-token")
	logicObj := &AdminLogic{BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx)}

	first := logicObj.RefreshAccessToken(&helper.CtxAdmin{ID: 1, Name: "admin"})
	second := logicObj.RefreshAccessToken(&helper.CtxAdmin{ID: 1, Name: "admin"})
	if first == nil || first.IsFailure() || second == nil || second.IsFailure() {
		t.Fatalf("幂等刷新结果异常，first = %+v second = %+v", first, second)
	}
	firstData, firstOK := first.Data.(map[string]interface{})
	secondData, secondOK := second.Data.(map[string]interface{})
	firstToken, firstTokenOK := firstData["token"].(string)
	secondToken, secondTokenOK := secondData["token"].(string)
	if !firstOK || !secondOK || !firstTokenOK || !secondTokenOK || firstToken == "" || firstToken != secondToken {
		t.Fatalf("幂等刷新 token 不一致，first = %#v second = %#v", first.Data, second.Data)
	}
}

// TestGetLoginAfterInfoRejectsMissingSession 验证登录后初始化遇到缓存 miss 时不会访问数据库或复活旧会话。
func TestGetLoginAfterInfoRejectsMissingSession(t *testing.T) {
	server := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer rds.Close()
	cfg := config.Config{AppID: "site-a", JwtSecret: "test-secret-please-change", JwtExpiresIn: 3600}
	previous := runtimecfg.Get()
	runtimecfg.Set(cfg)
	t.Cleanup(func() { runtimecfg.Restore(previous) })
	svcCtx := svc.NewServiceContext(cfg, svc.Dependencies{Rds: rds})
	ctx, _ := requestctx.New(context.Background())
	requestctx.SetAccessToken(ctx, "expired-session-token")
	logicObj := &AdminLogic{BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx)}

	result := logicObj.GetLoginAfterInfo(&helper.CtxAdmin{ID: 1, Name: "admin"})
	if result == nil || result.Code != 401 {
		t.Fatalf("缺失会话时应返回未授权，实际为 %+v", result)
	}
	if server.Exists(keys.AdminInfoRedisKey(1)) {
		t.Fatal("登录后初始化不应使用旧请求 token 复活缺失会话")
	}
}
