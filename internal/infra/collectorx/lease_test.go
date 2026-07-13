package collectorx

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestLeaseHeartbeatStopWaitsInflightRenew 验证正常停止不会取消已开始的续租 CAS。
func TestLeaseHeartbeatStopWaitsInflightRenew(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	heartbeat := startLeaseHeartbeat(context.Background(), 10*time.Millisecond, func() {}, func(ctx context.Context) error {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("等待续租开始超时")
	}
	result := make(chan error, 1)
	go func() { result <- heartbeat.StopAndRenew() }()
	select {
	case err := <-result:
		t.Fatalf("在途续租结束前 StopAndRenew 不应返回: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("StopAndRenew() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("等待 StopAndRenew 返回超时")
	}
	if calls.Load() < 2 {
		t.Fatalf("期望在途续租和最终续租各执行一次，calls=%d", calls.Load())
	}
}

// TestProcessBizBatchWaitsForCancelledProcessorReturn 验证取消不会让仍在执行的 Processor 变成孤儿协程。
func TestProcessBizBatchWaitsForCancelledProcessorReturn(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	manager := &Manager{}
	processor := ProcessorFunc(func(context.Context, []Event) ([]ProcessResult, error) {
		close(started)
		<-release
		return nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := manager.processBizBatch(ctx, "biz", processor, []Event{{BizType: "biz", EventID: "event-1"}})
		result <- err
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		t.Fatalf("Processor 返回前不应提前结束并留下孤儿协程: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("父上下文取消后应返回取消错误")
		}
	case <-time.After(time.Second):
		t.Fatal("等待 Processor 返回超时")
	}
}
