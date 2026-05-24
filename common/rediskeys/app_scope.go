package keys

import (
	"admin/common/runtimecfg"
	"strings"
)

// HasPrefix 判断值是否已经带有完整 app_id 命名空间。
func HasPrefix(key string) bool {
	_, ok := Owner(key)
	return ok
}

// Owner 解析完整 app_id 命名空间中的 app_id。
func Owner(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, ScopeRoot) {
		return "", false
	}
	rest := strings.TrimPrefix(key, ScopeRoot)
	index := strings.Index(rest, ":")
	if index <= 0 || index >= len(rest)-1 {
		return "", false
	}
	return rest[:index], true
}

// IsForeignKey 判断完整 Redis key 是否属于其它 app_id。
func IsForeignKey(key string) bool {
	ownerAppID, ok := Owner(key)
	appID := runtimecfg.AppID()
	return ok && (appID == "" || ownerAppID != appID)
}

// Prefix 返回当前应用 Redis key 命名空间前缀。
func Prefix() string {
	appID := runtimecfg.AppID()
	if appID == "" {
		return ""
	}
	return ScopeRoot + appID + ":"
}

// WithPrefix 给内部业务 key 追加当前应用 app_id 命名空间。
// 外部传入的完整 Redis key 必须属于当前 app_id，避免跨站点 key 串用。
func WithPrefix(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return key
	}
	if ownerAppID, ok := Owner(key); ok {
		if ownerAppID != runtimecfg.AppID() {
			return ""
		}
		return key
	}
	prefix := Prefix()
	if prefix == "" {
		return ""
	}
	return prefix + key
}

// TrimPrefix 去掉任意 app_id 的直接 Redis 命名空间前缀。
func TrimPrefix(key string) string {
	key = strings.TrimSpace(key)
	appID, ok := Owner(key)
	if !ok {
		return key
	}
	return strings.TrimPrefix(key, ScopeRoot+appID+":")
}
