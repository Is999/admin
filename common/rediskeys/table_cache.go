package keys

import "strings"

const tableCacheSegment = "table"

// TableCachePrefix 根据站点 AppID 生成 table-cache 真实 Redis Key 前缀。
// 最终 key 形如 `app:{appID}:table:{key}`，业务 key 本身不带 table 前缀。
func TableCachePrefix(appID string) string {
	return AppScopedKey(appID, tableCacheLogicalPrefix())
}

// IsTableCacheKey 判断 key 是否属于指定 app_id 的 table-cache 真实 Redis key。
func IsTableCacheKey(appID string, key string) bool {
	key = strings.TrimSpace(key)
	appID = NormalizeAppID(appID)
	if appID == "" || !strings.HasPrefix(key, AppScopedPrefix(appID)) {
		return false
	}
	logicalKey := TrimAppScopedPrefix(key)
	logicalPrefix := tableCacheLogicalPrefix()
	return strings.HasPrefix(logicalKey, logicalPrefix) && len(logicalKey) > len(logicalPrefix)
}

// TrimTableCachePrefix 去掉指定 AppID 的 table-cache 项目前缀，返回业务逻辑 key。
// 只有当前 app_id 的 `app:{appID}:table:` 会被截断，跨站点或直接 Redis key 会保持原样。
func TrimTableCachePrefix(appID string, key string) string {
	key = strings.TrimSpace(key)
	if !IsTableCacheKey(appID, key) {
		return key
	}
	return strings.TrimPrefix(TrimAppScopedPrefix(key), tableCacheLogicalPrefix())
}

func tableCacheLogicalPrefix() string {
	return tableCacheSegment + ":"
}
