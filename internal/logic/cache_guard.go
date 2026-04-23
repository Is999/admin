package logic

import (
	"fmt"
	"time"

	keys "admin_cron/common/rediskeys"

	"github.com/Is999/go-utils/errors"
)

var (
	// errCacheEmptyMarker 表示命中了空值缓存占位，用于避免缓存穿透时持续回源数据库。
	errCacheEmptyMarker = errors.New("cache empty marker")
)

const (
	// cacheRebuildLockTTL 表示缓存重建锁默认持有时间。
	cacheRebuildLockTTL = 10 * time.Second
	// cacheWaitStep 表示未抢到重建锁时的单次等待时间。
	cacheWaitStep = 80 * time.Millisecond
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

// emptyCacheTTL 返回空值缓存的过期时间，确保不存在的数据也能短时间挡住回源请求。
func emptyCacheTTL() time.Duration {
	return jitterTTL(2 * time.Minute)
}

// cacheIsEmptyMarker 判断当前缓存字段是否为统一空值占位符。
func cacheIsEmptyMarker(value string) bool {
	return value == keys.EmptyValueMarker
}

// cacheLockKey 返回当前 app_id 作用域下的缓存重建锁 Redis 键。
func (l *BaseLogic) cacheLockKey(cacheKey string) string {
	cacheKey = keys.TrimAppScopedPrefix(keys.TrimTableCachePrefix(cacheKey))
	return l.AppRedisKey(fmt.Sprintf(keys.CacheRebuildLock, cacheKey))
}

// tryRebuildCacheWithLock 使用轻量级 Redis 锁保护缓存重建，避免热点 key 并发击穿数据库。
func (l *BaseLogic) tryRebuildCacheWithLock(cacheKey string, rebuild func() error) error {
	if l == nil || l.Redis() == nil || cacheKey == "" {
		return rebuild()
	}
	lockKey := l.cacheLockKey(cacheKey)
	locked, err := l.Redis().SetNX(l.Context(), lockKey, "1", cacheRebuildLockTTL).Result()
	if err != nil {
		return errors.Tag(err)
	}
	if !locked {
		return nil
	}
	defer func() {
		_ = l.Redis().Del(l.Context(), lockKey).Err()
	}()
	return rebuild()
}
