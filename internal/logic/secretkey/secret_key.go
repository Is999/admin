package secretkey

import (
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	keys "admin/common/rediskeys"
	"admin/internal/config"
	"admin/internal/model"
	"admin/internal/security"
	"admin/internal/svc"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	// SecretKeyTypeAES 表示读取 AES 秘钥配置。
	SecretKeyTypeAES = "AES"
	// SecretKeyTypeRSA 表示读取 RSA 秘钥配置。
	SecretKeyTypeRSA = "RSA"

	// RSAUserPublicKey 表示用户 RSA 公钥，用于请求验签和响应加密。
	RSAUserPublicKey = "user_public_key"
	// RSAServerPublicKey 表示服务端 RSA 公钥，用于对外展示和调试。
	RSAServerPublicKey = "server_public_key"
	// RSAServerPrivateKey 表示服务端 RSA 私钥，用于请求解密和响应签名。
	RSAServerPrivateKey = "server_private_key"

	// secretKeyCacheFieldAESKeyRef 表示 AES KEY 文件路径缓存字段。
	secretKeyCacheFieldAESKeyRef = "aes_key_ref"
	// secretKeyCacheFieldAESIVRef 表示 AES IV 文件路径缓存字段。
	secretKeyCacheFieldAESIVRef = "aes_iv_ref"
	// secretKeyCacheFieldRSAPublicKeyUserRef 表示用户 RSA 公钥文件路径缓存字段。
	secretKeyCacheFieldRSAPublicKeyUserRef = "rsa_public_key_user_ref"
	// secretKeyCacheFieldRSAPublicKeyServerRef 表示服务端 RSA 公钥文件路径缓存字段。
	secretKeyCacheFieldRSAPublicKeyServerRef = "rsa_public_key_server_ref"
	// secretKeyCacheFieldRSAPrivateKeyServerRef 表示服务端 RSA 私钥文件路径缓存字段。
	secretKeyCacheFieldRSAPrivateKeyServerRef = "rsa_private_key_server_ref"
	// secretKeyCacheFieldStableVersion 表示稳定版本缓存字段。
	secretKeyCacheFieldStableVersion = "stable_version"
	// secretKeyCacheFieldGrayVersion 表示灰度版本缓存字段。
	secretKeyCacheFieldGrayVersion = "gray_version"
	// secretKeyCacheFieldGrayPercent 表示灰度比例缓存字段。
	secretKeyCacheFieldGrayPercent = "gray_percent"
	// secretKeyCacheFieldGraySalt 表示灰度哈希盐值缓存字段。
	secretKeyCacheFieldGraySalt = "gray_salt"
	// secretKeyCacheFieldSignStatus 表示签名验签状态缓存字段。
	secretKeyCacheFieldSignStatus = "sign_status"
	// secretKeyCacheFieldCryptoStatus 表示加密解密状态缓存字段。
	secretKeyCacheFieldCryptoStatus = "crypto_status"
)

// SecretKeyLogic 承载签名与加密所需的 secret_key 配置读取和缓存逻辑。
type SecretKeyLogic struct {
	*corelogic.BaseLogic // 复用上下文、数据库、Redis 和日志能力
}

// NewSecretKeyLogic 创建秘钥业务逻辑对象。
func NewSecretKeyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SecretKeyLogic {
	return &SecretKeyLogic{
		BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx),
	}
}

// AESKey 表示启用状态的 AES KEY 与 IV。
type AESKey struct {
	Key string // Key 是 AES 密钥明文，长度必须为 16、24 或 32 位
	IV  string // IV 是 AES CBC 初始化向量，长度必须为 16 位
}

// SecretKeyRouteConfig 表示运行时版本选路配置。
type SecretKeyRouteConfig struct {
	UUID          string // UUID 表示接入应用 AppID
	StableVersion string // StableVersion 表示稳定版本号
	GrayVersion   string // GrayVersion 表示灰度版本号
	GrayPercent   int    // GrayPercent 表示灰度流量百分比
	GraySalt      string // GraySalt 表示灰度哈希盐值
	Status        int    // Status 表示 AppID 总状态：1启用，0停用
	SignStatus    int    // SignStatus 表示签名验签状态：1启用，0停用
	CryptoStatus  int    // CryptoStatus 表示加密解密状态：1启用，0停用
}

// SignEnabled 返回当前 AppID 是否启用签名验签链路。
func (c *SecretKeyRouteConfig) SignEnabled() bool {
	return c != nil && c.Status == 1 && c.SignStatus == 1
}

// CryptoEnabled 返回当前 AppID 是否启用加密解密链路。
func (c *SecretKeyRouteConfig) CryptoEnabled() bool {
	return c != nil && c.Status == 1 && c.CryptoStatus == 1
}

// GetRouteConfig 读取指定 AppID 的运行时路由配置。
func (l *SecretKeyLogic) GetRouteConfig(appID string) (*SecretKeyRouteConfig, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil, errors.Errorf("AppID不能为空")
	}
	return l.getSecretKeyRoute(appID)
}

