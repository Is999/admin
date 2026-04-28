package admin

import (
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	rbaclogic "admin/internal/logic/rbac"
	securitylogic "admin/internal/logic/security"
	"net/http"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	"admin/common/codes"
	i18n "admin/common/i18n"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AdminManageLogic 承载管理员管理页面的查询、编辑、状态和角色分配逻辑。
type AdminManageLogic struct {
	*AdminLogic // 复用管理员账号与权限相关公共能力
}

// NewAdminManageLogic 创建管理员管理业务逻辑对象。
func NewAdminManageLogic(r *http.Request, svcCtx *svc.ServiceContext) *AdminManageLogic {
	return &AdminManageLogic{
		AdminLogic: NewAdminLogic(r, svcCtx),
	}
}

// List 分页查询管理员列表，并补齐已绑定角色信息。
func (l *AdminManageLogic) List(req *types.AdminListReq) *types.BizResult {
	// 查询管理员列表时优先走只读库；当按角色筛选时通过关系表做内连接过滤。
	dbq := l.Svc.ReadDB(svc.DatabaseMain).Model(&model.Admin{})
	if req.Username != "" {
		dbq = dbq.Where("admin.name LIKE ?", "%"+req.Username+"%")
	}
	if req.RealName != "" {
		dbq = dbq.Where("admin.real_name LIKE ?", "%"+req.RealName+"%")
	}
	if req.Status != nil {
		dbq = dbq.Where("admin.status = ?", *req.Status)
	}
	if req.RoleID != nil && *req.RoleID > 0 {
		roleID := *req.RoleID
		// 角色筛选改为 `id IN (subquery)`，避免在分页 Count() 阶段生成
		// `COUNT(DISTINCT admin.*)` 这类 MySQL 不支持的 SQL。
		roleSubQuery := l.Svc.ReadDB(svc.DatabaseMain).
			Model(&model.AdminRoleRel{}).
			Select("user_id").
			Where("role_id = ?", roleID)
		dbq = dbq.Where("admin.id IN (?)", roleSubQuery)
	}

	// 排序字段先映射成数据库字段，再交给通用分页查询执行。
	orderBy := corelogic.NormalizedOrderField(req.OrderBy, "id")
	list, total, err := model.List[model.Admin](dbq, req.Page, req.PageSize, orderBy, corelogic.NormalizedOrderDirection(req.Order))
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.List 查询管理员列表失败").ToBizResult()
	}

	// 管理员列表需要展示角色名称，统一批量加载避免循环查询。
	roleMap, err := l.adminRoleMap(list)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.List 查询管理员角色失败").ToBizResult()
	}

	items := make([]types.AdminItem, 0, len(list))
	for _, admin := range list {
		roles := roleMap[admin.ID]
		roleIDs := make([]int, 0, len(roles))
		for _, role := range roles {
			roleIDs = append(roleIDs, role.ID)
		}
		items = append(items, adminModelToItem(admin, roleIDs, roles))
	}

	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.ListResp[types.AdminItem]{List: items, Total: total})
}

// Get 查询单个管理员详情。
func (l *AdminManageLogic) Get(req *types.IDPathReq) *types.BizResult {
	admin, err := l.GetAdminByID(req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyUserNotFound, err,
				"AdminManageLogic.Get 管理员ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.Get 查询管理员ID[%d]失败", req.ID).ToBizResult()
	}

	roles, err := l.adminRoles(admin.ID)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.Get 查询管理员ID[%d]角色失败", req.ID).ToBizResult()
	}
	roleIDs := make([]int, 0, len(roles))
	for _, role := range roles {
		roleIDs = append(roleIDs, role.ID)
	}

	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(adminModelToItem(*admin, roleIDs, roles))
}

