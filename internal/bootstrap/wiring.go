package bootstrap

import (
	"context"

	"github.com/Is999/go-utils/errors"

	"admin_cron/internal/config"

	"github.com/zeromicro/go-zero/core/conf"
)

// LoadConfig 读取并解析配置文件，供启动期和热加载复用。
func LoadConfig(file string) (config.Config, error) {
	c, err := loadBaseConfig(file)
	if err != nil {
		return config.Config{}, errors.Tag(err)
	}
	if err = applyExternalConfigFiles(file, &c); err != nil {
		return config.Config{}, errors.Tag(err)
	}
	normalizeBootstrapConfig(&c)
	if err = validateBootstrapConfig(c); err != nil {
		return config.Config{}, errors.Tag(err)
	}
	return c, nil
}

// validateBootstrapConfig 校验启动与热加载阶段必须立即失败的基础配置。
// 这里放置跨组件安全约束，避免错误配置进入运行态后才在后台组件里表现为半生效。
func validateBootstrapConfig(c config.Config) error {
	if err := validateTaskRedisConfig(c.Task.Redis); err != nil {
		return errors.Wrap(err, "校验任务系统 Redis 配置失败")
	}
	return nil
}

// normalizeBootstrapConfig 补齐跨配置块的默认值，避免样例配置重复维护公共参数。
func normalizeBootstrapConfig(c *config.Config) {
	if c == nil {
		return
	}
	inheritCollectorKafkaCommon(c)
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

// loadBaseConfig 只读取主配置文件，不处理 config_files 引用。
// 热加载指纹需要先解析主配置拿到外部文件路径，因此这里单独拆出基础加载能力。
func loadBaseConfig(file string) (config.Config, error) {
	var c config.Config
	if err := conf.Load(file, &c); err != nil {
		return config.Config{}, errors.Tag(err)
	}
	return c, nil
}

// ResolveRunMode 把命令行传入模式和配置文件模式合并成最终启动模式。
// 规则为：命令行 `-mode` 优先；未显式传入时回退到配置文件 `run_mode`；两者都未设置时使用默认全量模式。
func ResolveRunMode(cliMode *int, cfg config.Config) (int, error) {
	if cliMode != nil && *cliMode != 0 {
		return normalizeMode(*cliMode)
	}
	return normalizeMode(cfg.RunMode)
}

// Wire 作为应用装配入口，统一负责读取配置并构建 App。
// main 入口或其他部署形态只需要决定配置文件、运行模式和扩展选项。
func Wire(ctx context.Context, configFile string, mode int, options ...Option) (*App, error) {
	cfg, err := LoadConfig(configFile)
	if err != nil {
		return nil, errors.Tag(err)
	}
	app, err := New(ctx, cfg, mode, options...)
	if err != nil {
		return nil, errors.Tag(err)
	}
	app.BindConfigFile(configFile)
	return app, nil
}

// WireWithConfigMode 先读取配置，再按“命令行优先、配置兜底”的规则决定最终启动模式。
func WireWithConfigMode(ctx context.Context, configFile string, cliMode *int, options ...Option) (*App, error) {
	cfg, err := LoadConfig(configFile)
	if err != nil {
		return nil, errors.Tag(err)
	}
	mode, err := ResolveRunMode(cliMode, cfg)
	if err != nil {
		return nil, errors.Tag(err)
	}
	app, err := New(ctx, cfg, mode, options...)
	if err != nil {
		return nil, errors.Tag(err)
	}
	app.BindConfigFile(configFile)
	return app, nil
}

// WireDefault 使用默认插件集合装配应用，适合大多数标准部署入口直接复用。
func WireDefault(ctx context.Context, configFile string, mode int) (*App, error) {
	return Wire(ctx, configFile, mode)
}