// GetAESKey 读取指定 AppID 在当前路由命中的 AES KEY 与 IV，并返回最终命中的版本号。
func (l *SecretKeyLogic) GetAESKey(appID string, versionHint string, grayKey string) (*AESKey, string, error) {
	appID = strings.TrimSpace(appID)
	if l.shouldUseConfigSecretKey(appID) {
		return l.getConfigAESKey(appID, versionHint, grayKey)
	}
	row, version, err := l.getSecretKeyVersion(appID, versionHint, grayKey, SecretKeyTypeAES)
	if err != nil {
		return nil, "", errors.Tag(err)
	}
	key, err := normalizeSecretText(row.AESKeyRef)
	if err != nil {
		return nil, "", errors.Wrap(err, "AES KEY解析失败")
	}
	iv, err := normalizeSecretText(row.AESIVRef)
	if err != nil {
		return nil, "", errors.Wrap(err, "AES IV解析失败")
	}
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, "", errors.Errorf("AES KEY长度必须是16、24或32位")
	}
	if len(iv) != 16 {
		return nil, "", errors.Errorf("AES IV长度必须是16位")
	}
	return &AESKey{Key: key, IV: iv}, version, nil
}

// GetRSAKey 读取指定 AppID 在当前路由命中的 RSA PEM 内容，并返回最终命中的版本号。
func (l *SecretKeyLogic) GetRSAKey(appID string, versionHint string, grayKey string, keyType string) (string, string, error) {
	appID = strings.TrimSpace(appID)
	if l.shouldUseConfigSecretKey(appID) {
		return l.getConfigRSAKey(appID, versionHint, grayKey, keyType)
	}
	row, version, err := l.getSecretKeyVersion(appID, versionHint, grayKey, SecretKeyTypeRSA)
	if err != nil {
		return "", "", errors.Tag(err)
	}
	keyRef := ""
	switch keyType {
	case RSAUserPublicKey:
		keyRef = row.RSAPublicKeyUserRef
	case RSAServerPublicKey:
		if strings.TrimSpace(row.RSAPublicKeyServerRef) == "" {
			text, err := deriveServerPublicPEMFromPrivateRef(row.RSAPrivateKeyServerRef)
			return text, version, errors.Tag(err)
		}
		keyRef = row.RSAPublicKeyServerRef
	case RSAServerPrivateKey:
		keyRef = row.RSAPrivateKeyServerRef
	default:
		return "", "", errors.Errorf("RSA秘钥类型不合法: %s", keyType)
	}
	text, err := resolvePEMText(keyRef)
	if err != nil {
		return "", "", errors.Tag(err)
	}
	return text, version, nil
}

// ResolveSecretKeyVersion 返回当前请求最终命中的秘钥版本。
func (l *SecretKeyLogic) ResolveSecretKeyVersion(appID string, versionHint string, grayKey string) (string, error) {
	_, version, err := l.resolveSecretKeyVersion(appID, versionHint, grayKey)
	return version, errors.Tag(err)
}

// getSecretKeyVersion 从缓存或数据库读取指定 AppID 当前命中的版本配置。
func (l *SecretKeyLogic) getSecretKeyVersion(appID string, versionHint string, grayKey string, typ string) (*model.SecretKeyVersion, string, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil, "", errors.Errorf("AppID不能为空")
	}
	resolvedVersion, err := l.ResolveSecretKeyVersion(appID, versionHint, grayKey)
	if err != nil {
		return nil, "", errors.Tag(err)
	}
	row, err := l.getSecretKeyVersionCache(appID, resolvedVersion, typ)
	if err != nil {
		if err == redis.Nil && l.Redis() != nil {
			row, err = l.renewSecretKeyVersionCache(appID, resolvedVersion)
		}
		if err != nil {
			return nil, "", errors.Tag(err)
		}
	}
	if row.Status != 1 {
		return nil, "", errors.Errorf("秘钥版本已停用")
	}
	return row, resolvedVersion, nil
}

// getSecretKeyRoute 读取指定 AppID 的选路配置。
func (l *SecretKeyLogic) getSecretKeyRoute(appID string) (*SecretKeyRouteConfig, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil, errors.Errorf("AppID不能为空")
	}
	if l.shouldUseConfigSecretKey(appID) {
		return l.getConfigSecretKeyRoute(appID)
	}
	if l.Redis() == nil {
		return l.renewSecretKeyRouteCache(appID)
	}
	route, err := l.getSecretKeyRouteCache(appID)
	if err == redis.Nil {
		return l.renewSecretKeyRouteCache(appID)
	}
	return route, errors.Tag(err)
}

// getSecretKeyRouteCache 读取选路缓存。
func (l *SecretKeyLogic) getSecretKeyRouteCache(appID string) (*SecretKeyRouteConfig, error) {
	key := l.secretKeyRoutePhysicalCacheKey(appID)
	data, err := l.Redis().HGetAll(l.Ctx, key).Result()
	if err != nil {
		return nil, errors.Tag(err)
	}
	if len(data) == 0 {
		return nil, redis.Nil
	}
	if corelogic.CacheIsEmptyMarker(data["value"]) {
		return nil, corelogic.ErrCacheEmptyMarker
	}
	status := 0
	_, _ = fmt.Sscanf(data["status"], "%d", &status)
	signStatus, signStatusExists := parseSecretKeyStatusField(data, secretKeyCacheFieldSignStatus)
	cryptoStatus, cryptoStatusExists := parseSecretKeyStatusField(data, secretKeyCacheFieldCryptoStatus)
	if !signStatusExists || !cryptoStatusExists {
		return nil, redis.Nil
	}
	grayPercent := 0
	_, _ = fmt.Sscanf(data[secretKeyCacheFieldGrayPercent], "%d", &grayPercent)
	return &SecretKeyRouteConfig{
		UUID:          appID,
		StableVersion: data[secretKeyCacheFieldStableVersion],
		GrayVersion:   data[secretKeyCacheFieldGrayVersion],
		GrayPercent:   grayPercent,
		GraySalt:      data[secretKeyCacheFieldGraySalt],
		Status:        status,
		SignStatus:    signStatus,
		CryptoStatus:  cryptoStatus,
	}, nil
}

