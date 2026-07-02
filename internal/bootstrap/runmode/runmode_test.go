package runmode

import (
	"testing"
)

// TestResolve 确保启动模式遵循“命令行优先、配置兜底、默认回退 7”的规则。
func TestResolve(t *testing.T) {
	if mode, err := Resolve(nil, 5); err != nil || mode != 5 {
		t.Fatalf("期望配置中的 run_mode 为 5，实际为 %d", mode)
	}

	cliMode := 2
	if mode, err := Resolve(&cliMode, 5); err != nil || mode != 2 {
		t.Fatalf("期望命令行模式为 2，实际为 %d", mode)
	}

	zeroCliMode := 0
	if mode, err := Resolve(&zeroCliMode, 5); err != nil || mode != 5 {
		t.Fatalf("期望命令行 mode=0 回退配置中的 5，实际为 %d", mode)
	}

	if mode, err := Resolve(nil, 0); err != nil || mode != All {
		t.Fatalf("期望回退模式为 %d，实际为 %d", All, mode)
	}
}

// TestResolveRejectsInvalidMode 确保非法启动模式不会被静默裁剪成其它模式。
func TestResolveRejectsInvalidMode(t *testing.T) {
	invalidMode := 8
	if _, err := Resolve(&invalidMode, 0); err == nil {
		t.Fatal("期望非法启动模式返回错误，实际为 nil")
	}
}

// TestNormalizeModeSupportsDocumentedCombinations 确保启动模式只接受文档声明的 1..7 组合。
func TestNormalizeModeSupportsDocumentedCombinations(t *testing.T) {
	tests := []struct {
		name string // name 表示测试场景名称。
		mode int    // mode 表示待规范化的 run_mode 配置值。
		want int    // want 表示期望得到的启动模式位掩码。
	}{
		{name: "default", mode: 0, want: All},
		{name: "api", mode: 1, want: API},
		{name: "worker", mode: 2, want: Worker},
		{name: "api_worker", mode: 3, want: API | Worker},
		{name: "scheduler", mode: 4, want: Scheduler},
		{name: "api_scheduler", mode: 5, want: API | Scheduler},
		{name: "worker_scheduler", mode: 6, want: Worker | Scheduler},
		{name: "all", mode: 7, want: All},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalize(tt.mode)
			if err != nil {
				t.Fatalf("规范化启动模式失败: %v", err)
			}
			if got != tt.want {
				t.Fatalf("期望启动模式为 %d，实际为 %d", tt.want, got)
			}
		})
	}
}

// TestNormalizeModeRejectsUnsupportedCombinations 确保非法模式不会被按位裁剪后误启动。
func TestNormalizeModeRejectsUnsupportedCombinations(t *testing.T) {
	for _, mode := range []int{-1, 8, 9, 15} {
		if _, err := Normalize(mode); err == nil {
			t.Fatalf("期望非法启动模式 %d 返回错误，实际为 nil", mode)
		}
	}
}

// TestModeLifecycleMatrix 确保运行模式与后台生命周期职责保持一致。
func TestModeLifecycleMatrix(t *testing.T) {
	tests := []struct {
		mode       int  // mode 表示待验证的启动模式。
		workerHook bool // workerHook 表示是否应启动 Worker 生命周期。
		taskHook   bool // taskHook 表示是否应启动任务生命周期。
		api        bool // api 表示是否应启动 API 服务。
	}{
		{mode: 1, workerHook: false, taskHook: false, api: true},
		{mode: 2, workerHook: true, taskHook: true, api: false},
		{mode: 3, workerHook: true, taskHook: true, api: true},
		{mode: 4, workerHook: false, taskHook: true, api: false},
		{mode: 5, workerHook: false, taskHook: true, api: true},
		{mode: 6, workerHook: true, taskHook: true, api: false},
		{mode: 7, workerHook: true, taskHook: true, api: true},
	}

	for _, tt := range tests {
		if got := ShouldStartWorker(tt.mode); got != tt.workerHook {
			t.Fatalf("mode=%d Worker 生命周期期望=%v 实际=%v", tt.mode, tt.workerHook, got)
		}
		if got := ShouldStartTask(tt.mode); got != tt.taskHook {
			t.Fatalf("mode=%d 任务生命周期期望=%v 实际=%v", tt.mode, tt.taskHook, got)
		}
		if got := Has(tt.mode, API); got != tt.api {
			t.Fatalf("mode=%d API 期望=%v 实际=%v", tt.mode, tt.api, got)
		}
	}
}

// TestFormatMode 确保位掩码模式按“数字(角色组合)”格式输出，便于和 run_mode 配置对照。
func TestFormatMode(t *testing.T) {
	tests := []struct {
		name string // name 表示测试场景名称。
		mode int    // mode 表示待格式化的启动模式。
		want string // want 表示期望输出的可读模式名称。
	}{
		{name: "none", mode: 0, want: "none"},
		{name: "api", mode: 1, want: "1(api)"},
		{name: "worker", mode: 2, want: "2(worker)"},
		{name: "scheduler", mode: 4, want: "4(scheduler)"},
		{name: "all", mode: 7, want: "7(api+worker+scheduler)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Format(tt.mode); got != tt.want {
				t.Fatalf("mode=%d 格式化结果不符合预期，期望=%q，实际=%q", tt.mode, tt.want, got)
			}
		})
	}
}
