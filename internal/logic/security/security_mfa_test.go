package security

import (
	"context"
	"strings"
	"testing"
	"time"

	"admin/internal/config"
	"admin/internal/model"
	"admin/internal/svc"

	"github.com/alicebob/miniredis/v2"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"
)

// newTestSecurityLogic 创建仅包含 MFA 加密配置的测试逻辑对象。
func newTestSecurityLogic() *SecurityLogic {
	return newTestSecurityLogicWithAppID("site-a")
}

// newTestSecurityLogicWithAppID 创建指定 app_id 的测试逻辑对象。
func newTestSecurityLogicWithAppID(appID string) *SecurityLogic {
	svcCtx := svc.NewServiceContext(config.Config{
		AppID:  appID,
		AppKey: "unit-test-app-key",
		// JwtExpiresIn 用于 CacheLogic.SetAdminInfo 写入过期时间，避免测试环境默认 0 导致缓存立即过期。
		JwtExpiresIn: 3600,
	}, svc.Dependencies{})
	return NewSecurityLogic(context.Background(), svcCtx)
}

// TestEncryptDecryptAdminMFASecret 验证管理员 MFA 秘钥会以密文写库，并可正确解密回原始种子。
func TestEncryptDecryptAdminMFASecret(t *testing.T) {
	logicObj := newTestSecurityLogic()
	plain := "JBSWY3DPEHPK3PXP"

	cipherText, err := logicObj.encryptAdminMFASecret(plain)
	if err != nil {
		t.Fatalf("encryptAdminMFASecret failed: %v", err)
	}
	if cipherText == plain {
		t.Fatalf("cipher text should not equal plain text")
	}
	if !strings.HasPrefix(cipherText, mfaSecretCipherPrefix) {
		t.Fatalf("cipher text prefix mismatch: %s", cipherText)
	}

	decrypted, err := logicObj.decryptAdminMFASecret(cipherText)
	if err != nil {
		t.Fatalf("decryptAdminMFASecret failed: %v", err)
	}
	if decrypted != plain {
		t.Fatalf("decrypt result mismatch, got %s want %s", decrypted, plain)
	}
}

// TestVerifyMFACodeWithEncryptedSecret 验证动态码校验会先解密库内密文，再参与 TOTP 校验。
func TestVerifyMFACodeWithEncryptedSecret(t *testing.T) {
	logicObj := newTestSecurityLogic()
	plain := "JBSWY3DPEHPK3PXP"

	cipherText, err := logicObj.encryptAdminMFASecret(plain)
	if err != nil {
		t.Fatalf("encryptAdminMFASecret failed: %v", err)
	}

	code, err := totp.GenerateCodeCustom(plain, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("GenerateCodeCustom failed: %v", err)
	}

	admin := &model.Admin{
		ID:           1,
		Name:         "super999",
		MfaSecureKey: cipherText,
		MfaStatus:    1,
	}
	if err := logicObj.VerifyMFACode(admin, code); err != nil {
		t.Fatalf("VerifyMFACode failed: %v", err)
	}
}

// TestBuildAdminMFAURLUsesMicrosoftAuthenticatorFormat 验证 MFA 绑定地址会对 label 做编码，并固定微软 Authenticator 所需参数顺序。
func TestBuildAdminMFAURLUsesMicrosoftAuthenticatorFormat(t *testing.T) {
	logicObj := newTestSecurityLogicWithAppID("102")
	plain := "RCABDVITFNQJJ4VJ"
	cipherText, err := logicObj.encryptAdminMFASecret(plain)
	if err != nil {
		t.Fatalf("encryptAdminMFASecret failed: %v", err)
	}
	admin := &model.Admin{
		ID:           9,
		Name:         "admin999",
		MfaSecureKey: cipherText,
		MfaStatus:    0,
	}
	got, err := logicObj.BuildAdminMFAURL(admin)
	if err != nil {
		t.Fatalf("BuildAdminMFAURL failed: %v", err)
	}
	want := "otpauth://totp/admin-102%3Aadmin999?secret=RCABDVITFNQJJ4VJ&issuer=admin-102&algorithm=SHA1&digits=6&period=30"
	if got != want {
		t.Fatalf("BuildAdminMFAURL = %q, want %q", got, want)
	}
}

