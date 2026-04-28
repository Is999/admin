package secretkey

import (
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	"context"
	"fmt"
	"strings"
	"testing"

	keys "admin/common/rediskeys"
	"admin/internal/config"
	"admin/internal/security"
	"admin/internal/svc"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestParseSecretKeyStatusFieldRequiresCurrentField 验证路由缓存必须写入签名/加密状态字段。
func TestParseSecretKeyStatusFieldRequiresCurrentField(t *testing.T) {
	status, exists := parseSecretKeyStatusField(map[string]string{
		"status": "1",
	}, secretKeyCacheFieldCryptoStatus)
	if exists {
		t.Fatalf("exists = %v, want false", exists)
	}
	if status != 0 {
		t.Fatalf("status = %d, want 0", status)
	}
	status, exists = parseSecretKeyStatusField(map[string]string{
		secretKeyCacheFieldCryptoStatus: "bad",
	}, secretKeyCacheFieldCryptoStatus)
	if exists {
		t.Fatalf("invalid exists = %v, want false", exists)
	}
	if status != 0 {
		t.Fatalf("invalid status = %d, want 0", status)
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

// TestDeleteSecretKeyCacheDeletesRouteAndVersionCaches 验证当前秘钥 UUID 变更时会同时清理路由与版本材料缓存。
func TestDeleteSecretKeyCacheDeletesRouteAndVersionCaches(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	svcCtx := svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client})
	logicObj := &SecretKeyLogic{BaseLogic: corelogic.NewBaseLogicWithContext(context.Background(), svcCtx)}
	ctx := context.Background()
	appID := "demo-app"

	cacheKeys := []string{
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.SecretKeyRoute, appID)),
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.SecretKeyAESVersion, appID, "v1")),
		cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.SecretKeyRSAVersion, appID, "v1")),
	}
	for _, key := range cacheKeys {
		if err := client.HSet(ctx, key, "value", "demo").Err(); err != nil {
			t.Fatalf("HSet(%s) error = %v", key, err)
		}
	}
	versionIndexKey := cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.SecretKeyVersionIndex, appID))
	if err := client.SAdd(ctx, versionIndexKey, cacheKeys[1], cacheKeys[2]).Err(); err != nil {
		t.Fatalf("SAdd(secret key version index) error = %v", err)
	}

	if err := logicObj.deleteSecretKeyCache(appID); err != nil {
		t.Fatalf("deleteSecretKeyCache(%s) error = %v", appID, err)
	}

	for _, key := range cacheKeys {
		if server.Exists(key) {
			t.Fatalf("deleteSecretKeyCache() key %s still exists", key)
		}
	}
	if server.Exists(versionIndexKey) {
		t.Fatalf("deleteSecretKeyCache() version index still exists")
	}
}
