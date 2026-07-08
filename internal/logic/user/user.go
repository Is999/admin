package user

import (
	"context"
	"net/http"
	"strings"
	"time"

	"admin/common/idgen"
	"github.com/Is999/go-utils/errors"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"admin/common/codes"
	i18n "admin/common/i18n"
	corelogic "admin/internal/logic"
	adminlogic "admin/internal/logic/admin"
	apiruntime "admin/internal/logic/apiruntime"
	securitylogic "admin/internal/logic/security"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"
)

const (
	// userDatabase 表示 user 业务用户表固定使用后台默认主库。
	userDatabase svc.DBName = svc.DatabaseMain
	// userIDNamespace 表示 api/admin 写同一用户表必须使用的业务命名空间。
	userIDNamespace = "user"
)

// Logic 承载后台直连管理业务用户表的业务逻辑。
type Logic struct {
	*adminlogic.AdminLogic // 复用后台登录态、MFA 和审计公共能力
}

// NewLogic 创建前台用户管理逻辑对象。
func NewLogic(r *http.Request, svcCtx *svc.ServiceContext) *Logic {
	return &Logic{AdminLogic: adminlogic.NewAdminLogic(r, svcCtx)}
}

// NewLogicWithContext 创建绑定任意上下文的前台用户管理逻辑对象。
func NewLogicWithContext(ctx context.Context, svcCtx *svc.ServiceContext) *Logic {
	return &Logic{AdminLogic: &adminlogic.AdminLogic{BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx)}}
}

// List 分页查询前台用户列表。
func (l *Logic) List(req *types.UserListReq) *types.BizResult {
	db, err := l.userReadDB()
	if err != nil {
		return l.userDBError("UserLogic.List 前台用户库未配置", err)
	}
	useIdentityList, err := l.useUserIdentityList(db)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.List 判断业务用户分表状态失败").ToBizResult()
	}
	if useIdentityList {
		return l.listByUserIdentity(db, req)
	}
	dbq := db.Model(&model.User{})
	if req.ID > 0 {
		dbq = dbq.Where("id = ?", req.ID)
	}
	if req.ShardNo != nil {
		dbq = dbq.Where("shard_no = ?", *req.ShardNo)
	}
	if req.Username != "" {
		dbq = dbq.Where("username LIKE ?", req.Username+"%")
	}
	if req.Email != "" {
		emailHash, err := model.UserContactIdentityHash(model.UserIdentityTypeEmail, req.Email, l.Svc.CurrentConfig().AppKey)
		if err != nil {
			return types.ParamErrorResult(err).WithError(err)
		}
		dbq = dbq.Where("email_hash = ?", emailHash)
	}
	if req.Phone != "" {
		phoneHash, err := model.UserContactIdentityHash(model.UserIdentityTypePhone, req.Phone, l.Svc.CurrentConfig().AppKey)
		if err != nil {
			return types.ParamErrorResult(err).WithError(err)
		}
		dbq = dbq.Where("phone_hash = ?", phoneHash)
	}
	if req.Status != nil {
		dbq = dbq.Where("status = ?", *req.Status)
	}

	list, total, err := model.List[model.User](dbq, req.Page, req.PageSize, userOrderField(req.OrderBy), corelogic.NormalizedOrderDirection(req.Order))
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.List 查询前台用户列表失败").ToBizResult()
	}
	items := make([]types.UserItem, 0, len(list))
	for _, row := range list {
		items = append(items, userModelToItem(row))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.ListResp[types.UserItem]{List: items, Total: total})
}