// parseSecretKeyStatusField 解析当前路由缓存中的签名/加密开关字段。
func parseSecretKeyStatusField(data map[string]string, field string) (int, bool) {
	value, ok := data[field]
	if !ok || strings.TrimSpace(value) == "" {
		return 0, false
	}
	status := 0
	if _, err := fmt.Sscanf(value, "%d", &status); err != nil {
		return 0, false
	}
	return status, true
}

// getSecretKeyVersionCache 读取版本材料缓存。
func (l *SecretKeyLogic) getSecretKeyVersionCache(appID string, keyVersion string, typ string) (*model.SecretKeyVersion, error) {
	key := l.secretKeyVersionPhysicalCacheKey(appID, keyVersion, typ)
	data, err := l.Redis().HGetAll(l.Ctx, key).Result()
	if err != nil {
		return nil, errors.Tag(err)
	}
	if len(data) == 0 {
		return nil, redis.Nil
	}
	if corelogic.CacheIsEmptyMarker(data["value"]) {
		return nil, corelogic.ErrCacheEmptyMarker
	}
	status := 0
	_, _ = fmt.Sscanf(data["status"], "%d", &status)
	return &model.SecretKeyVersion{
		UUID:                   appID,
		KeyVersion:             keyVersion,
		AESKeyRef:              data[secretKeyCacheFieldAESKeyRef],
		AESIVRef:               data[secretKeyCacheFieldAESIVRef],
		RSAPublicKeyUserRef:    data[secretKeyCacheFieldRSAPublicKeyUserRef],
		RSAPublicKeyServerRef:  data[secretKeyCacheFieldRSAPublicKeyServerRef],
		RSAPrivateKeyServerRef: data[secretKeyCacheFieldRSAPrivateKeyServerRef],
		Status:                 status,
	}, nil
}

// renewSecretKeyRouteCache 回源数据库刷新选路缓存。
func (l *SecretKeyLogic) renewSecretKeyRouteCache(appID string) (*SecretKeyRouteConfig, error) {
	// 秘钥路由回源使用主库写连接，确保读取到最新启停状态与灰度配置。
	writeDB, err := l.configWriteDB()
	if err != nil {
		return nil, errors.Tag(err)
	}
	routeCacheKey := l.secretKeyRoutePhysicalCacheKey(appID)
	var row model.SecretKey
	if err := writeDB.Where("uuid = ?", appID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) && l.Redis() != nil {
			ctx := l.Ctx
			pipe := l.Redis().Pipeline()
			pipe.Del(ctx, routeCacheKey)
			pipe.HSet(ctx, routeCacheKey, map[string]any{"value": keys.EmptyValueMarker})
			pipe.Expire(ctx, routeCacheKey, corelogic.EmptyCacheTTL())
			_, _ = pipe.Exec(ctx)
		}
		return nil, errors.Tag(err)
	}
	result := &SecretKeyRouteConfig{
		UUID:          row.UUID,
		StableVersion: row.StableVersion,
		GrayVersion:   row.GrayVersion,
		GrayPercent:   row.GrayPercent,
		GraySalt:      row.GraySalt,
		Status:        row.Status,
		SignStatus:    row.SignStatus,
		CryptoStatus:  row.CryptoStatus,
	}
	if l.Redis() == nil {
		return result, nil
	}
	ctx := l.Ctx
	pipe := l.Redis().Pipeline()
	pipe.Del(ctx, routeCacheKey)
	pipe.HSet(ctx, routeCacheKey, map[string]any{
		secretKeyCacheFieldStableVersion: row.StableVersion,
		secretKeyCacheFieldGrayVersion:   row.GrayVersion,
		secretKeyCacheFieldGrayPercent:   row.GrayPercent,
		secretKeyCacheFieldGraySalt:      row.GraySalt,
		secretKeyCacheFieldSignStatus:    row.SignStatus,
		secretKeyCacheFieldCryptoStatus:  row.CryptoStatus,
		"status":                         row.Status,
	})
	pipe.Expire(ctx, routeCacheKey, corelogic.JitterTTL(time.Hour))
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, errors.Tag(err)
	}
	return result, nil
}

