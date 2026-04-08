package logic

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	keys "admin_cron/common/rediskeys"
	"admin_cron/internal/model"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// seedBoolSecurityConfig 在 Redis 中写入布尔型安全配置缓存，供单测直接复用读取链路。
func seedBoolSecurityConfig(t *testing.T, client *redis.Client, uuid string, enabled bool) {
	t.Helper()
	if client == nil {
		t.Fatalf("seedBoolSecurityConfig client is nil")
	}
	value := "false"
	if enabled {
		value = "true"
	}
	cacheKey := keys.TableCachePrefix("") + fmt.Sprintf(keys.SysConfigUUID, uuid)
	if err := client.HSet(context.Background(), cacheKey, map[string]any{
		"type":  "6",
		"value": value,
	}).Err(); err != nil {
		t.Fatalf("seedBoolSecurityConfig HSet failed: %v", err)
	}
}

// TestBuildCompatUserInfoReturnsMFAURLWhenDisabled 验证未启用 MFA 但已有秘钥时，个人中心仍返回二维码地址供继续绑定。
func TestBuildCompatUserInfoReturnsMFAURLWhenDisabled(t *testing.T) {
	securityLogic := newTestSecurityLogic()
	oldSecret := "RCABDVITFNQJJ4VJ"
	cipherText, err := securityLogic.encryptAdminMFASecret(oldSecret)
	if err != nil {
		t.Fatalf("encryptAdminMFASecret failed: %v", err)
	}
	userLogic := &UserCompatLogic{BaseLogic: securityLogic.BaseLogic}
	info, err := userLogic.BuildCompatUserInfo(&model.Admin{
		ID:           9,
		Name:         "admin999",
		MfaSecureKey: cipherText,
		MfaStatus:    0,
	}, "test-token")
	if err != nil {
		t.Fatalf("buildCompatUserInfo failed: %v", err)
	}
	if info == nil || info.BuildMFAURL == "" {
		t.Fatalf("buildCompatUserInfo() BuildMFAURL empty, want non-empty")
	}
	if strings.Contains(info.BuildMFAURL, oldSecret) {
		t.Fatalf("buildCompatUserInfo() should issue fresh MFA url, got reused old secret url %q", info.BuildMFAURL)
	}
}

// TestBuildCompatUserInfoSkipsLoginMFAWhenNeedResetPassword 验证必须改密时不会在登录后先触发 MFA 校验。
func TestBuildCompatUserInfoSkipsLoginMFAWhenNeedResetPassword(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	securityLogic := newTestSecurityLogic()
	securityLogic.svc.Rds = client
	cipherText, err := securityLogic.encryptAdminMFASecret("RCABDVITFNQJJ4VJ")
	if err != nil {
		t.Fatalf("encryptAdminMFASecret failed: %v", err)
	}
	userLogic := &UserCompatLogic{BaseLogic: securityLogic.BaseLogic}
	info, err := userLogic.BuildCompatUserInfo(&model.Admin{
		ID:                9,
		Name:              "admin999",
		MfaSecureKey:      cipherText,
		MfaStatus:         1,
		NeedResetPassword: 1,
	}, "test-token")
	if err != nil {
		t.Fatalf("buildCompatUserInfo failed: %v", err)
	}
	if info.NeedResetPassword != 1 {
		t.Fatalf("buildCompatUserInfo() needResetPassword = %d, want 1", info.NeedResetPassword)
	}
	if info.MFACheck != 0 {
		t.Fatalf("buildCompatUserInfo() mfaCheck = %d, want 0", info.MFACheck)
	}
}

