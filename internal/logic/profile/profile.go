package profile

import (
	corelogic "admin/internal/logic"
	adminlogic "admin/internal/logic/admin"
	cachelogic "admin/internal/logic/cache"
	rbaclogic "admin/internal/logic/rbac"
	securitylogic "admin/internal/logic/security"
	"net/http"
	"strings"
	"time"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ProfileLogic 承载对接 `vue-vben-admin` 的接口逻辑。
type ProfileLogic struct {
	*corelogic.BaseLogic // 复用上下文、数据库、Redis 和日志能力
}

// NewProfileLogic 创建个人中心逻辑对象。
func NewProfileLogic(r *http.Request, svcCtx *svc.ServiceContext) *ProfileLogic {
	return &ProfileLogic{
		BaseLogic: corelogic.NewBaseLogic(r, svcCtx),
	}
}

// Login 前端 `/user/login` 登录接口，返回 token 与用户资料。
func (l *ProfileLogic) Login(req *types.ProfileLoginReq) *types.BizResult {
	return l.login(req, true)
}

// BuildSecretVerifyAccount 前端“绑定 MFA 前验证账号密码”的登录接口。
func (l *ProfileLogic) BuildSecretVerifyAccount(req *types.ProfileLoginReq) *types.BizResult {
	return l.login(req, false)
}

// login 承担登录与账号密码预校验的公共流程，可按场景决定是否强制校验图形验证码。
func (l *ProfileLogic) login(req *types.ProfileLoginReq, requireCaptcha bool) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(errors.Errorf("登录请求不能为空"))
	}
	loginReq := &types.LoginReq{
		Username:   req.Username,
		Captcha:    req.Captcha,
		Key:        req.Key,
		Password:   req.Password,
		SecureCode: req.SecureCode,
		IP:         l.ClientIP(),
	}
	adminLogic := &adminlogic.AdminLogic{BaseLogic: l.BaseLogic}
	if requireCaptcha {
		captchaResp := adminLogic.VerifyLoginCaptcha(loginReq.Key, loginReq.Captcha)
		if captchaResp.IsFailure() {
			return captchaResp
		}
	}
	loginResp := adminLogic.Login(loginReq)
	if loginResp == nil || loginResp.IsFailure() {
		return loginResp
	}
	data, ok := loginResp.Data.(*types.ProfileLoginResp)
	if !ok || data == nil || strings.TrimSpace(data.Token) == "" || data.User == nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Errorf("ProfileLogic.Login 账号[%s]返回登录资料为空", loginReq.Username))
	}
	return loginResp
}

// Mine 返回当前登录管理员的个人信息。
func (l *ProfileLogic) Mine() *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("ProfileLogic.Mine", err)
	}
	info, err := securitylogic.NewSecurityLogic(l.Ctx, l.Svc).BuildProfileInfo(admin, l.AccessToken())
	if err != nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrap(err, "ProfileLogic.Mine 构造用户信息失败"))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(info)
}

// Permissions 返回当前登录管理员的角色、权限与 MFA 场景配置。
func (l *ProfileLogic) Permissions() *types.BizResult {
	admin := l.GetCtxAdmin()
	if admin == nil || admin.ID <= 0 {
		return types.Unauthorized(i18n.MsgKeyNeedLogin).ToBizResult()
	}
	securityLogic := securitylogic.NewSecurityLogic(l.Ctx, l.Svc)
	roleIDs, err := securityLogic.EnabledRoleIDs(admin.ID)
	if err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyRoleFetchFail).
			WithError(errors.Wrapf(err, "ProfileLogic.Permissions 查询管理员ID[%d]角色失败", admin.ID))
	}
	roles, err := (&rbaclogic.AdminRoleRelLogic{BaseLogic: l.BaseLogic}).GetRolesByUserID(int64(admin.ID))
	if err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyRoleFetchFail).
			WithError(errors.Wrapf(err, "ProfileLogic.Permissions 查询管理员ID[%d]角色名称失败", admin.ID))
	}
	permResp := (&adminlogic.AdminLogic{BaseLogic: l.BaseLogic}).GetUserPermissionCodes(admin.ID)
	if permResp == nil || permResp.IsFailure() {
		return permResp
	}
	permissions, _ := permResp.Data.([]string)
	superUserRole := 0
	for _, roleID := range roleIDs {
		if roleID == corelogic.AdminSuperRoleID {
			superUserRole = 1
			break
		}
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(&types.ProfileRoleInfo{
			SuperUserRole:            superUserRole,
			Roles:                    roles,
			Permissions:              permissions,
			CheckMFAScenariosDisable: securityLogic.MFADisabledScenarios(),
		})
}

