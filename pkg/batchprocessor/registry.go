package batchprocessor

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/Is999/go-utils/errors"
)

// RegistryConfig 定义批量处理收集器的全局配置。
// - MaxConcurrentFlush/MaxConcurrentProcess 用于限制全局并发，避免多个 bizType 同时 flush/处理造成下游尖峰。
type RegistryConfig struct {
	Enabled bool // 是否启用批量处理收集器

	MaxConcurrentFlush   int       // 全局最大并发 flush 数
	MaxConcurrentProcess int       // 全局最大并发 process 数
	AlertHook            AlertHook // 后台运行异常告警钩子；为空时仅返回错误或由业务自行记录
}

// Registry 提供 Register(bizType, module, policy) 的插件注册入口，并托管每个 bizType 的独立运行时。
// 每个 bizType 会拥有独立的 collector 与 processor，从而支持业务隔离策略（并发/重试/白名单等）。
type Registry struct {
	cfg RegistryConfig // 配置快照

	mu      sync.RWMutex           // 保护 modules 与 started
	modules map[string]*bizRuntime // bizType -> runtime

	started bool // 是否已启动（用于支持 Start 后动态 Register）

	flushLimiter   chan struct{} // flush 全局并发限制器
	processLimiter chan struct{} // process 全局并发限制器
	alertHook      AlertHook     // 后台运行异常告警钩子

	rndMu sync.Mutex // 保护 rnd
	rnd   *rand.Rand // jitter 随机源
}

// bizRuntime 表示单个 bizType 的运行时容器。
type bizRuntime struct {
	bizType string // 业务类型
	module  Module // 业务模块实现
	policy  Policy // bizType 策略

	collector *collector // 负责批量收集与 flush
	processor *processor // 负责周期/手动触发批量处理

	startOnce sync.Once // 确保只启动一次
	stopOnce  sync.Once // 确保只停止一次
}

