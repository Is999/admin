package cache

import (
	"testing"

	"admin/common/runtimecfg"
	"admin/internal/config"
)

// useRuntimeAppID 模拟测试进程完成启动配置发布。
func useRuntimeAppID(t *testing.T, appID string) {
	t.Helper()
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: appID})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})
}
