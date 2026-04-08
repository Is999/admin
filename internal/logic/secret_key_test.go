package logic

import (
	"context"
	"strings"
	"testing"

	"admin_cron/internal/config"
	"admin_cron/internal/security"
	"admin_cron/internal/svc"
)

// TestParseSecretKeyStatusFieldLegacyCache 验证旧路由缓存未写入 sign_status/crypto_status 时，
// 仍按启用兼容，避免发布新字段后历史缓存把链路误判为关闭。
func TestParseSecretKeyStatusFieldLegacyCache(t *testing.T) {
	status, exists := parseSecretKeyStatusField(map[string]string{
		"status": "1",
	}, secretKeyCacheFieldCryptoStatus)
	if exists {
		t.Fatalf("exists = %v, want false", exists)
	}
	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
}

// TestSecretKeyRouteConfigNeedsStatusBackfill 验证旧缓存会触发回源刷新，新缓存不会重复刷新。
func TestSecretKeyRouteConfigNeedsStatusBackfill(t *testing.T) {
	legacy := &SecretKeyRouteConfig{
		Status:                   1,
		SignStatus:               1,
		CryptoStatus:             1,
		cacheMissingCryptoStatus: true,
	}
	if !legacy.needsStatusBackfill() {
		t.Fatal("legacy cache should require backfill")
	}

	current := &SecretKeyRouteConfig{
		Status:       1,
		SignStatus:   1,
		CryptoStatus: 1,
	}
	if current.needsStatusBackfill() {
		t.Fatal("current cache should not require backfill")
	}
}

// TestNormalizeSecretKeyVersionHintRejectsHighCardinalityInput 验证异常版本号不会进入 Redis key。
func TestNormalizeSecretKeyVersionHintRejectsHighCardinalityInput(t *testing.T) {
	if got, err := normalizeSecretKeyVersionHint(" v1 "); err != nil || got != "v1" {
		t.Fatalf("normalizeSecretKeyVersionHint() = %q, %v, want v1", got, err)
	}
	for _, value := range []string{"v*", "v?", "bad version", strings.Repeat("v", 65)} {
		if _, err := normalizeSecretKeyVersionHint(value); err == nil {
			t.Fatalf("期望版本号 %q 被拒绝，实际返回 nil", value)
		}
	}
}

// TestSecretKeyVersionShouldCacheEmptyMarkerOnlyForRouteVersions 验证只有稳定版/灰度版缺失时才写空值占位。
func TestSecretKeyVersionShouldCacheEmptyMarkerOnlyForRouteVersions(t *testing.T) {
	routeVersions := []string{"v1", "v2"}
	if !secretKeyVersionShouldCacheEmptyMarker("v1", routeVersions) {
		t.Fatal("稳定版应允许写空值占位")
	}
	if secretKeyVersionShouldCacheEmptyMarker("random", routeVersions) {
		t.Fatal("随机版本不应写空值占位")
	}
}

// TestSecretKeyLogicUsesConfigWhenAppIDMatches 验证请求 AppID 命中顶层 app_id 时读取配置文件秘钥。
func TestSecretKeyLogicUsesConfigWhenAppIDMatches(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		AppID: "1",
		Security: config.SecurityConfig{
			SecretKey: config.SecuritySecretKeyConfig{
				KeyVersion:   "v1",
				AESKey:       "1234567890123456",
				AESIV:        "abcdefghijklmnop",
				SignStatus:   1,
				CryptoStatus: 1,
			},
		},
	}, svc.Dependencies{})

	logicObj := NewSecretKeyLogic(context.Background(), svcCtx)
	route, err := logicObj.GetRouteConfig("1")
	if err != nil {
		t.Fatalf("GetRouteConfig() error = %v", err)
	}
	if route.StableVersion != "v1" || !route.SignEnabled() || !route.CryptoEnabled() {
		t.Fatalf("配置文件路由读取异常: %+v", route)
	}
	aesKey, version, err := logicObj.GetAESKey("1", "", "")
	if err != nil {
		t.Fatalf("GetAESKey() error = %v", err)
	}
	if version != "v1" {
		t.Fatalf("version = %q, want v1", version)
	}
	if aesKey == nil || aesKey.Key != "1234567890123456" || aesKey.IV != "abcdefghijklmnop" {
		t.Fatalf("AESKey = %+v, want config value", aesKey)
	}
}

// TestSecretKeyLogicDerivesConfigServerPublicKey 验证 YAML 可只配置服务端私钥，服务端公钥按需派生。
func TestSecretKeyLogicDerivesConfigServerPublicKey(t *testing.T) {
	serverPrivatePEM, wantPublicPEM := generateTestRSAPEMPair(t)
	svcCtx := svc.NewServiceContext(config.Config{
		AppID: "1",
		Security: config.SecurityConfig{
			SecretKey: config.SecuritySecretKeyConfig{
				KeyVersion:             "v1",
				SignStatus:             1,
				RSAPrivateKeyServer:    serverPrivatePEM,
				RSAPrivateKeyServerRef: "",
			},
		},
	}, svc.Dependencies{})

	gotPublicPEM, version, err := NewSecretKeyLogic(context.Background(), svcCtx).GetRSAKey("1", "", "", RSAServerPublicKey)
	if err != nil {
		t.Fatalf("GetRSAKey(RSAServerPublicKey) error = %v", err)
	}
	if version != "v1" {
		t.Fatalf("version = %q, want v1", version)
	}
	gotPublicKey, err := security.ParseRSAPublicKey(gotPublicPEM)
	if err != nil {
		t.Fatalf("derived public key parse error = %v", err)
	}
	wantPublicKey, err := security.ParseRSAPublicKey(wantPublicPEM)
	if err != nil {
		t.Fatalf("expected public key parse error = %v", err)
	}
	if gotPublicKey.N.Cmp(wantPublicKey.N) != 0 || gotPublicKey.E != wantPublicKey.E {
		t.Fatal("derived server public key does not match private key")
	}
}

// TestSecretKeyLogicFallsBackWhenAppIDDiffers 验证 AppID 不匹配时继续沿用数据库回源逻辑。
func TestSecretKeyLogicFallsBackWhenAppIDDiffers(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		AppID: "1",
		Security: config.SecurityConfig{
			SecretKey: config.SecuritySecretKeyConfig{
				KeyVersion:   "v1",
				SignStatus:   1,
				CryptoStatus: 1,
			},
		},
	}, svc.Dependencies{})

	_, err := NewSecretKeyLogic(context.Background(), svcCtx).GetRouteConfig("204")
	if err == nil {
		t.Fatal("GetRouteConfig() should fall back to database and fail without main DB")
	}
	if !strings.Contains(err.Error(), "主库数据库未初始化") {
		t.Fatalf("err = %v, want 主库数据库未初始化", err)
	}
}
