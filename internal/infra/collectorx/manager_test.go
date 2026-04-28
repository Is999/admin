package collectorx

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"admin/internal/config"
)

// TestProcessorFuncProcessBatch 确保业务方可以直接用函数注册批量消费逻辑。
func TestProcessorFuncProcessBatch(t *testing.T) {
	processor := ProcessorFunc(func(ctx context.Context, events []Event) ([]ProcessResult, error) {
		if ctx == nil {
			t.Fatal("期望上下文不为空")
		}
		if len(events) != 2 {
			t.Fatalf("期望收到 2 条事件，实际 %d", len(events))
		}
		return nil, nil
	})

	if _, err := processor.ProcessBatch(context.Background(), []Event{
		{EventID: "e1", BizType: "biz", Payload: json.RawMessage(`{"id":1}`)},
		{EventID: "e2", BizType: "biz", Payload: json.RawMessage(`{"id":2}`)},
	}); err != nil {
		t.Fatalf("批量消费函数返回错误: %v", err)
	}
}

// TestProcessBatchTreatsEmptyResultAsSuccess 确保 Processor 返回空结果且无错误时，整批事件被视为成功。
func TestProcessBatchTreatsEmptyResultAsSuccess(t *testing.T) {
	manager := &Manager{processors: make(map[string]Processor)}
	if err := manager.RegisterProcessorFunc("biz", func(context.Context, []Event) ([]ProcessResult, error) {
		return nil, nil
	}); err != nil {
		t.Fatalf("注册 Processor 失败: %v", err)
	}

	success, failed := manager.processBatch(context.Background(), []Event{
		{EventID: "e1", BizType: "biz"},
		{EventID: "e2", BizType: "biz"},
	})
	if len(success) != 2 || len(failed) != 0 {
		t.Fatalf("期望整批成功，success=%v failed=%v", success, failed)
	}
}

// TestProcessBatchUsesPerEventResult 确保 Processor 可以精确表达部分成功、部分失败和漏返回结果。
func TestProcessBatchUsesPerEventResult(t *testing.T) {
	manager := &Manager{processors: make(map[string]Processor)}
	if err := manager.RegisterProcessorFunc("biz", func(context.Context, []Event) ([]ProcessResult, error) {
		return []ProcessResult{
			{EventID: "e1", Success: true},
			{EventID: "e2", Success: false, Error: "duplicate"},
			{EventID: "unknown", Success: true},
			{EventID: "e1", Success: false, Error: "duplicate result"},
		}, nil
	}); err != nil {
		t.Fatalf("注册 Processor 失败: %v", err)
	}

	success, failed := manager.processBatch(context.Background(), []Event{
		{EventID: "e1", BizType: "biz"},
		{EventID: "e2", BizType: "biz"},
		{EventID: "e3", BizType: "biz"},
	})
	if _, ok := success["e1"]; !ok {
		t.Fatalf("期望 e1 成功，success=%v", success)
	}
	if failed["e2"] != "duplicate" {
		t.Fatalf("期望 e2 返回业务失败原因，failed=%v", failed)
	}
	if failed["e3"] == "" {
		t.Fatalf("期望 e3 因 Processor 漏返回而失败，failed=%v", failed)
	}
	if _, ok := success["unknown"]; ok {
		t.Fatalf("未知事件不应被计入成功集合，success=%v", success)
	}
}

// TestProcessBatchFailsUnregisteredBizType 确保未注册 bizType 不会被静默吞掉。
func TestProcessBatchFailsUnregisteredBizType(t *testing.T) {
	manager := &Manager{processors: make(map[string]Processor)}

	success, failed := manager.processBatch(context.Background(), []Event{
		{EventID: "e1", BizType: "missing"},
	})
	if len(success) != 0 {
		t.Fatalf("未注册 bizType 不应成功，success=%v", success)
	}
	if failed["e1"] == "" {
		t.Fatalf("未注册 bizType 应返回失败原因，failed=%v", failed)
	}
}

