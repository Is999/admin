package admin

import (
	"admin/common/codes"
	i18n "admin/common/i18n"
	keys "admin/common/rediskeys"
	"admin/helper"
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	rbaclogic "admin/internal/logic/rbac"
	securitylogic "admin/internal/logic/security"
	"fmt"

	"net/http"
	"strings"
	"time"

	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	tablecache "github.com/Is999/table-cache"
	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// AdminLogic 承载管理员登录、会话、账号创建和权限码查询等核心逻辑。
type AdminLogic struct {
	*corelogic.BaseLogic // 复用上下文、日志、数据库和缓存等公共能力
}

// NewAdminLogic 创建管理员业务逻辑对象，继承带请求上下文的 BaseLogic。
func NewAdminLogic(r *http.Request, svcCtx *svc.ServiceContext) *AdminLogic {
	return &AdminLogic{
		BaseLogic: corelogic.NewBaseLogic(r, svcCtx),
	}
}

// buildAdminInfoCache 把管理员模型和当前 token 统一转换成登录态缓存结构。
func buildAdminInfoCache(admin *model.Admin, token string) *types.AdminInfo {
	return cachelogic.BuildAdminProfileCache(admin).ToAdminInfo(token)
}

// Login 校验管理员账号密码，更新登录态并写入缓存会话信息。
func (l *AdminLogic) Login(req *types.LoginReq) *types.BizResult {
	// 登录属于强一致鉴权链路，必须直接查主库，避免主从延迟导致禁用/改密状态未及时生效。
	admin, err := model.FindUserByName(l.Svc.WriteDB(svc.DatabaseMain), req.Username)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminLogic.Login 账号[%s]", req.Username).ToBizResult()
	}

	if admin == nil {
		return types.NotFound(i18n.MsgKeyAccountPwdInvalid, nil,
			"AdminLogic.Login 账号[%s]不存在", req.Username).ToBizResult()
	}

	// 比较密码
	if err = bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(admin.PasswordWithSalt(req.Password))); err != nil {
		return types.NewBizResult(codes.InvalidPassword).
			SetI18nMessage(i18n.MsgKeyInvalidPassword).
			WithError(errors.Errorf("AdminLogic.Login 账号[%s]密码错误", req.Username))
	}

	// 检查用户状态
	if admin.Status != 1 {
		return types.NewBizResult(codes.UserDisabled).
			SetI18nMessage(i18n.MsgKeyUserDisabled).
			WithError(errors.Errorf("AdminLogic.Login 账号[%s]已被禁用", req.Username))
	}

	// 更新最后登录时间和 IP
	admin.LastLoginIP = req.IP
	admin.LastLoginTime = time.Now()
	admin.UpdatedAt = time.Now()

	update := map[string]any{
		"last_login_time": admin.LastLoginTime,
		"last_login_ip":   admin.LastLoginIP,
		"updated_at":      admin.UpdatedAt,
	}
	if err = model.UpdateAdmin(l.Svc.WriteDB(svc.DatabaseMain), admin.ID, update); err != nil {
		return types.DBError(i18n.MsgKeyInternalErrorFormat, err,
			"AdminLogic.Login 账号[%s]更新最后登录信息失败", req.Username).ToBizResult()
	}

	// 生成 JWT 令牌
	token, err := l.generateJWT(admin.ID, admin.Name, admin.LastLoginIP)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"AdminLogic.Login 账号[%s]生成令牌失败", req.Username).ToBizResult()
	}

	info := buildAdminInfoCache(admin, token)

	// 把用户资料和 token 一并缓存到 Redis，后续鉴权和登录后初始化信息都优先走缓存。
	cacheLogic := cachelogic.NewCacheLogic(l.Ctx, l.Svc)
	if err = cacheLogic.SetAdminInfo(admin.ID, info); err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"AdminLogic.Login 账号[%s]缓存用户信息失败", req.Username).ToBizResult()
	}
	_ = cacheLogic.ClearAdminLogoutToken(admin.ID)

	userInfo, err := securitylogic.NewSecurityLogic(l.Ctx, l.Svc).BuildProfileInfo(admin, token)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"AdminLogic.Login 账号[%s]构造登录用户上下文失败", req.Username).ToBizResult()
	}

	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(&types.ProfileLoginResp{
			Token: token,
			User:  userInfo,
		})
}

