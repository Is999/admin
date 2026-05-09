package keys_test

import (
	"testing"

	"admin/common/runtimecfg"
	"admin/internal/config"
)

// useAppID 临时切换测试进程的 app_id，并在用例结束后恢复。
func useAppID(t *testing.T, appID string) {
	t.Helper()
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: appID})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})
}