// TestBuildAdminMFAURLUsesDefaultIssuerWithAppID 验证默认发行方会携带当前 app_id。
func TestBuildAdminMFAURLUsesDefaultIssuerWithAppID(t *testing.T) {
	got, err := newTestSecurityLogic().BuildAdminMFAURLBySecret(&model.Admin{
		ID:   9,
		Name: "admin999",
	}, "RCABDVITFNQJJ4VJ")
	if err != nil {
		t.Fatalf("BuildAdminMFAURLBySecret failed: %v", err)
	}
	want := "otpauth://totp/admin-site-a%3Aadmin999?secret=RCABDVITFNQJJ4VJ&issuer=admin-site-a&algorithm=SHA1&digits=6&period=30"
	if got != want {
		t.Fatalf("BuildAdminMFAURLBySecret = %q, want %q", got, want)
	}
}

// TestBuildFreshAdminMFAURLKeepsDatabaseSecret 验证刷新二维码生成的新秘钥不会直接替换库内正式秘钥。
func TestBuildFreshAdminMFAURLKeepsDatabaseSecret(t *testing.T) {
	logicObj := newTestSecurityLogic()
	oldSecret := "RCABDVITFNQJJ4VJ"
	oldCipher, err := logicObj.encryptAdminMFASecret(oldSecret)
	if err != nil {
		t.Fatalf("encryptAdminMFASecret failed: %v", err)
	}
	admin := &model.Admin{
		ID:           9,
		Name:         "admin999",
		MfaSecureKey: oldCipher,
		MfaStatus:    0,
	}

	got, err := logicObj.BuildFreshAdminMFAURL(admin)
	if err != nil {
		t.Fatalf("BuildFreshAdminMFAURL failed: %v", err)
	}
	if got == "" {
		t.Fatalf("BuildFreshAdminMFAURL returned empty url")
	}
	if strings.Contains(got, oldSecret) {
		t.Fatalf("BuildFreshAdminMFAURL should not reuse old database secret")
	}
	if admin.MfaSecureKey != oldCipher {
		t.Fatalf("admin.MfaSecureKey changed after build fresh url, got %q want %q", admin.MfaSecureKey, oldCipher)
	}
}

// TestVerifyBindingMFACodeUsesRequestSecret 验证未启用场景会优先使用前端回传的新秘钥做校验。
func TestVerifyBindingMFACodeUsesRequestSecret(t *testing.T) {
	logicObj := newTestSecurityLogic()
	oldSecret := "RCABDVITFNQJJ4VJ"
	requestSecret := "JBSWY3DPEHPK3PXP"
	oldCipher, err := logicObj.encryptAdminMFASecret(oldSecret)
	if err != nil {
		t.Fatalf("encryptAdminMFASecret old secret failed: %v", err)
	}
	admin := &model.Admin{
		ID:           9,
		Name:         "admin999",
		MfaSecureKey: oldCipher,
		MfaStatus:    0,
	}
	code, err := totp.GenerateCodeCustom(requestSecret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("GenerateCodeCustom failed: %v", err)
	}
	verifyResult, err := logicObj.VerifyBindingMFACodeDetail(admin, requestSecret, code)
	if err != nil {
		t.Fatalf("VerifyBindingMFACode failed: %v", err)
	}
	if verifyResult == nil {
		t.Fatalf("VerifyBindingMFACodeDetail returned nil result")
	}
	if verifyResult.SecretSource != mfaTwoStepSecretSourceRequest {
		t.Fatalf("VerifyBindingMFACodeDetail secret source = %q, want %q", verifyResult.SecretSource, mfaTwoStepSecretSourceRequest)
	}
	if verifyResult.SecretDigest != hashMFASecret(requestSecret) {
		t.Fatalf("VerifyBindingMFACodeDetail secret digest mismatch, got %q", verifyResult.SecretDigest)
	}
	if err := logicObj.VerifyMFACode(admin, code); err != ErrAdminMFACodeInvalid {
		t.Fatalf("VerifyMFACode error = %v, want %v", err, ErrAdminMFACodeInvalid)
	}
}

