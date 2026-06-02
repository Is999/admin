package taskruntime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Is999/go-utils/errors"

	"admin/internal/config"
	usertagtask "admin/internal/jobs/usertag/task"
	"admin/internal/svc"
	"admin/internal/task/queue"

	"github.com/hibiken/asynq"
)

// mockPlugin 表示测试使用的辅助结构。
type mockPlugin struct {
	name string               // name 表示测试场景名称。
	run  func(*Runtime) error // run 表示测试字段。
}

// Name 表示测试辅助逻辑。
func (p mockPlugin) Name() string { return p.name }

// Setup 表示测试辅助逻辑。
func (p mockPlugin) Setup(runtime *Runtime) error {
	if p.run == nil {
		return nil
	}
	return p.run(runtime)
}

// TestBuiltinPluginsNoDefaultCacheTargets 确认移除 AI 模块后，默认插件不再注册示例缓存刷新目标。
func TestBuiltinPluginsNoDefaultCacheTargets(t *testing.T) {
	runtime := NewRuntime(nil, nil)
	if err := runtime.RegisterPlugins(BuiltinPlugins()...); err != nil {
		t.Fatalf("注册内置插件失败: %v", err)
	}

	targets := runtime.DefaultCacheRefreshTargets()
	if len(targets) != 0 {
		t.Fatalf("期望默认缓存刷新目标数为 0，实际为 %d", len(targets))
	}
}

// TestBuiltinPluginSpecsValid 确保默认插件规格字段完整且名称唯一。
func TestBuiltinPluginSpecsValid(t *testing.T) {
	specs := BuiltinPluginSpecs()
	if len(specs) == 0 {
		t.Fatal("BuiltinPluginSpecs() 不能为空")
	}

	nameSeen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if spec.Name == "" || spec.File == "" || spec.Method == "" || spec.Description == "" {
			t.Fatalf("内置插件规格字段不完整: %+v", spec)
		}
		if _, ok := nameSeen[spec.Name]; ok {
			t.Fatalf("内置插件名称重复: %s", spec.Name)
		}
		nameSeen[spec.Name] = struct{}{}
		if spec.Build == nil {
			t.Fatalf("内置插件构造函数为空: %+v", spec)
		}
	}
}

// TestComposePluginsPreservesOrder 确保插件组合函数按输入顺序拼接，避免基础插件和业务插件顺序漂移。
func TestComposePluginsPreservesOrder(t *testing.T) {
	plugins := ComposePlugins(
		[]Plugin{NewPluginFunc("first", nil)},
		[]Plugin{NewPluginFunc("second", nil), NewPluginFunc("third", nil)},
	)
	if len(plugins) != 3 {
		t.Fatalf("期望共有 3 个插件，实际为 %d", len(plugins))
	}
	if plugins[0].Name() != "first" || plugins[1].Name() != "second" || plugins[2].Name() != "third" {
		t.Fatalf("插件顺序不符合预期: %s, %s, %s", plugins[0].Name(), plugins[1].Name(), plugins[2].Name())
	}
}

// TestRegisterReturnsRuntime 确保统一注册入口会返回可继续使用的运行时对象。
func TestRegisterReturnsRuntime(t *testing.T) {
	runtime, err := Register(&svc.ServiceContext{}, nil, NewPluginFunc("demo", nil))
	if err != nil {
		t.Fatalf("注册运行时失败: %v", err)
	}
	if runtime != nil {
		t.Fatal("期望 manager 为空时返回 nil runtime")
	}

	managerRuntime, err := Register(&svc.ServiceContext{}, newTestManager(), NewPluginFunc("demo", nil))
	if err != nil {
		t.Fatalf("挂载 manager 后注册运行时失败: %v", err)
	}
	if managerRuntime == nil {
		t.Fatal("期望返回运行时对象，实际为 nil")
	}
}

// TestRegisterPluginsWrapsPluginError 确保插件初始化失败时会带上插件名，方便启动期排查。
func TestRegisterPluginsWrapsPluginError(t *testing.T) {
	runtime := NewRuntime(nil, nil)
	err := runtime.RegisterPlugins(mockPlugin{
		name: "broken",
		run: func(*Runtime) error {
			return errors.New("boom")
		},
	})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
	if got := err.Error(); !strings.Contains(got, "初始化任务插件失败 broken") {
		t.Fatalf("错误内容不符合预期: %s", got)
	}
	if root := errors.Root(err); root == nil || !strings.Contains(root.Error(), "boom") {
		t.Fatalf("错误根因不符合预期: %v", root)
	}
}

