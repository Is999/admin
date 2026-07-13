package cache

import (
	"context"
	"sync"
	"testing"
	"time"

	keys "admin/common/rediskeys"
	"admin/internal/config"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestAdminSessionCacheUsesAppPrefix 验证管理员登录态只写入 app_id 命名空间下的 Redis key。
func TestAdminSessionCacheUsesAppPrefix(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})

	cacheLogic := NewCacheLogic(context.Background(), svc.NewServiceContext(config.Config{
		AppID:        "site-a",
		JwtExpiresIn: int64((4 * time.Hour) / time.Second),
	}, svc.Dependencies{Rds: client}))
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{
		ID:       7,
		UserName: "admin",
		Token:    "token-7",
	}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}

	if server.Exists(keys.AdminInfoLogicalKey(7)) {
		t.Fatal("不应写入裸管理员登录态 key")
	}
	if !server.Exists(keys.AdminInfoRedisKey(7)) {
		t.Fatal("期望写入 app_id 命名空间下的管理员登录态 key")
	}
	if ttl := server.TTL(keys.AdminInfoRedisKey(7)); ttl != 4*time.Hour {
		t.Fatalf("登录态 TTL = %s, want %s", ttl, 4*time.Hour)
	}
}

// TestRotateAdminTokenRenewsOnlyCurrentSession 验证 token 匹配时会原子轮换并续期，其他会话字段保持不变。
func TestRotateAdminTokenRenewsOnlyCurrentSession(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cacheLogic := NewCacheLogic(context.Background(), svc.NewServiceContext(config.Config{
		AppID:        "site-a",
		JwtExpiresIn: 3600,
	}, svc.Dependencies{Rds: client}))
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{
		ID:       7,
		UserName: "admin",
		RealName: "管理员",
		Token:    "old-token",
	}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}
	server.FastForward(3590 * time.Second)

	activeToken, err := cacheLogic.RotateAdminToken(7, "old-token", "new-token")
	if err != nil {
		t.Fatalf("RotateAdminToken() error = %v", err)
	}
	if activeToken != "new-token" {
		t.Fatalf("RotateAdminToken() = %q, want new-token", activeToken)
	}
	info, err := cacheLogic.GetAdminInfo(7)
	if err != nil {
		t.Fatalf("GetAdminInfo() error = %v", err)
	}
	if info.Token != "new-token" || info.RealName != "管理员" {
		t.Fatalf("轮换后会话字段异常: %+v", info)
	}
	if ttl := server.TTL(keys.AdminInfoRedisKey(7)); ttl != time.Hour {
		t.Fatalf("轮换后会话 TTL = %s, want %s", ttl, time.Hour)
	}
	if allowed, err := cacheLogic.CanUseAdminSessionToken(7, "old-token"); err != nil || !allowed {
		t.Fatalf("宽限期内旧 token 应允许刷新，allowed = %v error = %v", allowed, err)
	}
	if err := client.HSet(context.Background(), keys.AdminInfoRedisKey(7), adminRefreshGraceUntilField, 0).Err(); err != nil {
		t.Fatalf("设置过期刷新宽限时间失败: %v", err)
	}
	if allowed, err := cacheLogic.CanUseAdminSessionToken(7, "old-token"); err != nil || allowed {
		t.Fatalf("宽限期结束后旧 token 应拒绝刷新，allowed = %v error = %v", allowed, err)
	}
}

// TestRotateAdminTokenRejectsMissingOrChangedSession 验证会话缺失或旧 token 不匹配时不会创建、覆盖会话。
func TestRotateAdminTokenRejectsMissingOrChangedSession(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cacheLogic := NewCacheLogic(context.Background(), svc.NewServiceContext(config.Config{
		AppID:        "site-a",
		JwtExpiresIn: 3600,
	}, svc.Dependencies{Rds: client}))
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{ID: 7, UserName: "admin", Token: "current-token"}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}

	activeToken, err := cacheLogic.RotateAdminToken(7, "stale-token", "unexpected-token")
	if err != nil {
		t.Fatalf("RotateAdminToken(stale) error = %v", err)
	}
	if activeToken != "" {
		t.Fatalf("旧 token 不匹配时不应轮换会话，实际返回 %q", activeToken)
	}
	if token, err := cacheLogic.GetAdminToken(7); err != nil || token != "current-token" {
		t.Fatalf("旧 token 不匹配后缓存 token = %q error = %v", token, err)
	}

	if err := cacheLogic.DeleteAdminInfo(7); err != nil {
		t.Fatalf("DeleteAdminInfo() error = %v", err)
	}
	activeToken, err = cacheLogic.RotateAdminToken(7, "current-token", "new-token")
	if err != nil {
		t.Fatalf("RotateAdminToken(missing) error = %v", err)
	}
	if activeToken != "" || server.Exists(keys.AdminInfoRedisKey(7)) {
		t.Fatal("缺失会话不应被 token 轮换重新创建")
	}
}

