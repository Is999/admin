package validators

import (
	"testing"

	"admin/internal/config"
)

// TestValidateBootstrapConfigRejectsProductionPlaceholderAppKey 确保生产环境不能使用占位 app_key。
func TestValidateBootstrapConfigRejectsProductionPlaceholderAppKey(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	cfg.AppKey = "replace-with-strong-app-key"
	if err := ValidateProduction(cfg); err == nil {
		t.Fatal("期望生产 app_key 占位值返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsProductionRedisTLSInsecure 确保生产环境不能跳过 Redis TLS 校验。
func TestValidateBootstrapConfigRejectsProductionRedisTLSInsecure(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	cfg.Redis.TLSInsecureSkipVerify = true
	if err := ValidateProduction(cfg); err == nil {
		t.Fatal("期望生产 redis.tls_insecure_skip_verify 返回错误，实际为 nil")
	}
}

// TestValidateProductionAllowsNoopVirusScanner 确保生产环境可以不部署外部病毒扫描服务。
func TestValidateProductionAllowsNoopVirusScanner(t *testing.T) {
	// cfg 模拟明确关闭病毒扫描的生产配置。
	cfg := validAdminProductionBootstrapConfig()
	cfg.FileStorage.VirusScanner.Name = config.VirusScannerNoop
	if err := ValidateProduction(cfg); err != nil {
		t.Fatalf("生产环境使用 noop 病毒扫描器不应失败: %v", err)
	}
}

// TestValidateProductionRequiresCollector 确保生产环境不会在管理员审计日志无法持久化时启动。
func TestValidateProductionRequiresCollector(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	cfg.Collector.Enabled = false
	if err := ValidateProduction(cfg); err == nil {
		t.Fatal("期望生产环境关闭 Collector 返回错误，实际为 nil")
	}
}

// TestValidateProductionRequiresBuiltinCollectorTasks 确保生产环境显式配置所有内置 Collector 任务。
func TestValidateProductionRequiresBuiltinCollectorTasks(t *testing.T) {
	tests := []struct {
		name    string // name 表示测试场景名称。
		bizType string // bizType 表示被移除的内置 Collector 业务类型。
	}{
		{name: "admin log", bizType: config.CollectorBizTypeAdminLogAudit},
		{name: "auth security", bizType: config.CollectorBizTypeAuthSecurity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validAdminProductionBootstrapConfig()
			delete(cfg.Collector.Tasks, tt.bizType)
			if err := ValidateProduction(cfg); err == nil {
				t.Fatalf("期望生产环境缺少 %s Collector 任务返回错误，实际为 nil", tt.bizType)
			}
		})
	}
}

// TestValidateProductionRequiresAdminLogCollectorRoute 确保管理员审计任务配置完整的 Topic 和消费组。
func TestValidateProductionRequiresAdminLogCollectorRoute(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	// adminLogTask 是待校验的管理员审计 Collector 任务配置。
	adminLogTask := cfg.Collector.Tasks[config.CollectorBizTypeAdminLogAudit]
	adminLogTask.GroupID = ""
	cfg.Collector.Tasks[config.CollectorBizTypeAdminLogAudit] = adminLogTask
	if err := ValidateProduction(cfg); err == nil {
		t.Fatal("期望生产环境管理员审计 Collector 任务缺少 group_id 返回错误，实际为 nil")
	}
}

// TestValidateProductionRequiresAuthSecurityTopic 确保认证风控事件只消费 API 约定的固定 Topic。
func TestValidateProductionRequiresAuthSecurityTopic(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	// authSecurityTask 是待校验的认证风控 Collector 任务配置。
	authSecurityTask := cfg.Collector.Tasks[config.CollectorBizTypeAuthSecurity]
	authSecurityTask.Topic = "wrong_auth_security_events"
	cfg.Collector.Tasks[config.CollectorBizTypeAuthSecurity] = authSecurityTask
	if err := ValidateProduction(cfg); err == nil {
		t.Fatal("期望生产环境认证风控 Collector Topic 错误返回错误，实际为 nil")
	}
}

// TestValidateProductionRejectsCDCValidationHandler 确保正式清洗器交付前不能在生产启用 CDC。
func TestValidateProductionRejectsCDCValidationHandler(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	cfg.CDC.Enabled = true
	if err := ValidateProduction(cfg); err == nil {
		t.Fatal("期望生产环境启用 CDC 验证处理器返回错误，实际为 nil")
	}
}

// TestValidateProductionRejectsAdminLogTestScenario 确保本地验证输出不会进入生产运行态。
func TestValidateProductionRejectsAdminLogTestScenario(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	cfg.TestScenarios.AdminLogAudit.CollectorEnabled = true
	if err := ValidateProduction(cfg); err == nil {
		t.Fatal("期望生产环境启用 admin_log 验证场景返回错误，实际为 nil")
	}
}

// TestValidateProductionRejectsUserTagSkeleton 确保用户标签业务阶段完成前不能在生产环境误启用。
func TestValidateProductionRejectsUserTagSkeleton(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	cfg.Workflows.UserTag.Enabled = true
	if err := ValidateProduction(cfg); err == nil {
		t.Fatal("期望生产环境启用用户标签骨架返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigRejectsSecurityGrayWithoutSaltInProduction 确保生产灰度秘钥需要稳定盐值。
func TestValidateBootstrapConfigRejectsSecurityGrayWithoutSaltInProduction(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	cfg.Security.SecretKey = validAdminSecuritySecretKey()
	cfg.Security.SecretKey.StableVersion = "v1"
	cfg.Security.SecretKey.GrayVersion = "v2"
	cfg.Security.SecretKey.GrayPercent = 10
	cfg.Security.SecretKey.GraySalt = ""
	cfg.Security.SecretKey.Versions = []config.SecuritySecretKeyVersionConfig{
		validAdminSecretKeyVersion("v1"),
		validAdminSecretKeyVersion("v2"),
	}
	if err := ValidateProduction(cfg); err == nil {
		t.Fatal("期望生产 secret_key 灰度缺少 gray_salt 返回错误，实际为 nil")
	}
}

// TestValidateBootstrapConfigAcceptsProductionSafeConfig 确保生产安全配置可以通过启动校验。
func TestValidateBootstrapConfigAcceptsProductionSafeConfig(t *testing.T) {
	cfg := validAdminProductionBootstrapConfig()
	cfg.Security.SecretKey = validAdminSecuritySecretKey()
	if err := ValidateProduction(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
