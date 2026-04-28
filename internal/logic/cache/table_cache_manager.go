package cache

import (
	"strconv"
	"strings"
	"time"

	keys "admin/common/rediskeys"
	corelogic "admin/internal/logic"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	tablecache "github.com/Is999/table-cache"
	"gorm.io/gorm"
)

const (
	// tableCacheRebuildLockTTL 表示 table-cache 回源锁默认持有时长。
	tableCacheRebuildLockTTL = 10 * time.Second
	// tableCacheWaitStep 表示等待其他实例回源完成的单次等待时间。
	tableCacheWaitStep = 80 * time.Millisecond
)

// tableCacheManager 创建 admin 的表数据缓存管理器。
func tableCacheManager(base *corelogic.BaseLogic) (*tablecache.Manager, error) {
	if base == nil || base.Redis() == nil {
		return nil, errors.Errorf("Redis未初始化")
	}
	return tablecache.NewManager(
		tablecache.NewRedisStore(base.Redis()),
		tableCacheTargets(base),
		tablecache.WithKeyPrefix(tableCacheKeyPrefix(base)),
		tablecache.WithEmptyMarker(keys.EmptyValueMarker, corelogic.EmptyCacheTTL()),
		tablecache.WithLockTTL(tableCacheRebuildLockTTL),
		tablecache.WithWait(tableCacheWaitStep, 3),
	)
}

// TableCacheManager 创建 admin 的表数据缓存管理器。
func TableCacheManager(base *corelogic.BaseLogic) (*tablecache.Manager, error) {
	return tableCacheManager(base)
}

// tableCacheKeyPrefix 返回当前站点 table-cache 托管缓存使用的 Redis Key 前缀。
// 前缀来源于运行期 app_id，确保多站点共用 Redis 时权限、配置和秘钥缓存不会相互覆盖。
func tableCacheKeyPrefix(base *corelogic.BaseLogic) string {
	appID := tableCacheAppID(base)
	if appID == "" {
		return ""
	}
	return keys.TableCachePrefix(appID)
}

// tableCachePhysicalKey 把逻辑缓存 key 转换为 table-cache 当前要求的真实 Redis key。
// 读穿缓存、刷新、删除和直接 Redis 删除都统一使用带前缀的真实 key，确保 miss 后能回源写入当前命名空间。
func tableCachePhysicalKey(base *corelogic.BaseLogic, key string) string {
	key = strings.TrimSpace(key)
	prefix := tableCacheKeyPrefix(base)
	if key == "" || prefix == "" || strings.HasPrefix(key, prefix) || keys.HasAppScopedPrefix(key) {
		return key
	}
	return prefix + key
}

// TableCachePhysicalKey 把逻辑缓存 key 转换为 table-cache 真实 Redis key。
func TableCachePhysicalKey(base *corelogic.BaseLogic, key string) string {
	return tableCachePhysicalKey(base, key)
}

// tableCacheLogicalKey 去掉 table-cache 项目级前缀，供分类、脱敏和模板匹配使用。
func tableCacheLogicalKey(base *corelogic.BaseLogic, key string) string {
	key = strings.TrimSpace(key)
	return keys.TrimTableCachePrefix(tableCacheAppID(base), key)
}

// TableCacheLogicalKey 去掉 table-cache 项目级前缀。
func TableCacheLogicalKey(base *corelogic.BaseLogic, key string) string {
	return tableCacheLogicalKey(base, key)
}

// tableCacheAppID 返回当前站点 table-cache 托管缓存使用的 App ID。
func tableCacheAppID(base *corelogic.BaseLogic) string {
	if base == nil || base.Svc == nil {
		return ""
	}
	return base.AppID()
}

// tableCachePhysicalKeys 批量转换 table-cache 托管缓存 key，并过滤空值。
func tableCachePhysicalKeys(base *corelogic.BaseLogic, cacheKeys ...string) []string {
	result := make([]string, 0, len(cacheKeys))
	for _, key := range cacheKeys {
		key = tableCachePhysicalKey(base, key)
		if key == "" {
			continue
		}
		result = append(result, key)
	}
	return result
}

// TableCachePhysicalKeys 批量转换 table-cache 托管缓存 key。
func TableCachePhysicalKeys(base *corelogic.BaseLogic, cacheKeys ...string) []string {
	return tableCachePhysicalKeys(base, cacheKeys...)
}

// tableCacheReadDB 统一获取表缓存回源所需的读库连接，缺失时返回明确错误，避免直接触发 GORM 空指针。
func tableCacheReadDB(base *corelogic.BaseLogic, database svc.DbName, databaseLabel string) (*gorm.DB, error) {
	if base == nil || base.Svc == nil {
		return nil, errors.Errorf("服务上下文未初始化")
	}
	readDB := base.Svc.ReadDB(database)
	if readDB == nil {
		return nil, errors.Errorf("%s读库未初始化", strings.TrimSpace(databaseLabel))
	}
	return readDB, nil
}

// TableCacheReadDB 获取表缓存回源读库连接。
func TableCacheReadDB(base *corelogic.BaseLogic, database svc.DbName, databaseLabel string) (*gorm.DB, error) {
	return tableCacheReadDB(base, database, databaseLabel)
}

