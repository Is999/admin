package batchprocessor

import (
	"context"
	"sync"
	"time"

	"github.com/Is999/go-utils/errors"
)

// processor 负责单个 bizType 的“周期触发/手动触发 -> 批量处理”能力。
// 具体如何处理由业务 Module.Process 实现，processor 只负责调度与并发控制。
type processor struct {
	bizType string             // 业务类型
	module  Module             // 业务模块实现
	policy  Policy             // 当前 bizType 的处理策略
	ctx     context.Context    // 后台处理器生命周期上下文
	cancel  context.CancelFunc // 后台处理器停止函数

	processLimiter chan struct{}                         // 全局 process 并发限制器，避免多个 bizType 同时执行冲击下游
	randDuration   func(max time.Duration) time.Duration // 随机抖动函数，用于打散周期触发尖峰
	alertHook      AlertHook                             // 后台运行异常告警钩子

	triggerCh chan struct{}  // 处理触发信号队列（cap=ProcessConcurrency，避免丢弃触发）
	stopCh    chan struct{}  // 停止信号
	stopOnce  sync.Once      // 确保停止信号只关闭一次
	wg        sync.WaitGroup // 等待调度器和 worker 退出
}

// newProcessor 创建单个 bizType 的处理器；当 policy.ProcessEnabled=false 时返回 nil。
func newProcessor(bizType string, module Module, policy Policy, processLimiter chan struct{}, randDuration func(max time.Duration) time.Duration) *processor {
	policy.normalize()
	if !policy.ProcessEnabled {
		return nil
	}
	triggerCap := policy.ProcessConcurrency
	if triggerCap <= 0 {
		triggerCap = 1
	}
	if randDuration == nil {
		randDuration = func(time.Duration) time.Duration {
			return 0
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &processor{
		bizType:        bizType,
		module:         module,
		policy:         policy,
		ctx:            ctx,
		cancel:         cancel,
		processLimiter: processLimiter,
		randDuration:   randDuration,
		triggerCh:      make(chan struct{}, triggerCap),
		stopCh:         make(chan struct{}),
	}
}

// start 启动调度器与 worker 池。
func (p *processor) start() {
	if p == nil {
		return
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.runScheduler()
	}()

	workers := p.policy.ProcessConcurrency
	if workers <= 0 {
		workers = 1
	}
	p.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer p.wg.Done()
			p.runWorker()
		}()
	}
}

// stop 停止后台协程。
func (p *processor) stop(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.stopOnce.Do(func() {
		p.cancel()
		close(p.stopCh)
	})
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "停止批处理执行器超时")
	}
}

// trigger 手动触发一次处理（非阻塞）。
func (p *processor) trigger() {
	if p == nil {
		return
	}
	select {
	case p.triggerCh <- struct{}{}:
	default:
	}
}

// runOnce 执行一次批量处理，并返回处理数量。
func (p *processor) runOnce(ctx context.Context, limit int) (int, error) {
	if p == nil {
		return 0, errors.Errorf("batchprocessor.processor 为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = p.policy.ProcessBatchSize
	}

	release, err := p.acquireProcessSlot(ctx)
	if err != nil {
		return 0, errors.Tag(err)
	}
	defer release()
	processed, err := p.processBatch(ctx, limit)
	return processed, errors.Tag(err)
}

// processBatch 执行业务批处理，并把业务 panic 转成错误，避免 worker 协程异常退出。
func (p *processor) processBatch(ctx context.Context, limit int) (processed int, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.Errorf("batchprocessor.Process panic bizType=%s panic=%v", p.bizType, recovered)
		}
	}()
	return p.module.Process(ctx, p.bizType, limit)
}

// runScheduler 周期性触发处理，并引入 jitter 打散尖峰。
// 单次触发会尽量投递 ProcessConcurrency 个信号，让 worker 池能并行处理。
func (p *processor) runScheduler() {
	initialDelay := p.randDuration(p.policy.ProcessInterval)
	timer := time.NewTimer(initialDelay)
	defer timer.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-timer.C:
			p.triggerN(p.policy.ProcessConcurrency)
			timer.Reset(p.nextInterval(p.policy.ProcessInterval, p.policy.ProcessJitter))
		}
	}
}

// runWorker 消费触发信号并执行处理。
func (p *processor) runWorker() {
	for {
		select {
		case <-p.stopCh:
			return
		case <-p.triggerCh:
			_, err := p.runOnce(p.ctx, p.policy.ProcessBatchSize)
			if err != nil {
				p.reportRuntimeAlert(p.ctx, RuntimeAlert{
					Kind:      runtimeAlertKindProcessFailed,
					Title:     "【P1 批处理后台执行失败】",
					Status:    "本轮后台 process 失败，后续周期会继续触发",
					Component: "batchprocessor.processor",
					Operation: "process_batch",
					Reason:    err.Error(),
					Advice:    "请检查业务 Process 的查询范围、下游写入和重试状态机；若失败持续出现，请结合 bizType 手动执行单轮排查死信或卡住数据。",
				})
			}
		}
	}
}

// reportRuntimeAlert 上报后台执行异常；未配置 hook 时保持原有行为。
func (p *processor) reportRuntimeAlert(ctx context.Context, alert RuntimeAlert) {
	if p == nil || p.alertHook == nil {
		return
	}
	alert.BizType = p.bizType
	if ctx == nil {
		ctx = context.Background()
	}
	p.alertHook(ctx, normalizeRuntimeAlert(alert))
}

// triggerN 尽力投递 n 个触发信号（非阻塞）。
// 该方法用于 scheduler 场景，确保在高并发策略下不会因为 channel cap=1 丢触发。
func (p *processor) triggerN(n int) {
	if p == nil || n <= 0 {
		return
	}
	for i := 0; i < n; i++ {
		select {
		case p.triggerCh <- struct{}{}:
		default:
			return
		}
	}
}

// nextInterval 基于 base + [0,jitter) 计算下一次周期触发间隔。
func (p *processor) nextInterval(base time.Duration, jitter time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	if jitter <= 0 {
		return base
	}
	return base + p.randDuration(jitter)
}

// acquireProcessSlot 获取全局 process 并发令牌，并尊重上下文取消。
func (p *processor) acquireProcessSlot(ctx context.Context) (func(), error) {
	if p.processLimiter == nil {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case p.processLimiter <- struct{}{}:
		return func() {
			<-p.processLimiter
		}, nil
	case <-ctx.Done():
		return nil, errors.Wrap(ctx.Err(), "获取 process 并发令牌失败")
	}
}
