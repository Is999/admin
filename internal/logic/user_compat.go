package logic

import (
	"net/http"
	"strings"
	"time"

	codes "admin_cron/common/codes"
	i18n "admin_cron/common/i18n"
	"admin_cron/internal/model"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

	"github.com/Is999/go-utils/errors"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// UserCompatLogic 承载对接 `vue-vben-admin` 的兼容接口逻辑。
type UserCompatLogic struct {
	*BaseLogic // 复用上下文、数据库、Redis 和日志能力
}

// NewUserCompatLogic 创建兼容用户中心逻辑对象。
func NewUserCompatLogic(r *http.Request, svcCtx *svc.ServiceContext) *UserCompatLogic {
	return &UserCompatLogic{
		BaseLogic: NewBaseLogic(r, svcCtx),
	}
}

// Login 兼容前端 `/user/login` 登录接口，返回 token 与用户资料。
func (l *UserCompatLogic) Login(req *types.UserCompatLoginReq) *types.BizResult {
	return l.login(req, true)
}

// BuildSecretVerifyAccount 兼容前端“绑定 MFA 前验证账号密码”的登录接口。
func (l *UserCompatLogic) BuildSecretVerifyAccount(req *types.UserCompatLoginReq) *types.BizResult {
	return l.login(req, false)
}

// login 承担兼容登录与账号密码预校验的公共流程，可按场景决定是否强制校验图形验证码。
func (l *UserCompatLogic) login(req *types.UserCompatLoginReq, requireCaptcha bool) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(errors.Errorf("登录请求不能为空"))
	}
	loginReq := &types.LoginReq{
		Username:   req.Username,
		Captcha:    req.Captcha,
		Key:        req.Key,
		Password:   req.Password,
		SecureCode: req.SecureCode,
		Ip:         l.ClientIP(),
	}
	adminLogic := &AdminLogic{BaseLogic: l.BaseLogic}
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
	data, ok := loginResp.Data.(*types.LoginResp)
	if !ok || data == nil || strings.TrimSpace(data.Token) == "" {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Errorf("UserCompatLogic.Login 账号[%s]返回令牌为空", loginReq.Username))
	}
	admin, err := adminLogic.GetAdminByID(data.Id)
	if err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyAccountFetchFail).
			WithError(errors.Wrapf(err, "UserCompatLogic.Login 账号[%s]查询管理员详情失败", loginReq.Username))
	}
	userInfo, err := l.BuildCompatUserInfo(admin, data.Token)
	if err != nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrapf(err, "UserCompatLogic.Login 账号[%s]构造兼容用户信息失败", loginReq.Username))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(&types.UserCompatLoginResp{
			Token: data.Token,
			User:  userInfo,
		})
}

// Mine 返回当前登录管理员的兼容个人信息。
func (l *UserCompatLogic) Mine() *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("UserCompatLogic.Mine", err)
	}
	info, err := l.BuildCompatUserInfo(admin, l.AccessToken())
	if err != nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrap(err, "UserCompatLogic.Mine 构造兼容用户信息失败"))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(info)
}

// Permissions 返回当前登录管理员的角色、权限与 MFA 场景配置。
func (l *UserCompatLogic) Permissions() *types.BizResult {
	admin := l.GetCtxAdmin()
	if admin == nil || admin.ID <= 0 {
		return types.Unauthorized(i18n.MsgKeyNeedLogin).ToBizResult()
	}
	securityLogic := NewSecurityLogic(l.Context(), l.svc)
	roleIDs, err := securityLogic.enabledRoleIDs(admin.ID)
	if err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyRoleFetchFail).
			WithError(errors.Wrapf(err, "UserCompatLogic.Permissions 查询管理员ID[%d]角色失败", admin.ID))
	}
	roles, err := (&AdminRoleRelLogic{BaseLogic: l.BaseLogic}).GetRolesByUserID(int64(admin.ID))
	if err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyRoleFetchFail).
			WithError(errors.Wrapf(err, "UserCompatLogic.Permissions 查询管理员ID[%d]角色名称失败", admin.ID))
	}
	permResp := (&AdminLogic{BaseLogic: l.BaseLogic}).GetUserPermissionCodes(admin.ID)
	if permResp == nil || permResp.IsFailure() {
		return permResp
	}
	permissions, _ := permResp.Data.([]string)
	superUserRole := 0
	for _, roleID := range roleIDs {
		if roleID == adminSuperRoleID {
			superUserRole = 1
			break
		}
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(&types.UserCompatRoleInfo{
			SuperUserRole:            superUserRole,
			Roles:                    roles,
			Permissions:              permissions,
			CheckMFAScenariosDisable: securityLogic.MFADisabledScenarios(),
		})
}