// CheckSecure 校验当前登录管理员密码，支持锁屏密码校验。
func (l *ProfileLogic) CheckSecure(req *types.ProfileCheckSecureReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("ProfileLogic.CheckSecure", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(admin.PasswordWithSalt(req.Secure))); err != nil {
		return types.NewBizResult(codes.InvalidPassword).
			SetI18nMessage(i18n.MsgKeyInvalidPassword).
			WithData(&types.BoolResp{IsOk: false})
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(&types.BoolResp{IsOk: true})
}

// CheckMFA 校验当前登录管理员的 MFA 动态码，并返回二次校验票据。
func (l *ProfileLogic) CheckMFA(req *types.ProfileCheckMFAReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("ProfileLogic.CheckMFA", err)
	}
	securityLogic := securitylogic.NewSecurityLogic(l.Ctx, l.Svc)
	verifyResult, err := securityLogic.VerifyBindingMFACodeDetail(admin, req.Secret(), req.Secure)
	if err != nil {
		if errors.Is(err, securitylogic.ErrAdminMFACodeInvalid) {
			return types.NewBizResult(codes.InvalidMFACode).
				SetI18nMessage(i18n.MsgKeyMFACodeInvalid).
				WithError(corelogic.WrapLogicError(err, "ProfileLogic.CheckMFA MFA动态码校验失败"))
		}
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrapf(err, "ProfileLogic.CheckMFA 校验管理员ID[%d]MFA动态码失败", admin.ID))
	}
	if req.Scenarios == securitylogic.MFAScenarioLogin {
		if err := securityLogic.MarkLoginMFACompleted(admin.ID); err != nil {
			return types.NewBizResult(codes.ServerError).
				SetI18nMessage(i18n.MsgKeyInternalError).
				WithError(errors.Wrapf(err, "ProfileLogic.CheckMFA 标记管理员ID[%d]登录MFA校验成功失败", admin.ID))
		}
	}
	twoStep, err := securityLogic.IssueMFATwoStepTicketWithVerifyResult(admin.ID, req.Scenarios, verifyResult)
	if err != nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrapf(err, "ProfileLogic.CheckMFA 生成管理员ID[%d]二次校验票据失败", admin.ID))
	}
	buildURL := ""
	if admin.MfaStatus != 1 {
		var buildErr error
		buildURL = strings.TrimSpace(req.MfaSecureKey)
		if buildURL != "" {
			buildURL, buildErr = securityLogic.BuildAdminMFAURLBySecret(admin, req.Secret())
		} else {
			buildURL, buildErr = securityLogic.BuildFreshAdminMFAURL(admin)
		}
		if buildErr != nil {
			// 未启用 MFA 时二维码是绑定入口，生成失败必须显式返回错误。
			return types.NewBizResult(codes.ServerError).
				SetI18nMessage(i18n.MsgKeyInternalError).
				WithError(errors.Wrapf(buildErr, "ProfileLogic.CheckMFA 生成管理员ID[%d]MFA绑定地址失败", admin.ID))
		}
	}
	existMFA := securityLogic.HasUsableAdminMFASecret(admin)
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(&types.ProfileMFACheckResp{
			IsOk:        true,
			Scenarios:   req.Scenarios,
			ExistMFA:    existMFA,
			BuildMFAURL: buildURL,
			MFACheck:    0,
			Frequency:   securityLogic.MFAFrequency(),
			TwoStep:     twoStep,
		})
}