// TestVerifyBindingMFACodeAllowsRequestSecretWhenEnabledSecretMissing 验证已启用但库内秘钥不可用时，登录绑定可使用本次新秘钥完成校验。
func TestVerifyBindingMFACodeAllowsRequestSecretWhenEnabledSecretMissing(t *testing.T) {
	logicObj := newTestSecurityLogic()
	requestSecret := "JBSWY3DPEHPK3PXP"
	code, err := totp.GenerateCodeCustom(requestSecret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("GenerateCodeCustom failed: %v", err)
	}

	verifyResult, err := logicObj.VerifyBindingMFACodeDetail(&model.Admin{
		ID:           16,
		Name:         "admin-enabled-missing-secret",
		MfaSecureKey: "",
		MfaStatus:    1,
	}, requestSecret, code)
	if err != nil {
		t.Fatalf("VerifyBindingMFACodeDetail failed: %v", err)
	}
	if verifyResult == nil {
		t.Fatalf("VerifyBindingMFACodeDetail returned nil result")
	}
	if verifyResult.SecretSource != mfaTwoStepSecretSourceRequest {
		t.Fatalf("VerifyBindingMFACodeDetail secret source = %q, want %q", verifyResult.SecretSource, mfaTwoStepSecretSourceRequest)
	}
	if verifyResult.SecretDigest != hashMFASecret(requestSecret) {
		t.Fatalf("VerifyBindingMFACodeDetail secret digest mismatch, got %q", verifyResult.SecretDigest)
	}
}

// TestVerifyBindingMFACodeFallsBackToCurrentSecret 验证新秘钥失败后回退当前秘钥。
func TestVerifyBindingMFACodeFallsBackToCurrentSecret(t *testing.T) {
	logicObj := newTestSecurityLogic()
	currentSecret := "RCABDVITFNQJJ4VJ"
	requestSecret := "JBSWY3DPEHPK3PXP"
	currentCipher, err := logicObj.encryptAdminMFASecret(currentSecret)
	if err != nil {
		t.Fatalf("encryptAdminMFASecret current secret failed: %v", err)
	}
	admin := &model.Admin{
		ID:           10,
		Name:         "admin-fallback",
		MfaSecureKey: currentCipher,
		MfaStatus:    0,
	}
	code, err := totp.GenerateCodeCustom(currentSecret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("GenerateCodeCustom failed: %v", err)
	}
	verifyResult, err := logicObj.VerifyBindingMFACodeDetail(admin, requestSecret, code)
	if err != nil {
		t.Fatalf("VerifyBindingMFACodeDetail failed: %v", err)
	}
	if verifyResult == nil {
		t.Fatalf("VerifyBindingMFACodeDetail returned nil result")
	}
	if verifyResult.SecretSource != mfaTwoStepSecretSourceCurrent {
		t.Fatalf("VerifyBindingMFACodeDetail secret source = %q, want %q", verifyResult.SecretSource, mfaTwoStepSecretSourceCurrent)
	}
	if verifyResult.SecretDigest != hashMFASecret(currentSecret) {
		t.Fatalf("VerifyBindingMFACodeDetail secret digest mismatch, got %q", verifyResult.SecretDigest)
	}
}