// TestProcessBatchRecoversProcessorPanic 确保业务 Processor panic 会转成失败结果，不会击穿 DB worker。
func TestProcessBatchRecoversProcessorPanic(t *testing.T) {
	manager := &Manager{processors: make(map[string]Processor)}
	if err := manager.RegisterProcessorFunc("biz", func(context.Context, []Event) ([]ProcessResult, error) {
		panic("processor panic")
	}); err != nil {
		t.Fatalf("注册 Processor 失败: %v", err)
	}

	success, failed := manager.processBatch(context.Background(), []Event{
		{EventID: "e1", BizType: "biz"},
	})
	if len(success) != 0 {
		t.Fatalf("Processor panic 不应成功，success=%v", success)
	}
	if !strings.Contains(failed["e1"], "panic") {
		t.Fatalf("期望失败原因包含 panic，failed=%v", failed)
	}
}

// TestCollectorBatchSizesAreBounded 确保误配置超大批次会被夹到安全上限，避免单轮占用过多内存和 DB 资源。
func TestCollectorBatchSizesAreBounded(t *testing.T) {
	manager := &Manager{cfg: config.CollectorConfig{
		Kafka: config.CollectorKafkaConfig{BatchSize: maxCollectorCarrierBatchSize + 100},
		Redis: config.CollectorRedisConfig{Count: int64(maxCollectorCarrierBatchSize + 200)},
		DB:    config.CollectorDBConfig{RunnerBatchSize: maxCollectorCarrierBatchSize + 300},
	}}
	if got := manager.kafkaFetchBatchSize(); got != maxCollectorCarrierBatchSize {
		t.Fatalf("kafkaFetchBatchSize = %d, want %d", got, maxCollectorCarrierBatchSize)
	}
	if got := manager.outboxInsertBatchSize(); got != maxCollectorCarrierBatchSize {
		t.Fatalf("outboxInsertBatchSize = %d, want %d", got, maxCollectorCarrierBatchSize)
	}
	if got := manager.dbRunnerBatchSize(); got != maxCollectorCarrierBatchSize {
		t.Fatalf("dbRunnerBatchSize = %d, want %d", got, maxCollectorCarrierBatchSize)
	}
	if got := boundedPositiveInt64(manager.cfg.Redis.Count, 100, maxCollectorCarrierBatchSize); got != maxCollectorCarrierBatchSize {
		t.Fatalf("redis count = %d, want %d", got, maxCollectorCarrierBatchSize)
	}
}

// TestNormalizeCollectorTransport 确保 transport 配置统一走常量和归一化逻辑，避免大小写或空值改变降级链路。
func TestNormalizeCollectorTransport(t *testing.T) {
	cases := []struct {
		name      string
		transport string
		want      string
	}{
		{name: "empty", transport: "", want: collectorTransportAuto},
		{name: "blank", transport: "  ", want: collectorTransportAuto},
		{name: "kafka uppercase", transport: " Kafka ", want: collectorTransportKafka},
		{name: "redis", transport: collectorTransportRedis, want: collectorTransportRedis},
		{name: "db", transport: collectorTransportDB, want: collectorTransportDB},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeCollectorTransport(tc.transport); got != tc.want {
				t.Fatalf("normalizeCollectorTransport(%q) = %q, want %q", tc.transport, got, tc.want)
			}
		})
	}
}

// TestDeliveriesTransportLabel 确保批量 outbox 指标标签使用统一 transport 常量，空值和混合来源有稳定兜底。
func TestDeliveriesTransportLabel(t *testing.T) {
	cases := []struct {
		name       string
		deliveries []eventDelivery
		want       string
	}{
		{name: "empty", deliveries: nil, want: collectorTransportUnknown},
		{name: "blank", deliveries: []eventDelivery{{transport: ""}}, want: collectorTransportUnknown},
		{name: "same kafka", deliveries: []eventDelivery{{transport: collectorTransportKafka}, {transport: collectorTransportKafka}}, want: collectorTransportKafka},
		{name: "mixed", deliveries: []eventDelivery{{transport: collectorTransportKafka}, {transport: collectorTransportRedis}}, want: collectorTransportMixed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deliveriesTransportLabel(tc.deliveries); got != tc.want {
				t.Fatalf("deliveriesTransportLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}