// TestBuildCompatUserInfoRequiresBindWhenForceMFAEnabled 验证系统强制启用 MFA 时，未启用账号登录会被要求先绑定并校验。
func TestBuildCompatUserInfoRequiresBindWhenForceMFAEnabled(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	securityLogic := newTestSecurityLogic()
	securityLogic.svc.Rds = client
	seedBoolSecurityConfig(t, client, ConfigAdminMFACheckEnable, true)
	cipherText, err := securityLogic.encryptAdminMFASecret("RCABDVITFNQJJ4VJ")
	if err != nil {
		t.Fatalf("encryptAdminMFASecret failed: %v", err)
	}

	userLogic := &UserCompatLogic{BaseLogic: securityLogic.BaseLogic}
	info, err := userLogic.BuildCompatUserInfo(&model.Admin{
		ID:           9,
		Name:         "admin999",
		MfaSecureKey: cipherText,
		MfaStatus:    0,
	}, "test-token")
	if err != nil {
		t.Fatalf("buildCompatUserInfo failed: %v", err)
	}
	if info == nil {
		t.Fatalf("buildCompatUserInfo() returned nil info")
	}
	if !info.ForceMFAEnabled {
		t.Fatalf("buildCompatUserInfo() forceMFAEnabled = false, want true")
	}
	if !info.MFABindRequired {
		t.Fatalf("buildCompatUserInfo() mfaBindRequired = false, want true")
	}
	if info.MFACheck != 1 {
		t.Fatalf("buildCompatUserInfo() mfaCheck = %d, want 1", info.MFACheck)
	}
	if info.BuildMFAURL == "" {
		t.Fatalf("buildCompatUserInfo() BuildMFAURL empty, want non-empty")
	}
}

// TestBuildCompatUserInfoKeepsEnabledMFAOutOfSelfRebind 验证账号已启用 MFA 时，即使秘钥不可用也不返回自助换绑信息。
func TestBuildCompatUserInfoKeepsEnabledMFAOutOfSelfRebind(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	securityLogic := newTestSecurityLogic()
	securityLogic.svc.Rds = client
	seedBoolSecurityConfig(t, client, ConfigAdminMFACheckEnable, true)

	userLogic := &UserCompatLogic{BaseLogic: securityLogic.BaseLogic}
	info, err := userLogic.BuildCompatUserInfo(&model.Admin{
		ID:           10,
		Name:         "admin-enabled",
		MfaSecureKey: "broken-secret",
		MfaStatus:    1,
	}, "test-token")
	if err != nil {
		t.Fatalf("buildCompatUserInfo failed: %v", err)
	}
	if info == nil {
		t.Fatalf("buildCompatUserInfo() returned nil info")
	}
	if info.ExistMFA {
		t.Fatalf("buildCompatUserInfo() existMFA = true, want false")
	}
	if info.MFABindRequired {
		t.Fatalf("buildCompatUserInfo() mfaBindRequired = true, want false")
	}
	if info.BuildMFAURL != "" {
		t.Fatalf("buildCompatUserInfo() BuildMFAURL = %q, want empty", info.BuildMFAURL)
	}
	if info.MFACheck != 1 {
		t.Fatalf("buildCompatUserInfo() mfaCheck = %d, want 1", info.MFACheck)
	}
}

// TestMarkLoginMFACompletedAfterEnable 验证启用 MFA 后补写登录 MFA 完成标记时，后续个人资料查询不会立刻被自己拦截。
func TestMarkLoginMFACompletedAfterEnable(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	svcCtx := &svc.ServiceContext{Rds: client}
	logicObj := NewSecurityLogic(context.Background(), svcCtx)
	admin := &model.Admin{
		ID:            2,
		Name:          "admin999",
		MfaStatus:     1,
		LastLoginTime: time.Now(),
	}
	if err := logicObj.MarkLoginMFACompleted(admin.ID); err != nil {
		t.Fatalf("MarkLoginMFACompleted failed: %v", err)
	}
	if err := logicObj.checkAdminMFA(admin); err != nil {
		t.Fatalf("checkAdminMFA after MarkLoginMFACompleted = %v, want nil", err)
	}
	cacheKey := fmt.Sprintf(keys.LoginCheckMFAFlag, admin.ID)
	if !server.Exists(cacheKey) {
		t.Fatalf("login MFA flag key %s not found", cacheKey)
	}
}

// TestSyncLoginMFAAfterPasswordUpdateForForcedReset 验证首次登录改密完成后会补写登录 MFA 完成标记。
func TestSyncLoginMFAAfterPasswordUpdateForForcedReset(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	securityLogic := newTestSecurityLogic()
	securityLogic.svc.Rds = client
	userLogic := &UserCompatLogic{BaseLogic: securityLogic.BaseLogic}

	userLogic.syncLoginMFAAfterPasswordUpdate(&model.Admin{
		ID:                21,
		Name:              "admin-reset",
		NeedResetPassword: 1,
	})

	cacheKey := fmt.Sprintf(keys.LoginCheckMFAFlag, 21)
	if !server.Exists(cacheKey) {
		t.Fatalf("login MFA flag key %s not found after forced reset password update", cacheKey)
	}
}

