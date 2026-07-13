package kafkax

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"admin/internal/config"

	"github.com/segmentio/kafka-go"
)

// TestNewProducerValidatesEnabledConfiguration 验证 Kafka 未启用时安全跳过，启用后要求 broker 和主题完整。
func TestNewProducerValidatesEnabledConfiguration(t *testing.T) {
	producer, err := NewProducer(config.KafkaConfig{})
	if err != nil || producer != nil {
		t.Fatalf("disabled producer = %v, error = %v", producer, err)
	}
	if _, err = NewProducer(config.KafkaConfig{Enabled: true}); err == nil {
		t.Fatal("enabled producer without brokers should fail")
	}
	if _, err = NewProducer(config.KafkaConfig{
		Enabled: true,
		Brokers: []string{"127.0.0.1:9092"},
	}); err == nil {
		t.Fatal("enabled producer without topic should fail")
	}
}

// TestNewProducerAppliesSafeDefaults 验证 Kafka 生产者归一化 broker 并应用有界批量和写入超时默认值。
func TestNewProducerAppliesSafeDefaults(t *testing.T) {
	producer, err := NewProducer(config.KafkaConfig{
		Enabled: true,
		Brokers: []string{"", " 127.0.0.1:9092 "},
		Topics:  config.KafkaTopicsConfig{UserTag: " user-tag-events "},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	defer producer.writer.Close()
	if !reflect.DeepEqual(producer.brokers, []string{"127.0.0.1:9092"}) {
		t.Fatalf("brokers = %+v", producer.brokers)
	}
	if producer.topicUserTag != "user-tag-events" || producer.BatchSize() != 500 {
		t.Fatalf("topic = %q, batch size = %d", producer.topicUserTag, producer.BatchSize())
	}
	if producer.writer.WriteTimeout != 10*time.Second {
		t.Fatalf("write timeout = %s, want 10s", producer.writer.WriteTimeout)
	}
}

// TestWriteKeyedJSONRejectsUnsupportedValueBeforeKafka 验证 JSON 编码失败时不会进入 Kafka 网络写入。
func TestWriteKeyedJSONRejectsUnsupportedValueBeforeKafka(t *testing.T) {
	producer := &Producer{writer: &kafka.Writer{}}
	err := producer.WriteKeyedJSON(context.Background(), []JSONMessage{{
		Key:   "user-1",
		Value: math.NaN(),
	}})
	if err == nil {
		t.Fatal("unsupported JSON value should be rejected")
	}
}
