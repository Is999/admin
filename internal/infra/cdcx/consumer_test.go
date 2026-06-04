package cdcx

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"admin/internal/config"

	"github.com/segmentio/kafka-go"
)

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

type fakeMessageWriter struct {
	messages []kafka.Message // 已写入的测试消息
}

func (w *fakeMessageWriter) WriteMessages(_ context.Context, messages ...kafka.Message) error {
	w.messages = append(w.messages, messages...)
	return nil
}

func (w *fakeMessageWriter) Close() error {
	return nil
}

type errTestFailure struct{}

func (errTestFailure) Error() string {
	return "processor failed"
}

type flakyProcessor struct {
	failures int // 前 N 次返回错误
	calls    int // 调用次数
}

func (p *flakyProcessor) ProcessCDC(context.Context, Event) error {
	p.calls++
	if p.calls <= p.failures {
		return errors.New("temporary failed")
	}
	return nil
}