// TestConsumeMFATwoStepTicketPreservesVerifyResult 验证二次票据会保存本次绑定校验通过的秘钥来源与摘要。
func TestConsumeMFATwoStepTicketPreservesVerifyResult(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := newTestSecurityLogic()
	logicObj.Svc.Rds = client
	verifyResult := &mfaBindingVerifyResult{
		SecretSource: mfaTwoStepSecretSourceRequest,
		SecretDigest: hashMFASecret("JBSWY3DPEHPK3PXP"),
	}
	twoStep, err := logicObj.IssueMFATwoStepTicketWithVerifyResult(99, MFAScenarioStatus, verifyResult)
	if err != nil {
		t.Fatalf("IssueMFATwoStepTicketWithVerifyResult failed: %v", err)
	}
	payload, err := logicObj.ConsumeMFATwoStepTicket(99, MFAScenarioStatus, twoStep.Key, twoStep.Value)
	if err != nil {
		t.Fatalf("ConsumeMFATwoStepTicket failed: %v", err)
	}
	if payload == nil {
		t.Fatalf("ConsumeMFATwoStepTicket returned nil payload")
	}
	if payload.SecretSource != verifyResult.SecretSource {
		t.Fatalf("payload secret source = %q, want %q", payload.SecretSource, verifyResult.SecretSource)
	}
	if payload.SecretDigest != verifyResult.SecretDigest {
		t.Fatalf("payload secret digest = %q, want %q", payload.SecretDigest, verifyResult.SecretDigest)
	}
}

// TestClearAdminMFATwoStepTicketsUsesIndex 验证 MFA 二次票据按索引精确清理，不依赖 Redis SCAN。
func TestClearAdminMFATwoStepTicketsUsesIndex(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := newTestSecurityLogic()
	logicObj.Svc.Rds = client

	twoStep, err := logicObj.IssueMFATwoStepTicket(99, MFAScenarioEditUser)
	if err != nil {
		t.Fatalf("IssueMFATwoStepTicket failed: %v", err)
	}
	adminTicketKey := logicObj.mfaTwoStepTicketKey(99, twoStep.Key)
	otherTicketKey := logicObj.mfaTwoStepTicketKey(100, "keep")
	if err := client.Set(context.Background(), otherTicketKey, "demo", time.Minute).Err(); err != nil {
		t.Fatalf("Set(otherTicketKey) error = %v", err)
	}
	if err := client.SAdd(context.Background(), logicObj.mfaTwoStepIndexKey(99), otherTicketKey).Err(); err != nil {
		t.Fatalf("SAdd(dirty member) error = %v", err)
	}

	if err := logicObj.ClearAdminMFATwoStepTickets(99); err != nil {
		t.Fatalf("ClearAdminMFATwoStepTickets failed: %v", err)
	}
	if server.Exists(adminTicketKey) {
		t.Fatalf("管理员票据 %s 未被清理", adminTicketKey)
	}
	if server.Exists(logicObj.mfaTwoStepIndexKey(99)) {
		t.Fatalf("管理员票据索引未被清理")
	}
	if !server.Exists(otherTicketKey) {
		t.Fatalf("其它管理员票据不应被脏索引误删")
	}
}

// TestVerifyMFATwoStepTicketAllowsWindowReuse 验证二次票据可在频率窗口内复用。
func TestVerifyMFATwoStepTicketAllowsWindowReuse(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := newTestSecurityLogic()
	logicObj.Svc.Rds = client
	twoStep, err := logicObj.IssueMFATwoStepTicket(99, MFAScenarioEditUser)
	if err != nil {
		t.Fatalf("IssueMFATwoStepTicket failed: %v", err)
	}
	if err := logicObj.VerifyMFATwoStepTicket(99, MFAScenarioEditUser, twoStep.Key, twoStep.Value); err != nil {
		t.Fatalf("first VerifyMFATwoStepTicket failed: %v", err)
	}
	if err := logicObj.VerifyMFATwoStepTicket(99, MFAScenarioEditUser, twoStep.Key, twoStep.Value); err != nil {
		t.Fatalf("second VerifyMFATwoStepTicket failed: %v", err)
	}
}

// TestVerifyMFATwoStepTicketAllowsGeneralScenarioReuse 验证普通敏感场景在频率窗口内可复用最近一次已签发的二次票据。
func TestVerifyMFATwoStepTicketAllowsGeneralScenarioReuse(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := newTestSecurityLogic()
	logicObj.Svc.Rds = client
	twoStep, err := logicObj.IssueMFATwoStepTicket(99, MFAScenarioEditUser)
	if err != nil {
		t.Fatalf("IssueMFATwoStepTicket failed: %v", err)
	}
	if err := logicObj.VerifyMFATwoStepTicket(99, MFAScenarioResetUserPassword, twoStep.Key, twoStep.Value); err != nil {
		t.Fatalf("VerifyMFATwoStepTicket failed: %v", err)
	}
}

