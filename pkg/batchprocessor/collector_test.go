package batchprocessor

import (
	"context"
	stdErrors "errors"
	"sync"
	"testing"
	"time"
)

// mockModule 是批处理测试使用的最小业务模块。
type mockModule struct {
	mu      sync.Mutex // 保护 flushed
	flushed []Data     // 已落地数据
}

// Validate 校验测试数据。
func (m *mockModule) Validate(context.Context, Data) error {
	return nil
}

// Flush 记录测试批次。
func (m *mockModule) Flush(_ context.Context, batch []Data) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushed = append(m.flushed, batch...)
	return nil
}

// RequiredFallback 模拟必达数据兜底。
func (m *mockModule) RequiredFallback(context.Context, Data, error) error {
	return nil
}

// Process 模拟批处理执行。
func (m *mockModule) Process(context.Context, string, int) (int, error) {
	return 0, nil
}

// panicFlushModule 用于验证业务 Flush panic 不会击穿收集器后台协程。
type panicFlushModule struct {
	mockModule
	fallbackCount int // fallbackCount 记录兜底落地次数
}

// Flush 模拟业务批量落地 panic。
func (m *panicFlushModule) Flush(context.Context, []Data) error {
	panic("flush panic")
}

// RequiredFallback 记录必达任务兜底次数。
func (m *panicFlushModule) RequiredFallback(context.Context, Data, error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fallbackCount++
	return nil
}

// slowFallbackModule 用于验证慢 fallback 不会阻塞 flush 主链路。
type slowFallbackModule struct {
	mockModule
	started chan struct{} // started 表示 fallback 已进入执行
	release chan struct{} // release 用于放行测试 fallback
}

// Flush 模拟批量落地失败。
func (m *slowFallbackModule) Flush(context.Context, []Data) error {
	return stdErrors.New("flush failed")
}

// RequiredFallback 阻塞到测试显式放行，用于观察 flushAll 是否提前返回。
func (m *slowFallbackModule) RequiredFallback(context.Context, Data, error) error {
	close(m.started)
	<-m.release
	return nil
}

// TestCollectorRejectsCollectAfterStop 确保收集器停止后不会继续接收新数据。
func TestCollectorRejectsCollectAfterStop(t *testing.T) {
	module := &mockModule{}
	collector := newCollector("demo", module, Policy{BatchSize: 2}, nil, nil)
	collector.start()

	if err := collector.stop(context.Background()); err != nil {
		t.Fatalf("停止收集器失败: %v", err)
	}
	if err := collector.collect(context.Background(), Data{Action: "insert"}); err == nil {
		t.Fatal("期望停止后的收集请求返回错误")
	}
}

