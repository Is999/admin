package apiuser

import (
	"net/http"
	"strings"
	"time"

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
	// defaultAPIUserDatabase 表示未显式配置时前台用户表所在的命名库。
	defaultAPIUserDatabase = "api"
)

// Logic 承载后台直连管理前台用户表的业务逻辑。
type Logic struct {
	*adminlogic.AdminLogic // 复用后台登录态、MFA 和审计公共能力
}

// NewLogic 创建前台用户管理逻辑对象。
func NewLogic(r *http.Request, svcCtx *svc.ServiceContext) *Logic {
	return &Logic{AdminLogic: adminlogic.NewAdminLogic(r, svcCtx)}
}

// List 分页查询前台用户列表。
func (l *Logic) List(req *types.APIUserListReq) *types.BizResult {
	db, err := l.apiUserReadDB()
	if err != nil {
		return l.apiUserDBError("APIUserLogic.List 前台用户库未配置", err)
	}
	dbq := db.Model(&model.APIUser{})
	if req.ID > 0 {
		dbq = dbq.Where("id = ?", req.ID)
	}
	if req.Username != "" {
		dbq = dbq.Where("username LIKE ?", req.Username+"%")
	}
	if req.Email != "" {
		dbq = dbq.Where("email LIKE ?", req.Email+"%")
	}
	if req.Phone != "" {
		dbq = dbq.Where("phone LIKE ?", req.Phone+"%")
	}
	if req.Status != nil {
		dbq = dbq.Where("status = ?", *req.Status)
	}

	list, total, err := model.List[model.APIUser](dbq, req.Page, req.PageSize, apiUserOrderField(req.OrderBy), corelogic.NormalizedOrderDirection(req.Order))
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "APIUserLogic.List 查询前台用户列表失败").ToBizResult()
	}
	items := make([]types.APIUserItem, 0, len(list))
	for _, row := range list {
		items = append(items, apiUserModelToItem(row))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.ListResp[types.APIUserItem]{List: items, Total: total})
}

// Get 查询前台用户详情。
func (l *Logic) Get(req *types.APIUserIDReq) *types.BizResult {
	row, err := l.getAPIUser(req.ID)
	if err != nil {
		return apiUserFindResult("APIUserLogic.Get", req.ID, err)
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(apiUserModelToItem(*row))
}

// Create 新增前台用户。
func (l *Logic) Create(req *types.CreateAPIUserReq) *types.BizResult {
	if err := l.RequireOperateMFATwoStep(securitylogic.MFAScenarioAPIUserManage, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.MFABizResult(err)
	}
	db, err := l.apiUserWriteDB()
	if err != nil {
		return l.apiUserDBError("APIUserLogic.Create 前台用户库未配置", err)
	}
	exists, err := model.FindAPIUserByUsername(db, req.Username)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "APIUserLogic.Create 查询前台用户名[%s]失败", req.Username).ToBizResult()
	}
	if exists != nil {
		return apiUserAlreadyExistsResult(req.Username, errors.New("前台用户名已存在"))
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(strings.TrimSpace(req.Password)), bcrypt.DefaultCost)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalError, err, "APIUserLogic.Create 生成前台用户密码哈希失败").ToBizResult()
	}

	now := time.Now()
	status := model.APIUserStatusEnabled
	if req.Status != nil {
		status = *req.Status
	}
	row := &model.APIUser{
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
	if err = db.Omit("last_login_at").Create(row).Error; err != nil {
		if corelogic.IsMySQLDuplicateEntryError(err) {
			return apiUserAlreadyExistsResult(req.Username, err)
		}
		return types.DBError(i18n.MsgKeyDBError, err, "APIUserLogic.Create 创建前台用户[%s]失败", req.Username).ToBizResult()
	}
	return types.NewBizResult(codes.AddSuccess).
		SetI18nMessage(i18n.MsgKeyAddSuccess).
		WithData(types.APIUserMutationResp{
			Item: ptrAPIUserItem(apiUserModelToItem(*row)),
			Sync: types.APIUserRuntimeSyncResp{
				Enabled: apiruntime.Configured(l.Svc.CurrentConfig().APIService),
				Success: true,
				UserID:  row.ID,
				Message: "新增用户无既有运行态缓存",
			},
		})
}

