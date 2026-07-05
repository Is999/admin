package collectorx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"admin/common/runtimecfg"
	"admin/internal/config"
	"admin/internal/model"

	"github.com/Is999/go-utils/errors"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestRuntimeRegistrySpecsValid 确保 Collector 运行时注册入口规格完整且名称唯一。
func TestRuntimeRegistrySpecsValid(t *testing.T) {
	specs := RuntimeRegistrySpecs()
	if len(specs) == 0 {
		t.Fatal("RuntimeRegistrySpecs() 不能为空")
	}
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if spec.Name == "" || spec.File == "" || spec.Method == "" || spec.Description == "" {
			t.Fatalf("运行时注册入口规格字段不完整: %+v", spec)
		}
		if _, ok := seen[spec.Name]; ok {
			t.Fatalf("运行时注册入口名称重复: %s", spec.Name)
		}
		seen[spec.Name] = struct{}{}
	}
}

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

// TestNewAllowsNilRedisWhenIdempotencyDisabled 确保未启用任务去重时 Collector 不把 Redis 变成强依赖。
func TestNewAllowsNilRedisWhenIdempotencyDisabled(t *testing.T) {
	manager, err := New(config.CollectorConfig{Enabled: true}, newCollectorDryRunDB(t), nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if manager == nil {
		t.Fatal("期望返回 Collector manager")
	}
}

// TestNewRequiresRedisWhenAnyTaskIdempotencyEnabled 确保只要存在任务去重需求就必须提供 Redis。
func TestNewRequiresRedisWhenAnyTaskIdempotencyEnabled(t *testing.T) {
	enabled := true
	_, err := New(config.CollectorConfig{
		Enabled: true,
		Tasks: map[string]config.CollectorTaskConfig{
			"biz": {IdempotencyEnabled: &enabled},
		},
	}, newCollectorDryRunDB(t), nil)
	if err == nil || !strings.Contains(err.Error(), "Redis") {
		t.Fatalf("期望开启任务去重但 Redis 缺失时返回错误，实际 err=%v", err)
	}
}

// TestEnqueueKafkaSuccessDoesNotRequireFailureDB 确保 Kafka 投递成功不会写 DB 失败账本。
func TestEnqueueKafkaSuccessDoesNotRequireFailureDB(t *testing.T) {
	writer := &fakeKafkaWriter{}
	manager := &Manager{
		cfg:      collectorTestConfigForTopic("collector_events"),
		writers:  map[string]kafkaMessageWriter{"collector_events": writer},
		failures: nil,
	}

	eventID, err := manager.Enqueue(context.Background(), Event{
		EventID:      "event-1",
		BizType:      "biz",
		PartitionKey: "user-1",
		Payload:      json.RawMessage(`{"id":1}`),
	})
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if eventID != "event-1" {
		t.Fatalf("eventID = %q, want event-1", eventID)
	}
	if len(writer.messages) != 1 {
		t.Fatalf("Kafka 写入数量 = %d, want 1", len(writer.messages))
	}
	if string(writer.messages[0].Key) != "biz:user-1" {
		t.Fatalf("Kafka key = %q, want biz:user-1", string(writer.messages[0].Key))
	}
}

// TestEnqueueRoutesByTaskTopic 确保不同 bizType 按任务配置写入不同 Kafka Topic。
func TestEnqueueRoutesByTaskTopic(t *testing.T) {
	adminWriter := &fakeKafkaWriter{}
	statWriter := &fakeKafkaWriter{}
	manager := &Manager{
		cfg: config.CollectorConfig{
			Enabled: true,
			Kafka:   config.CollectorKafkaConfig{WriteTimeout: 1},
			DefaultTask: config.CollectorTaskConfig{
				Topic:   "collector_default",
				GroupID: "collector_default_group",
			},
			Tasks: map[string]config.CollectorTaskConfig{
				"admin_log.audit": {Topic: "admin_log_events", GroupID: "admin_log_collector"},
				"user_stat":       {Topic: "user_stat_events", GroupID: "user_stat_collector"},
			},
		},
		writers: map[string]kafkaMessageWriter{
			"admin_log_events": adminWriter,
			"user_stat_events": statWriter,
		},
	}

	if _, err := manager.Enqueue(context.Background(), Event{EventID: "admin-1", BizType: "admin_log.audit"}); err != nil {
		t.Fatalf("投递 admin_log.audit 失败: %v", err)
	}
	if _, err := manager.Enqueue(context.Background(), Event{EventID: "stat-1", BizType: "user_stat"}); err != nil {
		t.Fatalf("投递 user_stat 失败: %v", err)
	}
	if len(adminWriter.messages) != 1 || len(statWriter.messages) != 1 {
		t.Fatalf("任务 Topic 路由不符合预期 admin=%d stat=%d", len(adminWriter.messages), len(statWriter.messages))
	}
}

// TestEnqueueRequiresStableEventID 确保调用方必须提供稳定幂等键，避免框架生成随机 ID 导致重复落库。
func TestEnqueueRequiresStableEventID(t *testing.T) {
	writer := &fakeKafkaWriter{}
	manager := &Manager{
		cfg:     collectorTestConfigForTopic("collector_events"),
		writers: map[string]kafkaMessageWriter{"collector_events": writer},
	}

	if _, err := manager.Enqueue(context.Background(), Event{BizType: "biz"}); err == nil || !strings.Contains(err.Error(), "eventId") {
		t.Fatalf("期望 EventID 为空时返回校验错误，实际 err=%v", err)
	}
	if len(writer.messages) != 0 {
		t.Fatalf("EventID 为空不应写 Kafka，messages=%d", len(writer.messages))
	}
}

// TestEnqueueKafkaFailureReturnsError 确保 Kafka 写入失败直接返回错误。
func TestEnqueueKafkaFailureReturnsError(t *testing.T) {
	manager := &Manager{
		cfg:     collectorTestConfigForTopic("collector_events"),
		writers: map[string]kafkaMessageWriter{"collector_events": &fakeKafkaWriter{err: errors.Errorf("kafka down")}},
	}

	if _, err := manager.Enqueue(context.Background(), Event{EventID: "event-1", BizType: "biz"}); err == nil {
		t.Fatal("期望 Kafka 写入失败时 Enqueue 返回错误")
	}
}

// TestKafkaMessageKeyIncludesBizType 确保 Kafka 分区键同时包含任务类型和业务分区，隔离不同任务的相同业务键。
func TestKafkaMessageKeyIncludesBizType(t *testing.T) {
	if got := kafkaMessageKey(Event{BizType: " user_stat ", PartitionKey: " 10001 "}); got != "user_stat:10001" {
		t.Fatalf("kafkaMessageKey() = %q, want user_stat:10001", got)
	}
	if got := kafkaMessageKey(Event{BizType: "user_stat"}); got != "user_stat" {
		t.Fatalf("kafkaMessageKey() = %q, want user_stat", got)
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

// TestProcessBatchIsolatesBizTypesInStableOrder 确保重试批次按任务隔离且调度顺序稳定。
func TestProcessBatchIsolatesBizTypesInStableOrder(t *testing.T) {
	manager := &Manager{processors: make(map[string]Processor)}
	calls := make([]string, 0, 2)
	register := func(bizType string) {
		t.Helper()
		if err := manager.RegisterProcessorFunc(bizType, func(_ context.Context, events []Event) ([]ProcessResult, error) {
			ids := make([]string, 0, len(events))
			for _, event := range events {
				ids = append(ids, event.EventID)
			}
			calls = append(calls, bizType+":"+strings.Join(ids, ","))
			return nil, nil
		}); err != nil {
			t.Fatalf("注册 Processor 失败: %v", err)
		}
	}
	register("biz-b")
	register("biz-a")

	success, failed := manager.processBatch(context.Background(), []Event{
		{EventID: "b1", BizType: "biz-b"},
		{EventID: "a1", BizType: "biz-a"},
		{EventID: "b2", BizType: "biz-b"},
	})
	if len(success) != 3 || len(failed) != 0 {
		t.Fatalf("期望全部成功，success=%v failed=%v", success, failed)
	}
	if got := strings.Join(calls, "|"); got != "biz-b:b1,b2|biz-a:a1" {
		t.Fatalf("Processor 调用顺序 = %q, want biz-b:b1,b2|biz-a:a1", got)
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

// TestProcessBatchReportsMissingProcessor 确保未注册 Processor 的后台失败会触发运行异常 hook。
func TestProcessBatchReportsMissingProcessor(t *testing.T) {
	alertCh := make(chan RuntimeAlert, 1)
	manager := &Manager{processors: make(map[string]Processor)}
	manager.SetAlertHook(func(_ context.Context, alert RuntimeAlert) {
		select {
		case alertCh <- alert:
		default:
		}
	})

	_, failed := manager.processBatch(context.Background(), []Event{{EventID: "e1", BizType: "missing"}})
	if failed["e1"] == "" {
		t.Fatalf("未注册 bizType 应返回失败原因，failed=%v", failed)
	}
	select {
	case alert := <-alertCh:
		if alert.Kind != RuntimeAlertKindWorkerFailed || alert.Operation != "process_missing_processor" || alert.BizType != "missing" {
			t.Fatalf("Collector 运行异常告警不符合预期: %+v", alert)
		}
	default:
		t.Fatal("期望未注册 Processor 触发 Collector 运行异常告警")
	}
}

// TestProcessBatchRecoversProcessorPanic 确保业务 Processor panic 会转成失败结果，不会击穿 worker。
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

// TestFailureSeedsFromResultsOnlyKeepsFailedEvents 确保 Kafka 部分失败只生成失败事件账本记录。
func TestFailureSeedsFromResultsOnlyKeepsFailedEvents(t *testing.T) {
	manager := &Manager{}
	now := time.Date(2026, 7, 6, 10, 0, 0, 0, time.Local)
	seeds := manager.failureSeedsFromResults([]Event{
		{EventID: "ok", BizType: "biz", Payload: json.RawMessage(`{"ok":true}`)},
		{EventID: "failed", BizType: "biz", Payload: json.RawMessage(`{"ok":false}`)},
	}, map[string]string{"failed": "write failed"}, now)
	if len(seeds) != 1 {
		t.Fatalf("失败账本记录数量 = %d, want 1", len(seeds))
	}
	if seeds[0].event.EventID != "failed" || seeds[0].state != model.CollectorFailedEventStateRetry || seeds[0].attempt != 1 {
		t.Fatalf("失败账本记录不符合预期: %+v", seeds[0])
	}
}

// TestInvalidKafkaFailureCreatesDeadEvent 确保坏消息进入死信状态，不会进入业务 Processor。
func TestInvalidKafkaFailureCreatesDeadEvent(t *testing.T) {
	manager := &Manager{cfg: config.CollectorConfig{FailureRetry: config.CollectorFailureRetryConfig{MaxRetryTimes: 6}}}
	seed := manager.invalidKafkaFailure(kafka.Message{Topic: "topic", Partition: 2, Offset: 9, Value: []byte("{")}, errors.Errorf("invalid json"))
	if seed.state != model.CollectorFailedEventStateDead || seed.attempt != 6 {
		t.Fatalf("坏消息死信状态不符合预期: %+v", seed)
	}
	if seed.event.BizType != "collector.invalid" || !strings.HasPrefix(seed.event.EventID, "collector:invalid:") {
		t.Fatalf("坏消息合成事件不符合预期: %+v", seed.event)
	}
}

// TestHandleKafkaBatchDeduplicatesEventIDWithinBatch 确保同批重复 EventID 只进入 Processor 一次。
func TestHandleKafkaBatchDeduplicatesEventIDWithinBatch(t *testing.T) {
	manager := &Manager{
		cfg:          configWithIdempotencyForTest(),
		idempotency:  newTestIdempotencyStore(t),
		processors:   make(map[string]Processor),
		runtimeStats: newCollectorRuntimeStats(),
	}
	calls := 0
	if err := manager.RegisterProcessorFunc("biz", func(_ context.Context, events []Event) ([]ProcessResult, error) {
		calls++
		if got := kafkaLaneEventIDs(events); got != "e1" {
			t.Fatalf("Processor 事件 = %q, want e1", got)
		}
		return nil, nil
	}); err != nil {
		t.Fatalf("注册 Processor 失败: %v", err)
	}

	err := manager.handleKafkaBatch(context.Background(), kafkaBatch{
		messages: []kafka.Message{{Offset: 1}, {Offset: 2}},
		events: []Event{
			{EventID: "e1", BizType: "biz"},
			{EventID: "e1", BizType: "biz"},
		},
	})
	if err != nil {
		t.Fatalf("handleKafkaBatch() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("Processor 调用次数 = %d, want 1", calls)
	}
	snapshot := manager.RuntimeMetricsSnapshot()
	if snapshot.Totals.Duplicate != 1 || snapshot.Totals.Processed != 1 {
		t.Fatalf("运行态去重指标不符合预期: %+v", snapshot.Totals)
	}
}

// TestHandleKafkaBatchSkipsIdempotencyWhenTaskDisabled 确保未开启幂等的任务不访问 Redis 且不拦截重复 EventID。
func TestHandleKafkaBatchSkipsIdempotencyWhenTaskDisabled(t *testing.T) {
	manager := &Manager{
		cfg:          configEnabledForRuntimeMetricTest(),
		processors:   make(map[string]Processor),
		runtimeStats: newCollectorRuntimeStats(),
	}
	calls := 0
	if err := manager.RegisterProcessorFunc("biz", func(_ context.Context, events []Event) ([]ProcessResult, error) {
		calls++
		if got := kafkaLaneEventIDs(events); got != "e1" {
			t.Fatalf("Processor 事件 = %q, want e1", got)
		}
		return nil, nil
	}); err != nil {
		t.Fatalf("注册 Processor 失败: %v", err)
	}

	batch := kafkaBatch{
		messages: []kafka.Message{{Offset: 1}},
		events:   []Event{{EventID: "e1", BizType: "biz"}},
	}
	if err := manager.handleKafkaBatch(context.Background(), batch); err != nil {
		t.Fatalf("first handleKafkaBatch() error = %v", err)
	}
	if err := manager.handleKafkaBatch(context.Background(), batch); err != nil {
		t.Fatalf("second handleKafkaBatch() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("未开启幂等时重复事件应继续进入 Processor，调用次数 = %d, want 2", calls)
	}
	if snapshot := manager.RuntimeMetricsSnapshot(); snapshot.Totals.Duplicate != 0 {
		t.Fatalf("未开启幂等时不应记录 duplicate 指标: %+v", snapshot.Totals)
	}
}

// TestHandleKafkaBatchSkipsAlreadyDoneEventID 确保 Kafka 重投已完成事件时不会再次调用 Processor。
func TestHandleKafkaBatchSkipsAlreadyDoneEventID(t *testing.T) {
	manager := &Manager{
		cfg:          configWithIdempotencyForTest(),
		idempotency:  newTestIdempotencyStore(t),
		processors:   make(map[string]Processor),
		runtimeStats: newCollectorRuntimeStats(),
	}
	calls := 0
	if err := manager.RegisterProcessorFunc("biz", func(_ context.Context, events []Event) ([]ProcessResult, error) {
		calls++
		if len(events) != 1 || events[0].EventID != "e1" {
			t.Fatalf("Processor 事件不符合预期: %+v", events)
		}
		return nil, nil
	}); err != nil {
		t.Fatalf("注册 Processor 失败: %v", err)
	}

	first := kafkaBatch{
		messages: []kafka.Message{{Offset: 1}},
		events:   []Event{{EventID: "e1", BizType: "biz"}},
	}
	second := kafkaBatch{
		messages: []kafka.Message{{Offset: 2}},
		events:   []Event{{EventID: "e1", BizType: "biz"}},
	}
	if err := manager.handleKafkaBatch(context.Background(), first); err != nil {
		t.Fatalf("first handleKafkaBatch() error = %v", err)
	}
	if err := manager.handleKafkaBatch(context.Background(), second); err != nil {
		t.Fatalf("second handleKafkaBatch() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("Processor 调用次数 = %d, want 1", calls)
	}
	snapshot := manager.RuntimeMetricsSnapshot()
	if snapshot.Totals.Duplicate != 1 || snapshot.Totals.Processed != 1 {
		t.Fatalf("运行态去重指标不符合预期: %+v", snapshot.Totals)
	}
}

// TestHandleKafkaBatchIdempotencyIsolatesBizType 确保不同任务相同 EventID 不会互相去重。
func TestHandleKafkaBatchIdempotencyIsolatesBizType(t *testing.T) {
	cfg := configEnabledForRuntimeMetricTest()
	cfg.Idempotency.Enabled = true
	manager := &Manager{
		cfg:          cfg,
		idempotency:  newTestIdempotencyStore(t),
		processors:   make(map[string]Processor),
		runtimeStats: newCollectorRuntimeStats(),
	}
	calls := make(map[string]int)
	for _, bizType := range []string{"biz-a", "biz-b"} {
		bizType := bizType
		if err := manager.RegisterProcessorFunc(bizType, func(_ context.Context, events []Event) ([]ProcessResult, error) {
			calls[bizType]++
			if len(events) != 1 || events[0].BizType != bizType || events[0].EventID != "same-id" {
				t.Fatalf("%s Processor 事件不符合预期: %+v", bizType, events)
			}
			return nil, nil
		}); err != nil {
			t.Fatalf("注册 %s Processor 失败: %v", bizType, err)
		}
	}

	batch := kafkaBatch{
		messages: []kafka.Message{{Offset: 1}, {Offset: 2}},
		events: []Event{
			{EventID: "same-id", BizType: "biz-a"},
			{EventID: "same-id", BizType: "biz-b"},
		},
	}
	if err := manager.handleKafkaBatch(context.Background(), batch); err != nil {
		t.Fatalf("first handleKafkaBatch() error = %v", err)
	}
	if calls["biz-a"] != 1 || calls["biz-b"] != 1 {
		t.Fatalf("不同任务相同 EventID 都应进入各自 Processor，calls=%+v", calls)
	}
	if err := manager.handleKafkaBatch(context.Background(), batch); err != nil {
		t.Fatalf("second handleKafkaBatch() error = %v", err)
	}
	if calls["biz-a"] != 1 || calls["biz-b"] != 1 {
		t.Fatalf("重复批次应被各自任务幂等跳过，calls=%+v", calls)
	}
	if snapshot := manager.RuntimeMetricsSnapshot(); snapshot.Totals.Duplicate != 2 {
		t.Fatalf("重复批次去重指标 = %+v, want duplicate=2", snapshot.Totals)
	}
}

// TestHandleKafkaBatchDeduplicatesAcrossManagers 确保 Redis 幂等状态在进程重启或实例切换后仍能跳过重复事件。
func TestHandleKafkaBatchDeduplicatesAcrossManagers(t *testing.T) {
	store := newTestIdempotencyStore(t)
	firstManager := &Manager{
		cfg:         configWithIdempotencyForTest(),
		idempotency: store,
		processors:  make(map[string]Processor),
	}
	secondManager := &Manager{
		cfg:          configWithIdempotencyForTest(),
		idempotency:  store,
		processors:   make(map[string]Processor),
		runtimeStats: newCollectorRuntimeStats(),
	}
	calls := 0
	processor := func(_ context.Context, events []Event) ([]ProcessResult, error) {
		calls++
		if len(events) != 1 || events[0].EventID != "e1" {
			t.Fatalf("Processor 事件不符合预期: %+v", events)
		}
		return nil, nil
	}
	if err := firstManager.RegisterProcessorFunc("biz", processor); err != nil {
		t.Fatalf("注册 first Processor 失败: %v", err)
	}
	if err := secondManager.RegisterProcessorFunc("biz", processor); err != nil {
		t.Fatalf("注册 second Processor 失败: %v", err)
	}
	batch := kafkaBatch{
		messages: []kafka.Message{{Offset: 1}},
		events:   []Event{{EventID: "e1", BizType: "biz"}},
	}
	if err := firstManager.handleKafkaBatch(context.Background(), batch); err != nil {
		t.Fatalf("first handleKafkaBatch() error = %v", err)
	}
	if err := secondManager.handleKafkaBatch(context.Background(), batch); err != nil {
		t.Fatalf("second handleKafkaBatch() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("跨实例重复 EventID 不应再次进入 Processor，调用次数 = %d", calls)
	}
	if snapshot := secondManager.RuntimeMetricsSnapshot(); snapshot.Totals.Duplicate != 1 {
		t.Fatalf("跨实例去重指标 = %+v", snapshot.Totals)
	}
}

// TestHandleKafkaBatchReleasesIdempotencyWhenFailureLedgerFails 确保失败账本写入失败后 Kafka 重投仍能重新进入 Processor。
func TestHandleKafkaBatchReleasesIdempotencyWhenFailureLedgerFails(t *testing.T) {
	manager := &Manager{
		cfg:         configWithIdempotencyForTest(),
		idempotency: newTestIdempotencyStore(t),
		processors:  make(map[string]Processor),
		failures:    newCollectorFailingDB(t),
	}
	calls := 0
	if err := manager.RegisterProcessorFunc("biz", func(_ context.Context, events []Event) ([]ProcessResult, error) {
		calls++
		if len(events) != 1 || events[0].EventID != "e1" {
			t.Fatalf("Processor 事件不符合预期: %+v", events)
		}
		return []ProcessResult{{EventID: "e1", Success: false, Error: "write failed"}}, nil
	}); err != nil {
		t.Fatalf("注册 Processor 失败: %v", err)
	}
	batch := kafkaBatch{
		messages: []kafka.Message{{Offset: 1}},
		events:   []Event{{EventID: "e1", BizType: "biz"}},
	}

	if err := manager.handleKafkaBatch(context.Background(), batch); err == nil {
		t.Fatal("首次失败账本写入失败应返回错误")
	}
	if err := manager.handleKafkaBatch(context.Background(), batch); err == nil {
		t.Fatal("重投时失败账本写入失败仍应返回错误")
	}
	if calls != 2 {
		t.Fatalf("失败账本写入失败后应释放幂等占用，Processor 调用次数 = %d, want 2", calls)
	}
}

// TestHandleKafkaBatchIdempotencyChunksRedisPipeline 确保大于 Pipeline 分块的批次仍能完成 Redis 去重状态流转。
func TestHandleKafkaBatchIdempotencyChunksRedisPipeline(t *testing.T) {
	manager := &Manager{
		cfg:          configWithIdempotencyForTest(),
		idempotency:  newTestIdempotencyStoreWithBatch(t, 2),
		processors:   make(map[string]Processor),
		runtimeStats: newCollectorRuntimeStats(),
	}
	calls := 0
	if err := manager.RegisterProcessorFunc("biz", func(_ context.Context, events []Event) ([]ProcessResult, error) {
		calls++
		if got := kafkaLaneEventIDs(events); got != "e1,e2,e3,e4,e5" {
			t.Fatalf("Processor 事件 = %q, want e1,e2,e3,e4,e5", got)
		}
		return nil, nil
	}); err != nil {
		t.Fatalf("注册 Processor 失败: %v", err)
	}
	batch := kafkaBatch{
		messages: []kafka.Message{{Offset: 1}, {Offset: 2}, {Offset: 3}, {Offset: 4}, {Offset: 5}},
		events: []Event{
			{EventID: "e1", BizType: "biz"},
			{EventID: "e2", BizType: "biz"},
			{EventID: "e3", BizType: "biz"},
			{EventID: "e4", BizType: "biz"},
			{EventID: "e5", BizType: "biz"},
		},
	}
	if err := manager.handleKafkaBatch(context.Background(), batch); err != nil {
		t.Fatalf("first handleKafkaBatch() error = %v", err)
	}
	if err := manager.handleKafkaBatch(context.Background(), batch); err != nil {
		t.Fatalf("second handleKafkaBatch() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("分块 Pipeline 去重后不应重复进入 Processor，调用次数 = %d, want 1", calls)
	}
	if snapshot := manager.RuntimeMetricsSnapshot(); snapshot.Totals.Duplicate != 5 {
		t.Fatalf("分块 Pipeline duplicate 指标 = %+v", snapshot.Totals)
	}
}

// TestKafkaPartitionLaneBatchesSameTaskBySize 确保同 partition 同任务达到 batch_size 后立即处理并提交。
func TestKafkaPartitionLaneBatchesSameTaskBySize(t *testing.T) {
	manager, cancel := newKafkaLaneTestManagerWithPolicy(t, 2, 1000)
	defer cancel()
	calls := make(chan string, 1)
	if err := manager.RegisterProcessorFunc("biz", func(_ context.Context, events []Event) ([]ProcessResult, error) {
		calls <- kafkaLaneEventIDs(events)
		return nil, nil
	}); err != nil {
		t.Fatalf("注册 Processor 失败: %v", err)
	}
	input := make(chan kafkaWorkItem, 2)
	commits := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.runKafkaPartitionLane(0, input, func(_ context.Context, messages ...kafka.Message) error {
			commits <- kafkaLaneOffsets(messages)
			return nil
		})
	}()

	input <- newKafkaLaneWorkItem(0, 1, "biz", "e1")
	input <- newKafkaLaneWorkItem(0, 2, "biz", "e2")
	if got := waitKafkaLaneString(t, calls); got != "e1,e2" {
		t.Fatalf("Processor 批次 = %q, want e1,e2", got)
	}
	if got := waitKafkaLaneString(t, commits); got != "1,2" {
		t.Fatalf("提交 offset = %q, want 1,2", got)
	}
	close(input)
	waitKafkaLaneDone(t, done)
}

// TestKafkaPartitionLaneFlushesBeforeDifferentTask 确保同 partition 内遇到不同任务时先完成当前任务，避免任务间互相合批。
func TestKafkaPartitionLaneFlushesBeforeDifferentTask(t *testing.T) {
	manager, cancel := newKafkaLaneTestManagerWithPolicy(t, 10, 1000)
	defer cancel()
	calls := make(chan string, 2)
	register := func(bizType string) {
		t.Helper()
		if err := manager.RegisterProcessorFunc(bizType, func(_ context.Context, events []Event) ([]ProcessResult, error) {
			calls <- bizType + ":" + kafkaLaneEventIDs(events)
			return nil, nil
		}); err != nil {
			t.Fatalf("注册 Processor 失败: %v", err)
		}
	}
	register("biz-a")
	register("biz-b")
	input := make(chan kafkaWorkItem, 2)
	commits := make(chan string, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.runKafkaPartitionLane(0, input, func(_ context.Context, messages ...kafka.Message) error {
			commits <- kafkaLaneOffsets(messages)
			return nil
		})
	}()

	input <- newKafkaLaneWorkItem(0, 1, "biz-a", "a1")
	input <- newKafkaLaneWorkItem(0, 2, "biz-b", "b1")
	close(input)
	if got := waitKafkaLaneString(t, calls); got != "biz-a:a1" {
		t.Fatalf("第一个 Processor 批次 = %q, want biz-a:a1", got)
	}
	if got := waitKafkaLaneString(t, calls); got != "biz-b:b1" {
		t.Fatalf("第二个 Processor 批次 = %q, want biz-b:b1", got)
	}
	if got := waitKafkaLaneString(t, commits); got != "1" {
		t.Fatalf("第一次提交 offset = %q, want 1", got)
	}
	if got := waitKafkaLaneString(t, commits); got != "2" {
		t.Fatalf("第二次提交 offset = %q, want 2", got)
	}
	waitKafkaLaneDone(t, done)
}

// TestKafkaPartitionLaneFlushesByWait 确保同任务未达到 batch_size 时会按 batch_wait 刷新。
func TestKafkaPartitionLaneFlushesByWait(t *testing.T) {
	manager, cancel := newKafkaLaneTestManagerWithPolicy(t, 10, 20)
	defer cancel()
	calls := make(chan string, 1)
	if err := manager.RegisterProcessorFunc("biz", func(_ context.Context, events []Event) ([]ProcessResult, error) {
		calls <- kafkaLaneEventIDs(events)
		return nil, nil
	}); err != nil {
		t.Fatalf("注册 Processor 失败: %v", err)
	}
	input := make(chan kafkaWorkItem, 1)
	commits := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.runKafkaPartitionLane(0, input, func(_ context.Context, messages ...kafka.Message) error {
			commits <- kafkaLaneOffsets(messages)
			return nil
		})
	}()

	input <- newKafkaLaneWorkItem(0, 1, "biz", "e1")
	if got := waitKafkaLaneString(t, calls); got != "e1" {
		t.Fatalf("Processor 批次 = %q, want e1", got)
	}
	if got := waitKafkaLaneString(t, commits); got != "1" {
		t.Fatalf("提交 offset = %q, want 1", got)
	}
	close(input)
	waitKafkaLaneDone(t, done)
}

// TestKafkaPartitionLaneUsesPerTaskBatchSize 确保不同 bizType 可以独立配置聚合条数。
func TestKafkaPartitionLaneUsesPerTaskBatchSize(t *testing.T) {
	manager, cancel := newKafkaLaneTestManagerWithConfig(t, config.CollectorConfig{
		Enabled:     true,
		DefaultTask: config.CollectorTaskConfig{BatchSize: 10, BatchWaitMilliseconds: 1000},
		Tasks: map[string]config.CollectorTaskConfig{
			"biz": {BatchSize: 2, BatchWaitMilliseconds: 1000},
		},
	})
	defer cancel()
	calls := make(chan string, 1)
	if err := manager.RegisterProcessorFunc("biz", func(_ context.Context, events []Event) ([]ProcessResult, error) {
		calls <- kafkaLaneEventIDs(events)
		return nil, nil
	}); err != nil {
		t.Fatalf("注册 Processor 失败: %v", err)
	}
	input := make(chan kafkaWorkItem, 2)
	commits := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.runKafkaPartitionLane(0, input, func(_ context.Context, messages ...kafka.Message) error {
			commits <- kafkaLaneOffsets(messages)
			return nil
		})
	}()

	input <- newKafkaLaneWorkItem(0, 1, "biz", "e1")
	input <- newKafkaLaneWorkItem(0, 2, "biz", "e2")
	if got := waitKafkaLaneString(t, calls); got != "e1,e2" {
		t.Fatalf("Processor 批次 = %q, want e1,e2", got)
	}
	if got := waitKafkaLaneString(t, commits); got != "1,2" {
		t.Fatalf("提交 offset = %q, want 1,2", got)
	}
	close(input)
	waitKafkaLaneDone(t, done)
}

// TestKafkaPartitionLaneUsesPerTaskBatchWait 确保单任务等待时间不被 Kafka 默认等待时间绑死。
func TestKafkaPartitionLaneUsesPerTaskBatchWait(t *testing.T) {
	manager, cancel := newKafkaLaneTestManagerWithConfig(t, config.CollectorConfig{
		Enabled:     true,
		DefaultTask: config.CollectorTaskConfig{BatchSize: 10, BatchWaitMilliseconds: 1000},
		Tasks: map[string]config.CollectorTaskConfig{
			"biz": {BatchSize: 10, BatchWaitMilliseconds: 20},
		},
	})
	defer cancel()
	calls := make(chan string, 1)
	if err := manager.RegisterProcessorFunc("biz", func(_ context.Context, events []Event) ([]ProcessResult, error) {
		calls <- kafkaLaneEventIDs(events)
		return nil, nil
	}); err != nil {
		t.Fatalf("注册 Processor 失败: %v", err)
	}
	input := make(chan kafkaWorkItem, 1)
	commits := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.runKafkaPartitionLane(0, input, func(_ context.Context, messages ...kafka.Message) error {
			commits <- kafkaLaneOffsets(messages)
			return nil
		})
	}()

	input <- newKafkaLaneWorkItem(0, 1, "biz", "e1")
	if got := waitKafkaLaneString(t, calls); got != "e1" {
		t.Fatalf("Processor 批次 = %q, want e1", got)
	}
	if got := waitKafkaLaneString(t, commits); got != "1" {
		t.Fatalf("提交 offset = %q, want 1", got)
	}
	close(input)
	waitKafkaLaneDone(t, done)
}

// TestFlushKafkaWorkItemsRetriesCommitBeforeAdvance 确保 offset 提交失败时重试当前批次，确认前不推进后续批次。
func TestFlushKafkaWorkItemsRetriesCommitBeforeAdvance(t *testing.T) {
	manager, cancel := newKafkaLaneTestManagerWithConfig(t, config.CollectorConfig{
		DefaultTask: config.CollectorTaskConfig{BatchSize: 10},
	})
	defer cancel()
	processCalls := 0
	if err := manager.RegisterProcessorFunc("biz", func(_ context.Context, events []Event) ([]ProcessResult, error) {
		processCalls++
		if len(events) != 1 || events[0].EventID != "e1" {
			t.Fatalf("Processor 事件不符合预期: %+v", events)
		}
		return nil, nil
	}); err != nil {
		t.Fatalf("注册 Processor 失败: %v", err)
	}
	attempts := 0
	ok := manager.flushKafkaWorkItems(0, []kafkaWorkItem{
		newKafkaLaneWorkItem(0, 1, "biz", "e1"),
	}, func(_ context.Context, messages ...kafka.Message) error {
		attempts++
		if len(messages) != 1 || messages[0].Offset != 1 {
			t.Fatalf("提交消息不符合预期: %+v", messages)
		}
		if attempts == 1 {
			return errors.Errorf("commit failed")
		}
		return nil
	})
	if !ok {
		t.Fatal("commit 重试成功后应返回 true")
	}
	if attempts != 2 {
		t.Fatalf("commit 尝试次数 = %d, want 2", attempts)
	}
	if processCalls != 1 {
		t.Fatalf("Processor 调用次数 = %d, want 1", processCalls)
	}
}

// TestFlushKafkaWorkItemsDoesNotCommitWhenFailureLedgerFails 确保失败账本写入失败时不提交 Kafka offset。
func TestFlushKafkaWorkItemsDoesNotCommitWhenFailureLedgerFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	manager := &Manager{
		cfg:         config.CollectorConfig{Enabled: true, DefaultTask: config.CollectorTaskConfig{BatchSize: 10}},
		idempotency: newTestIdempotencyStore(t),
		processors:  make(map[string]Processor),
		failures:    newCollectorFailingDB(t),
		ctx:         ctx,
		cancel:      cancel,
	}
	if err := manager.RegisterProcessorFunc("biz", func(_ context.Context, events []Event) ([]ProcessResult, error) {
		if len(events) != 1 || events[0].EventID != "e1" {
			t.Fatalf("Processor 事件不符合预期: %+v", events)
		}
		return []ProcessResult{{EventID: "e1", Success: false, Error: "db write failed"}}, nil
	}); err != nil {
		t.Fatalf("注册 Processor 失败: %v", err)
	}
	committed := false
	ok := manager.flushKafkaWorkItems(0, []kafkaWorkItem{
		newKafkaLaneWorkItem(0, 1, "biz", "e1"),
	}, func(_ context.Context, _ ...kafka.Message) error {
		committed = true
		return nil
	})
	if ok {
		t.Fatal("失败账本写入失败时不应返回处理成功")
	}
	if committed {
		t.Fatal("失败账本写入失败时不应提交 Kafka offset")
	}
}

// TestCollectorBatchSizesAreBounded 确保误配置超大批次会被夹到安全上限，避免单轮占用过多内存和 DB 资源。
func TestCollectorBatchSizesAreBounded(t *testing.T) {
	manager := &Manager{cfg: config.CollectorConfig{
		DefaultTask:  config.CollectorTaskConfig{BatchSize: maxCollectorCarrierBatchSize + 100},
		FailureRetry: config.CollectorFailureRetryConfig{RunnerBatchSize: maxCollectorCarrierBatchSize + 300},
	}}
	if got := manager.kafkaFetchBatchSize(); got != maxCollectorCarrierBatchSize {
		t.Fatalf("kafkaFetchBatchSize = %d, want %d", got, maxCollectorCarrierBatchSize)
	}
	if got := manager.failureWriteBatchSize(); got != maxCollectorCarrierBatchSize {
		t.Fatalf("failureWriteBatchSize = %d, want %d", got, maxCollectorCarrierBatchSize)
	}
	if got := manager.failureRetryBatchSize(); got != maxCollectorCarrierBatchSize {
		t.Fatalf("failureRetryBatchSize = %d, want %d", got, maxCollectorCarrierBatchSize)
	}
}

// TestSaveFailureEventsUsesFailedEventTable 确保失败账本写入使用新的 failed_event 表语义。
func TestSaveFailureEventsUsesFailedEventTable(t *testing.T) {
	db := newCollectorDryRunDB(t)
	manager := &Manager{failures: db}
	err := manager.saveFailureEvents(context.Background(), []failureEventSeed{{
		event: Event{
			EventID:      "event-1",
			BizType:      "biz",
			PartitionKey: "key",
			Payload:      json.RawMessage(`{"id":1}`),
		},
		state:     model.CollectorFailedEventStateRetry,
		attempt:   1,
		nextRunAt: time.Now(),
		lastError: "failed",
	}})
	if err != nil {
		t.Fatalf("saveFailureEvents() error = %v", err)
	}
}

// fakeKafkaWriter 是 Enqueue 单测使用的 Kafka 写入器。
type fakeKafkaWriter struct {
	messages []kafka.Message // 捕获写入消息
	err      error           // 写入时返回的错误
}

// WriteMessages 捕获 Kafka 写入参数。
func (w *fakeKafkaWriter) WriteMessages(_ context.Context, messages ...kafka.Message) error {
	w.messages = append(w.messages, messages...)
	return w.err
}

// Close 实现 kafkaMessageWriter。
func (w *fakeKafkaWriter) Close() error {
	return nil
}

// newCollectorDryRunDB 创建 Collector 失败账本 SQL 断言使用的 DryRun DB。
func newCollectorDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db
}

// newCollectorFailingDB 创建执行 SQL 时快速失败的 DB，校验失败账本错误不会提前提交 offset。
func newCollectorFailingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(127.0.0.1:1)/gorm?charset=utf8mb4&parseTime=True&loc=Local&timeout=50ms&readTimeout=50ms&writeTimeout=50ms",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db
}

// collectorTestConfigForTopic 返回 Enqueue 单测使用的任务级 Kafka 路由配置。
func collectorTestConfigForTopic(topic string) config.CollectorConfig {
	return config.CollectorConfig{
		Enabled: true,
		Kafka:   config.CollectorKafkaConfig{WriteTimeout: 1},
		DefaultTask: config.CollectorTaskConfig{
			Topic:   topic,
			GroupID: "collector_test_group",
		},
	}
}

// newTestIdempotencyStore 创建 Collector Redis 幂等测试存储。
func newTestIdempotencyStore(t *testing.T) *idempotencyStore {
	t.Helper()
	return newTestIdempotencyStoreWithBatch(t, defaultIdempotencyPipelineBatchSize)
}

// newTestIdempotencyStoreWithBatch 创建指定 Pipeline 分块大小的 Redis 幂等测试存储。
func newTestIdempotencyStoreWithBatch(t *testing.T, pipelineBatchSize int) *idempotencyStore {
	t.Helper()
	useCollectorTestAppID(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})
	return newIdempotencyStore(client, defaultIdempotencyTTL, defaultIdempotencyProcessingTTL, pipelineBatchSize)
}

// useCollectorTestAppID 设置测试用 app_id，确保 Redis key helper 不生成裸 key。
func useCollectorTestAppID(t *testing.T) {
	t.Helper()
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "collector-test"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})
}

