package cache

import (
	"strings"

	"github.com/Is999/go-utils/errors"
)

var (
	// ErrRedisUnavailable 表示核心缓存链路无法使用 Redis。
	ErrRedisUnavailable = errors.New("Redis不可用")
	// ErrSecurityCacheSyncPending 表示安全缓存失效任务尚未补偿完成。
	ErrSecurityCacheSyncPending = errors.New("安全缓存同步中")
)

// WrapRedisUnavailable 保留底层错误并统一附加可识别的 Redis 依赖错误。
func WrapRedisUnavailable(err error, operation string) error {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		operation = "Redis操作失败"
	}
	if err == nil {
		return errors.Wrap(ErrRedisUnavailable, operation)
	}
	return errors.Join(ErrRedisUnavailable, errors.Wrap(err, operation))
}