// UpdatePassword 修改当前登录管理员密码。
func (l *ProfileLogic) UpdatePassword(req *types.ProfilePasswordReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("ProfileLogic.UpdatePassword", err)
	}
	// 个人中心修改自己密码时，只要提供并通过当前密码校验，就不再额外要求 MFA 二次票据。
	// 若后续扩展出“无当前密码改自己密码”的入口，再复用修改密码场景票据校验。
	if strings.TrimSpace(req.PasswordOld) == "" {
		if err := l.requireScenarioTwoStep(admin, securitylogic.MFAScenarioChangePassword, req.TwoStepKey, req.TwoStepValue); err != nil {
			return l.mfaResultByError(err)
		}
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(admin.PasswordWithSalt(req.PasswordOld))); err != nil {
		return types.NewBizResult(codes.InvalidPassword).
			SetI18nMessage(i18n.MsgKeyInvalidPassword).
			WithError(corelogic.WrapLogicError(err, "ProfileLogic.UpdatePassword 当前密码校验失败"))
	}
	password, err := bcrypt.GenerateFromPassword([]byte(admin.PasswordWithSalt(req.PasswordNew)), bcrypt.DefaultCost)
	if err != nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrapf(err, "ProfileLogic.UpdatePassword 生成管理员ID[%d]密码哈希失败", admin.ID))
	}
	if err := l.Svc.WriteDB(svc.DatabaseMain).Model(&model.Admin{}).Where("id = ?", admin.ID).Updates(map[string]any{
		"password":            string(password),
		"need_reset_password": 0,
		"updated_at":          time.Now(),
	}).Error; err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyDBError).
			WithError(errors.Wrapf(err, "ProfileLogic.UpdatePassword 更新管理员ID[%d]密码失败", admin.ID))
	}
	cachelogic.InvalidateAdminRelationCachePreserveSession(l.BaseLogic, admin.ID)
	l.syncCurrentAdminNeedResetPassword(admin.ID, 0)
	l.syncLoginMFAAfterPasswordUpdate(admin)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// syncLoginMFAAfterPasswordUpdate 统一处理个人中心改密后的登录 MFA 标记。
// 首次登录/临时密码阶段改密成功后，允许当前会话继续使用，因此补写已完成标记；
// 普通“改自己密码”不应清理现有标记，否则前端紧接着刷新 `mine` 会被误判成未完成登录 MFA。
func (l *ProfileLogic) syncLoginMFAAfterPasswordUpdate(admin *model.Admin) {
	if admin == nil {
		return
	}
	if admin.NeedResetPassword != 1 {
		return
	}
	_ = securitylogic.NewSecurityLogic(l.Ctx, l.Svc).MarkLoginMFACompleted(admin.ID)
}

// syncCurrentAdminNeedResetPassword 立即同步当前会话缓存里的强制改密状态，
// 避免个人中心改密成功后，紧接着请求 `/auth/profile` 仍命中未刷新的 `admin:info` 值。
func (l *ProfileLogic) syncCurrentAdminNeedResetPassword(adminID int, needResetPassword int) {
	if adminID <= 0 {
		return
	}
	cacheLogic := cachelogic.NewCacheLogic(l.Ctx, l.Svc)
	if err := cacheLogic.SetAdminInfoByField(adminID, "needResetPassword", needResetPassword); err == nil {
		return
	}
	token := strings.TrimSpace(l.AccessToken())
	if token == "" {
		return
	}
	_, _ = cacheLogic.RebuildAdminInfo(adminID, token)
}

// UpdateMine 更新当前登录管理员的基础资料。
func (l *ProfileLogic) UpdateMine(req *types.ProfileUpdateReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("ProfileLogic.UpdateMine", err)
	}
	updates := map[string]any{
		// 个人中心基础资料页按整表单提交，后端这里直接按归一化后的当前值落库，
		// 避免用户把邮箱、手机号、备注清空时出现“提示成功但数据库未真正清空”的语义偏差。
		"real_name":   req.RealName,
		"email":       req.Email,
		"phone":       req.Phone,
		"avatar":      req.Avatar,
		"description": req.Description,
		"updated_at":  time.Now(),
	}
	if err := l.Svc.WriteDB(svc.DatabaseMain).Model(&model.Admin{}).Where("id = ?", admin.ID).Updates(updates).Error; err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyDBError).
			WithError(errors.Wrapf(err, "ProfileLogic.UpdateMine 更新管理员ID[%d]资料失败", admin.ID))
	}
	cachelogic.InvalidateAdminRelationCache(l.BaseLogic, admin.ID)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// UpdateMFAStatus 修改当前登录管理员的 MFA 状态。
