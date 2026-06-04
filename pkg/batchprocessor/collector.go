package batchprocessor

import (
	"context"
	"sync"
	"time"

	"github.com/Is999/go-utils/errors"
)

// bufferedData 表示收集器内部缓冲单元。
// Required=true 时会携带 ack 通道，用于把 flush 或 fallback 的结果回传给调用方。
type bufferedData struct {
	data Data       // 业务数据
	ack  chan error // 必达任务的落地结果回传通道；非必达任务为空
}

// collector 负责单个 bizType 的“批量收集 -> 批量落地”能力。
// 注意：collector 不知道“如何落地”，只负责按策略把数据聚合成 batch 并调用 module.Flush。
type collector struct {
	bizType string             // 业务类型
	module  Module             // 业务模块实现
	policy  Policy             // 当前 bizType 的收集策略
	ctx     context.Context    // 上下文
	cancel  context.CancelFunc // 取消函数

	flushLimiter  chan struct{}                         // 全局 flush 并发限制器，避免多个 bizType 同时 flush 冲击下游
	fallbackLimit chan struct{}                         // 必达任务 fallback 并发限制器，避免降级落地反向拖垮下游
	randDuration  func(max time.Duration) time.Duration // 随机抖动函数，用于打散 flush 尖峰

	mu       sync.Mutex     // 保护 buffer
	buffer   []bufferedData // 待落地的缓冲数据队列
	inFlight int            // 已从 buffer 弹出但尚未确认落地/回滚的数据数量
	stopped  bool           // 是否已停止，避免停止瞬间仍有新数据入队
	flushMu  sync.Mutex     // 串行化 flush，避免停止和定时触发并发落地

	flushCh    chan struct{}  // flush 触发信号（cap=1，防止信号风暴）
	spaceCh    chan struct{}  // 缓冲区释放信号（cap=1，防止唤醒风暴）
	stopCh     chan struct{}  // 停止信号
	wg         sync.WaitGroup // 等待组
	fallbackWG sync.WaitGroup // 等待必达 fallback 协程退出
}

// bufferedDataBatchPool 复用 popBatch 的批次数组，降低高吞吐收集场景下的短生命周期切片分配。
var bufferedDataBatchPool = sync.Pool{
	New: func() any {
		batch := make([]bufferedData, 0, 200)
		return &batch
	},
}

// newCollector 创建单个 bizType 的收集器实例。
func newCollector(bizType string, module Module, policy Policy, flushLimiter chan struct{}, randDuration func(max time.Duration) time.Duration) *collector {
	policy.normalize()
	if randDuration == nil {
		randDuration = func(time.Duration) time.Duration {
			return 0
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &collector{
		bizType:       bizType,
		module:        module,
		policy:        policy,
		ctx:           ctx,
		cancel:        cancel,
		flushLimiter:  flushLimiter,
		fallbackLimit: make(chan struct{}, fallbackConcurrency(policy)),
		randDuration:  randDuration,
		buffer:        make([]bufferedData, 0, policy.BatchSize),
		flushCh:       make(chan struct{}, 1),
		spaceCh:       make(chan struct{}, 1),
		stopCh:        make(chan struct{}),
	}
}

// start 启动后台 flush loop。
func (c *collector) start() {
	if c == nil {
		return
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.runLoop()
	}()
}

// stop 停止后台协程，并尽力把 buffer 内数据落地。
func (c *collector) stop(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return nil
	}
	c.stopped = true
	close(c.stopCh)
	c.cancel()
	c.mu.Unlock()

	flushErr := c.flushAll(ctx)
	waitErr := c.wait(ctx)
	if flushErr != nil {
		return errors.Tag(flushErr)
	}
	if waitErr != nil {
		return errors.Tag(waitErr)
	}
	return nil
}

// wait 等待后台 flush loop 和 fallback 协程退出，并尊重调用方停止超时。
func (c *collector) wait(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		c.fallbackWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "停止批处理收集器超时")
	}
}