// TestRefreshTargetsUsesRegisteredHandler 确保缓存刷新逻辑通过注册表分发，而不是依赖硬编码 switch。
func TestRefreshTargetsUsesRegisteredHandler(t *testing.T) {
	runtime := NewRuntime(nil, nil)
	var called int
	if err := runtime.RegisterCacheRefreshTarget("demo.target", func(ctx context.Context, _ *svc.ServiceContext) error {
		called++
		if ctx == nil {
			t.Fatal("context 不应为空")
		}
		return nil
	}); err != nil {
		t.Fatalf("注册缓存刷新目标失败: %v", err)
	}

	if err := runtime.refreshTargets(context.Background(), "test", []string{"demo.target"}); err != nil {
		t.Fatalf("执行缓存刷新失败: %v", err)
	}
	if called != 1 {
		t.Fatalf("期望处理函数只调用一次，实际为 %d", called)
	}
}

// TestRefreshTargetsRejectsUnknownTarget 确保未知缓存目标会返回错误，避免任务静默丢失。
func TestRefreshTargetsRejectsUnknownTarget(t *testing.T) {
	runtime := NewRuntime(nil, nil)
	err := runtime.refreshTargets(context.Background(), "test", []string{"unknown"})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestRegisterPluginsRejectsDuplicatePlugin 确保同名插件不会被重复注册，避免初始化顺序污染。
func TestRegisterPluginsRejectsDuplicatePlugin(t *testing.T) {
	runtime := NewRuntime(nil, nil)
	plugin := NewPluginFunc("dup", nil)
	if err := runtime.RegisterPlugins(plugin); err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}
	if err := runtime.RegisterPlugins(plugin); err == nil {
		t.Fatal("期望返回重复插件错误，实际为 nil")
	}
}

// TestRegisterPluginsConcurrentDuplicate 确保并发注册同名插件时只有一个插件会执行初始化逻辑。
func TestRegisterPluginsConcurrentDuplicate(t *testing.T) {
	runtime := NewRuntime(nil, nil)
	var setupCount atomic.Int32
	plugin := NewPluginFunc("dup.concurrent", func(*Runtime) error {
		setupCount.Add(1)
		return nil
	})

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- runtime.RegisterPlugins(plugin)
		}()
	}
	wg.Wait()
	close(errCh)

	var successCount int
	var duplicateCount int
	for err := range errCh {
		if err == nil {
			successCount++
			continue
		}
		if strings.Contains(err.Error(), "任务插件已注册") {
			duplicateCount++
			continue
		}
		t.Fatalf("期望只出现重复插件错误，实际为 %v", err)
	}
	if successCount != 1 || duplicateCount != 1 {
		t.Fatalf("并发注册结果不符合预期，成功=%d 重复=%d", successCount, duplicateCount)
	}
	if got := setupCount.Load(); got != 1 {
		t.Fatalf("期望插件初始化只执行 1 次，实际为 %d", got)
	}
}

// TestRegisterCacheRefreshTargetConcurrent 确保缓存刷新目标注册表在并发插件初始化时保持稳定。
func TestRegisterCacheRefreshTargetConcurrent(t *testing.T) {
	runtime := NewRuntime(nil, nil)
	const targetCount = 32

	var wg sync.WaitGroup
	errCh := make(chan error, targetCount)
	for i := 0; i < targetCount; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			target := fmt.Sprintf("demo.target.%02d", i)
			errCh <- runtime.RegisterCacheRefreshTarget(target, func(context.Context, *svc.ServiceContext) error {
				return nil
			})
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("并发注册缓存刷新目标失败: %v", err)
		}
	}
	if got := len(runtime.DefaultCacheRefreshTargets()); got != targetCount {
		t.Fatalf("期望注册 %d 个缓存刷新目标，实际为 %d", targetCount, got)
	}
}

// TestRegisterHandlerRejectsDuplicatePattern 确保任务处理器模式不会被重复覆盖。
func TestRegisterHandlerRejectsDuplicatePattern(t *testing.T) {
	runtime := NewRuntime(nil, nil)
	handler := asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil })
	if err := runtime.RegisterHandler("demo:handler", handler); err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}
	if err := runtime.RegisterHandler("demo:handler", handler); err == nil {
		t.Fatal("期望返回重复处理器错误，实际为 nil")
	}
}

// TestRegisterMiddlewareRejectsDuplicateName 确保运行时对外暴露的任务中间件注册入口不会重复覆盖。
func TestRegisterMiddlewareRejectsDuplicateName(t *testing.T) {
	runtime := NewRuntime(nil, nil)
	middleware := func(next asynq.Handler) asynq.Handler { return next }
	if err := runtime.RegisterMiddleware("trace", middleware); err != nil {
		t.Fatalf("首次注册中间件失败: %v", err)
	}
	if err := runtime.RegisterMiddleware("trace", middleware); err == nil {
		t.Fatal("期望返回重复中间件错误，实际为 nil")
	}
}