// newKafkaLaneTestManagerWithPolicy 创建指定默认聚合策略的 partition lane 单测 Manager。
func newKafkaLaneTestManagerWithPolicy(t *testing.T, batchSize int, batchWaitMilliseconds int) (*Manager, context.CancelFunc) {
	t.Helper()
	return newKafkaLaneTestManagerWithConfig(t, config.CollectorConfig{
		Enabled: true,
		DefaultTask: config.CollectorTaskConfig{
			BatchSize:             batchSize,
			BatchWaitMilliseconds: batchWaitMilliseconds,
		},
	})
}

// newKafkaLaneTestManagerWithConfig 创建支持完整 Collector 配置的 partition lane 单测 Manager。
func newKafkaLaneTestManagerWithConfig(t *testing.T, cfg config.CollectorConfig) (*Manager, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cfg.Enabled = true
	return &Manager{
		cfg:         cfg,
		idempotency: newTestIdempotencyStore(t),
		processors:  make(map[string]Processor),
		ctx:         ctx,
		cancel:      cancel,
	}, cancel
}

// configWithIdempotencyForTest 返回按 bizType 显式开启 Redis 幂等的测试配置。
func configWithIdempotencyForTest() config.CollectorConfig {
	enabled := true
	cfg := configEnabledForRuntimeMetricTest()
	cfg.Tasks = map[string]config.CollectorTaskConfig{
		"biz": {IdempotencyEnabled: &enabled},
	}
	return cfg
}

