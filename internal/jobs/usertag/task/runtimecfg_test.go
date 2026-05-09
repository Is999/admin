package task

import (
	"os"
	"testing"

	"admin/common/runtimecfg"
	"admin/internal/config"
)

func TestMain(m *testing.M) {
	runtimecfg.Set(config.Config{AppID: "215"})
	os.Exit(m.Run())
}
