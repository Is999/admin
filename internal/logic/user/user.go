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
		WithData(types.ListResp[types.UserItem]{
			List:  items,
			Total: total,
			Meta: types.UserListMeta{
				ExactTotal:            true,
				StatusFilterSupported: true,
			},
		})
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
	dbq := db.Model(&model.UserIdentity{}).Table(tableName)
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
	orderDirection := corelogic.NormalizedOrderDirection(req.Order)
	if req.CursorID > 0 {
		if orderDirection == "asc" {
			dbq = dbq.Where("user_id > ?", req.CursorID)
		} else {
			dbq = dbq.Where("user_id < ?", req.CursorID)
		}
	}
	var identities []model.UserIdentity
	if err := dbq.Order("user_id " + orderDirection).Limit(req.PageSize + 1).Find(&identities).Error; err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.List 查询前台用户身份索引失败").ToBizResult()
	}
	hasMore := len(identities) > req.PageSize
	if hasMore {
		identities = identities[:req.PageSize]
	}
	for index := range identities {
		identities[index].IdentityType = identityType
	}
	users, err := model.FindUsersByIdentityRows(db, identities)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.List 批量读取前台用户失败").ToBizResult()
	}
	items := make([]types.UserItem, 0, len(identities))
	for index := range identities {
		identity := identities[index]
		row := users[identity.UserID]
		if req.Username != "" && !strings.HasPrefix(strings.ToLower(row.Username), strings.ToLower(req.Username)) {
			continue
		}
		items = append(items, userModelToItem(row))
	}
	nextCursorID := int64(0)
	if hasMore && len(identities) > 0 {
		nextCursorID = identities[len(identities)-1].UserID
	}
	// 分表阶段用“当前已浏览行数 + 下一页占位”驱动分页控件，不执行高成本实时 COUNT。
	total := int64((req.Page-1)*req.PageSize + len(items))
	if hasMore {
		total++
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.ListResp[types.UserItem]{
			List:  items,
			Total: total,
			Meta: types.UserListMeta{
				ExactTotal:            false,
				HasMore:               hasMore,
				NextCursorID:          nextCursorID,
				StatusFilterSupported: false,
			},
		})
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
	row, err := l.getUserForMutation(req.ID)
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
		row, err = l.getUserForMutation(req.ID)
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
		syncResp, err = l.syncUserRuntime(req.ID, true, false, 0, "admin_update_user_profile")
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
	row, err := l.getUserForMutation(req.ID)
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
		db, err := l.userWriteDB()
		if err != nil {
			return l.userDBError("UserLogic.UpdateStatus 前台用户库未配置", err)
		}
		now := time.Now()
		authVersion, err := model.UpdateUserStatusAndAuthVersion(db, req.ID, status, now)
		if err != nil {
			return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.UpdateStatus 修改前台用户 ID[%d]状态失败", req.ID).ToBizResult()
		}
		row.Status = status
		row.AuthVersion = authVersion
		row.UpdatedAt = now
		reason := "admin_update_user_status"
		if status == model.UserStatusDisabled {
			reason = "admin_disable_user"
		}
		syncResp, err = l.syncUserRuntime(req.ID, true, true, authVersion, reason)
		if err != nil {
			syncResp = l.apiRuntimeSyncWarning(req.ID, syncResp, i18n.MsgKeyAPIRuntimeStatusSyncWarning, err)
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
	row, err := l.getUserForMutation(req.ID)
	if err != nil {
		return userFindResult("UserLogic.ResetPassword", req.ID, err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(strings.TrimSpace(req.Password)), bcrypt.DefaultCost)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalError, err, "UserLogic.ResetPassword 生成前台用户 ID[%d]密码哈希失败", req.ID).ToBizResult()
	}
	db, err := l.userWriteDB()
	if err != nil {
		return l.userDBError("UserLogic.ResetPassword 前台用户库未配置", err)
	}
	now := time.Now()
	authVersion, err := model.UpdateUserPasswordAndAuthVersion(db, req.ID, string(passwordHash), now)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.ResetPassword 更新前台用户 ID[%d]密码失败", req.ID).ToBizResult()
	}
	row.AuthVersion = authVersion
	row.UpdatedAt = now
	syncResp, err := l.syncUserRuntime(req.ID, false, true, authVersion, "admin_reset_user_password")
	if err != nil {
		syncResp = l.apiRuntimeSyncWarning(req.ID, syncResp, i18n.MsgKeyAPIRuntimeSessionSyncWarning, err)
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess).
		WithData(types.UserMutationResp{Item: ptrUserItem(userModelToItem(*row)), Sync: syncResp})
}

