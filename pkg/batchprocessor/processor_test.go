package batchprocessor

import (
	"context"
	"strings"
	"testing"
	"time"
)

// panicProcessModule 用于验证业务 Process panic 会被执行器转成错误。
type panicProcessModule struct {
	mockModule // 嵌入字段表示测试复用的基础能力。
}

// Process 模拟业务批处理 panic。
func (m *panicProcessModule) Process(context.Context, string, int) (int, error) {
	panic("process panic")
}

// TestProcessorRunOnceRecoversProcessPanic 确保业务 Process panic 不会击穿 worker。
func TestProcessorRunOnceRecoversProcessPanic(t *testing.T) {
	processor := newProcessor("demo", &panicProcessModule{}, Policy{
		ProcessEnabled:     true,
		ProcessBatchSize:   1,
		ProcessConcurrency: 1,
		ProcessInterval:    time.Hour,
	}, nil, nil)

	processed, err := processor.runOnce(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "panic") {
		t.Fatalf("期望 Process panic 被转成错误，processed=%d err=%v", processed, err)
	}
}

// TestProcessorStopHonorsContextWhenLimiterBlocked 确保处理器等待限流令牌时可被停止上下文打断。
func TestProcessorStopHonorsContextWhenLimiterBlocked(t *testing.T) {
	module := &mockModule{}
	limiter := make(chan struct{}, 1)
	limiter <- struct{}{}
	processor := newProcessor("demo", module, Policy{
		ProcessEnabled:     true,
		ProcessBatchSize:   1,
		ProcessConcurrency: 1,
		ProcessInterval:    time.Hour,
	}, limiter, nil)
	processor.start()
	processor.trigger()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := processor.stop(ctx); err != nil {
		t.Fatalf("处理器停止应被取消上下文唤醒，实际错误: %v", err)
	}
}