// NewRegistry 创建注册中心实例。
func NewRegistry(cfg RegistryConfig) *Registry {
	if cfg.MaxConcurrentFlush <= 0 {
		cfg.MaxConcurrentFlush = 4
	}
	if cfg.MaxConcurrentProcess <= 0 {
		cfg.MaxConcurrentProcess = 4
	}
	return &Registry{
		cfg:            cfg,
		modules:        make(map[string]*bizRuntime),
		flushLimiter:   make(chan struct{}, cfg.MaxConcurrentFlush),
		processLimiter: make(chan struct{}, cfg.MaxConcurrentProcess),
		alertHook:      cfg.AlertHook,
		rnd:            rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Register 注册一个业务模块。
// - bizType 作为唯一键；重复注册会返回错误
// - policy 会在注册时 normalize 补齐默认值
func (r *Registry) Register(bizType string, module Module, policy Policy) error {
	if r == nil || !r.cfg.Enabled {
		return errors.Errorf("batchprocessor.Registry 未启用")
	}
	if err := validateRegisterParams(bizType, module); err != nil {
		return errors.Tag(err)
	}
	policy.normalize()

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.modules[bizType]; ok {
		return errors.Errorf("batchprocessor.Registry 重复注册 bizType=%s", bizType)
	}

	runtime := &bizRuntime{
		bizType: bizType,
		module:  module,
		policy:  policy,
	}
	runtime.collector = newCollector(bizType, module, policy, r.flushLimiter, r.randDuration)
	if runtime.collector != nil {
		runtime.collector.alertHook = r.alertHook
	}
	runtime.processor = newProcessor(bizType, module, policy, r.processLimiter, r.randDuration)
	if runtime.processor != nil {
		runtime.processor.alertHook = r.alertHook
	}
	r.modules[bizType] = runtime

	if r.started {
		runtime.startOnce.Do(func() {
			if runtime.collector != nil {
				runtime.collector.start()
			}
			if runtime.processor != nil {
				runtime.processor.start()
			}
		})
	}

	return nil
}

// Start 启动所有已注册 bizType 的后台协程（collector/processor）。
func (r *Registry) Start() {
	if r == nil || !r.cfg.Enabled {
		return
	}
	r.mu.Lock()
	r.started = true
	r.mu.Unlock()
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rt := range r.modules {
		rt.startOnce.Do(func() {
			if rt.collector != nil {
				rt.collector.start()
			}
			if rt.processor != nil {
				rt.processor.start()
			}
		})
	}
}

// IsStarted 返回注册中心后台 collector/processor 是否已启动。
// 该方法供上层业务判断是否可以使用异步内存缓冲，未启动时应选择同步可靠落地或其它降级路径。
func (r *Registry) IsStarted() bool {
	if r == nil || !r.cfg.Enabled {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.started
}

// Stop 停止所有已注册 bizType 的后台协程，并尽力 flush buffer。
func (r *Registry) Stop(ctx context.Context) error {
	if r == nil || !r.cfg.Enabled {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r.mu.Lock()
	r.started = false
	runtimes := make([]*bizRuntime, 0, len(r.modules))
	for _, rt := range r.modules {
		runtimes = append(runtimes, rt)
	}
	r.mu.Unlock()

	var firstErr error
	for _, rt := range runtimes {
		rt.stopOnce.Do(func() {
			if rt.processor != nil {
				if err := rt.processor.stop(ctx); err != nil && firstErr == nil {
					firstErr = errors.Tag(err)
				}
			}
			if rt.collector != nil {
				if err := rt.collector.stop(ctx); err != nil && firstErr == nil {
					firstErr = errors.Tag(err)
				}
			}
		})
	}
	return firstErr
}

// Collect 收集一条业务数据。
// 必达任务会等待落地结果；非必达任务会异步批量落地。
func (r *Registry) Collect(ctx context.Context, bizType string, data Data) error {
	if r == nil || !r.cfg.Enabled {
		return errors.Errorf("batchprocessor.Registry 未启用")
	}
	if bizType == "" {
		return errors.Errorf("batchprocessor.Collect bizType 为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r.mu.RLock()
	rt := r.modules[bizType]
	started := r.started
	r.mu.RUnlock()
	if !started {
		return errors.Errorf("batchprocessor.Registry 未启动")
	}
	if rt == nil || rt.collector == nil {
		return errors.Errorf("batchprocessor.Collect bizType 未注册: %s", bizType)
	}

	data.BizType = bizType
	if data.CreatedAt.IsZero() {
		data.CreatedAt = time.Now()
	}
	if err := rt.validatePolicy(data); err != nil {
		return errors.Tag(err)
	}
	return errors.Tag(rt.collector.collect(ctx, data))
}

// TriggerFlush 触发指定 bizType 的一次 flush（非阻塞）。
func (r *Registry) TriggerFlush(bizType string) {
	if r == nil || !r.cfg.Enabled {
		return
	}
	r.mu.RLock()
	rt := r.modules[bizType]
	r.mu.RUnlock()
	if rt == nil || rt.collector == nil {
		return
	}
	rt.collector.triggerFlush()
}

// TriggerProcess 触发指定 bizType 的一次处理（非阻塞）。
func (r *Registry) TriggerProcess(bizType string) {
	if r == nil || !r.cfg.Enabled {
		return
	}
	r.mu.RLock()
	rt := r.modules[bizType]
	r.mu.RUnlock()
	if rt == nil || rt.processor == nil {
		return
	}
	rt.processor.trigger()
}

// RunNow 立即执行指定 bizType 的一次处理，并返回处理数量。
func (r *Registry) RunNow(ctx context.Context, bizType string, limit int) (int, error) {
	if r == nil || !r.cfg.Enabled {
		return 0, errors.Errorf("batchprocessor.Registry 未启用")
	}
	if bizType == "" {
		return 0, errors.Errorf("batchprocessor.RunNow bizType 为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.RLock()
	rt := r.modules[bizType]
	r.mu.RUnlock()
	if rt == nil || rt.processor == nil {
		return 0, errors.Errorf("batchprocessor.RunNow bizType 未注册: %s", bizType)
	}
	return rt.processor.runOnce(ctx, limit)
}

// PolicyOf 返回指定 bizType 的策略快照（只读）。
func (r *Registry) PolicyOf(bizType string) (Policy, bool) {
	if r == nil {
		return Policy{}, false
	}
	r.mu.RLock()
	rt := r.modules[bizType]
	r.mu.RUnlock()
	if rt == nil {
		return Policy{}, false
	}
	return rt.policy, true
}

// validatePolicy 执行策略层面的兜底校验，避免业务模块被危险输入击穿。
// 业务自定义校验仍应在 Module.Validate 中完成，这里只做框架级约束。
func (rt *bizRuntime) validatePolicy(data Data) error {
	if rt == nil {
		return errors.Errorf("batchprocessor bizRuntime 为空")
	}
	if len(rt.policy.AllowActions) > 0 && data.Action == "" {
		return errors.Errorf("batchprocessor.Action 必填")
	}
	if data.Action != "" && actionDenied(data.Action, rt.policy.DenyActions) {
		return errors.Errorf("batchprocessor.Action 禁止: %s", data.Action)
	}
	if len(rt.policy.AllowActions) > 0 && data.Action != "" && !actionAllowed(data.Action, rt.policy.AllowActions) {
		return errors.Errorf("batchprocessor.Action 不在白名单: %s", data.Action)
	}
	if rt.policy.RequireIdempotencyKey && data.IdempotencyKey == "" {
		return errors.Errorf("batchprocessor.IdempotencyKey 必填")
	}
	return nil
}

// randDuration 返回 [0, max) 的随机时间，用于 jitter 抖动。
func (r *Registry) randDuration(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	r.rndMu.Lock()
	defer r.rndMu.Unlock()
	return time.Duration(r.rnd.Int63n(int64(max)))
}

// actionDenied 判断动作是否命中黑名单。
func actionDenied(action string, deny []string) bool {
	for _, v := range deny {
		if v == action {
			return true
		}
	}
	return false
}

// actionAllowed 判断动作是否命中白名单。
func actionAllowed(action string, allow []string) bool {
	for _, v := range allow {
		if v == action {
			return true
		}
	}
	return false
}