// TestRotateAdminTokenReturnsSameTokenToConcurrentRequests 验证并发刷新只轮换一次，并向所有宽限重试返回同一 token。
func TestRotateAdminTokenReturnsSameTokenToConcurrentRequests(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cacheLogic := NewCacheLogic(context.Background(), svc.NewServiceContext(config.Config{
		AppID:        "site-a",
		JwtExpiresIn: 3600,
	}, svc.Dependencies{Rds: client}))
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{ID: 7, UserName: "admin", Token: "old-token"}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}

	start := make(chan struct{})
	results := make(chan string, 2)
	errorsCh := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for _, nextToken := range []string{"new-token-a", "new-token-b"} {
		nextToken := nextToken
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			activeToken, err := cacheLogic.RotateAdminToken(7, "old-token", nextToken)
			results <- activeToken
			errorsCh <- err
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("RotateAdminToken() concurrent error = %v", err)
		}
	}
	returnedTokens := make([]string, 0, 2)
	for activeToken := range results {
		returnedTokens = append(returnedTokens, activeToken)
	}
	if len(returnedTokens) != 2 || returnedTokens[0] == "" || returnedTokens[0] != returnedTokens[1] {
		t.Fatalf("并发轮换返回 token = %#v，期望两个请求返回同一非空 token", returnedTokens)
	}
	if cachedToken, err := cacheLogic.GetAdminToken(7); err != nil || cachedToken != returnedTokens[0] {
		t.Fatalf("并发轮换后缓存 token = %q error = %v，返回 token = %#v", cachedToken, err, returnedTokens)
	}
}

// TestSetAdminInfoClearsRefreshGrace 验证新登录会在同 key 事务中覆盖 token 并清除上一轮刷新宽限。
func TestSetAdminInfoClearsRefreshGrace(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cacheLogic := NewCacheLogic(context.Background(), svc.NewServiceContext(config.Config{
		AppID:        "site-a",
		JwtExpiresIn: 3600,
	}, svc.Dependencies{Rds: client}))
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{ID: 7, UserName: "admin", Token: "old-token"}); err != nil {
		t.Fatalf("SetAdminInfo(old) error = %v", err)
	}
	if _, err := cacheLogic.RotateAdminToken(7, "old-token", "rotated-token"); err != nil {
		t.Fatalf("RotateAdminToken() error = %v", err)
	}
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{ID: 7, UserName: "admin", Token: "login-token"}); err != nil {
		t.Fatalf("SetAdminInfo(login) error = %v", err)
	}
	if allowed, err := cacheLogic.CanUseAdminSessionToken(7, "old-token"); err != nil || allowed {
		t.Fatalf("新登录后旧 token 宽限应失效，allowed = %v error = %v", allowed, err)
	}
	if token, err := cacheLogic.GetAdminToken(7); err != nil || token != "login-token" {
		t.Fatalf("新登录后缓存 token = %q error = %v", token, err)
	}
	fields, err := client.HMGet(context.Background(), keys.AdminInfoRedisKey(7), adminRefreshPreviousTokenDigestField, adminRefreshGraceUntilField).Result()
	if err != nil {
		t.Fatalf("HMGet(refresh grace) error = %v", err)
	}
	if fields[0] != nil || fields[1] != nil {
		t.Fatalf("新登录后刷新宽限字段未清理: %#v", fields)
	}
}

// TestSetAdminInfoFieldsIsAtomicAndDoesNotCreateSession 验证资料字段只在完整会话存在时原子更新。
func TestSetAdminInfoFieldsIsAtomicAndDoesNotCreateSession(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cacheLogic := NewCacheLogic(context.Background(), svc.NewServiceContext(config.Config{
		AppID:        "site-a",
		JwtExpiresIn: 3600,
	}, svc.Dependencies{Rds: client}))
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{
		ID:       7,
		UserName: "admin",
		RealName: "旧姓名",
		Phone:    "13000000000",
		Token:    "token-7",
	}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}
	if err := cacheLogic.SetAdminInfoFields(7, map[string]any{
		"phone":    "13800000000",
		"realName": "新姓名",
	}); err != nil {
		t.Fatalf("SetAdminInfoFields() error = %v", err)
	}
	info, err := cacheLogic.GetAdminInfo(7)
	if err != nil {
		t.Fatalf("GetAdminInfo() error = %v", err)
	}
	if info.RealName != "新姓名" || info.Phone != "13800000000" || info.Token != "token-7" {
		t.Fatalf("资料同步后会话字段异常: %+v", info)
	}

	if err := client.HDel(context.Background(), keys.AdminInfoRedisKey(7), "phone").Err(); err != nil {
		t.Fatalf("HDel(phone) error = %v", err)
	}
	if err := cacheLogic.SetAdminInfoFields(7, map[string]any{"phone": "13900000000", "realName": "不应写入"}); err == nil {
		t.Fatal("目标字段缺失时应拒绝整批更新")
	}
	if value := client.HGet(context.Background(), keys.AdminInfoRedisKey(7), "realName").Val(); value != "新姓名" {
		t.Fatalf("原子更新失败后 realName = %q, want 新姓名", value)
	}

	if err := cacheLogic.DeleteAdminInfo(7); err != nil {
		t.Fatalf("DeleteAdminInfo() error = %v", err)
	}
	if err := cacheLogic.SetAdminInfoFields(7, map[string]any{"avatar": "new-avatar"}); err == nil {
		t.Fatal("缺失会话时应拒绝资料字段更新")
	}
	if server.Exists(keys.AdminInfoRedisKey(7)) {
		t.Fatal("资料字段更新不应复活缺失会话")
	}
}