func (l *ProfileLogic) UpdateMFAStatus(req *types.ProfileMFAStatusReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("ProfileLogic.UpdateMFAStatus", err)
	}
	securityLogic := securitylogic.NewSecurityLogic(l.Ctx, l.Svc)
	switch req.Status() {
	case 0:
		if securityLogic.ForceLoginMFAEnabled() && admin.MfaStatus == 1 {
			return types.NewBizResult(codes.Forbidden).
				SetI18nMessage(i18n.MsgKeyMFAForceEnabledDisallowDisable).
				WithError(errors.Errorf("ProfileLogic.UpdateMFAStatus 管理员ID[%d]系统已开启强制启用MFA，禁止停用", admin.ID))
		}
		// 个人中心关闭 MFA 属于当前账号的高敏操作，始终要求先完成同场景二次校验。
		if err := securityLogic.VerifyMFATwoStepTicket(admin.ID, securitylogic.MFAScenarioStatus, req.TwoStepKey, req.TwoStepValue); err != nil {
			return l.mfaResultByError(err)
		}
	case 1:
		twoStepPayload, err := securityLogic.ConsumeMFATwoStepTicket(admin.ID, securitylogic.MFAScenarioStatus, req.TwoStepKey, req.TwoStepValue)
		if err != nil {
			return l.mfaResultByError(err)
		}
		secretToEnable, err := l.resolveEnableMFASecret(admin, req.Secret(), twoStepPayload)
		if err != nil {
			return l.mfaResultByError(err)
		}
		req.MfaSecureKey = secretToEnable
	}
	updates := map[string]any{
		"mfa_status": req.Status(),
		"updated_at": time.Now(),
	}
	if req.Status() == 1 {
		encryptedSecret, err := securityLogic.EncryptAdminMFASecret(req.Secret())
		if err != nil {
			return types.NewBizResult(codes.ServerError).
				SetI18nMessage(i18n.MsgKeyInternalError).
				WithError(errors.Wrapf(err, "ProfileLogic.UpdateMFAStatus 加密管理员ID[%d]MFA秘钥失败", admin.ID))
		}
		updates["mfa_secure_key"] = encryptedSecret
	}
	if err := l.Svc.WriteDB(svc.DatabaseMain).Model(&model.Admin{}).Where("id = ?", admin.ID).Updates(updates).Error; err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyDBError).
			WithError(errors.Wrapf(err, "ProfileLogic.UpdateMFAStatus 更新管理员ID[%d]MFA状态失败", admin.ID))
	}
	cachelogic.InvalidateAdminRelationCachePreserveSession(l.BaseLogic, admin.ID)
	l.rebuildCurrentAdminSessionCache(admin.ID)
	l.syncLoginMFAAfterStatusUpdate(securityLogic, admin, req.Status())
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// syncLoginMFAAfterStatusUpdate 同步当前会话的登录 MFA 完成标记。
// 绑定或关闭 MFA 都已经完成当前场景的二次校验，因此当前会话应继续视为“已完成登录 MFA”。
func (l *ProfileLogic) syncLoginMFAAfterStatusUpdate(securityLogic *securitylogic.SecurityLogic, admin *model.Admin, _ int) {
	if admin == nil || securityLogic == nil {
		return
	}
	_ = securityLogic.MarkLoginMFACompleted(admin.ID)
}

// rebuildCurrentAdminSessionCache 使用当前请求携带的 token 立即重建登录态缓存，
// 避免个人中心更新资料后立刻跳转其它页面时命中 admin:info 刚被删除的短暂窗口。
func (l *ProfileLogic) rebuildCurrentAdminSessionCache(adminID int) {
	if adminID <= 0 {
		return
	}
	token := strings.TrimSpace(l.AccessToken())
	if token == "" {
		return
	}
	_, _ = cachelogic.NewCacheLogic(l.Ctx, l.Svc).RebuildAdminInfo(adminID, token)
}

