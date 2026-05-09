package security

import (
	"os"
	"testing"

	"admin/common/runtimecfg"
	"admin/internal/config"
)

func TestMain(m *testing.M) {
	runtimecfg.Set(config.Config{AppID: "site-a"})
	os.Exit(m.Run())
}