// listByUserIdentity 在物理分表阶段通过身份索引定位用户，避免扫描所有分表。
func (l *Logic) listByUserIdentity(db *gorm.DB, req *types.UserListReq) *types.BizResult {
	if err := validateUserIdentityListReq(req); err != nil {
		return types.ParamErrorResult(err).WithError(err)
	}
	identityType := model.UserIdentityTypeUsername
	identityHash := ""
	var err error
	if req.Email != "" {
		identityType = model.UserIdentityTypeEmail
		identityHash, err = model.UserContactIdentityHash(identityType, req.Email, l.Svc.CurrentConfig().AppKey)
		if err != nil {
			return types.ParamErrorResult(err).WithError(err)
		}
	} else if req.Phone != "" {
		identityType = model.UserIdentityTypePhone
		identityHash, err = model.UserContactIdentityHash(identityType, req.Phone, l.Svc.CurrentConfig().AppKey)
		if err != nil {
			return types.ParamErrorResult(err).WithError(err)
		}
	}
	tableName, err := model.UserIdentityTableName(identityType)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.List 获取用户身份索引表失败").ToBizResult()
	}
	dbq := db.Model(&model.UserIdentity{}).Table(userIdentityListTableName(tableName, identityType, req.Username))
	if req.ID > 0 {
		dbq = dbq.Where("user_id = ?", req.ID)
	}
	if req.ShardNo != nil {
		dbq = dbq.Where("user_shard_no = ?", *req.ShardNo)
	}
	if identityHash != "" {
		dbq = dbq.Where("identity_hash = ?", identityHash)
	} else if req.Username != "" {
		dbq = dbq.Where("identity_value LIKE ?", strings.ToLower(req.Username)+"%")
	}
	orderField := userIdentityOrderField(req.OrderBy, identityType)
	orderDirection := corelogic.NormalizedOrderDirection(req.Order)
	var identities []model.UserIdentity
	if err := dbq.Order(orderField + " " + orderDirection).Limit(req.PageSize).Find(&identities).Error; err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.List 查询前台用户身份索引失败").ToBizResult()
	}
	items := make([]types.UserItem, 0, len(identities))
	for index := range identities {
		identities[index].IdentityType = identityType
		identity := identities[index]
		row, err := model.FindUserByIdentityRow(db, &identity)
		if err != nil {
			return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.List 读取前台用户 ID[%d]失败", identity.UserID).ToBizResult()
		}
		if req.Username != "" && !strings.HasPrefix(strings.ToLower(row.Username), strings.ToLower(req.Username)) {
			continue
		}
		items = append(items, userModelToItem(*row))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.ListResp[types.UserItem]{List: items, Total: int64(len(items)), Meta: types.UserListMeta{ExactTotal: false}})
}

// Get 查询前台用户详情。
func (l *Logic) Get(req *types.UserIDReq) *types.BizResult {
	row, err := l.getUser(req.ID)
	if err != nil {
		return userFindResult("UserLogic.Get", req.ID, err)
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(userModelToItem(*row))
}

// Create 新增前台用户。
func (l *Logic) Create(req *types.CreateUserReq) *types.BizResult {
	if err := l.RequireOperateMFATwoStep(securitylogic.MFAScenarioUserManage, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.MFABizResult(err)
	}
	db, err := l.userWriteDB()
	if err != nil {
		return l.userDBError("UserLogic.Create 前台用户库未配置", err)
	}
	privacySecret := l.Svc.CurrentConfig().AppKey
	exists, err := model.FindUserIdentity(db, model.UserIdentityTypeUsername, model.UserIdentityProviderLocal, req.Username, privacySecret)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.Create 查询前台用户身份[%s]失败", req.Username).ToBizResult()
	}
	if exists != nil {
		return userAlreadyExistsResult(req.Username, errors.New("前台用户身份已存在"))
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(strings.TrimSpace(req.Password)), bcrypt.DefaultCost)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalError, err, "UserLogic.Create 生成前台用户密码哈希失败").ToBizResult()
	}
	userID, err := idgen.NextID(userIDNamespace)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalError, err, "UserLogic.Create 生成前台用户 ID失败").ToBizResult()
	}

	now := time.Now()
	status := model.UserStatusEnabled
	if req.Status != nil {
		status = *req.Status
	}
	row := &model.User{
		ID:           userID,
		ShardNo:      idgen.ShardNo(userID),
		Username:     strings.TrimSpace(req.Username),
		Nickname:     strings.TrimSpace(req.Nickname),
		PasswordHash: string(passwordHash),
		Email:        strings.TrimSpace(req.Email),
		Phone:        strings.TrimSpace(req.Phone),
		Avatar:       strings.TrimSpace(req.Avatar),
		Status:       status,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if row.Nickname == "" {
		row.Nickname = row.Username
	}
	routeShardCount := l.Svc.CurrentConfig().User.RouteShardCount
	if err = model.CreateUserWithIdentities(db, row, routeShardCount, privacySecret, "last_login_at"); err != nil {
		if corelogic.IsMySQLDuplicateEntryError(err) {
			return userAlreadyExistsResult(req.Username, err)
		}
		return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.Create 创建前台用户[%s]失败", req.Username).ToBizResult()
	}
	return types.NewBizResult(codes.AddSuccess).
		SetI18nMessage(i18n.MsgKeyAddSuccess).
		WithData(types.UserMutationResp{
			Item: ptrUserItem(userModelToItem(*row)),
			Sync: types.UserRuntimeSyncResp{
				Enabled: apiruntime.Configured(l.Svc.CurrentConfig().APIService),
				Success: true,
				UserID:  row.ID,
				Message: l.Message(i18n.MsgKeyAPIRuntimeUserCreateNoCache),
			},
		})
}