// renewSecretKeyVersionCache 回源数据库刷新指定版本的材料缓存。
func (l *SecretKeyLogic) renewSecretKeyVersionCache(appID string, keyVersion string) (*model.SecretKeyVersion, error) {
	// 版本材料包含 AES/RSA 文件引用，回源时必须强制走 config 主库。
	writeDB, err := l.configWriteDB()
	if err != nil {
		return nil, errors.Tag(err)
	}
	var row model.SecretKeyVersion
	if err := writeDB.Where("uuid = ? AND key_version = ?", appID, keyVersion).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) && l.Redis() != nil {
			ctx := l.Ctx
			aesCacheKey, rsaCacheKey := l.secretKeyVersionPhysicalCacheKeys(appID, keyVersion)
			pipe := l.Redis().Pipeline()
			pipe.Del(ctx, aesCacheKey)
			pipe.Del(ctx, rsaCacheKey)
			if secretKeyVersionShouldCacheEmptyMarker(keyVersion, l.secretKeyRouteVersionHints(appID)) {
				indexKey := l.secretKeyVersionPhysicalIndexKey(appID)
				pipe.HSet(ctx, aesCacheKey, map[string]any{"value": keys.EmptyValueMarker})
				pipe.HSet(ctx, rsaCacheKey, map[string]any{"value": keys.EmptyValueMarker})
				pipe.Expire(ctx, aesCacheKey, corelogic.EmptyCacheTTL())
				pipe.Expire(ctx, rsaCacheKey, corelogic.EmptyCacheTTL())
				// 仅对稳定版/灰度版这类受控版本写空值索引，避免恶意 X-Key-Version 打出大量随机 Redis key。
				pipe.SAdd(ctx, indexKey, aesCacheKey, rsaCacheKey)
				pipe.Expire(ctx, indexKey, secretKeyVersionIndexTTL())
			}
			_, _ = pipe.Exec(ctx)
		}
		return nil, errors.Tag(err)
	}
	if l.Redis() == nil {
		return &row, nil
	}
	ctx := l.Ctx
	aesCacheKey, rsaCacheKey := l.secretKeyVersionPhysicalCacheKeys(appID, keyVersion)
	indexKey := l.secretKeyVersionPhysicalIndexKey(appID)
	pipe := l.Redis().Pipeline()
	pipe.Del(ctx, aesCacheKey)
	pipe.Del(ctx, rsaCacheKey)
	pipe.HSet(ctx, aesCacheKey, map[string]any{
		secretKeyCacheFieldAESKeyRef: row.AESKeyRef,
		secretKeyCacheFieldAESIVRef:  row.AESIVRef,
		"status":                     row.Status,
	})
	pipe.HSet(ctx, rsaCacheKey, map[string]any{
		secretKeyCacheFieldRSAPublicKeyUserRef:    row.RSAPublicKeyUserRef,
		secretKeyCacheFieldRSAPublicKeyServerRef:  row.RSAPublicKeyServerRef,
		secretKeyCacheFieldRSAPrivateKeyServerRef: row.RSAPrivateKeyServerRef,
		"status": row.Status,
	})
	pipe.Expire(ctx, aesCacheKey, corelogic.JitterTTL(time.Hour))
	pipe.Expire(ctx, rsaCacheKey, corelogic.JitterTTL(time.Hour))
	// 版本材料缓存写入 AppID 维度索引，刷新/删除时按索引精确删除，避免生产 Redis 使用前缀 SCAN。
	pipe.SAdd(ctx, indexKey, aesCacheKey, rsaCacheKey)
	pipe.Expire(ctx, indexKey, secretKeyVersionIndexTTL())
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, errors.Tag(err)
	}
	return &row, nil
}

// RenewSecretKeyCache 刷新指定 AppID 的路由与所有版本材料缓存。
func (l *SecretKeyLogic) RenewSecretKeyCache(appID string) error {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return errors.Errorf("AppID不能为空")
	}
	if l.shouldUseConfigSecretKey(appID) {
		return l.validateConfigSecretKey(appID)
	}
	if _, err := l.renewSecretKeyRouteCache(appID); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.Tag(err)
	}
	writeDB, err := l.configWriteDB()
	if err != nil {
		return errors.Tag(err)
	}
	var versions []model.SecretKeyVersion
	if err := writeDB.Where("uuid = ?", appID).Find(&versions).Error; err != nil {
		return errors.Tag(err)
	}
	versionHints := make([]string, 0, len(versions))
	for _, version := range versions {
		versionHints = append(versionHints, version.KeyVersion)
	}
	if err := l.deleteSecretKeyVersionCaches(appID, versionHints...); err != nil {
		return errors.Tag(err)
	}
	for _, version := range versions {
		if _, err := l.renewSecretKeyVersionCache(appID, version.KeyVersion); err != nil {
			return errors.Tag(err)
		}
	}
	return nil
}

// configWriteDB 返回秘钥配置回源使用的主库写连接。
func (l *SecretKeyLogic) configWriteDB() (*gorm.DB, error) {
	if l == nil || l.Svc == nil {
		return nil, errors.Errorf("主库数据库未初始化")
	}
	// 空连接直接转成业务错误，避免 GORM 在 nil DB 上触发空指针。
	db := l.Svc.WriteDB(svc.DatabaseMain)
	if db == nil {
		return nil, errors.Errorf("主库数据库未初始化")
	}
	return db, nil
}

// shouldUseConfigSecretKey 判断当前 AppID 是否应使用 config.yaml 中的 security.secret_key 配置。
// 只有请求 AppID 与顶层 app_id 完全一致时才命中配置文件，其它 AppID 继续走 secret_key 表和缓存。
func (l *SecretKeyLogic) shouldUseConfigSecretKey(appID string) bool {
	_, ok := l.currentConfigSecretKey(appID)
	return ok
}

// currentConfigSecretKey 读取当前运行期配置中的站点秘钥配置，并完成 AppID 匹配。
func (l *SecretKeyLogic) currentConfigSecretKey(appID string) (config.SecuritySecretKeyConfig, bool) {
	if l == nil || l.Svc == nil {
		return config.SecuritySecretKeyConfig{}, false
	}
	appID = strings.TrimSpace(appID)
	cfg := l.Svc.CurrentConfig()
	configAppID := strings.TrimSpace(cfg.AppID)
	if appID == "" || configAppID == "" || appID != configAppID {
		return config.SecuritySecretKeyConfig{}, false
	}
	return cfg.Security.SecretKey, true
}

