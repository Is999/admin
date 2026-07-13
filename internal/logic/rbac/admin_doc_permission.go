package rbac

import (
	"net/http"
	"strings"
	"time"

	"admin/common/codes"
	i18n "admin/common/i18n"
	keys "admin/common/rediskeys"
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	tablecache "github.com/Is999/table-cache"
	"gorm.io/gorm"
)

// AdminDocPermissionLogic 处理文档权限定义查询与全局启停。
type AdminDocPermissionLogic struct {
	*corelogic.BaseLogic // 复用上下文、数据库和缓存能力
}

// NewAdminDocPermissionLogic 创建文档权限业务逻辑对象。
func NewAdminDocPermissionLogic(r *http.Request, svcCtx *svc.ServiceContext) *AdminDocPermissionLogic {
	return &AdminDocPermissionLogic{
		BaseLogic: corelogic.NewBaseLogic(r, svcCtx),
	}
}

// List 从共享文档权限缓存筛选并分页，确保管理页与角色授权、文档鉴权读取同一份定义。
func (l *AdminDocPermissionLogic) List(req *types.DocPermissionListReq) *types.BizResult {
	permissions, err := loadDocPermissionsWithCache(l.BaseLogic)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminDocPermissionLogic.List 查询文档权限缓存失败").ToBizResult()
	}
	items := make([]types.AdminDocPermissionListItem, 0, len(permissions))
	for _, permission := range permissions {
		if !matchDocPermissionList(permission, req) {
			continue
		}
		items = append(items, docPermissionModelToListItem(permission))
	}
	total := int64(len(items))
	start := (req.Page - 1) * req.PageSize
	if start >= len(items) {
		items = []types.AdminDocPermissionListItem{}
	} else {
		end := min(start+req.PageSize, len(items))
		items = items[start:end]
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.ListResp[types.AdminDocPermissionListItem]{List: items, Total: total})
}

// UpdateStatus 修改文档权限全局状态，并用 RBAC 写锁串行化角色授权与定义变更。
func (l *AdminDocPermissionLogic) UpdateStatus(req *types.DocPermissionStatusReq) *types.BizResult {
	return WithRBACWriteLock(l.BaseLogic, "AdminDocPermissionLogic.UpdateStatus", func(lockedBaseLogic *corelogic.BaseLogic) *types.BizResult {
		return (&AdminDocPermissionLogic{BaseLogic: lockedBaseLogic}).updateStatus(req)
	})
}

// updateStatus 在 RBAC 写锁内修改文档权限状态。
func (l *AdminDocPermissionLogic) updateStatus(req *types.DocPermissionStatusReq) *types.BizResult {
	var permission model.AdminDocPermission
	db := l.Svc.WriteDB(svc.DatabaseMain)
	if err := db.Where("id = ?", req.ID).First(&permission).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyNotFound, err,
				"AdminDocPermissionLogic.UpdateStatus 文档权限ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminDocPermissionLogic.UpdateStatus 查询文档权限ID[%d]失败", req.ID).ToBizResult()
	}
	status := req.StatusValue()
	if permission.Status == status {
		return types.NewBizResult(codes.UpdateSuccess).
			SetI18nMessage(i18n.MsgKeyStatusChangeOK)
	}
	if err := db.Model(&model.AdminDocPermission{}).
		Where("id = ?", req.ID).
		Updates(map[string]any{"status": status, "updated_at": time.Now()}).Error; err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminDocPermissionLogic.UpdateStatus 修改文档权限ID[%d]状态失败", req.ID).ToBizResult()
	}
	if err := l.refreshDocPermissionCache(); err != nil {
		return corelogic.CacheSyncPendingResult(l.Logger, codes.UpdateSuccess, i18n.MsgKeyAdminCacheInvalidationPending, err,
			"AdminDocPermissionLogic.UpdateStatus 文档权限缓存同步失败")
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyStatusChangeOK)
}

// loadDocPermissionsWithCache 优先读取全部文档权限缓存，未命中时自动回源主库。
func loadDocPermissionsWithCache(baseLogic *corelogic.BaseLogic) ([]model.AdminDocPermission, error) {
	manager, err := cachelogic.TableCacheManager(baseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	var permissions []model.AdminDocPermission
	result, err := manager.LoadThrough(
		baseLogic.Ctx,
		cachelogic.TableCachePhysicalKey(baseLogic, keys.DocPermissionList),
		&permissions,
		nil,
	)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if result.State == tablecache.LookupStateEmpty {
		return []model.AdminDocPermission{}, nil
	}
	return permissions, nil
}

// matchDocPermissionList 判断文档权限是否命中管理列表筛选条件。
func matchDocPermissionList(permission model.AdminDocPermission, req *types.DocPermissionListReq) bool {
	if req.Site != "" && permission.Site != req.Site {
		return false
	}
	if req.Status != nil && permission.Status != *req.Status {
		return false
	}
	if req.Title != "" && !strings.Contains(strings.ToLower(permission.Title), strings.ToLower(req.Title)) {
		return false
	}
	return req.Path == "" || strings.Contains(strings.ToLower(permission.Path), strings.ToLower(req.Path))
}

// docPermissionModelToListItem 把文档权限模型转换为管理列表项。
func docPermissionModelToListItem(permission model.AdminDocPermission) types.AdminDocPermissionListItem {
	return types.AdminDocPermissionListItem{
		ID:          permission.ID,
		Site:        permission.Site,
		Path:        permission.Path,
		Title:       permission.Title,
		Description: permission.Description,
		Status:      permission.Status,
		CreatedAt:   corelogic.FormatDateTime(permission.CreatedAt),
		UpdatedAt:   corelogic.FormatDateTime(permission.UpdatedAt),
	}
}

// refreshDocPermissionCache 失效文档定义快照和启用资源反向索引。
func (l *AdminDocPermissionLogic) refreshDocPermissionCache() error {
	return cachelogic.DeleteTableCacheKeysExact(
		l.BaseLogic,
		"AdminDocPermissionLogic.refreshDocPermissionCache 删除文档权限缓存",
		cachelogic.TableCachePhysicalKeys(l.BaseLogic, keys.DocPermissionList, keys.DocResourcePermissionID),
	)
}
