package logic

import (
	"time"

	keys "admin/common/rediskeys"

	"github.com/Is999/go-utils/errors"
)

var (
	// errCacheEmptyMarker 表示命中了空值缓存占位，用于避免缓存穿透时持续回源数据库。
	errCacheEmptyMarker = errors.New("cache empty marker")
)

var (
	// ErrCacheEmptyMarker 表示命中了空值缓存占位。
	ErrCacheEmptyMarker = errCacheEmptyMarker
)

// jitterTTL 为基础过期时间添加抖动，降低同类缓存集中失效导致的雪崩风险。
func jitterTTL(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	jitterRange := base / 10
	if jitterRange <= 0 {
		jitterRange = time.Second
	}
	return base + time.Duration(time.Now().UnixNano()%int64(jitterRange))
}

// JitterTTL 为基础过期时间添加抖动。
func JitterTTL(base time.Duration) time.Duration {
	return jitterTTL(base)
}

// emptyCacheTTL 返回空值缓存的过期时间，确保不存在的数据也能短时间挡住回源请求。
func emptyCacheTTL() time.Duration {
	return jitterTTL(2 * time.Minute)
}

// EmptyCacheTTL 返回空值缓存的过期时间。
func EmptyCacheTTL() time.Duration {
	return emptyCacheTTL()
}

// cacheIsEmptyMarker 判断当前缓存字段是否为统一空值占位符。
func cacheIsEmptyMarker(value string) bool {
	return value == keys.EmptyValueMarker
}

// CacheIsEmptyMarker 判断当前缓存字段是否为统一空值占位符。
func CacheIsEmptyMarker(value string) bool {
	return cacheIsEmptyMarker(value)
}