// getConfigSecretKeyRoute 从配置文件读取当前 AppID 的版本选路和链路开关。
func (l *SecretKeyLogic) getConfigSecretKeyRoute(appID string) (*SecretKeyRouteConfig, error) {
	secretCfg, ok := l.currentConfigSecretKey(appID)
	if !ok {
		return nil, errors.Errorf("AppID未命中配置文件秘钥: %s", appID)
	}
	if configSecretKeyIsEmpty(secretCfg) {
		return nil, errors.Errorf("配置文件security.secret_key未配置")
	}
	stableVersion := configSecretKeyStableVersion(secretCfg)
	if stableVersion == "" {
		return nil, errors.Errorf("配置文件security.secret_key.key_version未配置")
	}
	return &SecretKeyRouteConfig{
		UUID:          appID,
		StableVersion: stableVersion,
		GrayVersion:   strings.TrimSpace(secretCfg.GrayVersion),
		GrayPercent:   secretCfg.GrayPercent,
		GraySalt:      strings.TrimSpace(secretCfg.GraySalt),
		Status:        1,
		SignStatus:    secretCfg.SignStatus,
		CryptoStatus:  secretCfg.CryptoStatus,
	}, nil
}

// getConfigAESKey 从配置文件读取当前命中版本的 AES KEY 与 IV。
func (l *SecretKeyLogic) getConfigAESKey(appID string, versionHint string, grayKey string) (*AESKey, string, error) {
	version, err := l.ResolveSecretKeyVersion(appID, versionHint, grayKey)
	if err != nil {
		return nil, "", errors.Tag(err)
	}
	versionCfg, err := l.configSecretKeyVersion(appID, version)
	if err != nil {
		return nil, "", errors.Tag(err)
	}
	key, err := resolveConfigSecretText(versionCfg.AESKey, versionCfg.AESKeyRef, "AES KEY")
	if err != nil {
		return nil, "", errors.Wrap(err, "AES KEY解析失败")
	}
	iv, err := resolveConfigSecretText(versionCfg.AESIV, versionCfg.AESIVRef, "AES IV")
	if err != nil {
		return nil, "", errors.Wrap(err, "AES IV解析失败")
	}
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, "", errors.Errorf("AES KEY长度必须是16、24或32位")
	}
	if len(iv) != 16 {
		return nil, "", errors.Errorf("AES IV长度必须是16位")
	}
	return &AESKey{Key: key, IV: iv}, version, nil
}

// getConfigRSAKey 从配置文件读取当前命中版本的 RSA PEM 内容。
func (l *SecretKeyLogic) getConfigRSAKey(appID string, versionHint string, grayKey string, keyType string) (string, string, error) {
	version, err := l.ResolveSecretKeyVersion(appID, versionHint, grayKey)
	if err != nil {
		return "", "", errors.Tag(err)
	}
	versionCfg, err := l.configSecretKeyVersion(appID, version)
	if err != nil {
		return "", "", errors.Tag(err)
	}
	switch keyType {
	case RSAUserPublicKey:
		text, err := resolveConfigPEMText(versionCfg.RSAPublicKeyUser, versionCfg.RSAPublicKeyUserRef, "用户 RSA公钥")
		return text, version, errors.Tag(err)
	case RSAServerPublicKey:
		if strings.TrimSpace(versionCfg.RSAPublicKeyServer) == "" && strings.TrimSpace(versionCfg.RSAPublicKeyServerRef) == "" {
			text, err := deriveConfigServerPublicPEM(versionCfg)
			return text, version, errors.Tag(err)
		}
		text, err := resolveConfigPEMText(versionCfg.RSAPublicKeyServer, versionCfg.RSAPublicKeyServerRef, "服务端 RSA公钥")
		return text, version, errors.Tag(err)
	case RSAServerPrivateKey:
		text, err := resolveConfigPEMText(versionCfg.RSAPrivateKeyServer, versionCfg.RSAPrivateKeyServerRef, "服务端 RSA私钥")
		return text, version, errors.Tag(err)
	default:
		return "", "", errors.Errorf("RSA秘钥类型不合法: %s", keyType)
	}
}

// configSecretKeyVersion 按版本号读取配置文件中的秘钥版本材料。
func (l *SecretKeyLogic) configSecretKeyVersion(appID string, keyVersion string) (config.SecuritySecretKeyVersionConfig, error) {
	secretCfg, ok := l.currentConfigSecretKey(appID)
	if !ok {
		return config.SecuritySecretKeyVersionConfig{}, errors.Errorf("AppID未命中配置文件秘钥: %s", appID)
	}
	keyVersion = strings.TrimSpace(keyVersion)
	if keyVersion == "" {
		return config.SecuritySecretKeyVersionConfig{}, errors.Errorf("秘钥版本不能为空")
	}
	singleVersion := buildSingleConfigSecretKeyVersion(secretCfg)
	for _, item := range secretCfg.Versions {
		if strings.TrimSpace(item.KeyVersion) == keyVersion {
			item.KeyVersion = strings.TrimSpace(item.KeyVersion)
			return item, nil
		}
	}
	if strings.TrimSpace(singleVersion.KeyVersion) == keyVersion {
		return singleVersion, nil
	}
	return config.SecuritySecretKeyVersionConfig{}, errors.Errorf("配置文件security.secret_key.versions未找到版本: %s", keyVersion)
}

// validateConfigSecretKey 校验配置文件秘钥是否满足当前启用的签名与加密链路。
func (l *SecretKeyLogic) validateConfigSecretKey(appID string) error {
	route, err := l.getConfigSecretKeyRoute(appID)
	if err != nil {
		return errors.Tag(err)
	}
	if route.CryptoEnabled() {
		if _, _, err := l.getConfigAESKey(appID, "", ""); err != nil {
			return errors.Tag(err)
		}
	}
	if route.SignEnabled() {
		if _, _, err := l.getConfigRSAKey(appID, "", "", RSAUserPublicKey); err != nil {
			return errors.Tag(err)
		}
		if _, _, err := l.getConfigRSAKey(appID, "", "", RSAServerPrivateKey); err != nil {
			return errors.Tag(err)
		}
	}
	return nil
}

