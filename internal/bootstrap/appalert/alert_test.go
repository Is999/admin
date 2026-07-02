package appalert

import (
	"errors"
	"testing"

	"admin/internal/svc"
)

// TestLifecycleFailureNormalizesPhase 验证生命周期告警默认阶段和唯一键稳定。
func TestLifecycleFailureNormalizesPhase(t *testing.T) {
	alert := LifecycleFailure(" ", " boot ", errors.New("start failed"))
	if alert.Kind != svc.TaskRuntimeAlertKindAppLifecycleFailed {
		t.Fatalf("Kind = %s, want %s", alert.Kind, svc.TaskRuntimeAlertKindAppLifecycleFailed)
	}
	if alert.Operation != "unknown" {
		t.Fatalf("Operation = %q, want unknown", alert.Operation)
	}
	if alert.TaskName != "boot" {
		t.Fatalf("TaskName = %q, want boot", alert.TaskName)
	}
	if alert.UniqueKey != "unknown:boot" {
		t.Fatalf("UniqueKey = %q, want unknown:boot", alert.UniqueKey)
	}
}

// TestConfigReloadFailureNormalizesSourceAndCategory 验证配置热加载告警来源和分类兜底。
func TestConfigReloadFailureNormalizesSourceAndCategory(t *testing.T) {
	alert := ConfigReloadFailure("reload failed", errors.New("invalid yaml"), " ", "", "")
	if alert.Kind != svc.TaskRuntimeAlertKindConfigReloadFailed {
		t.Fatalf("Kind = %s, want %s", alert.Kind, svc.TaskRuntimeAlertKindConfigReloadFailed)
	}
	if alert.Operation != "unknown" {
		t.Fatalf("Operation = %q, want unknown", alert.Operation)
	}
	if alert.TaskName != "unknown" {
		t.Fatalf("TaskName = %q, want unknown", alert.TaskName)
	}
	if alert.UniqueKey != "unknown:unknown" {
		t.Fatalf("UniqueKey = %q, want unknown:unknown", alert.UniqueKey)
	}
	if alert.Reason != "reload failed；invalid yaml" {
		t.Fatalf("Reason = %q, want reload failed；invalid yaml", alert.Reason)
	}
}

// TestConfigReloadFailureKeepsConfigFile 验证配置热加载告警保留失败文件路径。
func TestConfigReloadFailureKeepsConfigFile(t *testing.T) {
	alert := ConfigReloadFailure("reload failed", errors.New("invalid yaml"), "watcher", "load", "/etc/admin/config.yaml")
	if alert.Reason != "reload failed；配置文件=/etc/admin/config.yaml；invalid yaml" {
		t.Fatalf("Reason = %q, want config file in reason", alert.Reason)
	}
	if alert.UniqueKey != "watcher:load" {
		t.Fatalf("UniqueKey = %q, want watcher:load", alert.UniqueKey)
	}
}

// TestRuntimeConfigReloadFailureBuildsActiveReleaseAlert 验证 DB 运行配置告警字段归属稳定。
func TestRuntimeConfigReloadFailureBuildsActiveReleaseAlert(t *testing.T) {
	alert := RuntimeConfigReloadFailure("publish", errors.New("missing active release"))
	if alert.Kind != svc.TaskRuntimeAlertKindRuntimeConfigReloadFailed {
		t.Fatalf("Kind = %s, want %s", alert.Kind, svc.TaskRuntimeAlertKindRuntimeConfigReloadFailed)
	}
	if alert.Component != "runtime_config" {
		t.Fatalf("Component = %q, want runtime_config", alert.Component)
	}
	if alert.Operation != "reload_active_release" {
		t.Fatalf("Operation = %q, want reload_active_release", alert.Operation)
	}
	if alert.UniqueKey != "publish" {
		t.Fatalf("UniqueKey = %q, want publish", alert.UniqueKey)
	}
}
