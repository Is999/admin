package keys

import "strings"

const tableCacheSegment = "table"

// TableCachePrefix 返回当前应用 table-cache 真实 Redis Key 前缀。
// 最终 key 形如 `app:{app_id}:table:{key}`，业务 key 本身不带 table 前缀。
func TableCachePrefix() string {
	return WithPrefix(tableCacheLogicalPrefix())
}

// IsTableCacheKey 判断 key 是否属于当前应用的 table-cache 真实 Redis key。
func IsTableCacheKey(key string) bool {
	key = strings.TrimSpace(key)
	prefix := Prefix()
	if prefix == "" || !strings.HasPrefix(key, prefix) {
		return false
	}
	logicalKey := TrimPrefix(key)
	logicalPrefix := tableCacheLogicalPrefix()
	return strings.HasPrefix(logicalKey, logicalPrefix) && len(logicalKey) > len(logicalPrefix)
}

// TrimTableCachePrefix 去掉当前应用的 table-cache 项目前缀，返回业务逻辑 key。
// 只有当前 app_id 的 `app:{app_id}:table:` 会被截断，跨站点或直接 Redis key 会保持原样。
func TrimTableCachePrefix(key string) string {
	key = strings.TrimSpace(key)
	if !IsTableCacheKey(key) {
		return key
	}
	return strings.TrimPrefix(TrimPrefix(key), tableCacheLogicalPrefix())
}

func tableCacheLogicalPrefix() string {
	return tableCacheSegment + ":"
}