// Update 编辑前台用户资料，并同步失效 API 资料缓存。
func (l *Logic) Update(req *types.UpdateUserReq) *types.BizResult {
	if err := l.RequireOperateMFATwoStep(securitylogic.MFAScenarioUserManage, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.MFABizResult(err)
	}
	row, err := l.getUser(req.ID)
	if err != nil {
		return userFindResult("UserLogic.Update", req.ID, err)
	}
	updates := buildUserProfileUpdates(req)
	if _, err = l.runtimeClient(); err != nil && len(updates) > 0 {
		return apiRuntimeRequiredResult("UserLogic.Update API 运行态同步未配置", err)
	}
	if len(updates) > 0 {
		db, err := l.userWriteDB()
		if err != nil {
			return l.userDBError("UserLogic.Update 前台用户库未配置", err)
		}
		updates["updated_at"] = time.Now()
		if err = model.UpdateUserProfileWithIdentities(db, req.ID, updates, l.Svc.CurrentConfig().AppKey); err != nil {
			return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.Update 更新前台用户 ID[%d]失败", req.ID).ToBizResult()
		}
		row, err = l.getUser(req.ID)
		if err != nil {
			return userFindResult("UserLogic.Update.reload", req.ID, err)
		}
	}
	syncResp := types.UserRuntimeSyncResp{
		Enabled: apiruntime.Configured(l.Svc.CurrentConfig().APIService),
		Success: true,
		UserID:  req.ID,
		Message: l.Message(i18n.MsgKeyAPIRuntimeProfileUnchanged),
	}
	if len(updates) > 0 {
		syncResp, err = l.syncUserRuntime(req.ID, true, false, "admin_update_user_profile")
		if err != nil {
			syncResp = l.apiRuntimeSyncWarning(req.ID, syncResp, i18n.MsgKeyAPIRuntimeProfileSyncWarning, err)
		}
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess).
		WithData(types.UserMutationResp{Item: ptrUserItem(userModelToItem(*row)), Sync: syncResp})
}