// Update 编辑管理员基础资料，可选同步重置密码和角色。
func (l *AdminManageLogic) Update(req *types.UpdateAdminReq) *types.BizResult {
	mfaScenario := securitylogic.MFAScenarioEditUser
	if req.Password != nil && strings.TrimSpace(*req.Password) != "" {
		mfaScenario = securitylogic.MFAScenarioChangePassword
	}
	if err := l.requireOperateMFATwoStep(mfaScenario, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.mfaBizResult(err)
	}

	admin, err := l.GetAdminByID(req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyUserNotFound, err,
				"AdminManageLogic.Update 管理员ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.Update 查询管理员ID[%d]失败", req.ID).ToBizResult()
	}

	updates := buildAdminUpdates(req, admin)
	roleIDs := types.UniquePositiveInts(req.RoleIDs)
	roleIDs, err = l.pruneInheritedAssignedRoleIDs(roleIDs)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.Update 归一化管理员ID[%d]角色失败", req.ID).ToBizResult()
	}
	if err := l.ensureAdminRoleManageScope(req.ID, roleIDs); err != nil {
		if errors.Is(err, rbaclogic.ErrRoleManageScopeExceeded) {
			return types.Forbidden(i18n.MsgKeyForbidden).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.Update 校验管理员ID[%d]角色范围失败", req.ID).ToBizResult()
	}
	if ctxAdmin := l.GetCtxAdmin(); ctxAdmin != nil && req.ID == ctxAdmin.ID && req.Status != nil && *req.Status != admin.Status {
		return types.NewBizResult(codes.Fail).
			SetI18nMessage(i18n.MsgKeyFail).
			WithError(errors.Errorf("AdminManageLogic.Update 不允许修改当前登录管理员ID[%d]状态", req.ID))
	}
	shouldUpdateRoles := req.IsUpdateRoles || len(roleIDs) > 0

	// 基础信息、密码和角色关系必须在同一事务内提交，避免页面看到半更新状态。
	if err = l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		if len(updates) > 0 {
			if err := tx.Model(&model.Admin{}).Where("id = ?", req.ID).Updates(updates).Error; err != nil {
				return errors.Wrap(err, "更新管理员基础资料失败")
			}
		}
		if req.Password != nil && strings.TrimSpace(*req.Password) != "" {
			password, err := bcrypt.GenerateFromPassword([]byte(admin.PasswordWithSalt(strings.TrimSpace(*req.Password))), bcrypt.DefaultCost)
			if err != nil {
				return errors.Wrap(err, "生成管理员密码哈希失败")
			}
			if err := tx.Model(&model.Admin{}).Where("id = ?", req.ID).Updates(map[string]any{
				"password":            string(password),
				"need_reset_password": 1,
			}).Error; err != nil {
				return errors.Wrap(err, "更新管理员密码失败")
			}
		}
		if shouldUpdateRoles {
			if err := l.replaceAdminRolesTx(tx, req.ID, roleIDs); err != nil {
				return errors.Tag(err)
			}
		}
		return nil
	}); err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.Update 更新管理员ID[%d]失败", req.ID).ToBizResult()
	}

	// 管理员资料、角色或权限变化后统一清理登录态与权限聚合缓存，保证下次读取回源最新数据。
	cachelogic.InvalidateAdminRelationCache(l.BaseLogic, req.ID)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// Delete 删除管理员账号，同时清理角色关系和登录态缓存。
func (l *AdminManageLogic) Delete(req *types.IDPathReq) *types.BizResult {
	if err := l.requireOperateMFATwoStep(securitylogic.MFAScenarioDeleteUser, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.mfaBizResult(err)
	}
	if req.ID == l.GetCtxAdmin().ID {
		return types.NewBizResult(codes.Fail).
			SetI18nMessage(i18n.MsgKeyFail).
			WithError(errors.Errorf("AdminManageLogic.Delete 不允许删除当前登录管理员ID[%d]", req.ID))
	}
	if err := l.ensureAdminRoleManageScope(req.ID, nil); err != nil {
		if errors.Is(err, rbaclogic.ErrRoleManageScopeExceeded) {
			return types.Forbidden(i18n.MsgKeyForbidden).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.Delete 校验管理员ID[%d]角色范围失败", req.ID).ToBizResult()
	}

	// 删除管理员时同步删除角色关系，避免关系表留下无主数据。
	if err := l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", req.ID).Delete(&model.AdminRoleRel{}).Error; err != nil {
			return errors.Wrap(err, "删除管理员角色关系失败")
		}
		result := tx.Where("id = ?", req.ID).Delete(&model.Admin{})
		if result.Error != nil {
			return errors.Wrap(result.Error, "删除管理员失败")
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyUserNotFound, err,
				"AdminManageLogic.Delete 管理员ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.Delete 删除管理员ID[%d]失败", req.ID).ToBizResult()
	}

	cachelogic.InvalidateAdminRelationCache(l.BaseLogic, req.ID)
	return types.NewBizResult(codes.DeleteSuccess).
		SetI18nMessage(i18n.MsgKeyDeleteSuccess)
}