// Logout 清理当前管理员缓存登录态，完成显式登出。
func (l *AdminLogic) Logout(ctxAdmin *helper.CtxAdmin) *types.BizResult {
	cacheLogic := cachelogic.NewCacheLogic(l.Ctx, l.Svc)
	if err := cacheLogic.MarkAdminLogoutToken(ctxAdmin.ID, l.AccessToken(), 7*24*time.Hour); err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"AdminLogic.Logout 账号[%s]记录登出令牌失败", ctxAdmin.Name).ToBizResult()
	}
	err := cacheLogic.DeleteAdminInfo(ctxAdmin.ID)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"AdminLogic.Logout 账号[%s]清理缓存失败", ctxAdmin.Name).ToBizResult()
	}

	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyLogoutSuccess)
}

// generateJWT 生成 JWT 令牌，sub/username/ip 会被后续鉴权中间件解析并回填到请求上下文。
func (l *AdminLogic) generateJWT(userID int, username string, IP string) (string, error) {
	cfg := l.Svc.CurrentConfig()
	claims := jwt.MapClaims{
		"sub":      userID,
		"username": username,
		"ip":       IP,
		"iat":      time.Now().Unix(),
		"exp":      time.Now().Add(7 * 24 * time.Hour).Unix(), // 令牌有效期为7天, 到期后强制重新登录
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.JwtSecret))
}

// Create 新增管理员账号，并在同一事务内完成落库和密码摘要回写。
func (l *AdminLogic) Create(req *types.AddAdminReq) *types.BizResult {
	if err := l.requireOperateMFATwoStep(securitylogic.MFAScenarioAddUser, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.mfaBizResult(err)
	}

	// 创建前唯一性校验也走主库，避免从库延迟导致“刚创建完又查不到”而误判可重复创建。
	q := model.NewQuery(l.Svc.WriteDB(svc.DatabaseMain), &model.Admin{})
	exists, err := q.Exists(func(db *gorm.DB) *gorm.DB {
		return db.Where("name = ?", req.Username)
	})
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminLogic.Create 账号[%s]", req.Username).ToBizResult()
	}
	if exists {
		return AdminNameAlreadyExistsResult(req.Username, ErrAdminNameAlreadyExists)
	}
	encryptedMFASecret := ""
	if strings.TrimSpace(req.MfaSecureKey) != "" {
		encryptedMFASecret, err = securitylogic.NewSecurityLogic(l.Ctx, l.Svc).EncryptAdminMFASecret(req.MfaSecureKey)
		if err != nil {
			return types.NewBizResult(codes.ServerError).
				SetI18nMessage(i18n.MsgKeyInternalError).
				WithError(errors.Wrapf(err, "AdminLogic.Create 账号[%s]加密MFA秘钥失败", req.Username))
		}
	}

	admin := model.Admin{
		ID:                0,
		Name:              req.Username,
		RealName:          req.RealName,
		Password:          "", // 密码稍后更新
		NeedResetPassword: 1,
		Email:             req.Email,
		Phone:             req.Phone,
		MfaSecureKey:      encryptedMFASecret,
		// 首次登录阶段允许用户先改密、后续再自行决定是否完成 MFA 绑定，因此新建账号默认保持待启用状态。
		MfaStatus:       0,
		Status:          1,
		Avatar:          req.Avatar,
		Description:     req.Description,
		LastLoginTime:   time.Time{},
		LastLoginIP:     "",
		LastLoginIPAddr: "",
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	// 创建用户和写入加密密码必须处于同一事务，统一交给闭包事务处理提交/回滚。
	roleIDs := types.UniquePositiveInts(req.RoleIDs)
	if len(roleIDs) > 0 {
		if err := (&rbaclogic.AdminRoleLogic{BaseLogic: l.BaseLogic}).EnsureRolesWithinManageScope(roleIDs); err != nil {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "AdminLogic.Create 账号[%s]初始角色超出可操作范围", req.Username))
		}
	}
	if err = l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&admin).Error; err != nil {
			return errors.Wrap(err, "tx.Create 创建用户失败")
		}

		// 先基于已落库账号信息生成带盐密码，再回写到当前事务中的记录。
		password, err := bcrypt.GenerateFromPassword([]byte(admin.PasswordWithSalt(req.Password)), bcrypt.DefaultCost)
		if err != nil {
			return errors.Wrap(err, "bcrypt.GenerateFromPassword 密码加密失败")
		}

		if err := tx.Model(&admin).Update("password", string(password)).Error; err != nil {
			return errors.Wrap(err, "tx.Update 更新用户密码失败")
		}
		if len(roleIDs) > 0 {
			if err := (&AdminManageLogic{AdminLogic: l}).replaceAdminRolesTx(tx, admin.ID, roleIDs); err != nil {
				return errors.Wrap(err, "绑定用户初始角色失败")
			}
		}
		return nil
	}); err != nil {
		if corelogic.IsMySQLDuplicateEntryError(err) {
			return AdminNameAlreadyExistsResult(req.Username, err)
		}
		return types.DBError(i18n.MsgKeyInternalErrorFormat, err,
			"AdminLogic.Create 账号[%s]事务执行失败", req.Username).ToBizResult()
	}

	return types.NewBizResult(codes.AddSuccess).
		SetI18nMessage(i18n.MsgKeyAddSuccess)
}