// UpdateMFASecureKey 修改当前登录管理员的 MFA 秘钥。
func (l *ProfileLogic) UpdateMFASecureKey(req *types.ProfileMFASecretReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("ProfileLogic.UpdateMFASecureKey", err)
	}
	if admin.MfaStatus == 1 {
		return types.NewBizResult(codes.Forbidden).
			SetI18nMessage(i18n.MsgKeyAuthFailed).
			WithError(errors.Errorf("ProfileLogic.UpdateMFASecureKey 管理员ID[%d]已启用MFA，不允许自助换绑秘钥", admin.ID))
	}
	// 个人中心修改/绑定 MFA 秘钥始终要求先完成当前场景动态码校验，
	// 未启用账号也需要先拿到本次绑定流程签发的二次票据，避免绕过新秘钥验证直接落库。
	if err := securitylogic.NewSecurityLogic(l.Ctx, l.Svc).VerifyMFATwoStepTicket(admin.ID, securitylogic.MFAScenarioSecret, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.mfaResultByError(err)
	}
	encryptedSecret, err := securitylogic.NewSecurityLogic(l.Ctx, l.Svc).EncryptAdminMFASecret(req.Secret())
	if err != nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrapf(err, "ProfileLogic.UpdateMFASecureKey 加密管理员ID[%d]MFA秘钥失败", admin.ID))
	}
	if err := l.Svc.WriteDB(svc.DatabaseMain).Model(&model.Admin{}).Where("id = ?", admin.ID).Updates(map[string]any{
		"mfa_secure_key": encryptedSecret,
		"mfa_status":     1,
		"updated_at":     time.Now(),
	}).Error; err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyDBError).
			WithError(errors.Wrapf(err, "ProfileLogic.UpdateMFASecureKey 更新管理员ID[%d]MFA秘钥失败", admin.ID))
	}
	cachelogic.InvalidateAdminRelationCachePreserveSession(l.BaseLogic, admin.ID)
	l.rebuildCurrentAdminSessionCache(admin.ID)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// RefreshMFASecretKey 重新生成当前登录管理员的 MFA 二维码与秘钥。
func (l *ProfileLogic) RefreshMFASecretKey(req *types.ProfileMFASecretRefreshReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("ProfileLogic.RefreshMFASecretKey", err)
	}
	securityLogic := securitylogic.NewSecurityLogic(l.Ctx, l.Svc)
	if admin.MfaStatus == 1 && securityLogic.HasUsableAdminMFASecret(admin) {
		return types.NewBizResult(codes.Forbidden).
			SetI18nMessage(i18n.MsgKeyAuthFailed).
			WithError(errors.Errorf("ProfileLogic.RefreshMFASecretKey 管理员ID[%d]已启用MFA，不允许自助重新生成绑定二维码", admin.ID))
	}
	buildURL, err := securityLogic.BuildFreshAdminMFAURL(admin)
	if err != nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrapf(err, "ProfileLogic.RefreshMFASecretKey 刷新管理员ID[%d]MFA绑定地址失败", admin.ID))
	}
	l.rebuildCurrentAdminSessionCache(admin.ID)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess).
		WithData(&types.MFAURLResp{BuildMFAURL: buildURL})
}

// UpdateAvatar 更新当前登录管理员头像。
func (l *ProfileLogic) UpdateAvatar(req *types.ProfileAvatarReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("ProfileLogic.UpdateAvatar", err)
	}
	if err := l.Svc.WriteDB(svc.DatabaseMain).Model(&model.Admin{}).Where("id = ?", admin.ID).Updates(map[string]any{
		"avatar":     strings.TrimSpace(req.Avatar),
		"updated_at": time.Now(),
	}).Error; err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyDBError).
			WithError(errors.Wrapf(err, "ProfileLogic.UpdateAvatar 更新管理员ID[%d]头像失败", admin.ID))
	}
	cachelogic.InvalidateAdminRelationCache(l.BaseLogic, admin.ID)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// BuildMFASecretKeyURL 为指定管理员生成 MFA 绑定地址。
func (l *ProfileLogic) BuildMFASecretKeyURL(req *types.IDPathReq) *types.BizResult {
	var admin model.Admin
	if err := l.Svc.WriteDB(svc.DatabaseMain).Where("id = ?", req.ID).First(&admin).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return types.NewBizResult(codes.NotFound).
				SetI18nMessage(i18n.MsgKeyUserNotFound).
				WithError(errors.Wrapf(err, "ProfileLogic.BuildMFASecretKeyURL 管理员ID[%d]不存在", req.ID))
		}
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyDBError).
			WithError(errors.Wrapf(err, "ProfileLogic.BuildMFASecretKeyURL 查询管理员ID[%d]失败", req.ID))
	}
	securityLogic := securitylogic.NewSecurityLogic(l.Ctx, l.Svc)
	buildURL := ""
	var err error
	if admin.MfaStatus != 1 || !securityLogic.HasUsableAdminMFASecret(&admin) {
		buildURL, err = securityLogic.BuildFreshAdminMFAURL(&admin)
	} else {
		buildURL, err = securityLogic.BuildAdminMFAURL(&admin)
	}
	if err != nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrapf(err, "ProfileLogic.BuildMFASecretKeyURL 生成管理员ID[%d]MFA绑定地址失败", req.ID))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(&types.MFAURLResp{BuildMFAURL: buildURL})
}