// TestSyncLoginMFAAfterPasswordUpdateKeepsExistingFlag 验证普通改自己密码不会清空当前会话已完成的登录 MFA 标记。
func TestSyncLoginMFAAfterPasswordUpdateKeepsExistingFlag(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	securityLogic := newTestSecurityLogic()
	securityLogic.svc.Rds = client
	userLogic := &UserCompatLogic{BaseLogic: securityLogic.BaseLogic}

	cacheKey := fmt.Sprintf(keys.LoginCheckMFAFlag, 22)
	if err := client.Set(context.Background(), cacheKey, 123456789, time.Minute).Err(); err != nil {
		t.Fatalf("seed login MFA flag failed: %v", err)
	}

	userLogic.syncLoginMFAAfterPasswordUpdate(&model.Admin{
		ID:                22,
		Name:              "admin-normal",
		NeedResetPassword: 0,
	})

	value, err := client.Get(context.Background(), cacheKey).Result()
	if err != nil {
		t.Fatalf("Get(%s) error = %v", cacheKey, err)
	}
	if value != "123456789" {
		t.Fatalf("login MFA flag = %s, want 123456789", value)
	}
}

// TestSyncCurrentAdminNeedResetPassword 验证个人中心改密成功后，会立即把当前登录态缓存里的 needResetPassword 同步为 0。
func TestSyncCurrentAdminNeedResetPassword(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	securityLogic := newTestSecurityLogic()
	securityLogic.svc.Rds = client
	userLogic := &UserCompatLogic{BaseLogic: securityLogic.BaseLogic}

	cacheLogic := NewCacheLogic(context.Background(), securityLogic.svc)
	if err := cacheLogic.SetAdminInfo(26, &types.AdminInfo{
		ID:                26,
		UserName:          "admin-reset-flag",
		NeedResetPassword: 1,
		Token:             "token-26",
	}); err != nil {
		t.Fatalf("SetAdminInfo failed: %v", err)
	}

	userLogic.syncCurrentAdminNeedResetPassword(26, 0)

	info, err := cacheLogic.GetAdminInfo(26)
	if err != nil {
		t.Fatalf("GetAdminInfo failed: %v", err)
	}
	if info.NeedResetPassword != 0 {
		t.Fatalf("needResetPassword = %d, want 0", info.NeedResetPassword)
	}
}

// TestSyncLoginMFAAfterDisableKeepsCurrentSessionWhenForceEnabled 验证强制 MFA 下当前会话保持有效。
func TestSyncLoginMFAAfterDisableKeepsCurrentSessionWhenForceEnabled(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	securityLogic := newTestSecurityLogic()
	securityLogic.svc.Rds = client
	seedBoolSecurityConfig(t, client, ConfigAdminMFACheckEnable, true)
	userLogic := &UserCompatLogic{BaseLogic: securityLogic.BaseLogic}

	userLogic.syncLoginMFAAfterStatusUpdate(securityLogic, &model.Admin{
		ID:   23,
		Name: "admin-disable-force",
	}, 0)

	cacheKey := fmt.Sprintf(keys.LoginCheckMFAFlag, 23)
	if !server.Exists(cacheKey) {
		t.Fatalf("login MFA flag key %s not found after disabling MFA in force mode", cacheKey)
	}
}

// TestSyncLoginMFAAfterDisableKeepsCurrentSessionWhenForceDisabled 验证非强制 MFA 下当前会话保持有效。
func TestSyncLoginMFAAfterDisableKeepsCurrentSessionWhenForceDisabled(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	securityLogic := newTestSecurityLogic()
	securityLogic.svc.Rds = client
	userLogic := &UserCompatLogic{BaseLogic: securityLogic.BaseLogic}

	cacheKey := fmt.Sprintf(keys.LoginCheckMFAFlag, 24)
	if err := client.Set(context.Background(), cacheKey, 123456789, time.Minute).Err(); err != nil {
		t.Fatalf("seed login MFA flag failed: %v", err)
	}

	userLogic.syncLoginMFAAfterStatusUpdate(securityLogic, &model.Admin{
		ID:   24,
		Name: "admin-disable-normal",
	}, 0)

	if !server.Exists(cacheKey) {
		t.Fatalf("login MFA flag key %s should keep existing after disabling MFA without force mode", cacheKey)
	}
}