// Update 编辑前台用户资料，并同步失效 API 资料缓存。
func (l *Logic) Update(req *types.UpdateAPIUserReq) *types.BizResult {
	if err := l.RequireOperateMFATwoStep(securitylogic.MFAScenarioAPIUserManage, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.MFABizResult(err)
	}
	row, err := l.getAPIUser(req.ID)
	if err != nil {
		return apiUserFindResult("APIUserLogic.Update", req.ID, err)
	}
	updates := buildAPIUserProfileUpdates(req)
	if _, err = l.runtimeClient(); err != nil && len(updates) > 0 {
		return apiRuntimeRequiredResult("APIUserLogic.Update API 运行态同步未配置", err)
	}
	if len(updates) > 0 {
		db, err := l.apiUserWriteDB()
		if err != nil {
			return l.apiUserDBError("APIUserLogic.Update 前台用户库未配置", err)
		}
		updates["updated_at"] = time.Now()
		if err = db.Model(&model.APIUser{}).Where("id = ?", req.ID).Updates(updates).Error; err != nil {
			return types.DBError(i18n.MsgKeyDBError, err, "APIUserLogic.Update 更新前台用户ID[%d]失败", req.ID).ToBizResult()
		}
		row, err = l.getAPIUser(req.ID)
		if err != nil {
			return apiUserFindResult("APIUserLogic.Update.reload", req.ID, err)
		}
	}
	syncResp := types.APIUserRuntimeSyncResp{
		Enabled: apiruntime.Configured(l.Svc.CurrentConfig().APIService),
		Success: true,
		UserID:  req.ID,
		Message: "资料未变更，无需同步 API 运行态",
	}
	if len(updates) > 0 {
		syncResp, err = l.syncAPIUserRuntime(req.ID, true, false, "admin_update_api_user_profile")
		if err != nil {
			syncResp = apiRuntimeSyncWarning(req.ID, syncResp, "资料已更新，API 资料缓存同步失败，请手动重试", err)
		}
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess).
		WithData(types.APIUserMutationResp{Item: ptrAPIUserItem(apiUserModelToItem(*row)), Sync: syncResp})
}

// UpdateStatus 修改前台用户状态，并在禁用时失效 API 登录态。
func (l *Logic) UpdateStatus(req *types.APIUserStatusReq) *types.BizResult {
	if err := l.RequireOperateMFATwoStep(securitylogic.MFAScenarioAPIUserManage, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.MFABizResult(err)
	}
	row, err := l.getAPIUser(req.ID)
	if err != nil {
		return apiUserFindResult("APIUserLogic.UpdateStatus", req.ID, err)
	}
	status := *req.Status
	syncResp := types.APIUserRuntimeSyncResp{
		Enabled: apiruntime.Configured(l.Svc.CurrentConfig().APIService),
		Success: true,
		UserID:  req.ID,
		Message: "状态未变更，无需同步 API 运行态",
	}
	if row.Status != status {
		if _, err := l.runtimeClient(); err != nil {
			return apiRuntimeRequiredResult("APIUserLogic.UpdateStatus API 运行态同步未配置", err)
		}
		if status == model.APIUserStatusDisabled {
			syncResp, err = l.syncAPIUserRuntime(req.ID, true, true, "admin_disable_api_user")
			if err != nil {
				return apiRuntimeSyncFailedResult("APIUserLogic.UpdateStatus 禁用前同步前台用户登录态失败", err)
			}
		}
		db, err := l.apiUserWriteDB()
		if err != nil {
			return l.apiUserDBError("APIUserLogic.UpdateStatus 前台用户库未配置", err)
		}
		now := time.Now()
		if err = db.Model(&model.APIUser{}).Where("id = ?", req.ID).Updates(map[string]any{
			"status":     status,
			"updated_at": now,
		}).Error; err != nil {
			return types.DBError(i18n.MsgKeyDBError, err, "APIUserLogic.UpdateStatus 修改前台用户ID[%d]状态失败", req.ID).ToBizResult()
		}
		row.Status = status
		row.UpdatedAt = now
		if status != model.APIUserStatusDisabled {
			syncResp, err = l.syncAPIUserRuntime(req.ID, true, false, "admin_update_api_user_status")
			if err != nil {
				syncResp = apiRuntimeSyncWarning(req.ID, syncResp, "状态已更新，API 资料缓存同步失败，请手动重试", err)
			}
		}
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyStatusChangeOK).
		WithData(types.APIUserMutationResp{Item: ptrAPIUserItem(apiUserModelToItem(*row)), Sync: syncResp})
}