// TestCollectorRecoversFlushPanicForRequiredData 确保必达数据在 Flush panic 时会走兜底且调用方不会被 panic 击穿。
func TestCollectorRecoversFlushPanicForRequiredData(t *testing.T) {
	module := &panicFlushModule{}
	collector := newCollector("demo", module, Policy{
		BatchSize:     1,
		FlushInterval: time.Hour,
	}, nil, nil)
	collector.start()
	defer func() {
		_ = collector.stop(context.Background())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := collector.collect(ctx, Data{Action: "insert", Required: true}); err != nil {
		t.Fatalf("Flush panic 后必达兜底成功时不应返回错误: %v", err)
	}

	module.mu.Lock()
	defer module.mu.Unlock()
	if module.fallbackCount != 1 {
		t.Fatalf("期望必达兜底执行 1 次，实际为 %d", module.fallbackCount)
	}
}

// TestCollectorSchedulesRequiredFallbackAsync 确保必达 fallback 慢时不会长时间占用 flushMu。
func TestCollectorSchedulesRequiredFallbackAsync(t *testing.T) {
	module := &slowFallbackModule{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	collector := newCollector("demo", module, Policy{
		BatchSize:     1,
		FlushInterval: time.Hour,
	}, nil, nil)
	if _, err := collector.appendBuffer(context.Background(), bufferedData{data: Data{Action: "insert", Required: true}, ack: make(chan error, 1)}); err != nil {
		t.Fatalf("写入测试 buffer 失败: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- collector.flushAll(context.Background())
	}()

	select {
	case <-module.started:
	case <-time.After(time.Second):
		t.Fatal("fallback 未启动")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("flushAll 应返回原始 flush 错误")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("flushAll 被慢 fallback 阻塞")
	}
	close(module.release)
	if err := collector.wait(context.Background()); err != nil {
		t.Fatalf("等待 fallback 退出失败: %v", err)
	}
}

// TestCollectorDoesNotBlockWhenAckAlreadySignaled 确保调用方超时或已收到兜底错误后，后续 flush 不会被满 ack 通道卡住。
func TestCollectorDoesNotBlockWhenAckAlreadySignaled(t *testing.T) {
	module := &mockModule{}
	collector := newCollector("demo", module, Policy{
		BatchSize:     1,
		FlushInterval: time.Hour,
	}, nil, nil)
	ack := make(chan error, 1)
	ack <- context.DeadlineExceeded
	if _, err := collector.appendBuffer(context.Background(), bufferedData{
		data: Data{Action: "insert", Required: true},
		ack:  ack,
	}); err != nil {
		t.Fatalf("写入测试 buffer 失败: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- collector.flushAll(context.Background())
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("flushAll 不应因为 ack 已满而失败: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("flushAll 被已满 ack 通道阻塞")
	}
}

// TestCollectorStopFlushesBufferedData 确保停止时会尽力刷出已有缓冲数据。
func TestCollectorStopFlushesBufferedData(t *testing.T) {
	module := &mockModule{}
	collector := newCollector("demo", module, Policy{BatchSize: 10}, nil, nil)
	collector.start()

	if err := collector.collect(context.Background(), Data{Action: "insert"}); err != nil {
		t.Fatalf("收集数据失败: %v", err)
	}
	if err := collector.stop(context.Background()); err != nil {
		t.Fatalf("停止收集器失败: %v", err)
	}

	module.mu.Lock()
	defer module.mu.Unlock()
	if len(module.flushed) != 1 {
		t.Fatalf("期望停止时刷出 1 条数据，实际为 %d", len(module.flushed))
	}
}

// TestCollectorStopHonorsContextWhenLimiterBlocked 确保 flush 限流器被占满时停止逻辑会尊重超时。
func TestCollectorStopHonorsContextWhenLimiterBlocked(t *testing.T) {
	module := &mockModule{}
	limiter := make(chan struct{}, 1)
	limiter <- struct{}{}
	collector := newCollector("demo", module, Policy{BatchSize: 10}, limiter, nil)
	collector.start()
	if err := collector.collect(context.Background(), Data{Action: "insert"}); err != nil {
		t.Fatalf("收集数据失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := collector.stop(ctx); err == nil {
		t.Fatal("期望限流器阻塞时停止返回超时错误")
	}
}

// TestCollectorRejectsWhenBufferFull 确保缓冲区达到上限时不会继续无限增长。
func TestCollectorRejectsWhenBufferFull(t *testing.T) {
	module := &mockModule{}
	collector := newCollector("demo", module, Policy{
		BatchSize:     1,
		MaxBufferSize: 1,
		FlushInterval: time.Hour,
		FlushJitter:   0,
	}, nil, nil)

	if err := collector.collect(context.Background(), Data{Action: "insert"}); err != nil {
		t.Fatalf("第一次收集数据失败: %v", err)
	}
	if err := collector.collect(context.Background(), Data{Action: "insert"}); err == nil {
		t.Fatal("期望缓冲满时返回错误")
	}
}

// TestCollectorWaitsForBufferSpace 确保配置等待时间后，缓冲区释放空间时收集请求可以继续完成。
func TestCollectorWaitsForBufferSpace(t *testing.T) {
	module := &mockModule{}
	collector := newCollector("demo", module, Policy{
		BatchSize:             1,
		MaxBufferSize:         1,
		FlushInterval:         time.Hour,
		BufferFullWaitTimeout: 500 * time.Millisecond,
	}, nil, nil)

	if err := collector.collect(context.Background(), Data{Action: "insert"}); err != nil {
		t.Fatalf("第一次收集数据失败: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- collector.collect(context.Background(), Data{Action: "update"})
	}()

	time.Sleep(20 * time.Millisecond)
	if err := collector.flushAll(context.Background()); err != nil {
		t.Fatalf("手动 flush 失败: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("释放空间后收集仍失败: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("释放空间后收集请求未返回")
	}
}