// CheckSecure 校验当前登录管理员密码，兼容锁屏密码校验。
func (l *UserCompatLogic) CheckSecure(req *types.UserCheckSecureReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("UserCompatLogic.CheckSecure", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(admin.PasswordWithSalt(req.Secure))); err != nil {
		return types.NewBizResult(codes.InvalidPassword).
			SetI18nMessage(i18n.MsgKeyInvalidPassword).
			WithData(&types.UserBoolResp{IsOk: false})
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(&types.UserBoolResp{IsOk: true})
}

// CheckMFA 校验当前登录管理员的 MFA 动态码，并返回二次校验票据。
func (l *UserCompatLogic) CheckMFA(req *types.UserCheckMFAReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("UserCompatLogic.CheckMFA", err)
	}
	securityLogic := NewSecurityLogic(l.Context(), l.svc)
	verifyResult, err := securityLogic.VerifyBindingMFACodeDetail(admin, req.Secret(), req.Secure)
	if err != nil {
		if errors.Is(err, ErrAdminMFACodeInvalid) {
			return types.NewBizResult(codes.InvalidMFACode).
				SetI18nMessage(i18n.MsgKeyMFACodeInvalid).
				WithError(wrapLogicError(err, "UserCompatLogic.CheckMFA MFA动态码校验失败"))
		}
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrapf(err, "UserCompatLogic.CheckMFA 校验管理员ID[%d]MFA动态码失败", admin.ID))
	}
	if req.Scenarios == MFAScenarioLogin {
		if err := securityLogic.MarkLoginMFACompleted(admin.ID); err != nil {
			return types.NewBizResult(codes.ServerError).
				SetI18nMessage(i18n.MsgKeyInternalError).
				WithError(errors.Wrapf(err, "UserCompatLogic.CheckMFA 标记管理员ID[%d]登录MFA校验成功失败", admin.ID))
		}
	}
	twoStep, err := securityLogic.IssueMFATwoStepTicketWithVerifyResult(admin.ID, req.Scenarios, verifyResult)
	if err != nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrapf(err, "UserCompatLogic.CheckMFA 生成管理员ID[%d]二次校验票据失败", admin.ID))
	}
	buildURL := ""
	if admin.MfaStatus != 1 {
		var buildErr error
		buildURL = strings.TrimSpace(req.MfaSecureKey)
		if buildURL != "" {
			buildURL, buildErr = buildAdminMFAURLBySecret(admin, req.Secret())
		} else {
			buildURL, buildErr = securityLogic.BuildFreshAdminMFAURL(admin)
		}
		if buildErr != nil {
			// 未启用 MFA 时二维码是绑定入口，生成失败必须显式返回错误。
			return types.NewBizResult(codes.ServerError).
				SetI18nMessage(i18n.MsgKeyInternalError).
				WithError(errors.Wrapf(buildErr, "UserCompatLogic.CheckMFA 生成管理员ID[%d]MFA绑定地址失败", admin.ID))
		}
	}
	existMFA := securityLogic.HasUsableAdminMFASecret(admin)
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(&types.UserCompatMFACheckResp{
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
func (l *UserCompatLogic) UpdatePassword(req *types.UserUpdatePasswordReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("UserCompatLogic.UpdatePassword", err)
	}
	// 个人中心修改自己密码时，只要提供并通过当前旧密码校验，就不再额外要求 MFA 二次票据。
	// 若后续扩展出“无旧密码改自己密码”的入口，再复用修改密码场景票据校验。
	if strings.TrimSpace(req.PasswordOld) == "" {
		if err := l.requireScenarioTwoStep(admin, MFAScenarioChangePassword, req.TwoStepKey, req.TwoStepValue); err != nil {
			return l.mfaResultByError(err)
		}
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(admin.PasswordWithSalt(req.PasswordOld))); err != nil {
		return types.NewBizResult(codes.InvalidPassword).
			SetI18nMessage(i18n.MsgKeyInvalidPassword).
			WithError(wrapLogicError(err, "UserCompatLogic.UpdatePassword 旧密码校验失败"))
	}
	password, err := bcrypt.GenerateFromPassword([]byte(admin.PasswordWithSalt(req.PasswordNew)), bcrypt.DefaultCost)
	if err != nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrapf(err, "UserCompatLogic.UpdatePassword 生成管理员ID[%d]密码哈希失败", admin.ID))
	}
	if err := l.svc.WriteDB(svc.DatabaseMain).Model(&model.Admin{}).Where("id = ?", admin.ID).Updates(map[string]any{
		"password":            string(password),
		"need_reset_password": 0,
		"updated_at":          time.Now(),
	}).Error; err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyDBError).
			WithError(errors.Wrapf(err, "UserCompatLogic.UpdatePassword 更新管理员ID[%d]密码失败", admin.ID))
	}
	invalidateAdminRelationCachePreserveSession(l.BaseLogic, admin.ID)
	l.syncCurrentAdminNeedResetPassword(admin.ID, 0)
	l.syncLoginMFAAfterPasswordUpdate(admin)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// syncLoginMFAAfterPasswordUpdate 统一处理个人中心改密后的登录 MFA 标记。