// ResetPassword 重置前台用户密码，并失效 API 登录态。
func (l *Logic) ResetPassword(req *types.ResetAPIUserPasswordReq) *types.BizResult {
	if err := l.RequireOperateMFATwoStep(securitylogic.MFAScenarioAPIUserManage, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.MFABizResult(err)
	}
	row, err := l.getAPIUser(req.ID)
	if err != nil {
		return apiUserFindResult("APIUserLogic.ResetPassword", req.ID, err)
	}
	if _, err := l.runtimeClient(); err != nil {
		return apiRuntimeRequiredResult("APIUserLogic.ResetPassword API 运行态同步未配置", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(strings.TrimSpace(req.Password)), bcrypt.DefaultCost)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalError, err, "APIUserLogic.ResetPassword 生成前台用户ID[%d]密码哈希失败", req.ID).ToBizResult()
	}
	syncResp, err := l.syncAPIUserRuntime(req.ID, false, true, "admin_reset_api_user_password")
	if err != nil {
		return apiRuntimeSyncFailedResult("APIUserLogic.ResetPassword 更新密码前同步前台用户登录态失败", err)
	}
	db, err := l.apiUserWriteDB()
	if err != nil {
		return l.apiUserDBError("APIUserLogic.ResetPassword 前台用户库未配置", err)
	}
	now := time.Now()
	if err = db.Model(&model.APIUser{}).Where("id = ?", req.ID).Updates(map[string]any{
		"password_hash": string(passwordHash),
		"updated_at":    now,
	}).Error; err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "APIUserLogic.ResetPassword 更新前台用户ID[%d]密码失败", req.ID).ToBizResult()
	}
	row.UpdatedAt = now
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess).
		WithData(types.APIUserMutationResp{Item: ptrAPIUserItem(apiUserModelToItem(*row)), Sync: syncResp})
}

// SyncRuntime 手动同步前台用户 API 运行态。
func (l *Logic) SyncRuntime(req *types.SyncAPIUserRuntimeReq) *types.BizResult {
	if err := l.RequireOperateMFATwoStep(securitylogic.MFAScenarioAPIUserManage, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.MFABizResult(err)
	}
	if _, err := l.getAPIUser(req.ID); err != nil {
		return apiUserFindResult("APIUserLogic.SyncRuntime", req.ID, err)
	}
	syncResp, err := l.syncAPIUserRuntime(req.ID, req.Profile, req.Sessions, "admin_manual_api_user_runtime_sync")
	if err != nil {
		return apiRuntimeSyncFailedResult("APIUserLogic.SyncRuntime 手动同步前台用户运行态失败", err)
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess).
		WithData(syncResp)
}

// apiUserReadDB 返回前台用户只读库连接。
func (l *Logic) apiUserReadDB() (*gorm.DB, error) {
	if l == nil || l.Svc == nil {
		return nil, errors.New("ServiceContext 未初始化")
	}
	db := l.Svc.ReadDB(apiUserDatabase(l.Svc.CurrentConfig().APIService.UserDatabase))
	if db == nil {
		return nil, errors.Errorf("site_mysql.%s 未配置", apiUserDatabase(l.Svc.CurrentConfig().APIService.UserDatabase))
	}
	return db, nil
}

// apiUserWriteDB 返回前台用户写库连接。
func (l *Logic) apiUserWriteDB() (*gorm.DB, error) {
	if l == nil || l.Svc == nil {
		return nil, errors.New("ServiceContext 未初始化")
	}
	db := l.Svc.WriteDB(apiUserDatabase(l.Svc.CurrentConfig().APIService.UserDatabase))
	if db == nil {
		return nil, errors.Errorf("site_mysql.%s 未配置", apiUserDatabase(l.Svc.CurrentConfig().APIService.UserDatabase))
	}
	return db, nil
}