// UpdateStatus 修改前台用户状态，并在禁用时失效 API 登录态。
func (l *Logic) UpdateStatus(req *types.UserStatusReq) *types.BizResult {
	if err := l.RequireOperateMFATwoStep(securitylogic.MFAScenarioUserManage, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.MFABizResult(err)
	}
	row, err := l.getUser(req.ID)
	if err != nil {
		return userFindResult("UserLogic.UpdateStatus", req.ID, err)
	}
	status := *req.Status
	syncResp := types.UserRuntimeSyncResp{
		Enabled: apiruntime.Configured(l.Svc.CurrentConfig().APIService),
		Success: true,
		UserID:  req.ID,
		Message: l.Message(i18n.MsgKeyAPIRuntimeStatusUnchanged),
	}
	if row.Status != status {
		if _, err := l.runtimeClient(); err != nil {
			return apiRuntimeRequiredResult("UserLogic.UpdateStatus API 运行态同步未配置", err)
		}
		if status == model.UserStatusDisabled {
			syncResp, err = l.syncUserRuntime(req.ID, true, true, "admin_disable_user")
			if err != nil {
				return apiRuntimeSyncFailedResult("UserLogic.UpdateStatus 禁用前同步前台用户登录态失败", err)
			}
		}
		db, err := l.userWriteDB()
		if err != nil {
			return l.userDBError("UserLogic.UpdateStatus 前台用户库未配置", err)
		}
		now := time.Now()
		if err = model.UpdateUser(db, req.ID, map[string]any{
			"status":     status,
			"updated_at": now,
		}); err != nil {
			return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.UpdateStatus 修改前台用户 ID[%d]状态失败", req.ID).ToBizResult()
		}
		row.Status = status
		row.UpdatedAt = now
		if status != model.UserStatusDisabled {
			syncResp, err = l.syncUserRuntime(req.ID, true, false, "admin_update_user_status")
			if err != nil {
				syncResp = l.apiRuntimeSyncWarning(req.ID, syncResp, i18n.MsgKeyAPIRuntimeStatusSyncWarning, err)
			}
		}
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyStatusChangeOK).
		WithData(types.UserMutationResp{Item: ptrUserItem(userModelToItem(*row)), Sync: syncResp})
}

// ResetPassword 重置前台用户密码，并失效 API 登录态。
func (l *Logic) ResetPassword(req *types.ResetUserPasswordReq) *types.BizResult {
	if err := l.RequireOperateMFATwoStep(securitylogic.MFAScenarioUserManage, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.MFABizResult(err)
	}
	row, err := l.getUser(req.ID)
	if err != nil {
		return userFindResult("UserLogic.ResetPassword", req.ID, err)
	}
	if _, err := l.runtimeClient(); err != nil {
		return apiRuntimeRequiredResult("UserLogic.ResetPassword API 运行态同步未配置", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(strings.TrimSpace(req.Password)), bcrypt.DefaultCost)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalError, err, "UserLogic.ResetPassword 生成前台用户 ID[%d]密码哈希失败", req.ID).ToBizResult()
	}
	syncResp, err := l.syncUserRuntime(req.ID, false, true, "admin_reset_user_password")
	if err != nil {
		return apiRuntimeSyncFailedResult("UserLogic.ResetPassword 更新密码前同步前台用户登录态失败", err)
	}
	db, err := l.userWriteDB()
	if err != nil {
		return l.userDBError("UserLogic.ResetPassword 前台用户库未配置", err)
	}
	now := time.Now()
	if err = model.UpdateUserPasswordHash(db, req.ID, string(passwordHash), now); err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.ResetPassword 更新前台用户 ID[%d]密码失败", req.ID).ToBizResult()
	}
	row.UpdatedAt = now
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess).
		WithData(types.UserMutationResp{Item: ptrUserItem(userModelToItem(*row)), Sync: syncResp})
}

// SyncRuntime 手动同步前台用户 API 运行态。
func (l *Logic) SyncRuntime(req *types.SyncUserRuntimeReq) *types.BizResult {
	if err := l.RequireOperateMFATwoStep(securitylogic.MFAScenarioUserManage, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.MFABizResult(err)
	}
	if _, err := l.getUser(req.ID); err != nil {
		return userFindResult("UserLogic.SyncRuntime", req.ID, err)
	}
	syncResp, err := l.syncUserRuntime(req.ID, req.Profile, req.Sessions, "admin_manual_user_runtime_sync")
	if err != nil {
		return apiRuntimeSyncFailedResult("UserLogic.SyncRuntime 手动同步前台用户运行态失败", err)
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess).
		WithData(syncResp)
}

// userReadDB 返回前台用户只读库连接。
func (l *Logic) userReadDB() (*gorm.DB, error) {
	if l == nil || l.Svc == nil {
		return nil, errors.New("ServiceContext 未初始化")
	}
	db := l.Svc.ReadDB(userDatabase)
	if db == nil {
		return nil, errors.New("默认主库未配置")
	}
	return db, nil
}