// TestRegisterGroupAggregatorRejectsDuplicate 确保聚合器按 group 维度唯一注册，避免批处理逻辑被覆盖。
func TestRegisterGroupAggregatorRejectsDuplicate(t *testing.T) {
	runtime := NewRuntime(nil, nil)
	aggregator := func(tasks []*asynq.Task) *asynq.Task { return nil }
	if err := runtime.RegisterGroupAggregator("demo.group", aggregator); err != nil {
		t.Fatalf("首次注册分组聚合器失败: %v", err)
	}
	if err := runtime.RegisterGroupAggregator("demo.group", aggregator); err == nil {
		t.Fatal("期望返回重复分组聚合器错误，实际为 nil")
	}
}

// TestRegisterCacheRefreshTargetRejectsDuplicate 确保缓存刷新目标不会被静默覆盖。
func TestRegisterCacheRefreshTargetRejectsDuplicate(t *testing.T) {
	runtime := NewRuntime(nil, nil)
	handler := func(context.Context, *svc.ServiceContext) error { return nil }
	if err := runtime.RegisterCacheRefreshTarget("demo.target", handler); err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}
	if err := runtime.RegisterCacheRefreshTarget("demo.target", handler); err == nil {
		t.Fatal("期望返回重复缓存目标错误，实际为 nil")
	}
}

// TestRegisterPeriodicTaskStoresConfig 确保周期任务能通过运行时注册，并保留给调度层消费。
func TestRegisterPeriodicTaskStoresConfig(t *testing.T) {
	runtime := NewRuntime(nil, nil)
	cfg := config.TaskPeriodicConfig{
		Name:     "demo-periodic",
		Cron:     "*/5 * * * *",
		Workflow: "cache.refresh",
	}
	if err := runtime.RegisterPeriodicTask(cfg); err != nil {
		t.Fatalf("注册周期任务失败: %v", err)
	}
	items := runtime.PeriodicTasks()
	if len(items) != 1 {
		t.Fatalf("期望共有 1 个周期任务，实际为 %d", len(items))
	}
	if items[0].Name != cfg.Name || items[0].Workflow != cfg.Workflow {
		t.Fatalf("周期任务配置不符合预期: %+v", items[0])
	}
}

// TestRegisterPeriodicTaskRejectsDuplicate 确保重复的周期任务定义会被拒绝，避免调度重复执行。
func TestRegisterPeriodicTaskRejectsDuplicate(t *testing.T) {
	runtime := NewRuntime(nil, nil)
	cfg := config.TaskPeriodicConfig{
		Name:     "demo-periodic",
		Cron:     "*/5 * * * *",
		Workflow: "cache.refresh",
	}
	if err := runtime.RegisterPeriodicTask(cfg); err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}
	if err := runtime.RegisterPeriodicTask(cfg); err == nil {
		t.Fatal("期望返回重复周期任务错误，实际为 nil")
	}
}

// TestNewPeriodicWorkflowPlugin 确保函数式周期插件能复用统一注册入口。
func TestNewPeriodicWorkflowPlugin(t *testing.T) {
	runtime := NewRuntime(nil, nil)
	plugin := NewPeriodicWorkflowPlugin("periodic.plugin", config.TaskPeriodicConfig{
		Name:     "demo-plugin",
		Cron:     "0 * * * *",
		Workflow: "cache.refresh",
	})
	if err := runtime.RegisterPlugins(plugin); err != nil {
		t.Fatalf("注册周期工作流插件失败: %v", err)
	}
	if len(runtime.PeriodicTasks()) != 1 {
		t.Fatalf("期望插件注册后存在 1 个周期任务，实际为 %d", len(runtime.PeriodicTasks()))
	}
}

// TestUserTagPluginRegistersMaintenanceWorkflows 验证用户标签插件会注册独立维护工作流。
func TestUserTagPluginRegistersMaintenanceWorkflows(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		Workflows: config.WorkflowsConfig{
			UserTag: config.UserTagConfig{Enabled: true},
		},
	}, svc.Dependencies{})
	manager := newTestManager()

	plugin := NewPluginFunc(usertagtask.PluginName, func(runtime *Runtime) error {
		return usertagtask.Setup(runtime)
	})
	if _, err := Register(svcCtx, manager, plugin); err != nil {
		t.Fatalf("注册用户标签插件失败: %v", err)
	}
	registered := manager.ListRegisteredWorkflows()
	want := map[string]bool{
		usertagtask.WorkflowNameUserTagEventOutboxRetryScan: false,
		usertagtask.WorkflowNameUserTagRuntimeCleanup:       false,
	}
	for _, item := range registered {
		if _, ok := want[item.Name]; ok {
			want[item.Name] = true
		}
	}
	for workflowName, ok := range want {
		if !ok {
			t.Fatalf("未注册用户标签维护工作流 %s，当前清单=%+v", workflowName, registered)
		}
	}
}

// newTestManager 构造测试依赖。
func newTestManager() *taskqueue.Manager {
	return &taskqueue.Manager{}
}