// RoleTreeList 返回角色树下拉数据，供账号管理页面选择角色。
func (l *ProfileLogic) RoleTreeList() *types.BizResult {
	roleResp := (&rbaclogic.AdminRoleLogic{BaseLogic: l.BaseLogic}).TreeList()
	if roleResp == nil || roleResp.IsFailure() {
		return roleResp
	}
	items, _ := roleResp.Data.([]types.AdminRoleItem)
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(l.profileRoleTree(items))
}

// UpdateAccountMFAStatus 修改指定管理员的 MFA 状态。
func (l *ProfileLogic) UpdateAccountMFAStatus(req *types.AdminMFAStatusReq) *types.BizResult {
	currentAdmin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("ProfileLogic.UpdateAccountMFAStatus", err)
	}
	if err := l.ensureAccountMFAStatusManageScope(req.ID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyUserNotFound, err,
				"ProfileLogic.UpdateAccountMFAStatus 管理员ID[%d]不存在", req.ID).ToBizResult()
		}
		if errors.Is(err, rbaclogic.ErrRoleManageScopeExceeded) {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "ProfileLogic.UpdateAccountMFAStatus 管理员ID[%d]超出可管理范围", req.ID))
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"ProfileLogic.UpdateAccountMFAStatus 校验管理员ID[%d]可管理范围失败", req.ID).ToBizResult()
	}
	securityLogic := securitylogic.NewSecurityLogic(l.Ctx, l.Svc)
	if securityLogic.NeedOperateMFATwoStep(securitylogic.MFAScenarioStatus) {
		if err := securityLogic.VerifyMFATwoStepTicket(currentAdmin.ID, securitylogic.MFAScenarioStatus, req.TwoStepKey, req.TwoStepValue); err != nil {
			return l.mfaResultByError(err)
		}
	}
	updates := map[string]any{
		"mfa_status": req.Status(),
		"updated_at": time.Now(),
	}
	if err := l.Svc.WriteDB(svc.DatabaseMain).Model(&model.Admin{}).Where("id = ?", req.ID).Updates(updates).Error; err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyDBError).
			WithError(errors.Wrapf(err, "ProfileLogic.UpdateAccountMFAStatus 更新管理员ID[%d]MFA状态失败", req.ID))
	}
	cachelogic.InvalidateAdminRelationCache(l.BaseLogic, req.ID)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// ensureAccountMFAStatusManageScope 校验指定管理员是否处于当前登录管理员可管理角色范围内。
func (l *ProfileLogic) ensureAccountMFAStatusManageScope(adminID int) error {
	if _, err := (&adminlogic.AdminLogic{BaseLogic: l.BaseLogic}).GetAdminByID(adminID); err != nil {
		return errors.Wrapf(err, "查询管理员ID[%d]失败", adminID)
	}
	roleLogic := &rbaclogic.AdminRoleLogic{BaseLogic: l.BaseLogic}
	roleIDs, err := roleLogic.UserRoleIDs(adminID)
	if err != nil {
		return errors.Wrapf(err, "查询管理员ID[%d]角色失败", adminID)
	}
	if err := roleLogic.EnsureRolesWithinManageScope(roleIDs); err != nil {
		return errors.Wrapf(err, "管理员ID[%d]角色超出当前管理员可管理范围", adminID)
	}
	return nil
}