// userWriteDB 返回前台用户写库连接。
func (l *Logic) userWriteDB() (*gorm.DB, error) {
	if l == nil || l.Svc == nil {
		return nil, errors.New("ServiceContext 未初始化")
	}
	db := l.Svc.WriteDB(userDatabase)
	if db == nil {
		return nil, errors.New("默认主库未配置")
	}
	return db, nil
}

// getUser 按 ID 查询前台用户。
func (l *Logic) getUser(id int64) (*model.User, error) {
	db, err := l.userReadDB()
	if err != nil {
		return nil, errors.Tag(err)
	}
	row, err := model.FindUserByID(db, id)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if row == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return row, nil
}

// runtimeClient 返回 API 内网运行态客户端。
func (l *Logic) runtimeClient() (*apiruntime.Client, error) {
	if l == nil || l.Svc == nil {
		return nil, errors.New("ServiceContext 未初始化")
	}
	return apiruntime.NewClient(l.Svc.CurrentConfig().APIService)
}

// syncUserRuntime 调用 API 失效资料缓存或登录态。
func (l *Logic) syncUserRuntime(userID int64, profile bool, sessions bool, reason string) (types.UserRuntimeSyncResp, error) {
	client, err := l.runtimeClient()
	if err != nil {
		return types.UserRuntimeSyncResp{Enabled: false, Success: false, UserID: userID, Message: l.Message(i18n.MsgKeyAPIRuntimeNotConfigured)}, errors.Tag(err)
	}
	resp, err := client.SyncUserRuntime(l.Ctx, userID, profile, sessions, reason)
	if err != nil {
		return types.UserRuntimeSyncResp{Enabled: true, Success: false, UserID: userID, Message: err.Error()}, errors.Tag(err)
	}
	return *resp, nil
}

// userDBError 返回业务用户库配置或访问错误。
func (l *Logic) userDBError(context string, err error) *types.BizResult {
	return types.DBError(i18n.MsgKeyDBError, err, context).ToBizResult()
}

// userOrderField 把前端排序字段映射到业务用户表字段。
func userOrderField(orderBy string) string {
	switch strings.TrimSpace(orderBy) {
	case "username":
		return "username"
	case "shardNo":
		return "shard_no"
	case "email":
		return "email_hash"
	case "phone":
		return "phone_hash"
	case "status":
		return "status"
	case "lastLoginAt":
		return "last_login_at"
	case "createdAt":
		return "created_at"
	case "updatedAt":
		return "updated_at"
	default:
		return "id"
	}
}

// userIdentityOrderField 把前端排序字段映射到身份索引表字段。
func userIdentityOrderField(orderBy string, identityType string) string {
	switch strings.TrimSpace(orderBy) {
	case "username":
		if identityType != model.UserIdentityTypeUsername {
			return "user_id"
		}
		return "identity_value"
	case "shardNo":
		return "user_shard_no"
	default:
		return "user_id"
	}
}

// userIdentityListTableName 返回分表列表身份表名；用户名左前缀搜索固定提示唯一索引，避免大表路径退化全扫。
func userIdentityListTableName(tableName string, identityType string, username string) string {
	if identityType == model.UserIdentityTypeUsername && strings.TrimSpace(username) != "" {
		return tableName + " FORCE INDEX (uk_user_identity_value)"
	}
	return tableName
}

// validateUserIdentityListReq 校验分表阶段身份索引列表支持的过滤和排序边界。
func validateUserIdentityListReq(req *types.UserListReq) error {
	if req == nil {
		return errors.New("用户列表请求为空")
	}
	if req.Page > 1 {
		return errors.New("用户分表阶段列表不支持深分页，请使用 ID、用户名、邮箱或手机号缩小查询范围")
	}
	if strings.TrimSpace(req.Email) != "" && strings.TrimSpace(req.Phone) != "" {
		return errors.New("用户分表阶段列表不支持同时按邮箱和手机号查询")
	}
	if req.Status != nil {
		return errors.New("用户分表阶段列表不支持按状态筛选，避免扫描所有用户分表")
	}
	switch strings.TrimSpace(req.OrderBy) {
	case "", "id", "username", "shardNo":
		return nil
	default:
		return errors.New("用户分表阶段列表仅支持按 id、username、shardNo 排序")
	}
}

