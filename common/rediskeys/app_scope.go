package keys

import "strings"

const (
	// AppScopedDefaultAppID 表示配置缺失 app_id 时的兜底 Redis 命名空间。
	AppScopedDefaultAppID = "default"
	// AppScopedDataPrefix 表示 Redis 缓存和锁的 app_id 命名空间前缀。
	AppScopedDataPrefix = "app:"
)

// NormalizeAppID 归一化 Redis 命名空间使用的 app_id。
func NormalizeAppID(appID string) string {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return AppScopedDefaultAppID
	}
	return appID
}

// AppScopedPrefix 返回指定 app_id 的直接 Redis 命名空间前缀。
func AppScopedPrefix(appID string) string {
	return AppScopedDataPrefix + NormalizeAppID(appID) + ":"
}

// AppScopedKey 给直接 Redis 缓存和锁追加 app_id 命名空间。
func AppScopedKey(appID string, key string) string {
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, AppScopedDataPrefix) || strings.HasPrefix(key, TableCacheDataPrefix) {
		return key
	}
	return AppScopedPrefix(appID) + key
}

// TrimAppScopedPrefix 去掉任意 app_id 的直接 Redis 命名空间前缀。
func TrimAppScopedPrefix(key string) string {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, AppScopedDataPrefix) {
		return key
	}
	rest := strings.TrimPrefix(key, AppScopedDataPrefix)
	index := strings.Index(rest, ":")
	if index < 0 {
		return key
	}
	return rest[index+1:]
}