// currentAdmin 读取当前登录管理员完整模型。
func (l *ProfileLogic) currentAdmin() (*model.Admin, error) {
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin == nil || ctxAdmin.ID <= 0 {
		return nil, types.Nil
	}
	admin, err := (&adminlogic.AdminLogic{BaseLogic: l.BaseLogic}).GetAdminByID(ctxAdmin.ID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return admin, nil
}

// resolveEnableMFASecret 根据二次票据中的秘钥来源，决定本次启用 MFA 应落库的正式秘钥。
func (l *ProfileLogic) resolveEnableMFASecret(admin *model.Admin, requestSecret string, payload *securitylogic.MFATwoStepTicketPayload) (string, error) {
	requestSecret = securitylogic.NormalizeMFASecret(requestSecret)
	if payload == nil {
		if securitylogic.IsUsableMFASecret(requestSecret) {
			return requestSecret, nil
		}
		return "", securitylogic.ErrAdminMFATwoStepExpired
	}
	switch payload.SecretSource {
	case "", securitylogic.MFATwoStepSecretSourceRequest:
		if !securitylogic.IsUsableMFASecret(requestSecret) {
			return "", securitylogic.ErrAdminMFATwoStepExpired
		}
		if payload.SecretDigest != "" && payload.SecretDigest != securitylogic.HashMFASecret(requestSecret) {
			return "", securitylogic.ErrAdminMFATwoStepExpired
		}
		return requestSecret, nil
	case securitylogic.MFATwoStepSecretSourceCurrent:
		securityLogic := securitylogic.NewSecurityLogic(l.Ctx, l.Svc)
		currentSecret, err := securityLogic.LoadAdminMFASecret(admin)
		if err != nil {
			return "", errors.Tag(err)
		}
		if !securitylogic.IsUsableMFASecret(currentSecret) {
			return "", securitylogic.ErrAdminMFATwoStepExpired
		}
		if payload.SecretDigest != "" && payload.SecretDigest != securitylogic.HashMFASecret(currentSecret) {
			return "", securitylogic.ErrAdminMFATwoStepExpired
		}
		return currentSecret, nil
	default:
		return "", securitylogic.ErrAdminMFATwoStepExpired
	}
}

// requireScenarioTwoStep 在当前场景需要 MFA 时校验二次校验票据。
func (l *ProfileLogic) requireScenarioTwoStep(admin *model.Admin, scenario int, key string, value string) error {
	securityLogic := securitylogic.NewSecurityLogic(l.Ctx, l.Svc)
	if !securityLogic.NeedMFATwoStep(admin, scenario) {
		return nil
	}
	return securityLogic.VerifyMFATwoStepTicket(admin.ID, scenario, key, value)
}

// mfaResultByError 把 MFA 相关错误统一转换成前端可识别的业务响应码。
func (l *ProfileLogic) mfaResultByError(err error) *types.BizResult {
	if err == nil {
		return nil
	}
	if errors.Is(err, securitylogic.ErrAdminMFATwoStepExpired) || errors.Is(err, securitylogic.ErrAdminMFARequired) {
		return types.NewBizResult(codes.CheckMFAAgain).
			SetI18nMessage(i18n.MsgKeyMFAExpired).
			WithError(corelogic.WrapLogicError(err, "ProfileLogic.mfaResultByError MFA二次校验已失效"))
	}
	return types.NewBizResult(codes.Forbidden).
		SetI18nMessage(i18n.MsgKeyAuthFailed).
		WithError(corelogic.WrapLogicError(err, "ProfileLogic.mfaResultByError MFA校验失败"))
}

// adminFetchErrorResult 统一转换当前管理员查询失败响应。
func (l *ProfileLogic) adminFetchErrorResult(operation string, err error) *types.BizResult {
	if err == types.Nil {
		return types.Unauthorized(i18n.MsgKeyNeedLogin).ToBizResult()
	}
	if err == gorm.ErrRecordNotFound {
		return types.NewBizResult(codes.NotFound).
			SetI18nMessage(i18n.MsgKeyUserNotFound).
			WithError(errors.Wrapf(err, "%s 当前管理员不存在", operation))
	}
	return types.NewBizResult(codes.DBError).
		SetI18nMessage(i18n.MsgKeyDBError).
		WithError(errors.Wrapf(err, "%s 查询当前管理员失败", operation))
}

// profileRoleTree 把角色树转换成前端下拉组件可直接使用的树结构。
func (l *ProfileLogic) profileRoleTree(items []types.AdminRoleItem) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		node := map[string]any{
			"id":       item.ID,
			"key":      item.ID,
			"title":    item.Title,
			"label":    item.Title,
			"value":    item.ID,
			"disabled": item.Status != 1,
		}
		if len(item.Children) > 0 {
			node["children"] = l.profileRoleTree(item.Children)
		}
		result = append(result, node)
	}
	return result
}
