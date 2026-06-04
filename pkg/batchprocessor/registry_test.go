package batchprocessor

import (
	"context"
	"testing"
)

// TestPolicyNormalizeCapsUnsafeValues 验证策略归一化会夹住危险配置，避免误配置造成内存或协程放大。
func TestPolicyNormalizeCapsUnsafeValues(t *testing.T) {
	policy := Policy{
		BatchSize:          maxPolicyBatchSize + 1,
		MaxBufferSize:      maxPolicyBufferSize + 1,
		ProcessBatchSize:   maxPolicyBatchSize + 2,
		ProcessConcurrency: maxPolicyProcessConcurrency + 1,
		FlushInterval:      1,
		ProcessInterval:    1,
	}
	policy.normalize()

	if policy.BatchSize != maxPolicyBatchSize {
		t.Fatalf("BatchSize = %d, want %d", policy.BatchSize, maxPolicyBatchSize)
	}
	if policy.MaxBufferSize != maxPolicyBufferSize {
		t.Fatalf("MaxBufferSize = %d, want %d", policy.MaxBufferSize, maxPolicyBufferSize)
	}
	if policy.ProcessBatchSize != maxPolicyBatchSize {
		t.Fatalf("ProcessBatchSize = %d, want %d", policy.ProcessBatchSize, maxPolicyBatchSize)
	}
	if policy.ProcessConcurrency != maxPolicyProcessConcurrency {
		t.Fatalf("ProcessConcurrency = %d, want %d", policy.ProcessConcurrency, maxPolicyProcessConcurrency)
	}
	if policy.FlushInterval != minPolicyInterval || policy.ProcessInterval != minPolicyInterval {
		t.Fatalf("周期最小值未正确归一化: flush=%s process=%s", policy.FlushInterval, policy.ProcessInterval)
	}
}

// TestRegistryIsStarted 确认注册中心启动状态可被上层可靠判断。
func TestRegistryIsStarted(t *testing.T) {
	registry := NewRegistry(RegistryConfig{Enabled: true})
	if registry.IsStarted() {
		t.Fatal("新建注册中心不应处于启动状态")
	}

	registry.Start()
	if !registry.IsStarted() {
		t.Fatal("Start 后注册中心应处于启动状态")
	}

	if err := registry.Stop(context.Background()); err != nil {
		t.Fatalf("停止注册中心失败: %v", err)
	}
	if registry.IsStarted() {
		t.Fatal("Stop 后注册中心不应处于启动状态")
	}
}

// TestRegistryCollectRejectsBeforeStart 确保未启动时不会把非必达数据静默堆入内存缓冲。
func TestRegistryCollectRejectsBeforeStart(t *testing.T) {
	registry := NewRegistry(RegistryConfig{Enabled: true})
	if err := registry.Register("demo", &mockModule{}, Policy{}); err != nil {
		t.Fatalf("注册测试模块失败: %v", err)
	}
	if err := registry.Collect(context.Background(), "demo", Data{Action: "insert"}); err == nil {
		t.Fatal("期望未启动 Registry 时 Collect 返回错误")
	}
}