// newKafkaLaneWorkItem 生成一条有效 Kafka lane 事件。
func newKafkaLaneWorkItem(partition int, offset int64, bizType string, eventID string) kafkaWorkItem {
	return kafkaWorkItem{
		message: kafka.Message{Topic: "collector", Partition: partition, Offset: offset},
		event: Event{
			EventID: eventID,
			BizType: bizType,
			Payload: json.RawMessage(`{"ok":true}`),
		},
	}
}

// kafkaLaneEventIDs 返回批次事件 ID 序列，便于断言同任务批量边界。
func kafkaLaneEventIDs(events []Event) string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.EventID)
	}
	return strings.Join(ids, ",")
}

// kafkaLaneOffsets 返回提交消息的 offset 序列。
func kafkaLaneOffsets(messages []kafka.Message) string {
	offsets := make([]string, 0, len(messages))
	for _, message := range messages {
		offsets = append(offsets, fmt.Sprintf("%d", message.Offset))
	}
	return strings.Join(offsets, ",")
}

// waitKafkaLaneString 等待 lane 异步结果，避免测试卡死。
func waitKafkaLaneString(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("等待 Kafka lane 结果超时")
		return ""
	}
}

// waitKafkaLaneDone 等待 lane 退出，避免泄漏 goroutine。
func waitKafkaLaneDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("等待 Kafka lane 退出超时")
	}
}
