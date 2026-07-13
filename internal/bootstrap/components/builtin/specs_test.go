package builtin

import (
	"testing"

	core "admin/internal/bootstrap/components"
)

// TestResolveStartupUsesOptions 确保内置组件开关与外部组件追加逻辑符合预期。
func TestResolveStartupUsesOptions(t *testing.T) {
	component := core.NewFunc("external", nil)
	components := ResolveStartup(false, []core.Component{component})

	if len(components) != 1 || components[0].Name() != "external" {
		t.Fatalf("期望只保留外部组件，实际为 %+v", components)
	}
}

// TestDefaultTaskPluginSpecsIncludeJobMetadata 确保默认启动链包含任务展示元数据插件。
func TestDefaultTaskPluginSpecsIncludeJobMetadata(t *testing.T) {
	names := make(map[string]struct{})
	for _, spec := range DefaultTaskPluginSpecs() {
		names[spec.Name] = struct{}{}
	}
	if _, ok := names["job_task_metadata"]; !ok {
		t.Fatal("默认任务插件清单缺少 job_task_metadata")
	}
}

// TestRuntimeAlertStartsBeforeDependents 确保运行告警先于依赖它的后台组件注册。
func TestRuntimeAlertStartsBeforeDependents(t *testing.T) {
	specs := DefaultSpecs()
	if len(specs) == 0 || specs[0].Name != NameRuntimeAlert {
		t.Fatalf("运行告警必须最先注册，实际启动清单为 %+v", specs)
	}
}
