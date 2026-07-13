package cdcx

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"admin/internal/config"

	"github.com/segmentio/kafka-go"
)

// TestConsumerReadyRequiresRunningWorker 验证 Worker 模式不会把未启动的 CDC 消费器误报为就绪。
func TestConsumerReadyRequiresRunningWorker(t *testing.T) {
	consumer, err := New(config.CDCConfig{
		Enabled: true,
		Brokers: []string{"127.0.0.1:9092"},
		GroupID: "admin-cdc-test",
		Topics:  []config.CDCTopicConfig{{Enabled: true, Topic: "admin.admin_log", Table: "admin.admin_log"}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err = consumer.RegisterProcessorFunc("admin.admin_log", func(context.Context, Event) error { return nil }); err != nil {
		t.Fatalf("RegisterProcessorFunc() error = %v", err)
	}
	if err = consumer.Ready(context.Background(), true); err == nil || !strings.Contains(err.Error(), "未运行") {
		t.Fatalf("未启动 CDC Worker 时应判定未就绪，实际=%v", err)
	}
}

// TestDisabledConsumerReadyIsSkipped 验证 CDC 关闭时就绪检查不会访问 Kafka。
func TestDisabledConsumerReadyIsSkipped(t *testing.T) {
	consumer, err := New(config.CDCConfig{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err = consumer.Ready(context.Background(), true); err != nil {
		t.Fatalf("关闭的 CDC 不应阻断就绪检查: %v", err)
	}
}

// TestConsumerReadyChecksEverySourceAndDeadLetterTopic 验证 readiness 只读探测全部源 Topic 与死信 Topic。
func TestConsumerReadyChecksEverySourceAndDeadLetterTopic(t *testing.T) {
	consumer, err := New(config.CDCConfig{
		Enabled:         true,
		Brokers:         []string{"127.0.0.1:9092"},
		GroupID:         "admin-cdc-ready",
		DeadLetterTopic: "cdc-dead-letter",
		Topics: []config.CDCTopicConfig{
			{Enabled: true, Topic: "source-b", Table: "admin.table_b"},
			{Enabled: true, Topic: "source-a", Table: "admin.table_a"},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, table := range []string{"admin.table_a", "admin.table_b"} {
		if err := consumer.RegisterProcessorFunc(table, func(context.Context, Event) error { return nil }); err != nil {
			t.Fatalf("RegisterProcessorFunc(%s) error = %v", table, err)
		}
	}
	probe := &fakeTopicMetadataProbe{}
	consumer.metadataProbe = probe
	if err := consumer.Ready(context.Background(), false); err != nil {
		t.Fatalf("全部 Topic 可用时 readiness 失败: %v", err)
	}
	if got, want := strings.Join(probe.topics, ","), "cdc-dead-letter,source-a,source-b"; got != want {
		t.Fatalf("readiness 探测 Topic=%s, want %s", got, want)
	}

	probe.topics = nil
	probe.failedTopic = "cdc-dead-letter"
	if err := consumer.Ready(context.Background(), false); err == nil || !strings.Contains(err.Error(), "cdc-dead-letter") {
		t.Fatalf("死信 Topic 不可用时应判定未就绪，实际=%v", err)
	}
}

// TestConsumerReadyLiveKafka 验证真实 Kafka Topic 元数据探测；未提供环境变量时跳过。
func TestConsumerReadyLiveKafka(t *testing.T) {
	broker := strings.TrimSpace(os.Getenv("ADMIN_CDC_LIVE_BROKER"))
	topic := strings.TrimSpace(os.Getenv("ADMIN_CDC_LIVE_TOPIC"))
	if broker == "" || topic == "" {
		t.Skip("未配置 ADMIN_CDC_LIVE_BROKER 和 ADMIN_CDC_LIVE_TOPIC")
	}
	consumer, err := New(config.CDCConfig{
		Enabled: true, Brokers: []string{broker}, GroupID: "admin-cdc-readiness-live",
		Topics: []config.CDCTopicConfig{{Enabled: true, Topic: topic, Table: "admin.admin_log"}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err = consumer.RegisterProcessorFunc("admin.admin_log", func(context.Context, Event) error { return nil }); err != nil {
		t.Fatalf("RegisterProcessorFunc() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = consumer.Ready(ctx, false); err != nil {
		t.Fatalf("真实 Kafka readiness 失败: %v", err)
	}
}

// fakeTopicMetadataProbe 记录 readiness 的只读 Topic 元数据探测。
type fakeTopicMetadataProbe struct {
	topics      []string // 已探测 Topic，按调用顺序记录
	failedTopic string   // 指定返回失败的 Topic
}

// Check 记录 Topic，并按配置返回确定性故障。
func (p *fakeTopicMetadataProbe) Check(_ context.Context, _ []string, topic string) error {
	p.topics = append(p.topics, topic)
	if topic == p.failedTopic {
		return errTestFailure{}
	}
	return nil
}

// TestConsumerRetriesSameMessageBeforeNextFetch 验证同一消息在拉取下一条前完成重试并清理失败计数。
func TestConsumerRetriesSameMessageBeforeNextFetch(t *testing.T) {
	consumer, err := New(config.CDCConfig{
		Enabled:             true,
		Brokers:             []string{"127.0.0.1:9092"},
		GroupID:             "admin-cdc-test",
		MaxRetryTimes:       5,
		RetryBackoffSeconds: 1,
		Topics: []config.CDCTopicConfig{{
			Enabled: true,
			Topic:   "dnmp-admin.admin.admin_log",
			Table:   "admin.admin_log",
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	consumer.ctx = context.Background()
	processor := &flakyProcessor{failures: 2}
	if err := consumer.RegisterProcessor("admin.admin_log", processor); err != nil {
		t.Fatalf("RegisterProcessor() error = %v", err)
	}

	msg := kafka.Message{
		Topic:     "dnmp-admin.admin.admin_log",
		Partition: 0,
		Offset:    7,
		Key:       []byte(`{"payload":{"id":1}}`),
		Value:     []byte(`{"before":null,"after":{"id":1},"source":{"db":"admin","table":"admin_log","ts_ms":1782311237336},"op":"c"}`),
	}
	if ok := consumer.processFetchedMessage(consumer.cfg.Topics[0], msg); !ok {
		t.Fatal("processFetchedMessage() = false")
	}
	if processor.calls != 3 {
		t.Fatalf("processor 调用次数 = %d, want 3", processor.calls)
	}
	if got := consumer.attempts[messageKey(msg)]; got != 0 {
		t.Fatalf("成功后失败计数 = %d", got)
	}
}

// TestConsumerRetriesCommitBeforeNextFetch 验证位点提交失败时原位重试，不会继续拉取后续消息。
func TestConsumerRetriesCommitBeforeNextFetch(t *testing.T) {
	consumer := &Consumer{
		cfg: config.CDCConfig{RetryBackoffSeconds: 1},
		ctx: context.Background(),
	}
	msg := kafka.Message{Topic: "admin.admin_log", Partition: 1, Offset: 9}
	commitCalls := 0
	ok := consumer.commitFetchedMessage(msg, func(_ context.Context, messages ...kafka.Message) error {
		commitCalls++
		if len(messages) != 1 || messages[0].Offset != msg.Offset {
			t.Fatalf("commit messages = %+v, want offset %d", messages, msg.Offset)
		}
		if commitCalls == 1 {
			return errors.New("temporary commit failure")
		}
		return nil
	})
	if !ok {
		t.Fatal("commitFetchedMessage() = false")
	}
	if commitCalls != 2 {
		t.Fatalf("commit calls = %d, want 2", commitCalls)
	}
}

// TestConsumerDeadLetterAfterMaxRetry 验证失败达到阈值后写入完整死信并清理计数。
func TestConsumerDeadLetterAfterMaxRetry(t *testing.T) {
	consumer, err := New(config.CDCConfig{
		Enabled:             true,
		Brokers:             []string{"127.0.0.1:9092"},
		GroupID:             "admin-cdc-test",
		MaxRetryTimes:       2,
		DeadLetterTopic:     "admin_cdc_dead_letter",
		RetryBackoffSeconds: 1,
		Topics: []config.CDCTopicConfig{{
			Enabled: true,
			Topic:   "dnmp-admin.admin.admin_log",
			Table:   "admin.admin_log",
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	writer := &fakeMessageWriter{}
	consumer.ctx = context.Background()
	consumer.deadWriter = writer

	msg := kafka.Message{
		Topic:     "dnmp-admin.admin.admin_log",
		Partition: 0,
		Offset:    7,
		Key:       []byte(`{"payload":{"id":1}}`),
		Value:     []byte(`{"after":{"id":1}}`),
	}
	if got := consumer.recordFailure(msg); got != 1 {
		t.Fatalf("首次失败次数 = %d", got)
	}
	if consumer.shouldDeadLetter(1) {
		t.Fatal("未达到阈值不应进入死信")
	}
	attempt := consumer.recordFailure(msg)
	if !consumer.shouldDeadLetter(attempt) {
		t.Fatal("达到阈值应进入死信")
	}
	if err := consumer.writeDeadLetter(context.Background(), consumer.cfg.Topics[0], msg, errTestFailure{}, attempt); err != nil {
		t.Fatalf("writeDeadLetter() error = %v", err)
	}
	if len(writer.messages) != 1 {
		t.Fatalf("死信消息数量 = %d", len(writer.messages))
	}

	var dead DeadLetterMessage
	if err := json.Unmarshal(writer.messages[0].Value, &dead); err != nil {
		t.Fatalf("解析死信消息失败: %v", err)
	}
	if dead.SourceTopic != msg.Topic || dead.SourceOffset != msg.Offset || dead.Attempt != 2 || dead.Table != "admin.admin_log" {
		t.Fatalf("死信内容不符合预期: %+v", dead)
	}
	consumer.clearFailure(msg)
	if got := consumer.attempts[messageKey(msg)]; got != 0 {
		t.Fatalf("clearFailure 后失败次数 = %d", got)
	}
}

// TestConsumerDeadLettersFetchedMessage 验证拉取消息持续失败后进入死信并结束当前重试。
func TestConsumerDeadLettersFetchedMessage(t *testing.T) {
	consumer, err := New(config.CDCConfig{
		Enabled:             true,
		Brokers:             []string{"127.0.0.1:9092"},
		GroupID:             "admin-cdc-test",
		MaxRetryTimes:       2,
		DeadLetterTopic:     "admin_cdc_dead_letter",
		RetryBackoffSeconds: 1,
		Topics: []config.CDCTopicConfig{{
			Enabled: true,
			Topic:   "dnmp-admin.admin.admin_log",
			Table:   "admin.admin_log",
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	consumer.ctx = context.Background()
	consumer.deadWriter = &fakeMessageWriter{}
	if err := consumer.RegisterProcessor("admin.admin_log", ProcessorFunc(func(context.Context, Event) error {
		return errors.New("always failed")
	})); err != nil {
		t.Fatalf("RegisterProcessor() error = %v", err)
	}

	msg := kafka.Message{
		Topic:     "dnmp-admin.admin.admin_log",
		Partition: 0,
		Offset:    8,
		Value:     []byte(`{"before":null,"after":{"id":1},"source":{"db":"admin","table":"admin_log"},"op":"c"}`),
	}
	if ok := consumer.processFetchedMessage(consumer.cfg.Topics[0], msg); !ok {
		t.Fatal("processFetchedMessage() = false")
	}
	writer := consumer.deadWriter.(*fakeMessageWriter)
	if len(writer.messages) != 1 {
		t.Fatalf("死信消息数量 = %d", len(writer.messages))
	}
	if got := consumer.attempts[messageKey(msg)]; got != 0 {
		t.Fatalf("死信后失败计数 = %d", got)
	}
}

// TestConsumerSkipFetchedMessage 验证主动跳过的消息不写死信且只累计跳过数。
func TestConsumerSkipFetchedMessage(t *testing.T) {
	consumer, err := New(config.CDCConfig{
		Enabled:             true,
		Brokers:             []string{"127.0.0.1:9092"},
		GroupID:             "admin-cdc-test",
		MaxRetryTimes:       2,
		DeadLetterTopic:     "admin_cdc_dead_letter",
		RetryBackoffSeconds: 1,
		Topics: []config.CDCTopicConfig{{
			Enabled: true,
			Topic:   "dnmp-admin.admin.admin_log",
			Table:   "admin.admin_log",
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	consumer.ctx = context.Background()
	consumer.deadWriter = &fakeMessageWriter{}
	if err := consumer.RegisterProcessor("admin.admin_log", ProcessorFunc(func(context.Context, Event) error {
		return Skip("ignore snapshot")
	})); err != nil {
		t.Fatalf("RegisterProcessor() error = %v", err)
	}

	msg := kafka.Message{
		Topic:     "dnmp-admin.admin.admin_log",
		Partition: 0,
		Offset:    9,
		Value:     []byte(`{"before":null,"after":{"id":1},"source":{"db":"admin","table":"admin_log"},"op":"r"}`),
	}
	consumer.markFetched(msg)
	if ok := consumer.processFetchedMessage(consumer.cfg.Topics[0], msg); !ok {
		t.Fatal("processFetchedMessage() = false")
	}
	writer := consumer.deadWriter.(*fakeMessageWriter)
	if len(writer.messages) != 0 {
		t.Fatalf("skip 不应写死信，实际 %d 条", len(writer.messages))
	}
	stat := consumer.Snapshot().Topics[msg.Topic]
	if stat.Skipped != 1 || stat.Failed != 0 || stat.DeadLettered != 0 {
		t.Fatalf("skip 状态不符合预期: %+v", stat)
	}
}

// TestConsumerSkipsTombstoneMessage 验证墓碑消息不分发处理器且按跳过计数。
func TestConsumerSkipsTombstoneMessage(t *testing.T) {
	consumer, err := New(config.CDCConfig{
		Enabled:             true,
		Brokers:             []string{"127.0.0.1:9092"},
		GroupID:             "admin-cdc-test",
		MaxRetryTimes:       2,
		DeadLetterTopic:     "admin_cdc_dead_letter",
		RetryBackoffSeconds: 1,
		Topics: []config.CDCTopicConfig{{
			Enabled: true,
			Topic:   "dnmp-admin.admin.admin_log",
			Table:   "admin.admin_log",
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	consumer.ctx = context.Background()
	processor := &flakyProcessor{}
	if err := consumer.RegisterProcessor("admin.admin_log", processor); err != nil {
		t.Fatalf("RegisterProcessor() error = %v", err)
	}

	msg := kafka.Message{
		Topic:     "dnmp-admin.admin.admin_log",
		Partition: 0,
		Offset:    10,
		Value:     nil,
	}
	if ok := consumer.processFetchedMessage(consumer.cfg.Topics[0], msg); !ok {
		t.Fatal("processFetchedMessage() = false")
	}
	if processor.calls != 0 {
		t.Fatalf("tombstone 不应分发给 processor，实际调用=%d", processor.calls)
	}
	stat := consumer.Snapshot().Topics[msg.Topic]
	if stat.Skipped != 1 || stat.Failed != 0 || stat.DeadLettered != 0 {
		t.Fatalf("tombstone 状态不符合预期: %+v", stat)
	}
}

// TestConsumerKeepsFailedMessageWithoutDeadLetterTopic 验证未配置死信主题时保留失败消息供后续重试。
func TestConsumerKeepsFailedMessageWithoutDeadLetterTopic(t *testing.T) {
	consumer, err := New(config.CDCConfig{
		Enabled:       true,
		Brokers:       []string{"127.0.0.1:9092"},
		GroupID:       "admin-cdc-test",
		MaxRetryTimes: 1,
		Topics: []config.CDCTopicConfig{{
			Enabled: true,
			Topic:   "dnmp-admin.admin.admin_log",
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	msg := kafka.Message{Topic: "topic", Partition: 1, Offset: 3}
	attempt := consumer.recordFailure(msg)
	if consumer.shouldDeadLetter(attempt) {
		t.Fatal("未配置死信 topic 时不应提交失败消息")
	}
}

// TestConsumerValidateProcessors 验证已启用表必须注册对应处理器。
func TestConsumerValidateProcessors(t *testing.T) {
	consumer, err := New(config.CDCConfig{
		Enabled: true,
		Brokers: []string{"127.0.0.1:9092"},
		GroupID: "admin-cdc-test",
		Topics: []config.CDCTopicConfig{{
			Enabled: true,
			Topic:   "dnmp-admin.admin.admin_log",
			Table:   "admin.admin_log",
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := consumer.ValidateProcessors(); err == nil {
		t.Fatal("未注册处理器应校验失败")
	}
	if err := consumer.RegisterProcessorFunc("admin.admin_log", func(context.Context, Event) error { return nil }); err != nil {
		t.Fatalf("RegisterProcessorFunc() error = %v", err)
	}
	if err := consumer.ValidateProcessors(); err != nil {
		t.Fatalf("ValidateProcessors() error = %v", err)
	}
	tables := consumer.RegisteredTables()
	if len(tables) != 1 || tables[0] != "admin.admin_log" {
		t.Fatalf("RegisteredTables() = %v", tables)
	}
}

// TestDecodeDeadLetterMessage 验证死信载荷解码时会规范化表名和错误文本。
func TestDecodeDeadLetterMessage(t *testing.T) {
	raw := []byte(`{"source_topic":"dnmp-admin.admin.admin_log","source_partition":0,"source_offset":7,"table":"Admin.Admin_Log","attempt":2,"error":" failed ","key":"eyJpZCI6MX0=","value":"eyJhZnRlciI6eyJpZCI6MX19","created_at":"2026-06-24T23:00:00Z"}`)
	msg, err := DecodeDeadLetterMessage(raw)
	if err != nil {
		t.Fatalf("DecodeDeadLetterMessage() error = %v", err)
	}
	if msg.SourceTopic != "dnmp-admin.admin.admin_log" || msg.Table != "admin.admin_log" || msg.Error != "failed" {
		t.Fatalf("死信消息不符合预期: %+v", msg)
	}
}

// TestConsumerStopHonorsContextDeadline 确保 Kafka 关闭阻塞时 CDC 不会突破应用停止期限。
func TestConsumerStopHonorsContextDeadline(t *testing.T) {
	closeBlock := make(chan struct{})
	consumer := &Consumer{deadWriter: &fakeMessageWriter{closeBlock: closeBlock}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := consumer.Stop(ctx); err == nil {
		close(closeBlock)
		t.Fatal("Stop() expected context deadline error")
	}
	close(closeBlock)
}

// fakeMessageWriter 记录测试期间写入的 Kafka 消息。
type fakeMessageWriter struct {
	messages   []kafka.Message // 已写入的测试消息
	closeBlock <-chan struct{} // 非空时阻塞 Close，用于停止期限测试
}

// WriteMessages 保存待断言的消息并模拟写入成功。
func (w *fakeMessageWriter) WriteMessages(_ context.Context, messages ...kafka.Message) error {
	w.messages = append(w.messages, messages...)
	return nil
}

// Close 模拟关闭消息写入器。
func (w *fakeMessageWriter) Close() error {
	if w.closeBlock != nil {
		<-w.closeBlock
	}
	return nil
}

// errTestFailure 提供固定的处理失败错误。
type errTestFailure struct{}

// Error 返回测试使用的固定错误文本。
func (errTestFailure) Error() string {
	return "processor failed"
}

// flakyProcessor 模拟前若干次失败后恢复的 CDC 处理器。
type flakyProcessor struct {
	failures int // 前 N 次返回错误
	calls    int // 调用次数
}

// ProcessCDC 按配置的失败次数返回临时错误。
func (p *flakyProcessor) ProcessCDC(context.Context, Event) error {
	p.calls++
	if p.calls <= p.failures {
		return errors.New("temporary failed")
	}
	return nil
}