// deriveServerPublicPEMFromPrivateRef 从服务端私钥文件派生对应公钥 PEM。
func deriveServerPublicPEMFromPrivateRef(privateRef string) (string, error) {
	privatePEM, err := resolvePEMText(privateRef)
	if err != nil {
		return "", errors.Wrap(err, "服务端 RSA私钥读取失败")
	}
	return deriveServerPublicPEM(privatePEM)
}

// deriveConfigServerPublicPEM 从配置文件中的服务端私钥派生公钥 PEM。
func deriveConfigServerPublicPEM(versionCfg config.SecuritySecretKeyVersionConfig) (string, error) {
	privatePEM, err := resolveConfigPEMText(versionCfg.RSAPrivateKeyServer, versionCfg.RSAPrivateKeyServerRef, "服务端 RSA私钥")
	if err != nil {
		return "", errors.Tag(err)
	}
	return deriveServerPublicPEM(privatePEM)
}

// deriveServerPublicPEM 从服务端私钥 PEM 派生公钥 PEM。
func deriveServerPublicPEM(privatePEM string) (string, error) {
	privateKey, err := security.ParseRSAPrivateKey(privatePEM)
	if err != nil {
		return "", errors.Wrap(err, "服务端 RSA私钥格式不合法")
	}
	return deriveRSAPublicPEMFromPrivateKey(privateKey)
}

// configSecretKeyIsEmpty 判断配置文件秘钥段是否完全未填写。
func configSecretKeyIsEmpty(secretCfg config.SecuritySecretKeyConfig) bool {
	return strings.TrimSpace(secretCfg.KeyVersion) == "" &&
		strings.TrimSpace(secretCfg.AESKey) == "" &&
		strings.TrimSpace(secretCfg.AESKeyRef) == "" &&
		strings.TrimSpace(secretCfg.AESIV) == "" &&
		strings.TrimSpace(secretCfg.AESIVRef) == "" &&
		strings.TrimSpace(secretCfg.RSAPublicKeyUser) == "" &&
		strings.TrimSpace(secretCfg.RSAPublicKeyUserRef) == "" &&
		strings.TrimSpace(secretCfg.RSAPublicKeyServer) == "" &&
		strings.TrimSpace(secretCfg.RSAPublicKeyServerRef) == "" &&
		strings.TrimSpace(secretCfg.RSAPrivateKeyServer) == "" &&
		strings.TrimSpace(secretCfg.RSAPrivateKeyServerRef) == "" &&
		secretCfg.SignStatus == 0 &&
		secretCfg.CryptoStatus == 0 &&
		strings.TrimSpace(secretCfg.StableVersion) == "" &&
		strings.TrimSpace(secretCfg.GrayVersion) == "" &&
		strings.TrimSpace(secretCfg.GraySalt) == "" &&
		len(secretCfg.Versions) == 0
}

// configSecretKeyStableVersion 返回配置文件秘钥的稳定版本；单版本配置优先使用 key_version。
func configSecretKeyStableVersion(secretCfg config.SecuritySecretKeyConfig) string {
	stableVersion := strings.TrimSpace(secretCfg.StableVersion)
	if stableVersion != "" {
		return stableVersion
	}
	return strings.TrimSpace(secretCfg.KeyVersion)
}

// buildSingleConfigSecretKeyVersion 把顶层单版本配置转换成统一版本结构，便于复用后续解析逻辑。
func buildSingleConfigSecretKeyVersion(secretCfg config.SecuritySecretKeyConfig) config.SecuritySecretKeyVersionConfig {
	return config.SecuritySecretKeyVersionConfig{
		KeyVersion:             configSecretKeyStableVersion(secretCfg),
		AESKey:                 secretCfg.AESKey,
		AESKeyRef:              secretCfg.AESKeyRef,
		AESIV:                  secretCfg.AESIV,
		AESIVRef:               secretCfg.AESIVRef,
		RSAPublicKeyUser:       secretCfg.RSAPublicKeyUser,
		RSAPublicKeyUserRef:    secretCfg.RSAPublicKeyUserRef,
		RSAPublicKeyServer:     secretCfg.RSAPublicKeyServer,
		RSAPublicKeyServerRef:  secretCfg.RSAPublicKeyServerRef,
		RSAPrivateKeyServer:    secretCfg.RSAPrivateKeyServer,
		RSAPrivateKeyServerRef: secretCfg.RSAPrivateKeyServerRef,
	}
}

// resolveConfigSecretText 从配置文件明文或文件引用读取普通秘钥文本。
func resolveConfigSecretText(value string, ref string, label string) (string, error) {
	value = strings.TrimSpace(value)
	if value != "" {
		return value, nil
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.Errorf("配置文件%s未配置", label)
	}
	return readSecretFileText(ref)
}

// resolveConfigPEMText 从配置文件明文或文件引用读取 RSA PEM 文本。
func resolveConfigPEMText(value string, ref string, label string) (string, error) {
	text, err := resolveConfigSecretText(value, ref, label)
	if err != nil {
		return "", errors.Tag(err)
	}
	if !strings.Contains(text, "-----BEGIN") {
		return "", errors.Errorf("配置文件%s不是有效PEM", label)
	}
	return text, nil
}

