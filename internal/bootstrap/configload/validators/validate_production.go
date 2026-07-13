package validators

import (
	"strings"

	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
)

const minAdminAppKeyLength = 16 // 生产 app_key 最小长度

// ValidateProduction 校验生产环境禁止使用的占位和不安全配置。
func ValidateProduction(c config.Config) error {
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
	if !c.Collector.Enabled {
		return errors.Errorf("生产环境必须启用 collector，管理员审计日志依赖该链路持久化")
	}
	// adminLogTask 是管理员审计日志必须显式配置的 Collector 任务。
	adminLogTask, ok := c.Collector.Tasks[config.CollectorBizTypeAdminLogAudit]
	if !ok {
		return errors.Errorf("生产环境 collector.tasks 必须显式配置 %s", config.CollectorBizTypeAdminLogAudit)
	}
	if strings.TrimSpace(adminLogTask.Topic) == "" || strings.TrimSpace(adminLogTask.GroupID) == "" {
		return errors.Errorf("生产环境 collector.tasks.%s 必须配置 topic 和 group_id", config.CollectorBizTypeAdminLogAudit)
	}
	// authSecurityTask 是前台认证风控必须显式配置的 Collector 任务。
	authSecurityTask, ok := c.Collector.Tasks[config.CollectorBizTypeAuthSecurity]
	if !ok {
		return errors.Errorf("生产环境 collector.tasks 必须显式配置 %s", config.CollectorBizTypeAuthSecurity)
	}
	if strings.TrimSpace(authSecurityTask.Topic) != config.CollectorTopicAuthSecurity {
		return errors.Errorf("生产环境 collector.tasks.%s.topic 必须为 %s", config.CollectorBizTypeAuthSecurity, config.CollectorTopicAuthSecurity)
	}
	if strings.TrimSpace(authSecurityTask.GroupID) == "" {
		return errors.Errorf("生产环境 collector.tasks.%s 必须配置 group_id", config.CollectorBizTypeAuthSecurity)
	}
	if c.CDC.Enabled {
		return errors.Errorf("CDC 当前只有本地验证处理器，生产环境不能启用 cdc.enabled")
	}
	if c.TestScenarios.AdminLogAudit.LarkEnabled || c.TestScenarios.AdminLogAudit.CollectorEnabled {
		return errors.Errorf("生产环境不能启用 test_scenarios.admin_log_audit")
	}
	if c.Workflows.UserTag.Enabled {
		return errors.Errorf("用户标签业务计算尚未实现，生产环境不能启用 workflows.user_tag.enabled")
	}
	if securitySecretKeyTouched(c.Security.SecretKey) &&
		strings.TrimSpace(c.Security.SecretKey.GrayVersion) != "" &&
		strings.TrimSpace(c.Security.SecretKey.GraySalt) == "" {
		return errors.Errorf("生产环境启用 secret_key 灰度版本时必须配置 gray_salt")
	}
	return nil
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