// UpdateStatus 修改管理员启用/禁用状态。
func (l *AdminManageLogic) UpdateStatus(req *types.AdminStatusReq) *types.BizResult {
	if err := l.requireOperateMFATwoStep(securitylogic.MFAScenarioUserStatus, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.mfaBizResult(err)
	}
	status := req.StatusValue()

	if req.ID == l.GetCtxAdmin().ID {
		return types.NewBizResult(codes.Fail).
			SetI18nMessage(i18n.MsgKeyFail).
			WithError(errors.Errorf("AdminManageLogic.UpdateStatus 不允许修改当前登录管理员ID[%d]状态", req.ID))
	}
	if err := l.ensureAdminRoleManageScope(req.ID, nil); err != nil {
		if errors.Is(err, rbaclogic.ErrRoleManageScopeExceeded) {
			return types.Forbidden(i18n.MsgKeyForbidden).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.UpdateStatus 校验管理员ID[%d]角色范围失败", req.ID).ToBizResult()
	}

	result := l.Svc.WriteDB(svc.DatabaseMain).Model(&model.Admin{}).
		Where("id = ?", req.ID).
		Updates(map[string]any{
			"status":     status,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		return types.DBError(i18n.MsgKeyDBError, result.Error,
			"AdminManageLogic.UpdateStatus 修改管理员ID[%d]状态失败", req.ID).ToBizResult()
	}
	if result.RowsAffected == 0 {
		return types.NotFound(i18n.MsgKeyUserNotFound, gorm.ErrRecordNotFound,
			"AdminManageLogic.UpdateStatus 管理员ID[%d]不存在", req.ID).ToBizResult()
	}

	cachelogic.InvalidateAdminRelationCache(l.BaseLogic, req.ID)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyStatusChangeOK)
}

// ResetPassword 重置管理员密码，并清理登录态缓存。
func (l *AdminManageLogic) ResetPassword(req *types.ResetAdminPasswordReq) *types.BizResult {
	if err := l.requireOperateMFATwoStep(securitylogic.MFAScenarioResetUserPassword, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.mfaBizResult(err)
	}
	if req.ID == l.GetCtxAdmin().ID {
		return types.NewBizResult(codes.Fail).
			SetI18nMessage(i18n.MsgKeyFail).
			WithError(errors.Errorf("AdminManageLogic.ResetPassword 不允许重置当前登录管理员ID[%d]密码", req.ID))
	}
	if err := l.ensureAdminRoleManageScope(req.ID, nil); err != nil {
		if errors.Is(err, rbaclogic.ErrRoleManageScopeExceeded) {
			return types.Forbidden(i18n.MsgKeyForbidden).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.ResetPassword 校验管理员ID[%d]角色范围失败", req.ID).ToBizResult()
	}

	admin, err := l.GetAdminByID(req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyUserNotFound, err,
				"AdminManageLogic.ResetPassword 管理员ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.ResetPassword 查询管理员ID[%d]失败", req.ID).ToBizResult()
	}

	password, err := bcrypt.GenerateFromPassword([]byte(admin.PasswordWithSalt(strings.TrimSpace(req.Password))), bcrypt.DefaultCost)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"AdminManageLogic.ResetPassword 生成管理员ID[%d]密码哈希失败", req.ID).ToBizResult()
	}

	if err = l.Svc.WriteDB(svc.DatabaseMain).Model(&model.Admin{}).
		Where("id = ?", req.ID).
		Updates(map[string]any{
			"password":            string(password),
			"need_reset_password": 1,
			"updated_at":          time.Now(),
		}).Error; err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.ResetPassword 更新管理员ID[%d]密码失败", req.ID).ToBizResult()
	}

	cachelogic.InvalidateAdminRelationCache(l.BaseLogic, req.ID)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// ResetInitialState 把管理员恢复为首次登录前状态，要求使用临时密码重新登录并强制修改新密码。