// secretKeyVersionCacheKey 根据秘钥类型返回对应版本材料缓存 key。
func secretKeyVersionCacheKey(appID string, keyVersion string, typ string) string {
	if typ == SecretKeyTypeRSA {
		return fmt.Sprintf(keys.SecretKeyRSAVersion, appID, keyVersion)
	}
	return fmt.Sprintf(keys.SecretKeyAESVersion, appID, keyVersion)
}

// secretKeyVersionCacheKeys 返回指定 AppID 与版本号对应的 AES/RSA 版本材料缓存 key。
func secretKeyVersionCacheKeys(appID string, keyVersion string) (string, string) {
	return fmt.Sprintf(keys.SecretKeyAESVersion, appID, keyVersion),
		fmt.Sprintf(keys.SecretKeyRSAVersion, appID, keyVersion)
}

// secretKeyRoutePhysicalCacheKey 返回秘钥路由缓存的 table-cache 真实 Redis key。
// 秘钥缓存按 AppID 隔离，写入和删除必须使用真实 key，避免 table-cache 新版本拒绝副作用操作。
func (l *SecretKeyLogic) secretKeyRoutePhysicalCacheKey(appID string) string {
	return cachelogic.TableCachePhysicalKey(l.BaseLogic, fmt.Sprintf(keys.SecretKeyRoute, appID))
}

// secretKeyVersionPhysicalCacheKey 根据秘钥类型返回版本材料缓存的 table-cache 真实 Redis key。
func (l *SecretKeyLogic) secretKeyVersionPhysicalCacheKey(appID string, keyVersion string, typ string) string {
	return cachelogic.TableCachePhysicalKey(l.BaseLogic, secretKeyVersionCacheKey(appID, keyVersion, typ))
}

// secretKeyVersionPhysicalCacheKeys 返回指定 AppID 与版本号对应的 AES/RSA 版本材料真实 Redis key。
func (l *SecretKeyLogic) secretKeyVersionPhysicalCacheKeys(appID string, keyVersion string) (string, string) {
	aesCacheKey, rsaCacheKey := secretKeyVersionCacheKeys(appID, keyVersion)
	return cachelogic.TableCachePhysicalKey(l.BaseLogic, aesCacheKey), cachelogic.TableCachePhysicalKey(l.BaseLogic, rsaCacheKey)
}

// secretKeyVersionPhysicalIndexKey 返回版本材料缓存索引的真实 Redis key。
// 索引记录的是版本材料真实 key，刷新/删除时可按索引精确删除，避免在生产 Redis 中做前缀扫描。
func (l *SecretKeyLogic) secretKeyVersionPhysicalIndexKey(appID string) string {
	return cachelogic.TableCachePhysicalKey(l.BaseLogic, fmt.Sprintf(keys.SecretKeyVersionIndex, appID))
}

// secretKeyVersionIndexTTL 返回版本材料缓存索引 TTL，索引略长于真实缓存，便于覆盖删除窗口。
func secretKeyVersionIndexTTL() time.Duration {
	return corelogic.JitterTTL(2 * time.Hour)
}

// resolveSecretKeyVersion 根据显式版本和灰度规则解析当前命中的版本。
func (l *SecretKeyLogic) resolveSecretKeyVersion(appID string, versionHint string, grayKey string) (*SecretKeyRouteConfig, string, error) {
	route, err := l.getSecretKeyRoute(appID)
	if err != nil {
		return nil, "", errors.Tag(err)
	}
	if route.Status != 1 {
		return nil, "", errors.Errorf("AppID已禁用")
	}
	versionHint, err = normalizeSecretKeyVersionHint(versionHint)
	if err != nil {
		return nil, "", errors.Tag(err)
	}
	if versionHint != "" {
		return route, versionHint, nil
	}
	stableVersion := strings.TrimSpace(route.StableVersion)
	if stableVersion == "" {
		return nil, "", errors.Errorf("稳定版本未配置")
	}
	grayVersion := strings.TrimSpace(route.GrayVersion)
	if grayVersion == "" || route.GrayPercent <= 0 {
		return route, stableVersion, nil
	}
	if route.GrayPercent >= 100 {
		return route, grayVersion, nil
	}
	if shouldRouteSecretKeyGray(appID, route.GraySalt, grayKey, route.GrayPercent) {
		return route, grayVersion, nil
	}
	return route, stableVersion, nil
}

// shouldRouteSecretKeyGray 根据稳定哈希结果判断当前请求是否命中灰度版本。
func shouldRouteSecretKeyGray(appID string, graySalt string, grayKey string, grayPercent int) bool {
	grayKey = strings.TrimSpace(grayKey)
	if grayPercent <= 0 {
		return false
	}
	if grayPercent >= 100 {
		return true
	}
	if grayKey == "" {
		grayKey = appID
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(appID))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(strings.TrimSpace(graySalt)))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(grayKey))
	bucket := int(h.Sum32()%100) + 1
	return bucket <= grayPercent
}