// tableCacheWriteDB 统一获取表缓存回源所需的主库连接，缺失时返回明确错误，避免直接触发 GORM 空指针。
func tableCacheWriteDB(base *corelogic.BaseLogic, database svc.DbName, databaseLabel string) (*gorm.DB, error) {
	if base == nil || base.Svc == nil {
		return nil, errors.Errorf("服务上下文未初始化")
	}
	writeDB := base.Svc.WriteDB(database)
	if writeDB == nil {
		return nil, errors.Errorf("%s主库未初始化", strings.TrimSpace(databaseLabel))
	}
	return writeDB, nil
}

// TableCacheWriteDB 获取表缓存回源主库连接。
func TableCacheWriteDB(base *corelogic.BaseLogic, database svc.DbName, databaseLabel string) (*gorm.DB, error) {
	return tableCacheWriteDB(base, database, databaseLabel)
}

// tableCacheItems 返回缓存管理页使用的通用表缓存目标列表。
func tableCacheItems(base *corelogic.BaseLogic) []types.CacheItem {
	targets := tableCacheTargets(base)
	items := make([]types.CacheItem, 0, len(targets))
	for _, target := range targets {
		keyTitle := target.KeyTitle
		if keyTitle == "" {
			keyTitle = target.Key
		}
		keyTitle = tableCachePhysicalKey(base, keyTitle)
		logicalKeyTitle := tableCacheLogicalKey(base, keyTitle)
		items = append(items, types.CacheItem{
			Index:        target.Index,
			Key:          keyTitle,
			KeyTitle:     keyTitle,
			Type:         string(target.Type),
			Remark:       target.Remark,
			Category:     tableCacheCategory(target.Index, logicalKeyTitle),
			IsTemplate:   isTemplateCachePattern(keyTitle),
			ExampleKey:   tableCacheExampleKey(keyTitle),
			AutoRebuild:  true,
			RefreshScope: tableCacheRefreshScope(target),
		})
	}
	return items
}

// TableCacheItems 返回缓存管理页使用的通用表缓存目标列表。
func TableCacheItems(base *corelogic.BaseLogic) []types.CacheItem {
	return tableCacheItems(base)
}

// tableCacheCategory 根据缓存目标索引和 key 模板归类缓存用途，供管理页分组展示。
func tableCacheCategory(index string, key string) string {
	switch {
	case strings.HasPrefix(key, keys.KeyTemplatePrefix(keys.AdminInfoLogicalPattern())):
		return "session"
	case strings.HasPrefix(key, "secret_key_"):
		return "secret"
	case strings.HasPrefix(key, "config_uuid:"):
		return "config"
	case strings.HasPrefix(key, "admin_"), strings.HasPrefix(key, "route_permission_"), strings.Contains(index, "permission"), strings.Contains(index, "role"):
		return "auth"
	default:
		return "system"
	}
}

// tableCacheExampleKey 为模板型 key 生成管理页可直接使用的示例 key。
func tableCacheExampleKey(key string) string {
	replacer := strings.NewReplacer(
		"%d", "1",
		"%s", "demo",
		"%v", "value",
		"{adminID}", "1",
		"{roleID}", "1",
		"{uuid}", "demo",
		"{keyVersion}", "v1",
		"{routeAlias}", "admin.list",
	)
	return replacer.Replace(key)
}

// tableCacheRefreshScope 返回缓存目标刷新粒度描述。
func tableCacheRefreshScope(target tablecache.Target) string {
	switch {
	case target.RefreshAll:
		return "all"
	case isTemplateCachePattern(target.KeyTitle):
		return "single"
	case strings.HasSuffix(target.Key, ":"):
		return "prefix"
	default:
		return "single"
	}
}

// isTemplateCachePattern 判断缓存管理页中的 key 是否属于模板型缓存键。
// 这里同时支持 `{id}` 与 `%s/%d/%v` 两类占位写法，保证前后端识别规则一致。
func isTemplateCachePattern(key string) bool {
	return strings.Contains(key, "{") || strings.Contains(key, "%")
}

// IsTemplateCachePattern 判断缓存键是否属于模板型缓存键。
func IsTemplateCachePattern(key string) bool {
	return isTemplateCachePattern(key)
}

// cacheTemplatePrefix 返回模板型缓存键的固定前缀部分。
// 管理页在匹配真实 Redis Key 与模板缓存项时，会基于该前缀做快速归类。
func cacheTemplatePrefix(key string) string {
	return keys.KeyTemplatePrefix(key)
}

// CacheTemplatePrefix 返回模板型缓存键的固定前缀部分。
func CacheTemplatePrefix(key string) string {
	return cacheTemplatePrefix(key)
}

// tableCacheFirstStringPart 读取前缀型缓存 key 的第一个参数。
func tableCacheFirstStringPart(params tablecache.LoadParams, title string) (string, error) {
	return tableCacheStringPart(params, 0, title)
}

// tableCacheStringPart 读取前缀型缓存 key 的指定字符串参数。
func tableCacheStringPart(params tablecache.LoadParams, index int, title string) (string, error) {
	if index < 0 || len(params.KeyParts) <= index || strings.TrimSpace(params.KeyParts[index]) == "" {
		return "", errors.Errorf("%s不能为空", title)
	}
	return strings.TrimSpace(params.KeyParts[index]), nil
}

// tableCacheFirstIntPart 读取前缀型缓存 key 的第一个整数参数。
func tableCacheFirstIntPart(params tablecache.LoadParams, title string) (int, error) {
	value, err := tableCacheFirstStringPart(params, title)
	if err != nil {
		return 0, errors.Tag(err)
	}
	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 {
		return 0, errors.Errorf("%s不合法: %s", title, value)
	}
	return id, nil
}