func (l *AdminManageLogic) ResetInitialState(req *types.ResetAdminInitialStateReq) *types.BizResult {
	if err := l.requireOperateMFATwoStep(securitylogic.MFAScenarioResetUserInitialState, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.mfaBizResult(err)
	}
	if req.ID == l.GetCtxAdmin().ID {
		return types.NewBizResult(codes.Fail).
			SetI18nMessage(i18n.MsgKeyFail).
			WithError(errors.Errorf("AdminManageLogic.ResetInitialState 不允许重置当前登录管理员ID[%d]首次状态", req.ID))
	}
	if err := l.ensureAdminRoleManageScope(req.ID, nil); err != nil {
		if errors.Is(err, rbaclogic.ErrRoleManageScopeExceeded) {
			return types.Forbidden(i18n.MsgKeyForbidden).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.ResetInitialState 校验管理员ID[%d]角色范围失败", req.ID).ToBizResult()
	}

	admin, err := l.GetAdminByID(req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyUserNotFound, err,
				"AdminManageLogic.ResetInitialState 管理员ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.ResetInitialState 查询管理员ID[%d]失败", req.ID).ToBizResult()
	}

	password, err := bcrypt.GenerateFromPassword([]byte(admin.PasswordWithSalt(strings.TrimSpace(req.Password))), bcrypt.DefaultCost)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"AdminManageLogic.ResetInitialState 生成管理员ID[%d]临时密码哈希失败", req.ID).ToBizResult()
	}

	if err = l.Svc.WriteDB(svc.DatabaseMain).Model(&model.Admin{}).
		Where("id = ?", req.ID).
		Updates(map[string]any{
			"password":            string(password),
			"need_reset_password": 1,
			"mfa_status":          0,
			"mfa_secure_key":      "",
			"last_login_time":     time.Time{},
			"last_login_ip":       "",
			"last_login_ipaddr":   "",
			"updated_at":          time.Now(),
		}).Error; err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.ResetInitialState 重置管理员ID[%d]首次登录状态失败", req.ID).ToBizResult()
	}

	cachelogic.InvalidateAdminRelationCache(l.BaseLogic, req.ID)
	_ = securitylogic.NewSecurityLogic(l.Ctx, l.Svc).ClearLoginMFACompleted(req.ID)
	if err := securitylogic.NewSecurityLogic(l.Ctx, l.Svc).ClearAdminMFATwoStepTickets(req.ID); err != nil {
		corelogic.LogWrappedError(l.Logger, err, "AdminManageLogic.ResetInitialState 清理管理员ID[%d]MFA二次票据失败", req.ID)
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// ListRoles 查询管理员已绑定角色列表。
func (l *AdminManageLogic) ListRoles(req *types.IDPathReq) *types.BizResult {
	if err := l.ensureAdminRoleManageScope(req.ID, nil); err != nil {
		if errors.Is(err, rbaclogic.ErrRoleManageScopeExceeded) {
			return types.Forbidden(i18n.MsgKeyForbidden).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.ListRoles 校验管理员ID[%d]角色范围失败", req.ID).ToBizResult()
	}
	roles, err := l.adminRoleListItems(req.ID)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.ListRoles 查询管理员ID[%d]角色失败", req.ID).ToBizResult()
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(roles)
}

// ReplaceRoles 替换管理员全部角色关系。
func (l *AdminManageLogic) ReplaceRoles(req *types.AdminRoleAssignReq) *types.BizResult {
	if err := l.requireOperateMFATwoStep(securitylogic.MFAScenarioEditUser, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.mfaBizResult(err)
	}
	roleIDs, err := l.pruneInheritedAssignedRoleIDs(types.UniquePositiveInts(req.RoleIDs))
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.ReplaceRoles 归一化管理员ID[%d]角色失败", req.ID).ToBizResult()
	}
	if err := l.ensureAdminRoleManageScope(req.ID, roleIDs); err != nil {
		if errors.Is(err, rbaclogic.ErrRoleManageScopeExceeded) {
			return types.Forbidden(i18n.MsgKeyForbidden).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.ReplaceRoles 校验管理员ID[%d]角色范围失败", req.ID).ToBizResult()
	}
	if err := l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		return l.replaceAdminRolesTx(tx, req.ID, roleIDs)
	}); err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.ReplaceRoles 替换管理员ID[%d]角色失败", req.ID).ToBizResult()
	}

	cachelogic.InvalidateAdminRelationCache(l.BaseLogic, req.ID)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// AddRole 为管理员追加角色关系。