// 首次登录/临时密码阶段改密成功后，允许当前会话继续使用，因此补写已完成标记；
// 普通“改自己密码”不应清理现有标记，否则前端紧接着刷新 `mine` 会被误判成未完成登录 MFA。
func (l *UserCompatLogic) syncLoginMFAAfterPasswordUpdate(admin *model.Admin) {
	if admin == nil {
		return
	}
	if admin.NeedResetPassword != 1 {
		return
	}
	_ = NewSecurityLogic(l.Context(), l.svc).MarkLoginMFACompleted(admin.ID)
}

// syncCurrentAdminNeedResetPassword 立即同步当前会话缓存里的强制改密状态，
// 避免个人中心改密成功后，紧接着请求 `/login/after/info` 仍命中旧的 `admin:info` 值。
func (l *UserCompatLogic) syncCurrentAdminNeedResetPassword(adminID int, needResetPassword int) {
	if adminID <= 0 {
		return
	}
	cacheLogic := NewCacheLogic(l.Context(), l.svc)
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
func (l *UserCompatLogic) UpdateMine(req *types.UserUpdateMineReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("UserCompatLogic.UpdateMine", err)
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
	if err := l.svc.WriteDB(svc.DatabaseMain).Model(&model.Admin{}).Where("id = ?", admin.ID).Updates(updates).Error; err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyDBError).
			WithError(errors.Wrapf(err, "UserCompatLogic.UpdateMine 更新管理员ID[%d]资料失败", admin.ID))
	}
	invalidateAdminRelationCache(l.BaseLogic, admin.ID)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// UpdateMFAStatus 修改当前登录管理员的 MFA 状态。
