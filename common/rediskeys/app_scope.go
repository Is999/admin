package keys

import "strings"

const (
	// AppScopedDataPrefix 表示 Redis 缓存和锁的 app_id 命名空间前缀。
	AppScopedDataPrefix = "app:"
)

// NormalizeAppID 裁剪 Redis 命名空间使用的 app_id。
func NormalizeAppID(appID string) string {
	return strings.TrimSpace(appID)
}

// HasAppScopedPrefix 判断值是否已经带有完整 app_id 命名空间。
func HasAppScopedPrefix(key string) bool {
	_, ok := AppScopedAppID(key)
	return ok
}

// AppScopedAppID 解析完整 app_id 命名空间中的 app_id。
func AppScopedAppID(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, AppScopedDataPrefix) {
		return "", false
	}
	rest := strings.TrimPrefix(key, AppScopedDataPrefix)
	index := strings.Index(rest, ":")
	if index <= 0 || index >= len(rest)-1 {
		return "", false
	}
	return rest[:index], true
}

// IsForeignAppScopedKey 判断完整 Redis key 是否属于其它 app_id。
func IsForeignAppScopedKey(appID string, key string) bool {
	ownerAppID, ok := AppScopedAppID(key)
	appID = NormalizeAppID(appID)
	return ok && appID != "" && ownerAppID != appID
}

// AppScopedPrefix 返回指定 app_id 的直接 Redis 命名空间前缀。
// app_id 缺失属于启动期配置错误；运行期 helper 返回空前缀，避免 panic 打挂服务。
func AppScopedPrefix(appID string) string {
	appID = NormalizeAppID(appID)
	if appID == "" {
		return ""
	}
	return AppScopedDataPrefix + appID + ":"
}

// AppScopedKey 给内部业务 key 追加 app_id 命名空间。
// 外部传入的完整 Redis key 必须先校验归属，避免跨站点 key 被改写到当前 app_id。
func AppScopedKey(appID string, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return key
	}
	if HasAppScopedPrefix(key) {
		return key
	}
	prefix := AppScopedPrefix(appID)
	if prefix == "" {
		return ""
	}
	return prefix + key
}

// TrimAppScopedPrefix 去掉任意 app_id 的直接 Redis 命名空间前缀。
func TrimAppScopedPrefix(key string) string {
	key = strings.TrimSpace(key)
	appID, ok := AppScopedAppID(key)
	if !ok {
		return key
	}
	return strings.TrimPrefix(key, AppScopedPrefix(appID))
}