// requireOperateMFATwoStep 在新增管理员等敏感操作前校验当前登录管理员的 MFA 二次票据。
func (l *AdminLogic) requireOperateMFATwoStep(scenario int, twoStepKey string, twoStepValue string) error {
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin == nil || ctxAdmin.ID <= 0 {
		return types.Nil
	}
	securityLogic := securitylogic.NewSecurityLogic(l.Ctx, l.Svc)
	if !securityLogic.NeedOperateMFATwoStep(scenario) {
		return nil
	}
	return securityLogic.VerifyMFATwoStepTicket(ctxAdmin.ID, scenario, twoStepKey, twoStepValue)
}

// RequireOperateMFATwoStep 校验后台敏感操作的 MFA 二次票据。
func (l *AdminLogic) RequireOperateMFATwoStep(scenario int, twoStepKey string, twoStepValue string) error {
	return l.requireOperateMFATwoStep(scenario, twoStepKey, twoStepValue)
}

// mfaBizResult 把后台管理敏感操作中的 MFA 错误转换成统一业务响应。
func (l *AdminLogic) mfaBizResult(err error) *types.BizResult {
	return securitylogic.OperateMFABizResult(err, "AdminLogic.mfaBizResult")
}

// MFABizResult 把后台敏感操作中的 MFA 错误转换成统一业务响应。
func (l *AdminLogic) MFABizResult(err error) *types.BizResult {
	return l.mfaBizResult(err)
}

// GetAdminByID 通过ID获取管理员详细信息。
// 当前仅用于登录后初始化、刷新令牌等会话链路，因此固定走主库保证强一致。
func (l *AdminLogic) GetAdminByID(id int) (*model.Admin, error) {
	var admin model.Admin
	err := l.Svc.WriteDB(svc.DatabaseMain).Where("id = ?", id).First(&admin).Error
	if err != nil {
		return nil, errors.Wrapf(err, "AdminLogic.GetAdminByID 查询管理员ID[%d]失败", id)
	}
	return &admin, nil
}

// GetAdminProfileByID 优先读取管理员公开资料缓存，未命中时自动回源并回填。
func (l *AdminLogic) GetAdminProfileByID(id int) (*types.AdminProfile, error) {
	if id <= 0 {
		return nil, errors.Errorf("管理员ID不能为空")
	}
	if l.Redis() == nil {
		admin, err := l.GetAdminByID(id)
		if err != nil {
			return nil, errors.Tag(err)
		}
		return cachelogic.BuildAdminProfileCache(admin), nil
	}
	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	profile := &types.AdminProfile{}
	result, err := manager.LoadThrough(l.Ctx, cachelogic.TableCachePhysicalKey(l.BaseLogic, fmt.Sprintf(keys.AdminProfile, id)), profile, nil)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if result.State == tablecache.LookupStateEmpty {
		return nil, gorm.ErrRecordNotFound
	}
	return profile, nil
}