func (l *UserCompatLogic) UpdateMFAStatus(req *types.UserUpdateMFAStatusReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("UserCompatLogic.UpdateMFAStatus", err)
	}
	securityLogic := NewSecurityLogic(l.Context(), l.svc)
	switch req.Status() {
	case 0:
		if securityLogic.ForceLoginMFAEnabled() && admin.MfaStatus == 1 {
			return types.NewBizResult(codes.Forbidden).
				SetI18nMessage(i18n.MsgKeyMFAForceEnabledDisallowDisable).
				WithError(errors.Errorf("UserCompatLogic.UpdateMFAStatus 管理员ID[%d]系统已开启强制启用MFA，禁止停用", admin.ID))
		}
		// 个人中心关闭 MFA 属于当前账号的高敏操作，始终要求先完成同场景二次校验。
		if err := securityLogic.VerifyMFATwoStepTicket(admin.ID, MFAScenarioStatus, req.TwoStepKey, req.TwoStepValue); err != nil {
			return l.mfaResultByError(err)
		}
	case 1:
		twoStepPayload, err := securityLogic.ConsumeMFATwoStepTicket(admin.ID, MFAScenarioStatus, req.TwoStepKey, req.TwoStepValue)
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
		encryptedSecret, err := securityLogic.encryptAdminMFASecret(req.Secret())
		if err != nil {
			return types.NewBizResult(codes.ServerError).
				SetI18nMessage(i18n.MsgKeyInternalError).
				WithError(errors.Wrapf(err, "UserCompatLogic.UpdateMFAStatus 加密管理员ID[%d]MFA秘钥失败", admin.ID))
		}
		updates["mfa_secure_key"] = encryptedSecret
	}
	if err := l.svc.WriteDB(svc.DatabaseMain).Model(&model.Admin{}).Where("id = ?", admin.ID).Updates(updates).Error; err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyDBError).
			WithError(errors.Wrapf(err, "UserCompatLogic.UpdateMFAStatus 更新管理员ID[%d]MFA状态失败", admin.ID))
	}
	invalidateAdminRelationCachePreserveSession(l.BaseLogic, admin.ID)
	l.rebuildCurrentAdminSessionCache(admin.ID)
	l.syncLoginMFAAfterStatusUpdate(securityLogic, admin, req.Status())
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// syncLoginMFAAfterStatusUpdate 同步当前会话的登录 MFA 完成标记。
// 绑定或关闭 MFA 都已经完成当前场景的二次校验，因此当前会话应继续视为“已完成登录 MFA”。
func (l *UserCompatLogic) syncLoginMFAAfterStatusUpdate(securityLogic *SecurityLogic, admin *model.Admin, _ int) {
	if admin == nil || securityLogic == nil {
		return
	}
	_ = securityLogic.MarkLoginMFACompleted(admin.ID)
}

// rebuildCurrentAdminSessionCache 使用当前请求携带的 token 立即重建登录态缓存，
// 避免个人中心更新资料后立刻跳转其它页面时命中 admin:info 刚被删除的短暂窗口。
func (l *UserCompatLogic) rebuildCurrentAdminSessionCache(adminID int) {
	if adminID <= 0 {
		return
	}
	token := strings.TrimSpace(l.AccessToken())
	if token == "" {
		return
	}
	_, _ = NewCacheLogic(l.Context(), l.svc).RebuildAdminInfo(adminID, token)
}

// UpdateMFASecureKey 修改当前登录管理员的 MFA 秘钥。
func (l *UserCompatLogic) UpdateMFASecureKey(req *types.UserUpdateMFASecureKeyReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("UserCompatLogic.UpdateMFASecureKey", err)
	}
	if admin.MfaStatus == 1 {
		return types.NewBizResult(codes.Forbidden).
			SetI18nMessage(i18n.MsgKeyAuthFailed).
			WithError(errors.Errorf("UserCompatLogic.UpdateMFASecureKey 管理员ID[%d]已启用MFA，不允许自助换绑秘钥", admin.ID))
	}
	// 个人中心修改/绑定 MFA 秘钥始终要求先完成当前场景动态码校验，
	// 未启用账号也需要先拿到本次绑定流程签发的二次票据，避免绕过新秘钥验证直接落库。
	if err := NewSecurityLogic(l.Context(), l.svc).VerifyMFATwoStepTicket(admin.ID, MFAScenarioSecret, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.mfaResultByError(err)
	}
	encryptedSecret, err := NewSecurityLogic(l.Context(), l.svc).encryptAdminMFASecret(req.Secret())
	if err != nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrapf(err, "UserCompatLogic.UpdateMFASecureKey 加密管理员ID[%d]MFA秘钥失败", admin.ID))
	}
	if err := l.svc.WriteDB(svc.DatabaseMain).Model(&model.Admin{}).Where("id = ?", admin.ID).Updates(map[string]any{
		"mfa_secure_key": encryptedSecret,
		"mfa_status":     1,
		"updated_at":     time.Now(),
	}).Error; err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyDBError).
			WithError(errors.Wrapf(err, "UserCompatLogic.UpdateMFASecureKey 更新管理员ID[%d]MFA秘钥失败", admin.ID))
	}
	invalidateAdminRelationCachePreserveSession(l.BaseLogic, admin.ID)
	l.rebuildCurrentAdminSessionCache(admin.ID)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// RefreshMFASecretKey 重新生成当前登录管理员的 MFA 二维码与秘钥。