// getAPIUser 按 ID 查询前台用户。
func (l *Logic) getAPIUser(id int64) (*model.APIUser, error) {
	db, err := l.apiUserReadDB()
	if err != nil {
		return nil, errors.Tag(err)
	}
	row, err := model.FindAPIUserByID(db, id)
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

// syncAPIUserRuntime 调用 API 失效资料缓存或登录态。
func (l *Logic) syncAPIUserRuntime(userID int64, profile bool, sessions bool, reason string) (types.APIUserRuntimeSyncResp, error) {
	client, err := l.runtimeClient()
	if err != nil {
		return types.APIUserRuntimeSyncResp{Enabled: false, Success: false, UserID: userID, Message: err.Error()}, errors.Tag(err)
	}
	resp, err := client.SyncUserRuntime(l.Ctx, userID, profile, sessions, reason)
	if err != nil {
		return types.APIUserRuntimeSyncResp{Enabled: true, Success: false, UserID: userID, Message: err.Error()}, errors.Tag(err)
	}
	return *resp, nil
}

// apiUserDBError 返回前台用户库配置或访问错误。
func (l *Logic) apiUserDBError(context string, err error) *types.BizResult {
	return types.DBError(i18n.MsgKeyDBError, err, context).ToBizResult()
}

// apiUserDatabase 解析前台用户表所在数据库名称；空配置默认使用命名库 api。
func apiUserDatabase(raw string) svc.DbName {
	name := strings.TrimSpace(raw)
	if name == "" {
		name = defaultAPIUserDatabase
	}
	return svc.NormalizeDbName(svc.DbName(name))
}

// apiUserOrderField 把前端排序字段映射到前台用户表字段。
func apiUserOrderField(orderBy string) string {
	switch strings.TrimSpace(orderBy) {
	case "username":
		return "username"
	case "email":
		return "email"
	case "phone":
		return "phone"
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

// buildAPIUserProfileUpdates 生成前台用户资料更新字段。
func buildAPIUserProfileUpdates(req *types.UpdateAPIUserReq) map[string]any {
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

// apiUserModelToItem 转换前台用户模型为接口展示项。
func apiUserModelToItem(row model.APIUser) types.APIUserItem {
	return types.APIUserItem{
		ID:          row.ID,
		Username:    row.Username,
		Nickname:    row.Nickname,
		Email:       row.Email,
		Phone:       row.Phone,
		Avatar:      row.Avatar,
		Status:      row.Status,
		LastLoginAt: corelogic.FormatDateTime(row.LastLoginAt),
		LastLoginIP: row.LastLoginIP,
		CreatedAt:   corelogic.FormatDateTime(row.CreatedAt),
		UpdatedAt:   corelogic.FormatDateTime(row.UpdatedAt),
	}
}

// ptrAPIUserItem 返回前台用户项指针。
func ptrAPIUserItem(item types.APIUserItem) *types.APIUserItem {
	return &item
}

// apiUserFindResult 统一转换前台用户查找失败响应。
func apiUserFindResult(context string, id int64, err error) *types.BizResult {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return types.NotFound(i18n.MsgKeyUserNotFound, errors.Wrapf(err, "%s 前台用户ID[%d]不存在", context, id)).ToBizResult()
	}
	return types.DBError(i18n.MsgKeyDBError, errors.Wrapf(err, "%s 查询前台用户ID[%d]失败", context, id)).ToBizResult()
}

// apiUserAlreadyExistsResult 返回前台用户名重复的明确业务响应。
func apiUserAlreadyExistsResult(username string, cause error) *types.BizResult {
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
func apiRuntimeSyncWarning(userID int64, resp types.APIUserRuntimeSyncResp, fallback string, err error) types.APIUserRuntimeSyncResp {
	resp.Success = false
	resp.UserID = userID
	if resp.Message == "" {
		resp.Message = fallback
	}
	if err != nil && !strings.Contains(resp.Message, err.Error()) {
		resp.Message = resp.Message + "：" + err.Error()
	}
	return resp
}
