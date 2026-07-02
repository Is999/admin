package configload

import (
	"strings"

	"admin/common/idgen"
	"admin/internal/bootstrap/configload/validators"
	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
)

const (
	minAdminJWTSecretLength    = 16 // JWT 密钥最小长度，避免明显弱配置启动
	defaultUserRouteShardCount = 1  // 业务用户默认保持单张物理表
)

// Validate 校验启动与热加载阶段必须立即失败的基础配置。
// 这里放置跨组件安全约束，避免错误配置进入运行态后才在后台组件里表现为半生效。
func Validate(c config.Config) error {
	if err := validateCoreConfig(c); err != nil {
		return errors.Tag(err)
	}
	if err := validateSnowflakeConfig(c.Snowflake); err != nil {
		return errors.Tag(err)
	}
	if err := validateUserConfig(c.User); err != nil {
		return errors.Tag(err)
	}
	if err := ValidateTaskRedisConfig(c.Task.Redis); err != nil {
		return errors.Wrap(err, "校验任务系统 Redis 配置失败")
	}
	if err := validators.ValidateCollector(c); err != nil {
		return errors.Tag(err)
	}
	if err := validateAlertConfig(c.Alert); err != nil {
		return errors.Tag(err)
	}
	if err := validators.ValidateSecurity(c); err != nil {
		return errors.Tag(err)
	}
	if err := validators.ValidateProduction(c); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// validateCoreConfig 校验所有环境都必须满足的基础配置。
func validateCoreConfig(c config.Config) error {
	if len(strings.TrimSpace(c.JwtSecret)) < minAdminJWTSecretLength {
		return errors.Errorf("jwt_secret 长度不能小于 %d", minAdminJWTSecretLength)
	}
	if strings.TrimSpace(c.AppID) == "" {
		return errors.Errorf("app_id 不能为空")
	}
	if len(nonEmptyStrings(c.Redis.Addrs)) == 0 {
		return errors.Errorf("redis.addrs 不能为空")
	}
	if c.Redis.PoolSize <= 0 {
		return errors.Errorf("redis.pool_size 必须大于 0")
	}
	if typ := strings.ToLower(strings.TrimSpace(c.Redis.Type)); typ != "" && typ != "single" && typ != "cluster" {
		return errors.Errorf("redis.type 仅支持 single/cluster")
	}
	return nil
}

// validateSnowflakeConfig 校验分布式雪花 ID worker 配置。
func validateSnowflakeConfig(cfg config.SnowflakeConfig) error {
	if _, err := resolveSnowflakeWorkerID(cfg); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// resolveSnowflakeWorkerID 解析配置或环境变量中的显式 worker_id。
func resolveSnowflakeWorkerID(cfg config.SnowflakeConfig) (int64, error) {
	if cfg.WorkerID == nil {
		return idgen.ResolveWorkerID(idgen.SnowflakeWorkerIDUnset)
	}
	return idgen.ResolveWorkerID(*cfg.WorkerID)
}

// ConfigureSnowflakeWorkerID 发布当前进程使用的雪花 ID worker 配置。
func ConfigureSnowflakeWorkerID(cfg config.SnowflakeConfig) error {
	workerID, err := resolveSnowflakeWorkerID(cfg)
	if err != nil {
		return errors.Tag(err)
	}
	return idgen.ConfigureWorkerID(workerID)
}

// validateUserConfig 校验业务用户默认物理表数量，防止后台写入路由不可迁移。
func validateUserConfig(cfg config.UserConfig) error {
	if cfg.RouteShardCount == 0 || validUserRouteShardCount(cfg.RouteShardCount) {
		return nil
	}
	return errors.Errorf("user.route_shard_count 仅支持 1/2/4/8/16/32/64/128/256/512/1024")
}

// validUserRouteShardCount 判断业务用户物理表数量是否能平分 1024 逻辑分片。
func validUserRouteShardCount(routeShardCount int) bool {
	return routeShardCount > 0 && routeShardCount <= idgen.ShardMod && routeShardCount&(routeShardCount-1) == 0
}

// HasTaskRedisConfig 判断是否为任务系统显式配置了独立 Redis。
func HasTaskRedisConfig(cfg config.RedisConfig) bool {
	for _, addr := range cfg.Addrs {
		if strings.TrimSpace(addr) != "" {
			return true
		}
	}
	return false
}

// ValidateTaskRedisConfig 校验任务系统独立 Redis 配置是否完整。
// task.redis 只要出现任一非地址字段，就必须显式配置 addrs，避免半配置静默复用主 Redis。
func ValidateTaskRedisConfig(cfg config.RedisConfig) error {
	if !taskRedisConfigTouched(cfg) {
		return nil
	}
	if !HasTaskRedisConfig(cfg) {
		return errors.Errorf("task.redis 已配置非地址字段时必须同时配置 addrs")
	}
	return nil
}

// taskRedisConfigTouched 判断 task.redis 是否被显式配置过。
func taskRedisConfigTouched(cfg config.RedisConfig) bool {
	return strings.TrimSpace(cfg.Type) != "" ||
		len(cfg.Addrs) > 0 ||
		len(cfg.AddrMap) > 0 ||
		strings.TrimSpace(cfg.Password) != "" ||
		cfg.DB != 0 ||
		cfg.PoolSize != 0 ||
		cfg.TLS ||
		cfg.TLSInsecureSkipVerify
}

// validateAlertConfig 校验外部告警通道的最小可用配置。
func validateAlertConfig(cfg config.AlertConfig) error {
	if !cfg.Lark.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.Lark.WebhookURL) == "" && strings.TrimSpace(cfg.Lark.WebhookURLRef) == "" {
		return errors.Errorf("alert.lark.enabled=true 时必须配置 webhook_url 或 webhook_url_ref")
	}
	if cfg.Lark.TimeoutSeconds < 0 {
		return errors.Errorf("alert.lark.timeout_seconds 不能小于 0")
	}
	if cfg.Lark.MaxErrorBytes < 0 {
		return errors.Errorf("alert.lark.max_error_bytes 不能小于 0")
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