// SyncRuntime 手动同步前台用户 API 运行态。
func (l *Logic) SyncRuntime(req *types.SyncUserRuntimeReq) *types.BizResult {
	if err := l.RequireOperateMFATwoStep(securitylogic.MFAScenarioUserManage, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.MFABizResult(err)
	}
	row, err := l.getUserForMutation(req.ID)
	if err != nil {
		return userFindResult("UserLogic.SyncRuntime", req.ID, err)
	}
	authVersion := uint64(0)
	if req.Sessions {
		db, err := l.userWriteDB()
		if err != nil {
			return l.userDBError("UserLogic.SyncRuntime 前台用户库未配置", err)
		}
		authVersion, err = model.BumpUserAuthVersion(db, req.ID, time.Now())
		if err != nil {
			return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.SyncRuntime 递增前台用户 ID[%d]认证版本失败", req.ID).ToBizResult()
		}
		row.AuthVersion = authVersion
	}
	syncResp, err := l.syncUserRuntime(req.ID, req.Profile, req.Sessions, authVersion, "admin_manual_user_runtime_sync")
	if err != nil {
		syncResp = l.apiRuntimeSyncWarning(req.ID, syncResp, i18n.MsgKeyAPIRuntimeSessionSyncWarning, err)
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

// getUserForMutation 从主库读取待修改用户，避免副本延迟跳过敏感更新或返回旧资料。
func (l *Logic) getUserForMutation(id int64) (*model.User, error) {
	db, err := l.userWriteDB()
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
func (l *Logic) syncUserRuntime(userID int64, profile bool, sessions bool, authVersion uint64, reason string) (types.UserRuntimeSyncResp, error) {
	client, err := l.runtimeClient()
	if err != nil {
		return types.UserRuntimeSyncResp{Enabled: false, Success: false, UserID: userID, AuthVersion: authVersion, Message: l.Message(i18n.MsgKeyAPIRuntimeNotConfigured)}, errors.Tag(err)
	}
	resp, err := client.SyncUserRuntime(l.Ctx, userID, profile, sessions, authVersion, reason)
	if err != nil {
		return types.UserRuntimeSyncResp{Enabled: true, Success: false, UserID: userID, AuthVersion: authVersion}, errors.Tag(err)
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

// validateUserIdentityListReq 校验分表阶段身份索引列表支持的过滤和排序边界。
func validateUserIdentityListReq(req *types.UserListReq) error {
	if req == nil {
		return errors.New("用户列表请求为空")
	}
	if req.Page > 1 && req.CursorID <= 0 {
		return errors.New("用户分表阶段翻页必须携带上一页返回的游标")
	}
	if strings.TrimSpace(req.Email) != "" && strings.TrimSpace(req.Phone) != "" {
		return errors.New("用户分表阶段列表不支持同时按邮箱和手机号查询")
	}
	if req.Status != nil {
		return errors.New("用户分表阶段列表不支持按状态筛选，避免扫描所有用户分表")
	}
	switch strings.TrimSpace(req.OrderBy) {
	case "", "id":
		return nil
	default:
		return errors.New("用户分表阶段列表仅支持按 id 排序")
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

// apiRuntimeSyncWarning 把写库后同步失败转换为可重试的运行态提示，避免误报数据库写入失败。
func (l *Logic) apiRuntimeSyncWarning(userID int64, resp types.UserRuntimeSyncResp, fallbackKey string, err error) types.UserRuntimeSyncResp {
	corelogic.LogWrappedError(l.Logger, err, "UserLogic API 运行态同步失败 user_id=%d", userID)
	resp.Success = false
	resp.UserID = userID
	if resp.Message == "" {
		resp.Message = l.Message(fallbackKey)
	}
	return resp
}