func (l *UserCompatLogic) RefreshMFASecretKey(req *types.UserRefreshMFASecretKeyReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("UserCompatLogic.RefreshMFASecretKey", err)
	}
	if admin.MfaStatus == 1 {
		return types.NewBizResult(codes.Forbidden).
			SetI18nMessage(i18n.MsgKeyAuthFailed).
			WithError(errors.Errorf("UserCompatLogic.RefreshMFASecretKey 管理员ID[%d]已启用MFA，不允许自助重新生成绑定二维码", admin.ID))
	}
	securityLogic := NewSecurityLogic(l.Context(), l.svc)
	buildURL, err := securityLogic.BuildFreshAdminMFAURL(admin)
	if err != nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrapf(err, "UserCompatLogic.RefreshMFASecretKey 刷新管理员ID[%d]MFA绑定地址失败", admin.ID))
	}
	l.rebuildCurrentAdminSessionCache(admin.ID)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess).
		WithData(&types.UserBuildMFAURLResp{BuildMFAURL: buildURL})
}

// UpdateAvatar 更新当前登录管理员头像。
func (l *UserCompatLogic) UpdateAvatar(req *types.UserUpdateAvatarReq) *types.BizResult {
	admin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("UserCompatLogic.UpdateAvatar", err)
	}
	if err := l.svc.WriteDB(svc.DatabaseMain).Model(&model.Admin{}).Where("id = ?", admin.ID).Updates(map[string]any{
		"avatar":     strings.TrimSpace(req.Avatar),
		"updated_at": time.Now(),
	}).Error; err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyDBError).
			WithError(errors.Wrapf(err, "UserCompatLogic.UpdateAvatar 更新管理员ID[%d]头像失败", admin.ID))
	}
	invalidateAdminRelationCache(l.BaseLogic, admin.ID)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// BuildMFASecretKeyURL 为指定管理员生成 MFA 绑定地址。
func (l *UserCompatLogic) BuildMFASecretKeyURL(req *types.IDPathReq) *types.BizResult {
	var admin model.Admin
	if err := l.svc.WriteDB(svc.DatabaseMain).Where("id = ?", req.ID).First(&admin).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return types.NewBizResult(codes.NotFound).
				SetI18nMessage(i18n.MsgKeyUserNotFound).
				WithError(errors.Wrapf(err, "UserCompatLogic.BuildMFASecretKeyURL 管理员ID[%d]不存在", req.ID))
		}
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyDBError).
			WithError(errors.Wrapf(err, "UserCompatLogic.BuildMFASecretKeyURL 查询管理员ID[%d]失败", req.ID))
	}
	securityLogic := NewSecurityLogic(l.Context(), l.svc)
	buildURL := ""
	var err error
	if admin.MfaStatus != 1 {
		buildURL, err = securityLogic.BuildFreshAdminMFAURL(&admin)
	} else {
		buildURL, err = securityLogic.BuildAdminMFAURL(&admin)
	}
	if err != nil {
		return types.NewBizResult(codes.ServerError).
			SetI18nMessage(i18n.MsgKeyInternalError).
			WithError(errors.Wrapf(err, "UserCompatLogic.BuildMFASecretKeyURL 生成管理员ID[%d]MFA绑定地址失败", req.ID))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(&types.UserBuildMFAURLResp{BuildMFAURL: buildURL})
}