func (l *AdminManageLogic) AddRole(req *types.AdminRoleAssignReq) *types.BizResult {
	if err := l.requireOperateMFATwoStep(securitylogic.MFAScenarioEditUser, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.mfaBizResult(err)
	}
	roleIDs, err := l.pruneInheritedAssignedRoleIDs(types.UniquePositiveInts(req.RoleIDs))
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.AddRole 归一化管理员ID[%d]角色失败", req.ID).ToBizResult()
	}
	if err := l.ensureAdminRoleManageScope(req.ID, roleIDs); err != nil {
		if errors.Is(err, rbaclogic.ErrRoleManageScopeExceeded) {
			return types.Forbidden(i18n.MsgKeyForbidden).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.AddRole 校验管理员ID[%d]角色范围失败", req.ID).ToBizResult()
	}
	if err := l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		if err := l.ensureAdminExistsTx(tx, req.ID); err != nil {
			return errors.Wrapf(err, "AdminManageLogic.AddRole 校验管理员ID[%d]存在失败", req.ID)
		}
		if err := l.ensureRolesUsableTx(tx, roleIDs); err != nil {
			return errors.Wrapf(err, "AdminManageLogic.AddRole 校验管理员ID[%d]角色可用性失败", req.ID)
		}
		rows := make([]model.AdminRoleRel, 0, len(roleIDs))
		now := time.Now()
		for _, roleID := range roleIDs {
			rows = append(rows, model.AdminRoleRel{UserID: req.ID, RoleID: roleID, CreatedAt: now})
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
			return errors.Tag(err)
		}
		return nil
	}); err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.AddRole 追加管理员ID[%d]角色失败", req.ID).ToBizResult()
	}

	cachelogic.InvalidateAdminRelationCache(l.BaseLogic, req.ID)
	return types.NewBizResult(codes.AddSuccess).
		SetI18nMessage(i18n.MsgKeyAddSuccess)
}