// triggerFlush 触发一次 flush 信号（非阻塞）。
func (c *collector) triggerFlush() {
	if c == nil {
		return
	}
	select {
	case c.flushCh <- struct{}{}:
	default:
	}
}

// collect 收集一条数据到 buffer。
// - 非必达数据：入 buffer 后直接返回
// - 必达数据：会等待本条数据被 flush 或 fallback 的结果回传
func (c *collector) collect(ctx context.Context, data Data) error {
	if c == nil {
		return errors.Errorf("batchprocessor.collector 为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return errors.Wrapf(err, "batchprocessor.collect 上下文已取消 bizType=%s", c.bizType)
	}
	if err := c.module.Validate(ctx, data); err != nil {
		return errors.Tag(err)
	}

	var ack chan error
	if data.Required {
		ack = make(chan error, 1)
	}
	shouldFlush, err := c.appendBuffer(ctx, bufferedData{data: data, ack: ack})
	if err != nil {
		return errors.Tag(err)
	}
	if shouldFlush {
		c.triggerFlush()
	}
	if !data.Required {
		return nil
	}

	select {
	case flushErr := <-ack:
		return errors.Tag(flushErr)
	case <-ctx.Done():
		return errors.Wrapf(ctx.Err(), "batchprocessor.collect 必达任务等待落地超时 bizType=%s", c.bizType)
	}
}

// appendBuffer 把数据写入有界内存缓冲区；缓冲满时按策略等待空间或快速失败。
func (c *collector) appendBuffer(ctx context.Context, item bufferedData) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	waitTimeout := c.policy.BufferFullWaitTimeout
	deadline := time.Time{}
	if waitTimeout > 0 {
		deadline = time.Now().Add(waitTimeout)
	}

	for {
		c.mu.Lock()
		if c.stopped {
			c.mu.Unlock()
			return false, errors.Errorf("batchprocessor.collector 已停止")
		}
		if c.bufferTotalLocked() < c.policy.MaxBufferSize {
			c.buffer = append(c.buffer, item)
			shouldFlush := len(c.buffer) >= c.policy.BatchSize
			c.notifySpaceLocked()
			c.mu.Unlock()
			return shouldFlush, nil
		}
		c.mu.Unlock()

		c.triggerFlush()
		if waitTimeout <= 0 {
			return false, errors.Errorf("batchprocessor.collector buffer 已满 bizType=%s max=%d", c.bizType, c.policy.MaxBufferSize)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, errors.Errorf("batchprocessor.collector buffer 等待空间超时 bizType=%s max=%d", c.bizType, c.policy.MaxBufferSize)
		}

		select {
		case <-c.spaceCh:
		case <-time.After(remaining):
			return false, errors.Errorf("batchprocessor.collector buffer 等待空间超时 bizType=%s max=%d", c.bizType, c.policy.MaxBufferSize)
		case <-ctx.Done():
			return false, errors.Wrapf(ctx.Err(), "batchprocessor.collect buffer 等待空间被取消 bizType=%s", c.bizType)
		}
	}
}

// runLoop 周期性触发 flush，并接收外部 TriggerFlush 信号。
func (c *collector) runLoop() {
	initialDelay := c.randDuration(c.policy.FlushInterval)
	timer := time.NewTimer(initialDelay)
	defer timer.Stop()
	for {
		select {
		case <-c.stopCh:
			_ = c.flushAll(c.ctx)
			return
		case <-timer.C:
			_ = c.flushAll(c.ctx)
			timer.Reset(c.nextInterval(c.policy.FlushInterval, c.policy.FlushJitter))
		case <-c.flushCh:
			_ = c.flushAll(c.ctx)
		}
	}
}

// nextInterval 基于 base + [0,jitter) 计算下一次周期触发间隔。
func (c *collector) nextInterval(base time.Duration, jitter time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	if jitter <= 0 {
		return base
	}
	delta := c.randDuration(jitter)
	return base + delta
}