// TestVerifyMFATwoStepTicketRejectsStatusScenarioReuse 验证 MFA 状态场景不能复用普通票据。
func TestVerifyMFATwoStepTicketRejectsStatusScenarioReuse(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := newTestSecurityLogic()
	logicObj.Svc.Rds = client
	twoStep, err := logicObj.IssueMFATwoStepTicket(99, MFAScenarioEditUser)
	if err != nil {
		t.Fatalf("IssueMFATwoStepTicket failed: %v", err)
	}
	if err := logicObj.VerifyMFATwoStepTicket(99, MFAScenarioStatus, twoStep.Key, twoStep.Value); err != ErrAdminMFATwoStepExpired {
		t.Fatalf("VerifyMFATwoStepTicket error = %v, want %v", err, ErrAdminMFATwoStepExpired)
	}
}

// TestVerifyMFATwoStepTicketRejectsMFASecretScenarioReuse 验证 MFA 秘钥场景不能复用普通票据。
func TestVerifyMFATwoStepTicketRejectsMFASecretScenarioReuse(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := newTestSecurityLogic()
	logicObj.Svc.Rds = client
	twoStep, err := logicObj.IssueMFATwoStepTicket(99, MFAScenarioEditUser)
	if err != nil {
		t.Fatalf("IssueMFATwoStepTicket failed: %v", err)
	}
	if err := logicObj.VerifyMFATwoStepTicket(99, MFAScenarioSecret, twoStep.Key, twoStep.Value); err != ErrAdminMFATwoStepExpired {
		t.Fatalf("VerifyMFATwoStepTicket error = %v, want %v", err, ErrAdminMFATwoStepExpired)
	}
}

// TestVerifyMFATwoStepTicketRejectsSecretKeyManageScenarioReuse 验证秘钥管理场景不能复用普通票据。
func TestVerifyMFATwoStepTicketRejectsSecretKeyManageScenarioReuse(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := newTestSecurityLogic()
	logicObj.Svc.Rds = client
	twoStep, err := logicObj.IssueMFATwoStepTicket(99, MFAScenarioEditUser)
	if err != nil {
		t.Fatalf("IssueMFATwoStepTicket failed: %v", err)
	}
	if err := logicObj.VerifyMFATwoStepTicket(99, MFAScenarioSecretKeyManage, twoStep.Key, twoStep.Value); err != ErrAdminMFATwoStepExpired {
		t.Fatalf("VerifyMFATwoStepTicket error = %v, want %v", err, ErrAdminMFATwoStepExpired)
	}
}

// TestVerifyMFATwoStepTicketSplitsMFASecretAndSecretKeyManage 验证 MFA 秘钥与秘钥管理票据不能串用。
func TestVerifyMFATwoStepTicketSplitsMFASecretAndSecretKeyManage(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := newTestSecurityLogic()
	logicObj.Svc.Rds = client
	twoStep, err := logicObj.IssueMFATwoStepTicket(99, MFAScenarioSecret)
	if err != nil {
		t.Fatalf("IssueMFATwoStepTicket failed: %v", err)
	}
	if err := logicObj.VerifyMFATwoStepTicket(99, MFAScenarioSecret, twoStep.Key, twoStep.Value); err != nil {
		t.Fatalf("VerifyMFATwoStepTicket same scenario failed: %v", err)
	}
	if err := logicObj.VerifyMFATwoStepTicket(99, MFAScenarioSecretKeyManage, twoStep.Key, twoStep.Value); err != ErrAdminMFATwoStepExpired {
		t.Fatalf("VerifyMFATwoStepTicket cross scenario error = %v, want %v", err, ErrAdminMFATwoStepExpired)
	}
}