// TestDeleteAdminSessionForLogout 只删除当前 token 对应的管理员会话。
func TestDeleteAdminSessionForLogout(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})

	cacheLogic := NewCacheLogic(context.Background(), svc.NewServiceContext(
		config.Config{AppID: "site-a", JwtExpiresIn: 3600},
		svc.Dependencies{Rds: client},
	))
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{ID: 7, Token: "current-token"}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}
	deleted, err := cacheLogic.DeleteAdminSessionForLogout(7, "current-token")
	if err != nil || !deleted {
		t.Fatalf("DeleteAdminSessionForLogout() deleted=%v error=%v", deleted, err)
	}
	if server.Exists(keys.AdminInfoRedisKey(7)) {
		t.Fatal("当前 token 登出后管理员会话仍存在")
	}
}

// TestDeleteAdminSessionForLogoutPreservesNewLogin 验证旧请求登出不会删除后来建立的新会话。
func TestDeleteAdminSessionForLogoutPreservesNewLogin(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})

	cacheLogic := NewCacheLogic(context.Background(), svc.NewServiceContext(
		config.Config{AppID: "site-a", JwtExpiresIn: 3600},
		svc.Dependencies{Rds: client},
	))
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{ID: 7, Token: "new-token"}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}
	deleted, err := cacheLogic.DeleteAdminSessionForLogout(7, "old-token")
	if err != nil || deleted {
		t.Fatalf("DeleteAdminSessionForLogout(old) deleted=%v error=%v", deleted, err)
	}
	if token := client.HGet(context.Background(), keys.AdminInfoRedisKey(7), "token").Val(); token != "new-token" {
		t.Fatalf("旧请求登出后缓存 token=%q，期望保留 new-token", token)
	}
}

// TestDeleteAdminSessionForLogoutAcceptsPreviousRefreshToken 验证刷新与退出并发时仍会撤销轮换后的会话。
func TestDeleteAdminSessionForLogoutAcceptsPreviousRefreshToken(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})

	cacheLogic := NewCacheLogic(context.Background(), svc.NewServiceContext(
		config.Config{AppID: "site-a", JwtExpiresIn: 3600},
		svc.Dependencies{Rds: client},
	))
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{ID: 7, Token: "old-token"}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}
	if activeToken, err := cacheLogic.RotateAdminToken(7, "old-token", "refreshed-token"); err != nil || activeToken != "refreshed-token" {
		t.Fatalf("RotateAdminToken() token=%q error=%v", activeToken, err)
	}

	deleted, err := cacheLogic.DeleteAdminSessionForLogout(7, "old-token")
	if err != nil || !deleted {
		t.Fatalf("DeleteAdminSessionForLogout(previous) deleted=%v error=%v", deleted, err)
	}
	if server.Exists(keys.AdminInfoRedisKey(7)) {
		t.Fatal("上一枚刷新 token 退出后管理员会话仍存在")
	}
}

// TestDeleteAdminSessionForLogoutRejectsExpiredPreviousToken 验证宽限过期的旧 token 不会删除当前会话。
func TestDeleteAdminSessionForLogoutRejectsExpiredPreviousToken(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})

	cacheLogic := NewCacheLogic(context.Background(), svc.NewServiceContext(
		config.Config{AppID: "site-a", JwtExpiresIn: 3600},
		svc.Dependencies{Rds: client},
	))
	if err := cacheLogic.SetAdminInfo(7, &types.AdminInfo{ID: 7, Token: "old-token"}); err != nil {
		t.Fatalf("SetAdminInfo() error = %v", err)
	}
	if activeToken, err := cacheLogic.RotateAdminToken(7, "old-token", "refreshed-token"); err != nil || activeToken != "refreshed-token" {
		t.Fatalf("RotateAdminToken() token=%q error=%v", activeToken, err)
	}
	if err := client.HSet(context.Background(), keys.AdminInfoRedisKey(7), adminRefreshGraceUntilField, 0).Err(); err != nil {
		t.Fatalf("设置过期刷新宽限时间失败: %v", err)
	}

	deleted, err := cacheLogic.DeleteAdminSessionForLogout(7, "old-token")
	if err != nil || deleted {
		t.Fatalf("DeleteAdminSessionForLogout(expired previous) deleted=%v error=%v", deleted, err)
	}
	if token := client.HGet(context.Background(), keys.AdminInfoRedisKey(7), "token").Val(); token != "refreshed-token" {
		t.Fatalf("过期旧 token 退出后缓存 token=%q，期望保留 refreshed-token", token)
	}
}