// flushAll 尽力把 buffer 内数据按批次落地。
// flush 失败时：
// - 必达数据：逐条执行 RequiredFallback，并把结果回传给调用方
// - 非必达数据：回滚到 buffer 头部，等待下一次 flush 重试
func (c *collector) flushAll(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.flushMu.Lock()
	defer c.flushMu.Unlock()

	for {
		batch := c.popBatch()
		if len(batch) == 0 {
			return nil
		}

		items := make([]Data, 0, len(batch))
		for _, item := range batch {
			items = append(items, item.data)
		}

		release, err := c.acquireFlushSlot(ctx)
		if err != nil {
			c.requeuePoppedBatch(batch)
			return errors.Tag(err)
		}

		err = func() error {
			defer release()
			return c.flushBatch(ctx, items)
		}()

		if err != nil {
			requeue := make([]bufferedData, 0, len(batch))
			for _, item := range batch {
				if item.data.Required {
					c.scheduleRequiredFallback(ctx, item, err)
					continue
				}
				requeue = append(requeue, item)
			}

			if len(requeue) > 0 {
				c.requeuePartialBatch(len(requeue), requeue)
			}
			releaseBufferedDataBatch(batch)
			return errors.Tag(err)
		}

		for _, item := range batch {
			signalBufferedDataAck(item.ack, nil)
		}
		c.completePoppedBatch(len(batch))
		releaseBufferedDataBatch(batch)
	}
}

// flushBatch 执行业务批量落地，并把业务 panic 转成错误，避免后台 flush 协程异常退出。
func (c *collector) flushBatch(ctx context.Context, items []Data) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.Errorf("batchprocessor.Flush panic bizType=%s panic=%v", c.bizType, recovered)
		}
	}()
	return errors.Tag(c.module.Flush(ctx, items))
}

// requiredFallback 执行必达任务兜底落地，并把业务 panic 转成错误返回给调用方。
func (c *collector) requiredFallback(ctx context.Context, data Data, flushErr error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.Errorf("batchprocessor.RequiredFallback panic bizType=%s panic=%v", c.bizType, recovered)
		}
	}()
	return errors.Tag(c.module.RequiredFallback(ctx, data, flushErr))
}

// scheduleRequiredFallback 把必达任务兜底提交到异步执行链路，避免慢 fallback 长时间持有 flushMu。
// fallback 成功后释放 inFlight 容量；失败时把该条数据放回队头等待后续 flush 重试。
func (c *collector) scheduleRequiredFallback(ctx context.Context, item bufferedData, flushErr error) {
	c.fallbackWG.Add(1)
	go func() {
		defer c.fallbackWG.Done()
		fallbackErr := errors.Tag(c.runRequiredFallbackWithLimit(ctx, item.data, flushErr))
		signalBufferedDataAck(item.ack, fallbackErr)
		if fallbackErr != nil {
			item.ack = nil
			c.requeuePartialBatch(1, []bufferedData{item})
			return
		}
		c.completePoppedBatch(1)
	}()
}

// runRequiredFallbackWithLimit 执行必达任务兜底，并用小并发池保护降级链路。
func (c *collector) runRequiredFallbackWithLimit(ctx context.Context, data Data, flushErr error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case c.fallbackLimit <- struct{}{}:
		defer func() {
			<-c.fallbackLimit
		}()
	case <-ctx.Done():
		return errors.Wrapf(ctx.Err(), "batchprocessor.RequiredFallback 等待并发令牌失败 bizType=%s", c.bizType)
	}
	return errors.Tag(c.requiredFallback(ctx, data, flushErr))
}

// acquireFlushSlot 获取全局 flush 并发令牌，并尊重上下文取消。
func (c *collector) acquireFlushSlot(ctx context.Context) (func(), error) {
	if c.flushLimiter == nil {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case c.flushLimiter <- struct{}{}:
		return func() {
			<-c.flushLimiter
		}, nil
	case <-ctx.Done():
		return nil, errors.Wrap(ctx.Err(), "获取 flush 并发令牌失败")
	}
}