// deleteSecretKeyVersionCaches 按索引和确定版本号精确删除指定 AppID 下的版本材料缓存。
func (l *SecretKeyLogic) deleteSecretKeyVersionCaches(appID string, versionHints ...string) error {
	appID = strings.TrimSpace(appID)
	if appID == "" || l.Redis() == nil {
		return nil
	}
	indexKeys := cachelogic.TableCachePhysicalKeys(l.BaseLogic, fmt.Sprintf(keys.SecretKeyVersionIndex, appID))
	members := make([]string, 0)
	for _, indexKey := range indexKeys {
		currentMembers, err := l.Redis().SMembers(l.Ctx, indexKey).Result()
		if err != nil {
			return errors.Tag(err)
		}
		members = append(members, currentMembers...)
	}
	deleteSet := make(map[string]struct{}, len(members)+len(versionHints)*2+3)
	for _, member := range members {
		member = strings.TrimSpace(member)
		if member == "" || !l.secretKeyVersionCacheBelongsToAppID(member, appID) {
			continue
		}
		deleteSet[member] = struct{}{}
	}
	for _, version := range append(versionHints, l.secretKeyRouteVersionHints(appID)...) {
		version = strings.TrimSpace(version)
		if version == "" {
			continue
		}
		aesCacheKey, rsaCacheKey := l.secretKeyVersionPhysicalCacheKeys(appID, version)
		deleteSet[aesCacheKey] = struct{}{}
		deleteSet[rsaCacheKey] = struct{}{}
	}
	deleteKeys := make([]string, 0, len(deleteSet)+1)
	for cacheKey := range deleteSet {
		deleteKeys = append(deleteKeys, cacheKey)
	}
	deleteKeys = append(deleteKeys, indexKeys...)
	return errors.Tag(l.RdsDelKeys(cachelogic.TableCachePhysicalKeys(l.BaseLogic, deleteKeys...)...))
}

// secretKeyRouteVersionHints 从路由缓存中提取稳定版和灰度版，作为索引缺失时的精确删除补充。
func (l *SecretKeyLogic) secretKeyRouteVersionHints(appID string) []string {
	if l.Redis() == nil {
		return nil
	}
	for _, routeKey := range cachelogic.TableCachePhysicalKeys(l.BaseLogic, fmt.Sprintf(keys.SecretKeyRoute, appID)) {
		data, err := l.Redis().HGetAll(l.Ctx, routeKey).Result()
		if err != nil || len(data) == 0 {
			continue
		}
		return []string{
			strings.TrimSpace(data[secretKeyCacheFieldStableVersion]),
			strings.TrimSpace(data[secretKeyCacheFieldGrayVersion]),
		}
	}
	return nil
}

// secretKeyVersionCacheBelongsToAppID 校验索引成员属于当前 AppID，防止脏索引误删其它应用缓存。
func (l *SecretKeyLogic) secretKeyVersionCacheBelongsToAppID(cacheKey string, appID string) bool {
	cacheKey = strings.TrimSpace(cacheKey)
	appID = strings.TrimSpace(appID)
	if cacheKey == "" || appID == "" {
		return false
	}
	cacheKey = keys.TrimTableCachePrefix(cacheKey)
	return strings.HasPrefix(cacheKey, keys.KeyTemplatePrefix(fmt.Sprintf(keys.SecretKeyAESVersion, appID, ""))) ||
		strings.HasPrefix(cacheKey, keys.KeyTemplatePrefix(fmt.Sprintf(keys.SecretKeyRSAVersion, appID, "")))
}

// secretKeyVersionShouldCacheEmptyMarker 判断缺失版本是否允许写空值占位。
func secretKeyVersionShouldCacheEmptyMarker(keyVersion string, routeVersions []string) bool {
	keyVersion = strings.TrimSpace(keyVersion)
	if keyVersion == "" {
		return false
	}
	for _, routeVersion := range routeVersions {
		if keyVersion == strings.TrimSpace(routeVersion) {
			return true
		}
	}
	return false
}

// normalizeSecretKeyVersionHint 规范化请求携带的秘钥版本号，避免异常长或通配符版本进入 Redis key。
func normalizeSecretKeyVersionHint(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if len(value) > 64 {
		return "", errors.Errorf("秘钥版本号长度不能超过64位")
	}
	if strings.ContainsAny(value, "*? \t\r\n") {
		return "", errors.Errorf("秘钥版本号不能包含空白字符或通配符")
	}
	return value, nil
}

// normalizeSecretRef 统一规范化秘钥文件引用，只允许绝对路径。
func normalizeSecretRef(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.Errorf("秘钥文件路径不能为空")
	}
	if strings.Contains(raw, "-----BEGIN") {
		return "", errors.Errorf("当前项目仅允许填写秘钥文件绝对路径，不再支持直接录入PEM内容")
	}
	if !filepath.IsAbs(raw) {
		return "", errors.Errorf("秘钥文件路径必须使用绝对路径")
	}
	return raw, nil
}

// readSecretFileText 读取秘钥文件内容，并统一去除首尾空白。
func readSecretFileText(filePath string) (string, error) {
	filePath, err := normalizeSecretRef(filePath)
	if err != nil {
		return "", errors.Tag(err)
	}
	body, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errors.Errorf("秘钥文件不存在: %s", filePath)
		}
		return "", errors.Wrapf(err, "读取秘钥文件失败: %s", filePath)
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "", errors.Errorf("秘钥文件为空: %s", filePath)
	}
	return text, nil
}

// normalizeSecretText 从 AES KEY/IV 文件路径读取实际内容。
func normalizeSecretText(raw string) (string, error) {
	return readSecretFileText(raw)
}

// resolvePEMText 从 RSA PEM 文件路径读取实际内容。
func resolvePEMText(raw string) (string, error) {
	text, err := readSecretFileText(raw)
	if err != nil {
		return "", errors.Tag(err)
	}
	if !strings.Contains(text, "-----BEGIN") {
		return "", errors.Errorf("RSA秘钥文件内容不是有效PEM: %s", raw)
	}
	return text, nil
}
