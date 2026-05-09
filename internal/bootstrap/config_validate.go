package bootstrap

import (
	"fmt"
	"strings"

	"github.com/Is999/go-utils/errors"

	keys "admin/common/rediskeys"
	"admin/internal/config"
)

const (
	minAdminJWTSecretLength = 16 // JWT 密钥最小长度，避免明显弱配置启动
	minAdminAppKeyLength    = 16 // 生产 app_key 最小长度
)

// Collector 载体枚举限定配置文件可声明的事件投递通道。
const (
	adminCollectorTransportAuto  = "auto"
	adminCollectorTransportKafka = "kafka"
	adminCollectorTransportRedis = "redis"
	adminCollectorTransportDB    = "db"
)

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

// validateAdminCollectorConfig 校验 Collector 载体配置是否自洽。
func validateAdminCollectorConfig(c config.Config) error {
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

// validateSecurityConfig 校验本地 secret_key 配置，避免安全中间件运行后才发现材料缺失。
func validateSecurityConfig(c config.Config) error {
	cfg := c.Security.SecretKey
	if !securitySecretKeyTouched(cfg) {
		return nil
	}
	if err := validateBinaryStatus("security.secret_key.sign_status", cfg.SignStatus); err != nil {
		return errors.Tag(err)
	}
	if err := validateBinaryStatus("security.secret_key.crypto_status", cfg.CryptoStatus); err != nil {
		return errors.Tag(err)
	}
	if cfg.GrayPercent < 0 || cfg.GrayPercent > 100 {
		return errors.Errorf("security.secret_key.gray_percent 必须在 0 到 100 之间")
	}
	if strings.TrimSpace(cfg.GrayVersion) != "" && cfg.GrayPercent <= 0 {
		return errors.Errorf("security.secret_key.gray_version 非空时 gray_percent 必须大于 0")
	}

	versionSet := make(map[string]struct{}, len(cfg.Versions))
	for i, item := range cfg.Versions {
		if err := validateSecretKeyVersion("security.secret_key.versions", i, item, cfg.SignStatus, cfg.CryptoStatus); err != nil {
			return errors.Tag(err)
		}
		versionSet[strings.TrimSpace(item.KeyVersion)] = struct{}{}
	}

	if len(cfg.Versions) == 0 {
		if err := validateSecretKeyVersion("security.secret_key", -1, config.SecuritySecretKeyVersionConfig{
			KeyVersion:             cfg.KeyVersion,
			AESKey:                 cfg.AESKey,
			AESKeyRef:              cfg.AESKeyRef,
			AESIV:                  cfg.AESIV,
			AESIVRef:               cfg.AESIVRef,
			RSAPublicKeyUser:       cfg.RSAPublicKeyUser,
			RSAPublicKeyUserRef:    cfg.RSAPublicKeyUserRef,
			RSAPrivateKeyServer:    cfg.RSAPrivateKeyServer,
			RSAPrivateKeyServerRef: cfg.RSAPrivateKeyServerRef,
		}, cfg.SignStatus, cfg.CryptoStatus); err != nil {
			return errors.Tag(err)
		}
		return nil
	}

	if stable := strings.TrimSpace(cfg.StableVersion); stable != "" {
		if _, ok := versionSet[stable]; !ok {
			return errors.Errorf("security.secret_key.stable_version 未在 versions 中定义")
		}
	}
	if gray := strings.TrimSpace(cfg.GrayVersion); gray != "" {
		if _, ok := versionSet[gray]; !ok {
			return errors.Errorf("security.secret_key.gray_version 未在 versions 中定义")
		}
	}
	return nil
}

// validateProductionConfig 校验生产环境禁止使用的占位和不安全配置。
func validateProductionConfig(c config.Config) error {
	if !isProductionMode(c.Mode) {
		return nil
	}
	if isPlaceholderSecret(c.JwtSecret) {
		return errors.Errorf("生产环境 jwt_secret 不能使用占位值")
	}
	if len(strings.TrimSpace(c.AppKey)) < minAdminAppKeyLength {
		return errors.Errorf("生产环境 app_key 长度不能小于 %d", minAdminAppKeyLength)
	}
	if isPlaceholderSecret(c.AppKey) {
		return errors.Errorf("生产环境 app_key 不能使用占位值")
	}
	if c.Redis.TLSInsecureSkipVerify {
		return errors.Errorf("生产环境 redis.tls_insecure_skip_verify 不能为 true")
	}
	if securitySecretKeyTouched(c.Security.SecretKey) &&
		strings.TrimSpace(c.Security.SecretKey.GrayVersion) != "" &&
		strings.TrimSpace(c.Security.SecretKey.GraySalt) == "" {
		return errors.Errorf("生产环境启用 secret_key 灰度版本时必须配置 gray_salt")
	}
	return nil
}

// validateBinaryStatus 校验配置中的二值开关只能使用 0 或 1。
func validateBinaryStatus(name string, value int) error {
	if value != 0 && value != 1 {
		return errors.Errorf("%s 只能为 0 或 1", name)
	}
	return nil
}

// validateSecretKeyVersion 校验单个秘钥版本在当前签名和加密开关下具备必要材料。
func validateSecretKeyVersion(prefix string, index int, cfg config.SecuritySecretKeyVersionConfig, signStatus, cryptoStatus int) error {
	name := prefix
	if index >= 0 {
		name = fmt.Sprintf("%s[%d]", prefix, index)
	}
	if strings.TrimSpace(cfg.KeyVersion) == "" {
		return errors.Errorf("%s.key_version 不能为空", name)
	}
	if cryptoStatus != 0 {
		if strings.TrimSpace(cfg.AESKey) == "" && strings.TrimSpace(cfg.AESKeyRef) == "" {
			return errors.Errorf("%s 启用加解密时必须配置 aes_key 或 aes_key_ref", name)
		}
		if strings.TrimSpace(cfg.AESIV) == "" && strings.TrimSpace(cfg.AESIVRef) == "" {
			return errors.Errorf("%s 启用加解密时必须配置 aes_iv 或 aes_iv_ref", name)
		}
	}
	if signStatus != 0 {
		if strings.TrimSpace(cfg.RSAPublicKeyUser) == "" && strings.TrimSpace(cfg.RSAPublicKeyUserRef) == "" {
			return errors.Errorf("%s 启用签名时必须配置 rsa_public_key_user 或 rsa_public_key_user_ref", name)
		}
		if strings.TrimSpace(cfg.RSAPrivateKeyServer) == "" && strings.TrimSpace(cfg.RSAPrivateKeyServerRef) == "" {
			return errors.Errorf("%s 启用签名时必须配置 rsa_private_key_server 或 rsa_private_key_server_ref", name)
		}
	}
	return nil
}

// securitySecretKeyTouched 判断 secret_key 是否显式配置过任一字段。
func securitySecretKeyTouched(cfg config.SecuritySecretKeyConfig) bool {
	return strings.TrimSpace(cfg.KeyVersion) != "" ||
		strings.TrimSpace(cfg.AESKey) != "" ||
		strings.TrimSpace(cfg.AESKeyRef) != "" ||
		strings.TrimSpace(cfg.AESIV) != "" ||
		strings.TrimSpace(cfg.AESIVRef) != "" ||
		strings.TrimSpace(cfg.RSAPublicKeyUser) != "" ||
		strings.TrimSpace(cfg.RSAPublicKeyUserRef) != "" ||
		strings.TrimSpace(cfg.RSAPublicKeyServer) != "" ||
		strings.TrimSpace(cfg.RSAPublicKeyServerRef) != "" ||
		strings.TrimSpace(cfg.RSAPrivateKeyServer) != "" ||
		strings.TrimSpace(cfg.RSAPrivateKeyServerRef) != "" ||
		strings.TrimSpace(cfg.StableVersion) != "" ||
		strings.TrimSpace(cfg.GrayVersion) != "" ||
		strings.TrimSpace(cfg.GraySalt) != "" ||
		(cfg.SignStatus != 0 && cfg.SignStatus != 1) ||
		(cfg.CryptoStatus != 0 && cfg.CryptoStatus != 1) ||
		len(cfg.Versions) > 0
}

// isProductionMode 判断当前运行模式是否属于生产环境。
func isProductionMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "pro", "prod", "production":
		return true
	default:
		return false
	}
}

// isPlaceholderSecret 判断密钥值是否仍是示例占位内容。
func isPlaceholderSecret(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return true
	}
	for _, pattern := range []string{"replace-with", "please-change", "change-me", "changeme", "your-", "todo"} {
		if strings.Contains(value, pattern) {
			return true
		}
	}
	return false
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