// DeleteRole 解除管理员与指定角色的关系。
func (l *AdminManageLogic) DeleteRole(req *types.AdminRoleDeleteReq) *types.BizResult {
	roleID := req.RoleID
	if err := l.ensureAdminRoleManageScope(req.ID, []int{roleID}); err != nil {
		if errors.Is(err, rbaclogic.ErrRoleManageScopeExceeded) {
			return types.Forbidden(i18n.MsgKeyForbidden).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminManageLogic.DeleteRole 校验管理员ID[%d]角色范围失败", req.ID).ToBizResult()
	}
	result := l.Svc.WriteDB(svc.DatabaseMain).Where("user_id = ? AND role_id = ?", req.ID, roleID).Delete(&model.AdminRoleRel{})
	if result.Error != nil {
		return types.DBError(i18n.MsgKeyDBError, result.Error,
			"AdminManageLogic.DeleteRole 解除管理员ID[%d]角色ID[%d]失败", req.ID, roleID).ToBizResult()
	}
	if result.RowsAffected == 0 {
		return types.NotFound(i18n.MsgKeyNotFound, gorm.ErrRecordNotFound,
			"AdminManageLogic.DeleteRole 管理员ID[%d]角色ID[%d]关系不存在", req.ID, roleID).ToBizResult()
	}

	cachelogic.InvalidateAdminRelationCache(l.BaseLogic, req.ID)
	return types.NewBizResult(codes.DeleteSuccess).
		SetI18nMessage(i18n.MsgKeyDeleteSuccess)
}

// pruneInheritedAssignedRoleIDs 过滤已被父角色覆盖的子角色，保证后台绑定关系始终收敛到最小角色集合。
func (l *AdminManageLogic) pruneInheritedAssignedRoleIDs(roleIDs []int) ([]int, error) {
	roleIDs = types.UniquePositiveInts(roleIDs)
	if len(roleIDs) <= 1 {
		return roleIDs, nil
	}
	type roleRow struct {
		ID   int
		Pids string
	}
	rows := make([]roleRow, 0, len(roleIDs))
	if err := l.Svc.ReadDB(svc.DatabaseMain).Model(&model.AdminRole{}).
		Select("id, pids").
		Where("id IN ? AND is_delete = 0", roleIDs).
		Order("id ASC").
		Scan(&rows).Error; err != nil {
		return nil, errors.Wrap(err, "AdminManageLogic.pruneInheritedAssignedRoleIDs 查询角色族谱失败")
	}
	selectedSet := make(map[int]struct{}, len(roleIDs))
	for _, roleID := range roleIDs {
		selectedSet[roleID] = struct{}{}
	}
	result := make([]int, 0, len(rows))
	for _, row := range rows {
		coveredByParent := false
		for parentRoleID := range selectedSet {
			if parentRoleID == row.ID {
				continue
			}
			if corelogic.ContainsTreeID(row.Pids, parentRoleID) {
				coveredByParent = true
				break
			}
		}
		if !coveredByParent {
			result = append(result, row.ID)
		}
	}
	return types.UniquePositiveInts(result), nil
}

// ensureAdminRoleManageScope 校验目标管理员当前角色与本次提交角色都在当前操作者可管理范围内。
func (l *AdminManageLogic) ensureAdminRoleManageScope(adminID int, nextRoleIDs []int) error {
	roleLogic := &rbaclogic.AdminRoleLogic{BaseLogic: l.BaseLogic}
	currentRoleIDs, err := roleLogic.UserRoleIDs(adminID)
	if err != nil {
		return errors.Wrapf(err, "AdminManageLogic.ensureAdminRoleManageScope 查询管理员ID[%d]当前角色失败", adminID)
	}
	scopeRoleIDs := append(types.UniquePositiveInts(currentRoleIDs), types.UniquePositiveInts(nextRoleIDs)...)
	if err := roleLogic.EnsureRolesWithinManageScope(scopeRoleIDs); err != nil {
		return errors.Wrapf(err, "AdminManageLogic.ensureAdminRoleManageScope 管理员ID[%d]角色超出可管理范围", adminID)
	}
	return nil
}

// buildAdminUpdates 根据请求和原模型构建管理员字段更新集合。
func buildAdminUpdates(req *types.UpdateAdminReq, old *model.Admin) map[string]any {
	updates := make(map[string]any)
	addString := func(field string, val *string, current string) {
		if val == nil {
			return
		}
		next := strings.TrimSpace(*val)
		if next != current {
			updates[field] = next
		}
	}
	addString("real_name", req.RealName, old.RealName)
	addString("email", req.Email, old.Email)
	addString("phone", req.Phone, old.Phone)
	addString("avatar", req.Avatar, old.Avatar)
	addString("description", req.Description, old.Description)
	if len(updates) > 0 {
		updates["updated_at"] = time.Now()
	}
	return updates
}

// adminModelToItem 把管理员模型转换成列表响应项。
func adminModelToItem(admin model.Admin, roleIDs []int, roles []types.AdminRoleItem) types.AdminItem {
	return types.AdminItem{
		ID:                admin.ID,
		Username:          admin.Name,
		RealName:          admin.RealName,
		NeedResetPassword: admin.NeedResetPassword,
		Email:             admin.Email,
		Phone:             admin.Phone,
		MfaStatus:         admin.MfaStatus,
		Status:            admin.Status,
		Avatar:            admin.Avatar,
		Description:       admin.Description,
		LastLoginTime:     corelogic.FormatDateTime(admin.LastLoginTime),
		LastLoginIP:       admin.LastLoginIP,
		LastLoginIpaddr:   admin.LastLoginIpaddr,
		RoleIDs:           roleIDs,
		Roles:             roles,
		CreatedAt:         corelogic.FormatDateTime(admin.CreatedAt),
		UpdatedAt:         corelogic.FormatDateTime(admin.UpdatedAt),
	}
}

// adminRoleMap 批量查询管理员角色映射。
func (l *AdminManageLogic) adminRoleMap(admins []model.Admin) (map[int][]types.AdminRoleItem, error) {
	adminIDs := make([]int, 0, len(admins))
	for _, admin := range admins {
		adminIDs = append(adminIDs, admin.ID)
	}
	roleMap := make(map[int][]types.AdminRoleItem, len(adminIDs))
	if len(adminIDs) == 0 {
		return roleMap, nil
	}

	type row struct {
		UserID      int
		RoleID      int
		Title       string
		Status      int
		Description string
	}
	rows := make([]row, 0)
	err := l.Svc.ReadDB(svc.DatabaseMain).Table(model.TableNameAdminRoleRel+" arr").
		Select("arr.user_id, r.id AS role_id, r.title, r.status, r.`describe` AS description").
		Joins("JOIN "+model.TableNameAdminRole+" r ON r.id = arr.role_id").
		Where("arr.user_id IN ?", adminIDs).
		Order("arr.user_id ASC, arr.role_id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, errors.Wrap(err, "AdminManageLogic.adminRoleMap 查询管理员角色映射失败")
	}
	for _, item := range rows {
		roleMap[item.UserID] = append(roleMap[item.UserID], types.AdminRoleItem{
			ID:          item.RoleID,
			Title:       item.Title,
			Status:      item.Status,
			Description: item.Description,
		})
	}
	return roleMap, nil
}

// adminRoles 查询管理员已绑定角色。
func (l *AdminManageLogic) adminRoles(adminID int) ([]types.AdminRoleItem, error) {
	roleMap, err := l.adminRoleMap([]model.Admin{{ID: adminID}})
	if err != nil {
		return nil, errors.Wrapf(err, "AdminManageLogic.adminRoles 查询管理员ID[%d]角色失败", adminID)
	}
	return roleMap[adminID], nil
}

// adminRoleListItems 查询管理员角色列表，包含关系创建时间。
func (l *AdminManageLogic) adminRoleListItems(adminID int) ([]types.AdminRoleListItem, error) {
	type row struct {
		RoleID      int
		Title       string
		Status      int
		Description string
		CreatedAt   time.Time
	}
	rows := make([]row, 0)
	err := l.Svc.ReadDB(svc.DatabaseMain).Table(model.TableNameAdminRoleRel+" arr").
		Select("arr.role_id, r.title, r.status, r.`describe` AS description, arr.created_at").
		Joins("JOIN "+model.TableNameAdminRole+" r ON r.id = arr.role_id").
		Where("arr.user_id = ?", adminID).
		Order("arr.role_id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, errors.Wrapf(err, "AdminManageLogic.adminRoleListItems 查询管理员ID[%d]角色列表失败", adminID)
	}
	items := make([]types.AdminRoleListItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, types.AdminRoleListItem{
			ID:          row.RoleID,
			Title:       row.Title,
			Status:      row.Status,
			Description: row.Description,
			CreatedAt:   corelogic.FormatDateTime(row.CreatedAt),
		})
	}
	return items, nil
}