// GetLoginAfterInfo 返回前端登录后初始化所需的管理员资料与基础角色信息。
func (l *AdminLogic) GetLoginAfterInfo(ctxAdmin *helper.CtxAdmin) *types.BizResult {
	info, err := cachelogic.NewCacheLogic(l.Ctx, l.Svc).GetAdminInfo(ctxAdmin.ID)
	if err != nil {
		// 缓存 miss 时回源数据库补全信息，避免因缓存丢失导致前端登录后初始化接口报错。
		profile, err := l.GetAdminProfileByID(ctxAdmin.ID)
		if err != nil {
			return &types.BizResult{
				Code:       codes.ServerError,
				MessageKey: i18n.MsgKeyAdminInfoFetchFail,
				Error:      errors.Wrapf(err, "AdminLogic.GetCurrentAdminInfo 账号[%s]l.GetAdminByID 获取管理员信息失败", ctxAdmin.Name),
			}
		}

		info = profile.ToAdminInfo(l.AccessToken())

		// 缓存用户信息
		if err = cachelogic.NewCacheLogic(l.Ctx, l.Svc).SetAdminInfo(ctxAdmin.ID, info); err != nil {
			return &types.BizResult{
				Code:       codes.InternalError,
				MessageKey: i18n.MsgKeyCacheInfoFail,
				Error:      errors.Wrapf(err, "AdminLogic.Login 账号[%s]NewCacheLogic.SetAdminInfo 缓存用户信息失败", info.UserName),
			}
		}
	}

	// 登录后初始化继续保持“缓存优先、miss 自动回源并回填缓存”的统一语义，
	// 避免每次进入后台都直接访问 MySQL。
	roleLogic := &rbaclogic.AdminRoleLogic{BaseLogic: l.BaseLogic}
	roleIDs, err := roleLogic.EnabledRoleIDsByUserWithCache(ctxAdmin.ID)
	if err != nil {
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyRoleFetchFail,
			Error:      errors.Wrapf(err, "AdminLogic.GetLoginAfterInfo 账号[%s]获取用户角色 ID失败", ctxAdmin.Name),
		}
	}
	roles, err := (&rbaclogic.AdminRoleRelLogic{BaseLogic: l.BaseLogic}).GetRolesByUserID(int64(ctxAdmin.ID))
	if err != nil {
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyRoleFetchFail,
			Error:      errors.Wrapf(err, "AdminLogic.GetLoginAfterInfo 账号[%s]获取用户角色失败", ctxAdmin.Name),
		}
	}
	// 角色 ID 为 1 的账号统一视为超级管理员，前端会基于该标记做权限码兜底。
	isSuperAdmin := false
	for _, roleID := range roleIDs {
		if roleID == corelogic.AdminSuperRoleID {
			isSuperAdmin = true
			break
		}
	}

	// 构造响应
	resp := types.AdminLoginAfterInfoResp{
		ID:                info.ID,
		UserName:          info.UserName,
		RealName:          info.RealName,
		NeedResetPassword: info.NeedResetPassword,
		IsSuperAdmin:      isSuperAdmin,
		Email:             info.Email,
		Phone:             info.Phone,
		MfaStatus:         info.MfaStatus,
		Status:            info.Status,
		Avatar:            info.Avatar,
		Description:       info.Description,
		LastLoginTime:     info.LastLoginTime,
		LastLoginIP:       info.LastLoginIP,
		LastLoginIPAddr:   info.LastLoginIPAddr,
		RoleIDs:           roleIDs,
		Roles:             roles,
		Token:             info.Token,
	}

	return &types.BizResult{
		Code:       codes.Success,
		MessageKey: i18n.MsgKeySuccess,
		Data:       resp,
	}
}

