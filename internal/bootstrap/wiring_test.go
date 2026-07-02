package bootstrap

import (
	"context"
	"path/filepath"
	"testing"

	"admin/internal/bootstrap/runmode"
)

// TestWireWithConfigModeReturnsConfigError 确保装配入口遇到配置错误时返回错误而不是 panic。
func TestWireWithConfigModeReturnsConfigError(t *testing.T) {
	missingFile := filepath.Join(t.TempDir(), "missing.yaml")
	if _, err := WireWithConfigMode(context.Background(), missingFile, nil); err == nil {
		t.Fatal("期望缺失配置文件返回错误")
	}
}

// TestRunModePublicConstants 确保 bootstrap 对外启动模式常量保持稳定。
func TestRunModePublicConstants(t *testing.T) {
	if ModeAPI != runmode.API || ModeWorker != runmode.Worker || ModeScheduler != runmode.Scheduler || ModeAll != runmode.All {
		t.Fatalf("bootstrap 启动模式常量与 runmode 子包不一致")
	}
}