// TestCheckAdminMFAToleratesOneSecondSkew 验证登录时间与 MFA 完成标记相差 1 秒时仍视为本次会话已完成校验。
func TestCheckAdminMFAToleratesOneSecondSkew(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	svcCtx := svc.NewServiceContext(config.Config{AppID: "site-a", AppKey: "unit-test-app-key"}, svc.Dependencies{Rds: client})
	logicObj := NewSecurityLogic(context.Background(), svcCtx)
	cipherText, err := logicObj.encryptAdminMFASecret("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("encryptAdminMFASecret failed: %v", err)
	}
	lastLoginTime := time.Unix(1_777_613_352, 0)
	admin := &model.Admin{
		ID:            1,
		Name:          "super999",
		MfaSecureKey:  cipherText,
		MfaStatus:     1,
		LastLoginTime: lastLoginTime,
	}
	cacheKey := logicObj.loginMFAFlagKey(admin.ID)
	if err := client.Set(context.Background(), cacheKey, lastLoginTime.Unix()-1, time.Minute).Err(); err != nil {
		t.Fatalf("Set(%s) error = %v", cacheKey, err)
	}
	if !logicObj.HasPassedLoginMFA(admin) {
		t.Fatalf("HasPassedLoginMFA() = false, want true")
	}
	if err := logicObj.checkAdminMFA(admin); err != nil {
		t.Fatalf("checkAdminMFA() = %v, want nil", err)
	}
}

// TestCheckAdminMFARequiresBindWhenEnabledSecretMissing 验证已启用但秘钥缺失时，登录 MFA 前置校验返回绑定业务码。
func TestCheckAdminMFARequiresBindWhenEnabledSecretMissing(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := newTestSecurityLogic()
	logicObj.Svc.Rds = client

	err := logicObj.checkAdminMFA(&model.Admin{
		ID:        19,
		Name:      "admin-enabled-missing-secret",
		MfaStatus: 1,
	})
	if err != ErrAdminMFABindRequired {
		t.Fatalf("checkAdminMFA() error = %v, want %v", err, ErrAdminMFABindRequired)
	}
}

// TestNeedMFATwoStepSkipsForcedPasswordChangeWhenNeedResetPassword 验证必须改密阶段修改密码时不再强制要求 MFA 二次票据。
func TestNeedMFATwoStepSkipsForcedPasswordChangeWhenNeedResetPassword(t *testing.T) {
	logicObj := newTestSecurityLogic()
	admin := &model.Admin{
		ID:                1,
		Name:              "super999",
		MfaStatus:         1,
		NeedResetPassword: 1,
	}
	if logicObj.NeedMFATwoStep(admin, MFAScenarioChangePassword) {
		t.Fatalf("NeedMFATwoStep() = true, want false when need_reset_password=1")
	}
}

// TestNeedOperateMFATwoStepDependsOnForceConfig 验证后台代操作是否需要 MFA 仅受系统强制开关与禁用场景配置控制。
func TestNeedOperateMFATwoStepDependsOnForceConfig(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := newTestSecurityLogic()
	logicObj.Svc.Rds = client
	if logicObj.NeedOperateMFATwoStep(MFAScenarioEditUser) {
		t.Fatalf("NeedOperateMFATwoStep() = true, want false when force config disabled")
	}
	seedBoolSecurityConfig(t, client, ConfigAdminMFACheckEnable, true)
	if !logicObj.NeedOperateMFATwoStep(MFAScenarioEditUser) {
		t.Fatalf("NeedOperateMFATwoStep() = false, want true when force config enabled")
	}
	if !logicObj.NeedOperateMFATwoStep(MFAScenarioResetUserPassword) {
		t.Fatalf("NeedOperateMFATwoStep(reset password) = false, want true")
	}
	if !logicObj.NeedOperateMFATwoStep(MFAScenarioResetUserInitialState) {
		t.Fatalf("NeedOperateMFATwoStep(reset initial state) = false, want true")
	}
	if !logicObj.NeedOperateMFATwoStep(MFAScenarioDeleteUser) {
		t.Fatalf("NeedOperateMFATwoStep(delete user) = false, want true")
	}
	if logicObj.NeedOperateMFATwoStep(MFAScenarioLogin) {
		t.Fatalf("NeedOperateMFATwoStep(login) = true, want false")
	}
}