// GetUserPermissionCodes 汇总当前管理员拥有的全部权限码集合。
func (l *AdminLogic) GetUserPermissionCodes(userID int) *types.BizResult {
	// `/auth/codes` 继续保持缓存优先；缓存 miss 时由下层统一回源数据库并更新缓存。
	securityLogic := securitylogic.NewSecurityLogic(l.Ctx, l.Svc)
	codesArr, err := securityLogic.UserPermissionUUIDsWithCache(userID)
	if err != nil {
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyPermCodeFetchFail,
			Error:      err,
		}
	}
	if len(codesArr) == 0 {
		return &types.BizResult{
			Code:       codes.Success,
			MessageKey: i18n.MsgKeySuccess,
			Data:       []string{},
		}
	}
	return &types.BizResult{
		Code:       codes.Success,
		MessageKey: i18n.MsgKeySuccess,
		Data:       codesArr,
	}
}

// RefreshAccessToken 刷新当前管理员访问令牌，并同步回写缓存中的 token 字段。
func (l *AdminLogic) RefreshAccessToken(ctxAdmin *helper.CtxAdmin) *types.BizResult {
	cacheLogic := cachelogic.NewCacheLogic(l.Ctx, l.Svc)
	info, err := cacheLogic.GetAdminInfo(ctxAdmin.ID)
	ip := l.ClientIP()
	if err == nil && info != nil {
		// 缓存中有用户信息，直接生成新Token返回
		token, err := l.generateJWT(info.ID, info.UserName, ip)
		if err != nil {
			return &types.BizResult{
				Code:       codes.InternalError,
				MessageKey: i18n.MsgKeyTokenGenerateFail,
				Error:      errors.Wrapf(err, "AdminLogic.RefreshAccessToken 账号[%s]l.generateJWT 生成新Token失败", info.UserName),
			}
		}
		if err = cacheLogic.SetAdminInfoByField(ctxAdmin.ID, "token", token); err != nil {
			return &types.BizResult{
				Code:       codes.InternalError,
				MessageKey: i18n.MsgKeyTokenCacheFail,
				Error:      errors.Wrapf(err, "AdminLogic.RefreshAccessToken 账号[%s]cacheLogic.SetAdminInfoByField 更新缓存Token失败", info.UserName),
			}
		}
		return &types.BizResult{
			Code:       codes.Success,
			MessageKey: i18n.MsgKeySuccess,
			Data:       map[string]interface{}{"token": token, "isRefresh": true},
		}
	}
	profile, err := l.GetAdminProfileByID(ctxAdmin.ID)
	if err != nil {
		return &types.BizResult{
			Code:       codes.ServerError,
			MessageKey: i18n.MsgKeyAdminInfoFetchFail,
			Error:      errors.Wrapf(err, "AdminLogic.RefreshAccessToken 账号[%s]GetAdminProfileByID 获取管理员资料失败", ctxAdmin.Name),
		}
	}
	token, err := l.generateJWT(profile.ID, profile.UserName, ip)
	if err != nil {
		return &types.BizResult{
			Code:       codes.InternalError,
			MessageKey: i18n.MsgKeyTokenGenerateFail,
			Error:      errors.Wrapf(err, "AdminLogic.RefreshAccessToken 账号[%s]l.generateJWT 生成新Token失败", profile.UserName),
		}
	}
	if err = cacheLogic.SetAdminInfo(profile.ID, profile.ToAdminInfo(token)); err != nil {
		return &types.BizResult{
			Code:       codes.InternalError,
			MessageKey: i18n.MsgKeyCacheInfoFail,
			Error:      errors.Wrapf(err, "AdminLogic.RefreshAccessToken 账号[%s]NewCacheLogic.SetAdminInfo 缓存用户信息失败", profile.UserName),
		}
	}
	return &types.BizResult{
		Code:       codes.Success,
		MessageKey: i18n.MsgKeySuccess,
		Data:       map[string]interface{}{"token": token, "isRefresh": true},
	}
}