// useUserIdentityList 判断当前请求是否应使用身份索引驱动的分表列表路径。
func (l *Logic) useUserIdentityList(db *gorm.DB) (bool, error) {
	if l == nil || l.Svc == nil {
		return false, nil
	}
	if l.Svc.CurrentConfig().User.RouteShardCount > model.UserRouteShardCountDefault {
		return true, nil
	}
	return model.HasSplitUserIdentities(db)
}

// buildUserProfileUpdates 生成前台用户资料更新字段。
func buildUserProfileUpdates(req *types.UpdateUserReq) map[string]any {
	updates := make(map[string]any, 4)
	if req.Nickname != nil {
		updates["nickname"] = strings.TrimSpace(*req.Nickname)
	}
	if req.Email != nil {
		updates["email"] = strings.TrimSpace(*req.Email)
	}
	if req.Phone != nil {
		updates["phone"] = strings.TrimSpace(*req.Phone)
	}
	if req.Avatar != nil {
		updates["avatar"] = strings.TrimSpace(*req.Avatar)
	}
	return updates
}

// userModelToItem 转换前台用户模型为接口展示项。
func userModelToItem(row model.User) types.UserItem {
	return types.UserItem{
		ID:          row.ID,
		ShardNo:     row.ShardNo,
		Username:    row.Username,
		Nickname:    row.Nickname,
		EmailMasked: row.EmailMasked,
		PhoneMasked: row.PhoneMasked,
		Avatar:      row.Avatar,
		Status:      row.Status,
		LastLoginAt: corelogic.FormatDateTime(row.LastLoginAt),
		LastLoginIP: row.LastLoginIP,
		CreatedAt:   corelogic.FormatDateTime(row.CreatedAt),
		UpdatedAt:   corelogic.FormatDateTime(row.UpdatedAt),
	}
}

// ptrUserItem 返回前台用户项指针。
func ptrUserItem(item types.UserItem) *types.UserItem {
	return &item
}

// userFindResult 统一转换前台用户查找失败响应。
func userFindResult(context string, id int64, err error) *types.BizResult {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return types.NotFound(i18n.MsgKeyUserNotFound, errors.Wrapf(err, "%s 前台用户 ID[%d]不存在", context, id)).ToBizResult()
	}
	return types.DBError(i18n.MsgKeyDBError, errors.Wrapf(err, "%s 查询前台用户 ID[%d]失败", context, id)).ToBizResult()
}

// userAlreadyExistsResult 返回前台用户名重复的明确业务响应。
func userAlreadyExistsResult(username string, cause error) *types.BizResult {
	return types.NewBizResult(codes.UserAlreadyExists).
		SetI18nMessage(i18n.MsgKeyUserExistsFormat, strings.TrimSpace(username)).
		WithError(errors.Tag(cause))
}

// apiRuntimeRequiredResult 返回运行态同步配置缺失导致的失败响应。
func apiRuntimeRequiredResult(context string, err error) *types.BizResult {
	return types.NewBizResult(codes.ServiceBusy).
		SetI18nMessage(i18n.MsgKeyFail).
		WithError(errors.Wrap(err, context))
}

// apiRuntimeSyncFailedResult 返回运行态同步失败响应。
func apiRuntimeSyncFailedResult(context string, err error) *types.BizResult {
	return types.ServerError(i18n.MsgKeyInternalError, err, context).ToBizResult()
}

// apiRuntimeSyncWarning 把写库后同步失败转换为可重试的运行态提示，避免误报数据库写入失败。
func (l *Logic) apiRuntimeSyncWarning(userID int64, resp types.UserRuntimeSyncResp, fallbackKey string, err error) types.UserRuntimeSyncResp {
	resp.Success = false
	resp.UserID = userID
	if resp.Message == "" {
		resp.Message = l.Message(fallbackKey)
	}
	if err != nil && !strings.Contains(resp.Message, err.Error()) {
		resp.Message = resp.Message + "：" + err.Error()
	}
	return resp
}