// requeuePoppedBatch 把整批未落地数据放回缓冲区头部，保持原始处理顺序。
func (c *collector) requeuePoppedBatch(batch []bufferedData) {
	if len(batch) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finishInFlightLocked(len(batch))
	c.buffer = append(batch, c.buffer...)
	c.notifySpaceLocked()
}

// requeuePartialBatch 把部分未落地数据放回缓冲区头部，并释放已落地/已兜底的数据容量。
func (c *collector) requeuePartialBatch(popped int, requeue []bufferedData) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finishInFlightLocked(popped)
	if len(requeue) > 0 {
		c.buffer = append(requeue, c.buffer...)
	}
	c.notifySpaceLocked()
}

// completePoppedBatch 标记一批已成功落地的数据完成，释放缓冲容量。
func (c *collector) completePoppedBatch(popped int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finishInFlightLocked(popped)
	c.notifySpaceLocked()
}

// popBatch 从 buffer 头部弹出一批数据（最多 policy.BatchSize 条）。
// 返回的是独立拷贝，避免后续 shift 影响引用。
func (c *collector) popBatch() []bufferedData {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.buffer) == 0 {
		return nil
	}
	n := c.policy.BatchSize
	if n <= 0 {
		n = 200
	}
	if len(c.buffer) < n {
		n = len(c.buffer)
	}
	batch := acquireBufferedDataBatch(n)
	copy(batch, c.buffer[:n])
	// 清空已弹出槽位，避免大 payload 在底层数组中被无意义持有。
	clear(c.buffer[:n])
	c.buffer = c.buffer[n:]
	c.inFlight += len(batch)
	return batch
}

// acquireBufferedDataBatch 从池中取出指定长度的批次数组。
func acquireBufferedDataBatch(size int) []bufferedData {
	if size <= 0 {
		return nil
	}
	batchPtr := bufferedDataBatchPool.Get().(*[]bufferedData)
	batch := *batchPtr
	if cap(batch) < size {
		batch = make([]bufferedData, size)
	} else {
		batch = batch[:size]
	}
	return batch
}

// releaseBufferedDataBatch 清空并回收批次数组；仍被 requeue 引用的切片不能调用该函数。
func releaseBufferedDataBatch(batch []bufferedData) {
	if cap(batch) == 0 {
		return
	}
	clear(batch)
	if cap(batch) > maxPolicyBatchSize {
		return
	}
	batch = batch[:0]
	bufferedDataBatchPool.Put(&batch)
}

// signalBufferedDataAck 非阻塞回传必达数据结果；调用方超时或已收到兜底错误时不能反向卡住 flush 链路。
func signalBufferedDataAck(ack chan error, err error) {
	if ack == nil {
		return
	}
	select {
	case ack <- err:
	default:
	}
}

// fallbackConcurrency 计算必达 fallback 并发度，复用单 bizType 的并发策略并受全局上限保护。
func fallbackConcurrency(policy Policy) int {
	concurrency := policy.ProcessConcurrency
	if concurrency <= 0 {
		return 1
	}
	if concurrency > maxPolicyProcessConcurrency {
		return maxPolicyProcessConcurrency
	}
	return concurrency
}

// bufferTotalLocked 返回当前内存缓冲和执行中批次占用的总容量；调用方必须已持有 c.mu。
func (c *collector) bufferTotalLocked() int {
	return len(c.buffer) + c.inFlight
}

// finishInFlightLocked 释放执行中批次容量；调用方必须已持有 c.mu。
func (c *collector) finishInFlightLocked(n int) {
	if n <= 0 {
		return
	}
	if c.inFlight >= n {
		c.inFlight -= n
		return
	}
	c.inFlight = 0
}

// notifySpaceLocked 非阻塞通知等待中的收集请求有机会重新检查容量；调用方必须已持有 c.mu。
func (c *collector) notifySpaceLocked() {
	if c.policy.MaxBufferSize > 0 && c.bufferTotalLocked() >= c.policy.MaxBufferSize {
		return
	}
	select {
	case c.spaceCh <- struct{}{}:
	default:
	}
}
