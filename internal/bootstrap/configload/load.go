package configload

import (
	"strings"

	"admin/internal/bootstrap/configload/runtimefile"
	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/conf"
)

// Load 读取并解析配置文件，供启动期和热加载复用。
func Load(file string) (config.Config, error) {
	c, err := loadBaseConfig(file)
	if err != nil {
		return config.Config{}, errors.Tag(err)
	}
	if err = runtimefile.Apply(file, &c); err != nil {
		return config.Config{}, errors.Tag(err)
	}
	Normalize(&c)
	if err = Validate(c); err != nil {
		return config.Config{}, errors.Tag(err)
	}
	return c, nil
}

// Normalize 补齐跨配置块的默认值，避免样例配置重复维护公共参数。
func Normalize(c *config.Config) {
	if c == nil {
		return
	}
	inheritCollectorKafkaCommon(c)
	inheritCDCKafkaCommon(c)
	c.Observability.Environment = strings.TrimSpace(c.Mode)
	if c.User.RouteShardCount <= 0 {
		c.User.RouteShardCount = defaultUserRouteShardCount
	}
}

// inheritCollectorKafkaCommon 让 Collector Kafka 复用顶层 Kafka 公共连接参数。
func inheritCollectorKafkaCommon(c *config.Config) {
	if len(c.Collector.Kafka.Brokers) == 0 && len(c.Kafka.Brokers) > 0 {
		c.Collector.Kafka.Brokers = append([]string(nil), c.Kafka.Brokers...)
	}
	if c.Collector.Kafka.BatchSize <= 0 {
		c.Collector.Kafka.BatchSize = c.Kafka.BatchSize
	}
	if c.Collector.Kafka.WriteTimeout <= 0 {
		c.Collector.Kafka.WriteTimeout = c.Kafka.WriteTimeout
	}
}

// inheritCDCKafkaCommon 让 CDC 消费器复用顶层 Kafka broker，减少本地配置重复。
func inheritCDCKafkaCommon(c *config.Config) {
	if len(c.CDC.Brokers) == 0 && len(c.Kafka.Brokers) > 0 {
		c.CDC.Brokers = append([]string(nil), c.Kafka.Brokers...)
	}
}

// loadBaseConfig 只读取主配置文件，不处理 config_files 引用。
// 热加载指纹需要先解析主配置拿到外部文件路径，因此这里单独拆出基础加载能力。
func loadBaseConfig(file string) (config.Config, error) {
	var c config.Config
	if err := conf.Load(file, &c); err != nil {
		return config.Config{}, errors.Tag(err)
	}
	return c, nil
}