// TestSyncLoginMFAAfterEnableKeepsCurrentSession 验证启用 MFA 后补写当前会话标记。
func TestSyncLoginMFAAfterEnableKeepsCurrentSession(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	securityLogic := newTestSecurityLogic()
	securityLogic.svc.Rds = client
	userLogic := &UserCompatLogic{BaseLogic: securityLogic.BaseLogic}

	userLogic.syncLoginMFAAfterStatusUpdate(securityLogic, &model.Admin{
		ID:   25,
		Name: "admin-enable",
	}, 1)

	cacheKey := fmt.Sprintf(keys.LoginCheckMFAFlag, 25)
	if !server.Exists(cacheKey) {
		t.Fatalf("login MFA flag key %s not found after enabling MFA", cacheKey)
	}
}

// TestResolveEnableMFASecretUsesRequestSecret 验证启用 MFA 时优先使用请求新秘钥。
func TestResolveEnableMFASecretUsesRequestSecret(t *testing.T) {
	securityLogic := newTestSecurityLogic()
	oldSecret := "RCABDVITFNQJJ4VJ"
	requestSecret := "JBSWY3DPEHPK3PXP"
	cipherText, err := securityLogic.encryptAdminMFASecret(oldSecret)
	if err != nil {
		t.Fatalf("encryptAdminMFASecret failed: %v", err)
	}
	userLogic := &UserCompatLogic{BaseLogic: securityLogic.BaseLogic}
	secret, err := userLogic.resolveEnableMFASecret(&model.Admin{
		ID:           11,
		Name:         "admin-request",
		MfaSecureKey: cipherText,
		MfaStatus:    0,
	}, requestSecret, &mfaTwoStepTicketPayload{
		Scenario:     MFAScenarioStatus,
		Value:        "ticket-value",
		SecretSource: mfaTwoStepSecretSourceRequest,
		SecretDigest: hashMFASecret(requestSecret),
	})
	if err != nil {
		t.Fatalf("resolveEnableMFASecret failed: %v", err)
	}
	if secret != requestSecret {
		t.Fatalf("resolveEnableMFASecret() = %q, want %q", secret, requestSecret)
	}
}

// TestResolveEnableMFASecretUsesCurrentSecret 验证启用 MFA 时可保留数据库旧秘钥。
func TestResolveEnableMFASecretUsesCurrentSecret(t *testing.T) {
	securityLogic := newTestSecurityLogic()
	currentSecret := "RCABDVITFNQJJ4VJ"
	requestSecret := "JBSWY3DPEHPK3PXP"
	cipherText, err := securityLogic.encryptAdminMFASecret(currentSecret)
	if err != nil {
		t.Fatalf("encryptAdminMFASecret failed: %v", err)
	}
	userLogic := &UserCompatLogic{BaseLogic: securityLogic.BaseLogic}
	secret, err := userLogic.resolveEnableMFASecret(&model.Admin{
		ID:           12,
		Name:         "admin-current",
		MfaSecureKey: cipherText,
		MfaStatus:    0,
	}, requestSecret, &mfaTwoStepTicketPayload{
		Scenario:     MFAScenarioStatus,
		Value:        "ticket-value",
		SecretSource: mfaTwoStepSecretSourceCurrent,
		SecretDigest: hashMFASecret(currentSecret),
	})
	if err != nil {
		t.Fatalf("resolveEnableMFASecret failed: %v", err)
	}
	if secret != currentSecret {
		t.Fatalf("resolveEnableMFASecret() = %q, want %q", secret, currentSecret)
	}
}

// TestCheckAdminMFARequiresWhenForceMFAEnabledAndDisabled 验证系统强制启用 MFA 时，未启用账号访问会被登录态 MFA 拦截。
func TestCheckAdminMFARequiresWhenForceMFAEnabledAndDisabled(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	securityLogic := newTestSecurityLogic()
	securityLogic.svc.Rds = client
	seedBoolSecurityConfig(t, client, ConfigAdminMFACheckEnable, true)

	err := securityLogic.checkAdminMFA(&model.Admin{
		ID:        9,
		Name:      "admin999",
		MfaStatus: 0,
	})
	if err != ErrAdminMFABindRequired {
		t.Fatalf("checkAdminMFA() error = %v, want %v", err, ErrAdminMFABindRequired)
	}
}
