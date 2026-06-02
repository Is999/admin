package task

import (
	"os"
	"testing"

	"admin/common/runtimecfg"
	"admin/internal/config"
)

// TestMain 验证对应场景符合预期。
func TestMain(m *testing.M) {
	runtimecfg.Set(config.Config{AppID: "215"})
	os.Exit(m.Run())
}
