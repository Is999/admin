package keys

import "strings"

const (
	// TableCacheDataPrefix 表示 table-cache 组件托管业务缓存的 app_id Redis Key 前缀。
	// 最终 key 形如 `app:{appID}:role_tree`，便于同 app_id 下的后台和 API 共享配置类缓存。
	TableCacheDataPrefix = AppScopedDataPrefix
	// TableCacheDefaultAppID 表示配置缺失 app_id 时的兜底命名空间，避免生成空 appID 前缀。
	TableCacheDefaultAppID = AppScopedDefaultAppID
)

// TableCachePrefix 根据站点 AppID 生成 table-cache 真实 Redis Key 前缀。
// AppID 来自运行期配置 app_id；为空时回退 default，保证本地测试和缺省配置仍有稳定命名空间。
func TableCachePrefix(appID string) string {
	return AppScopedPrefix(appID)
}

// TrimTableCachePrefix 去掉任意 AppID 的 table-cache 项目前缀，返回业务逻辑 key。
// 无法识别完整 appID 分隔符时保留原值，避免误截断非托管 key。
func TrimTableCachePrefix(key string) string {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, TableCacheDataPrefix) {
		return key
	}
	rest := strings.TrimPrefix(key, TableCacheDataPrefix)
	index := strings.Index(rest, ":")
	if index < 0 {
		return key
	}
	return rest[index+1:]
}
