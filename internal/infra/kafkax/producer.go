package kafkax

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
	"github.com/segmentio/kafka-go"
)

// Producer 封装 Kafka 写入器，集中处理用户标签变更消息。
type Producer struct {
	writer       *kafka.Writer // 底层 Kafka writer
	brokers      []string      // Kafka broker 地址，用于 readiness 连通性探测
	topicUserTag string        // 用户标签变更主题
	batchSize    int           // 单批最大消息数
}

// JSONMessage 表示一条带 Kafka key 的 JSON 消息。
type JSONMessage struct {
	Key   string // Kafka 分区和消费幂等 key
	Value any    // JSON 消息体
}

// NewProducer 根据配置创建 Kafka 生产者。
// 未启用时返回 nil，业务侧可以安全跳过标签变更推送。
func NewProducer(cfg config.KafkaConfig) (*Producer, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	brokers := normalizeBrokers(cfg.Brokers)
	if len(brokers) == 0 {
		return nil, errors.Errorf("缺少 Kafka brokers 配置")
	}
	topic := strings.TrimSpace(cfg.Topics.UserTag)
	if topic == "" {
		return nil, errors.Errorf("缺少 Kafka 用户标签主题配置")
	}
	timeout := time.Duration(cfg.WriteTimeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 500
	}
	return &Producer{
		writer: &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Topic:        topic,
			Balancer:     &kafka.Hash{},
			BatchSize:    batchSize,
			WriteTimeout: timeout,
		},
		brokers:      append([]string(nil), brokers...),
		topicUserTag: topic,
		batchSize:    batchSize,
	}, nil
}

// Ping 通过 Kafka 元数据协议检查用户标签 Topic 是否可用。
func (p *Producer) Ping(ctx context.Context) error {
	if !p.Enabled() {
		return errors.New("Kafka生产者未初始化")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var lastErr error
	dialer := &kafka.Dialer{Timeout: time.Second}
	for _, broker := range p.brokers {
		partitions, err := dialer.LookupPartitions(ctx, "tcp", broker, p.topicUserTag)
		if err == nil && len(partitions) > 0 {
			return nil
		}
		if err == nil {
			err = errors.Errorf("Kafka Topic不存在或无分区 topic=%s", p.topicUserTag)
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr == nil {
		return errors.New("Kafka broker未配置")
	}
	return errors.Wrap(lastErr, "Kafka broker或Topic不可用")
}

// Enabled 返回 Kafka 生产者是否可用。
func (p *Producer) Enabled() bool {
	return p != nil && p.writer != nil
}

// BatchSize 返回配置的批量写入大小。
func (p *Producer) BatchSize() int {
	if p == nil || p.batchSize <= 0 {
		return 500
	}
	return p.batchSize
}

// WriteJSON 批量写入 JSON 消息，key 用于 Kafka 分区哈希。
func (p *Producer) WriteJSON(ctx context.Context, key string, values []any) error {
	if !p.Enabled() || len(values) == 0 {
		return nil
	}
	items := make([]JSONMessage, 0, len(values))
	for _, value := range values {
		items = append(items, JSONMessage{Key: key, Value: value})
	}
	return p.WriteKeyedJSON(ctx, items)
}

// WriteKeyedJSON 批量写入 JSON 消息，每条消息使用自己的 Kafka key。
func (p *Producer) WriteKeyedJSON(ctx context.Context, items []JSONMessage) error {
	if !p.Enabled() || len(items) == 0 {
		return nil
	}
	messages := make([]kafka.Message, 0, len(items))
	for _, item := range items {
		body, err := json.Marshal(item.Value)
		if err != nil {
			return errors.Tag(err)
		}
		messages = append(messages, kafka.Message{
			Key:   []byte(strings.TrimSpace(item.Key)),
			Value: body,
		})
	}
	return p.writer.WriteMessages(ctx, messages...)
}

// Close 在应用停止期限内关闭底层 Kafka writer。
func (p *Producer) Close(ctx context.Context) error {
	if !p.Enabled() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan error, 1)
	go func() {
		done <- p.writer.Close()
	}()
	select {
	case err := <-done:
		return errors.Tag(err)
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "关闭 Kafka writer 超时")
	}
}

// normalizeBrokers 归一化 broker 地址列表，过滤空值并保留原始顺序。
func normalizeBrokers(items []string) []string {
	brokers := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			brokers = append(brokers, trimmed)
		}
	}
	return brokers
}