// replaceAdminRolesTx 在事务内替换管理员角色关系。
func (l *AdminManageLogic) replaceAdminRolesTx(tx *gorm.DB, adminID int, roleIDs []int) error {
	var err error
	roleIDs, err = l.pruneInheritedAssignedRoleIDs(roleIDs)
	if err != nil {
		return errors.Wrapf(err, "AdminManageLogic.replaceAdminRolesTx 归一化管理员ID[%d]角色失败", adminID)
	}
	if err := l.ensureAdminExistsTx(tx, adminID); err != nil {
		return errors.Wrapf(err, "AdminManageLogic.replaceAdminRolesTx 校验管理员ID[%d]存在失败", adminID)
	}
	if err := l.ensureRolesUsableTx(tx, roleIDs); err != nil {
		return errors.Wrapf(err, "AdminManageLogic.replaceAdminRolesTx 校验管理员ID[%d]角色可用性失败", adminID)
	}
	if err := tx.Where("user_id = ?", adminID).Delete(&model.AdminRoleRel{}).Error; err != nil {
		return errors.Wrap(err, "清理管理员原角色关系失败")
	}
	if len(roleIDs) == 0 {
		return nil
	}
	rows := make([]model.AdminRoleRel, 0, len(roleIDs))
	now := time.Now()
	for _, roleID := range roleIDs {
		rows = append(rows, model.AdminRoleRel{UserID: adminID, RoleID: roleID, CreatedAt: now})
	}
	if err := tx.Create(&rows).Error; err != nil {
		return errors.Wrap(err, "写入管理员新角色关系失败")
	}
	return nil
}

// ensureAdminExistsTx 确认管理员存在，避免关系表写入孤儿 user_id。
func (l *AdminManageLogic) ensureAdminExistsTx(tx *gorm.DB, adminID int) error {
	var count int64
	if err := tx.Model(&model.Admin{}).Where("id = ?", adminID).Count(&count).Error; err != nil {
		return errors.Wrap(err, "检查管理员是否存在失败")
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ensureRolesUsableTx 确认角色均存在且可用。
func (l *AdminManageLogic) ensureRolesUsableTx(tx *gorm.DB, roleIDs []int) error {
	roleIDs = types.UniquePositiveInts(roleIDs)
	if len(roleIDs) == 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&model.AdminRole{}).
		Where("id IN ? AND status = 1 AND is_delete = 0", roleIDs).
		Count(&count).Error; err != nil {
		return errors.Wrap(err, "检查角色是否可用失败")
	}
	if int(count) != len(roleIDs) {
		return errors.Errorf("存在不可用角色: %v", roleIDs)
	}
	return nil
}
