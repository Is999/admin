package validators

import (
	"strings"

	keys "admin/common/rediskeys"
	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
)

// Collector 载体枚举限定配置文件可声明的事件投递通道。
const (
	adminCollectorTransportAuto  = "auto"  // 自动选择可用的 Collector 载体
	adminCollectorTransportKafka = "kafka" // 强制使用 Kafka 作为 Collector 载体
	adminCollectorTransportRedis = "redis" // 强制使用 Redis Stream 作为 Collector 载体
	adminCollectorTransportDB    = "db"    // 强制使用数据库 outbox 作为 Collector 载体
)

// ValidateCollector 校验 Collector 载体配置是否自洽。
func ValidateCollector(c config.Config) error {
	cfg := c.Collector
	transport := strings.ToLower(strings.TrimSpace(cfg.Transport))
	switch transport {
	case "", adminCollectorTransportAuto, adminCollectorTransportKafka, adminCollectorTransportRedis, adminCollectorTransportDB:
	default:
		return errors.Errorf("collector.transport 仅支持 auto/kafka/redis/db")
	}

	if cfg.Kafka.Enabled {
		if len(nonEmptyStrings(cfg.Kafka.Brokers)) == 0 {
			return errors.Errorf("collector.kafka.enabled=true 时必须配置 collector.kafka.brokers 或顶层 kafka.brokers")
		}
		if strings.TrimSpace(cfg.Kafka.Topic) == "" {
			return errors.Errorf("collector.kafka.enabled=true 时必须配置 collector.kafka.topic")
		}
		if cfg.Enabled && strings.TrimSpace(cfg.Kafka.GroupID) == "" {
			return errors.Errorf("collector.enabled=true 且 collector.kafka.enabled=true 时必须配置 collector.kafka.group_id")
		}
	}
	if cfg.Redis.Enabled && strings.TrimSpace(cfg.Redis.Stream) == "" {
		return errors.Errorf("collector.redis.enabled=true 时必须配置 collector.redis.stream")
	}
	if ownerAppID, ok := keys.Owner(cfg.Redis.Stream); ok && strings.TrimSpace(c.AppID) != ownerAppID {
		return errors.Errorf("collector.redis.stream 属于其它 app_id[%s]", ownerAppID)
	}

	switch transport {
	case adminCollectorTransportKafka:
		if !cfg.Kafka.Enabled {
			return errors.Errorf("collector.transport=kafka 时必须启用 collector.kafka.enabled")
		}
	case adminCollectorTransportRedis:
		if !cfg.Redis.Enabled {
			return errors.Errorf("collector.transport=redis 时必须启用 collector.redis.enabled")
		}
	case adminCollectorTransportDB:
		return nil
	}
	return nil
}

// nonEmptyStrings 过滤空白字符串，避免配置数组里的空项参与校验。
func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