// RoleTreeList 返回角色树下拉数据，兼容账号管理页面选择角色。
func (l *UserCompatLogic) RoleTreeList() *types.BizResult {
	roleResp := (&AdminRoleLogic{BaseLogic: l.BaseLogic}).TreeList()
	if roleResp == nil || roleResp.IsFailure() {
		return roleResp
	}
	items, _ := roleResp.Data.([]types.AdminRoleItem)
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(l.compatRoleTree(items))
}

// UpdateAccountMFAStatus 修改指定管理员的 MFA 状态。
func (l *UserCompatLogic) UpdateAccountMFAStatus(req *types.AdminMFAStatusReq) *types.BizResult {
	currentAdmin, err := l.currentAdmin()
	if err != nil {
		return l.adminFetchErrorResult("UserCompatLogic.UpdateAccountMFAStatus", err)
	}
	securityLogic := NewSecurityLogic(l.Context(), l.svc)
	if securityLogic.NeedOperateMFATwoStep(MFAScenarioStatus) {
		if err := securityLogic.VerifyMFATwoStepTicket(currentAdmin.ID, MFAScenarioStatus, req.TwoStepKey, req.TwoStepValue); err != nil {
			return l.mfaResultByError(err)
		}
	}
	updates := map[string]any{
		"mfa_status": req.Status(),
		"updated_at": time.Now(),
	}
	if err := l.svc.WriteDB(svc.DatabaseMain).Model(&model.Admin{}).Where("id = ?", req.ID).Updates(updates).Error; err != nil {
		return types.NewBizResult(codes.DBError).
			SetI18nMessage(i18n.MsgKeyDBError).
			WithError(errors.Wrapf(err, "UserCompatLogic.UpdateAccountMFAStatus 更新管理员ID[%d]MFA状态失败", req.ID))
	}
	invalidateAdminRelationCache(l.BaseLogic, req.ID)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// currentAdmin 读取当前登录管理员完整模型。
func (l *UserCompatLogic) currentAdmin() (*model.Admin, error) {
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin == nil || ctxAdmin.ID <= 0 {
		return nil, types.Nil
	}
	admin, err := (&AdminLogic{BaseLogic: l.BaseLogic}).GetAdminByID(ctxAdmin.ID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return admin, nil
}

// resolveEnableMFASecret 根据二次票据中的秘钥来源，决定本次启用 MFA 应落库的正式秘钥。
func (l *UserCompatLogic) resolveEnableMFASecret(admin *model.Admin, requestSecret string, payload *mfaTwoStepTicketPayload) (string, error) {
	requestSecret = normalizeMFASecret(requestSecret)
	if payload == nil {
		if isUsableMFASecret(requestSecret) {
			return requestSecret, nil
		}
		return "", ErrAdminMFATwoStepExpired
	}
	switch payload.SecretSource {
	case "", mfaTwoStepSecretSourceRequest:
		if !isUsableMFASecret(requestSecret) {
			return "", ErrAdminMFATwoStepExpired
		}
		if payload.SecretDigest != "" && payload.SecretDigest != hashMFASecret(requestSecret) {
			return "", ErrAdminMFATwoStepExpired
		}
		return requestSecret, nil
	case mfaTwoStepSecretSourceCurrent:
		securityLogic := NewSecurityLogic(l.Context(), l.svc)
		currentSecret, err := securityLogic.loadAdminMFASecret(admin)
		if err != nil {
			return "", errors.Tag(err)
		}
		if !isUsableMFASecret(currentSecret) {
			return "", ErrAdminMFATwoStepExpired
		}
		if payload.SecretDigest != "" && payload.SecretDigest != hashMFASecret(currentSecret) {
			return "", ErrAdminMFATwoStepExpired
		}
		return currentSecret, nil
	default:
		return "", ErrAdminMFATwoStepExpired
	}
}

// buildCompatUserInfo 构造前端兼容用户资料。
func (l *UserCompatLogic) BuildCompatUserInfo(admin *model.Admin, token string) (*types.UserCompatInfo, error) {
	if admin == nil {
		return nil, errors.Errorf("管理员信息不能为空")
	}
	securityLogic := NewSecurityLogic(l.Context(), l.svc)
	forceMFAEnabled := securityLogic.ForceLoginMFAEnabled()
	existMFA := securityLogic.HasUsableAdminMFASecret(admin)
	buildURL := ""
	needBindMFA := securityLogic.NeedBindMFAOnLogin(admin)
	// 仅未启用账号允许返回静态绑定二维码；已启用账号即使设备不可用，也只能联系管理员处理，
	// 不能在登录态或个人中心继续走自助换绑流程。
	if admin.MfaStatus != 1 {
		var err error
		buildURL, err = securityLogic.BuildFreshAdminMFAURL(admin)
		if err != nil {
			// 登录后资料里的二维码直接驱动 MFA 绑定流程，失败时返回错误让上层按链路记录并提示重试。
			return nil, errors.Wrapf(err, "UserCompatLogic.buildCompatUserInfo 生成管理员ID[%d]MFA绑定地址失败", admin.ID)
		}
	}
	mfaCheck := 0
	// 登录兼容接口统一复用安全链路的登录 MFA 判定，避免强制启用配置与账号自身状态出现分叉。
	needLoginMFA := securityLogic.NeedLoginMFA(admin)
	if needLoginMFA && !securityLogic.HasPassedLoginMFA(admin) {
		mfaCheck = 1
	}
	return &types.UserCompatInfo{
		ID:                admin.ID,
		Username:          admin.Name,
		RealName:          admin.RealName,
		NeedResetPassword: admin.NeedResetPassword,
		Email:             admin.Email,
		Phone:             admin.Phone,
		Status:            admin.Status,
		MfaStatus:         admin.MfaStatus,
		GroupID:           0,
		ExistMFA:          existMFA,
		BuildMFAURL:       buildURL,
		ForceMFAEnabled:   forceMFAEnabled,
		MFABindRequired:   forceMFAEnabled && needBindMFA,
		Avatar:            admin.Avatar,
		Description:       admin.Description,
		LastLoginTime:     formatDateTime(admin.LastLoginTime),
		LastLoginIP:       admin.LastLoginIP,
		LastLoginAddr:     admin.LastLoginIpaddr,
		CreatedAt:         formatDateTime(admin.CreatedAt),
		UpdatedAt:         formatDateTime(admin.UpdatedAt),
		MFACheck:          mfaCheck,
		Frequency:         securityLogic.MFAFrequency(),
	}, nil
}

// requireScenarioTwoStep 在当前场景需要 MFA 时校验二次校验票据。
func (l *UserCompatLogic) requireScenarioTwoStep(admin *model.Admin, scenario int, key string, value string) error {
	securityLogic := NewSecurityLogic(l.Context(), l.svc)
	if !securityLogic.NeedMFATwoStep(admin, scenario) {
		return nil
	}
	return securityLogic.VerifyMFATwoStepTicket(admin.ID, scenario, key, value)
}

// mfaResultByError 把 MFA 相关错误统一转换成前端可识别的业务响应码。
func (l *UserCompatLogic) mfaResultByError(err error) *types.BizResult {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrAdminMFATwoStepExpired) || errors.Is(err, ErrAdminMFARequired) {
		return types.NewBizResult(codes.CheckMFAAgain).
			SetI18nMessage(i18n.MsgKeyMFAExpired).
			WithError(wrapLogicError(err, "UserCompatLogic.mfaResultByError MFA二次校验已失效"))
	}
	return types.NewBizResult(codes.Forbidden).
		SetI18nMessage(i18n.MsgKeyAuthFailed).
		WithError(wrapLogicError(err, "UserCompatLogic.mfaResultByError MFA校验失败"))
}

// adminFetchErrorResult 统一转换当前管理员查询失败响应。
func (l *UserCompatLogic) adminFetchErrorResult(operation string, err error) *types.BizResult {
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

// compatRoleTree 把角色树转换成前端下拉组件可直接使用的树结构。
func (l *UserCompatLogic) compatRoleTree(items []types.AdminRoleItem) []map[string]any {
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
			node["children"] = l.compatRoleTree(item.Children)
		}
		result = append(result, node)
	}
	return result
}
