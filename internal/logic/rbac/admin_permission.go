package rbac

import (
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"admin/helper"

	"github.com/Is999/go-utils/errors"
	tablecache "github.com/Is999/table-cache"

	"admin/common/codes"
	i18n "admin/common/i18n"
	keys "admin/common/rediskeys"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"
	"net/http"

	"gorm.io/gorm"
)

// AdminPermissionLogic 处理路由权限定义、树结构和共享缓存。
type AdminPermissionLogic struct {
	*corelogic.BaseLogic // 复用上下文、数据库和日志能力
}

// NewAdminPermissionLogic 创建权限业务逻辑对象。
func NewAdminPermissionLogic(r *http.Request, svcCtx *svc.ServiceContext) *AdminPermissionLogic {
	return &AdminPermissionLogic{
		BaseLogic: corelogic.NewBaseLogic(r, svcCtx),
	}
}

// List 分页查询权限列表，支持按 UUID、名称、模块、类型和父级筛选。
func (l *AdminPermissionLogic) List(req *types.PermissionListReq) *types.BizResult {
	// 权限管理列表保持有界分页，避免一次读取整张权限表。
	dbq := l.Svc.ReadDB(svc.DatabaseMain).Model(&model.AdminPermission{})
	if req.UUID != "" {
		dbq = dbq.Where("uuid = ?", req.UUID)
	}
	if req.Title != "" {
		dbq = dbq.Where("title LIKE ?", "%"+req.Title+"%")
	}
	if req.Module != "" {
		dbq = dbq.Where("module LIKE ?", "%"+req.Module+"%")
	}
	if len(req.Types) > 0 {
		dbq = dbq.Where("type IN ?", req.Types)
	}
	if req.Status != nil {
		dbq = dbq.Where("status = ?", *req.Status)
	}
	if req.Pid != nil {
		if req.IsGenealogy > 0 {
			// 权限层级筛选统一走 `FIND_IN_SET`，避免祖先链模糊匹配拖慢列表查询。
			dbq = corelogic.ApplyGenealogyScopeFilter(dbq, "pids", *req.Pid)
		} else {
			dbq = dbq.Where("pid = ?", *req.Pid)
		}
	}

	orderBy := corelogic.NormalizedOrderField(req.OrderBy, "id")
	list, total, err := model.List[model.AdminPermission](dbq, req.Page, req.PageSize, orderBy, corelogic.NormalizedOrderDirection(req.Order))
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.List 查询权限列表失败").ToBizResult()
	}

	items := make([]types.AdminPermissionItem, 0, len(list))
	for _, permission := range list {
		items = append(items, permissionModelToItem(permission, false, false, nil))
	}
	permissionTree, err := l.LoadPermissionTreeWithCache()
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.List 查询权限树缓存失败").ToBizResult()
	}
	manageablePermissionSet, err := l.manageablePermissionItemIDSet(permissionTree)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.List 计算权限可操作范围失败").ToBizResult()
	}
	items = markPermissionTreeManageScope(items, manageablePermissionSet)
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.ListResp[types.AdminPermissionItem]{List: items, Total: total})
}

// TreeList 查询权限树，供权限选择和角色授权使用。
func (l *AdminPermissionLogic) TreeList() *types.BizResult {
	items, err := l.LoadPermissionTreeWithCache()
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.TreeList 查询权限树失败").ToBizResult()
	}
	manageablePermissionSet, err := l.manageablePermissionItemIDSet(items)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.TreeList 计算权限可操作范围失败").ToBizResult()
	}
	items = markPermissionTreeManageScope(items, manageablePermissionSet)
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(items)
}

// Create 新增权限节点。
func (l *AdminPermissionLogic) Create(req *types.CreatePermissionReq) *types.BizResult {
	return l.withPermissionWriteLock("AdminPermissionLogic.Create", func() *types.BizResult {
		return l.create(req)
	})
}

// create 在 RBAC 写锁内新增权限节点。
func (l *AdminPermissionLogic) create(req *types.CreatePermissionReq) *types.BizResult {
	if req.UUID == "" {
		nextUUID, err := l.nextPermissionUUID()
		if err != nil {
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminPermissionLogic.Create 生成权限UUID失败").ToBizResult()
		}
		req.UUID = nextUUID
	}
	if err := l.ensurePermissionParentWithinManageScope(req.Pid); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(err, "AdminPermissionLogic.Create 父级权限ID[%d]超出可操作范围", req.Pid))
	}
	disabledPermissionID, err := l.disabledPermissionPathID(req.Pid)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.Create 校验父级权限ID[%d]状态失败", req.Pid).ToBizResult()
	}
	if disabledPermissionID > 0 {
		return types.NewBizResult(codes.Fail).
			SetI18nMessage(i18n.MsgKeyStatusChangeFail).
			WithError(errors.Errorf("AdminPermissionLogic.Create 父级路径包含禁用权限ID[%d]", disabledPermissionID))
	}
	permission := model.AdminPermission{
		UUID:        req.UUID,
		Title:       req.Title,
		Module:      req.Module,
		Pid:         req.Pid,
		Type:        req.TypeValue(),
		Description: req.Description,
		Status:      corelogic.IntPtrDefault(req.Status, 1),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		pids, err := l.permissionPidsTx(tx, req.Pid, 0)
		if err != nil {
			return errors.Tag(err)
		}
		permission.Pids = pids
		if err := l.ensurePermissionUUIDUniqueTx(tx, req.UUID, 0); err != nil {
			return errors.Tag(err)
		}
		if err := tx.Create(&permission).Error; err != nil {
			return errors.Wrap(err, "创建权限失败")
		}
		return nil
	}); err != nil {
		if errors.Is(err, ErrPermissionUUIDAlreadyExists) || corelogic.IsMySQLDuplicateEntryError(err) {
			return PermissionUUIDAlreadyExistsResult(req.UUID, err)
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.Create 创建权限[%s]失败", req.Title).ToBizResult()
	}

	if err := l.refreshPermissionRelatedCache(); err != nil {
		return corelogic.CacheSyncPendingResult(l.Logger, codes.AddSuccess, i18n.MsgKeyAdminCacheInvalidationPending, err,
			"AdminPermissionLogic.Create RBAC缓存同步失败")
	}
	return types.NewBizResult(codes.AddSuccess).
		SetI18nMessage(i18n.MsgKeyAddSuccess)
}

// Update 编辑权限节点。
func (l *AdminPermissionLogic) Update(req *types.UpdatePermissionReq) *types.BizResult {
	return l.withPermissionWriteLock("AdminPermissionLogic.Update", func() *types.BizResult {
		return l.update(req)
	})
}

// update 在 RBAC 写锁内编辑权限节点。
func (l *AdminPermissionLogic) update(req *types.UpdatePermissionReq) *types.BizResult {
	var permission model.AdminPermission
	if err := l.Svc.WriteDB(svc.DatabaseMain).Where("id = ?", req.ID).First(&permission).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyNotFound, err,
				"AdminPermissionLogic.Update 权限ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.Update 查询权限ID[%d]失败", req.ID).ToBizResult()
	}

	nextPid := req.ParentID()
	pidChanged := permissionParentChanged(permission.Pid, nextPid)
	nextUUID := permission.UUID
	if req.UUID != "" {
		nextUUID = req.UUID
	}
	if err := l.ensurePermissionsWithinManageScope([]int{req.ID}); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(err, "AdminPermissionLogic.Update 权限ID[%d]超出可操作范围", req.ID))
	}
	// 仅在父级真实变化时校验目标父级范围。
	// 已有顶级目录保持 pid=0 编辑时，不按创建顶级权限处理。
	if pidChanged {
		if err := l.ensurePermissionParentWithinManageScope(nextPid); err != nil {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "AdminPermissionLogic.Update 父级权限ID[%d]超出可操作范围", nextPid))
		}
		disabledPermissionID, err := l.disabledPermissionPathID(nextPid)
		if err != nil {
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminPermissionLogic.Update 校验父级权限ID[%d]状态失败", nextPid).ToBizResult()
		}
		if disabledPermissionID > 0 {
			return types.NewBizResult(codes.Fail).
				SetI18nMessage(i18n.MsgKeyStatusChangeFail).
				WithError(errors.Errorf("AdminPermissionLogic.Update 父级路径包含禁用权限ID[%d]", disabledPermissionID))
		}
	}
	if err := l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		pids, err := l.permissionPidsTx(tx, nextPid, req.ID)
		if err != nil {
			return errors.Tag(err)
		}
		if err := l.ensurePermissionUUIDUniqueTx(tx, nextUUID, req.ID); err != nil {
			return errors.Tag(err)
		}
		if err := tx.Model(&model.AdminPermission{}).Where("id = ?", req.ID).Updates(map[string]any{
			"uuid":        nextUUID,
			"title":       req.Title,
			"module":      *req.Module,
			"pid":         nextPid,
			"pids":        pids,
			"type":        req.TypeValue(),
			"description": *req.Description,
			"updated_at":  time.Now(),
		}).Error; err != nil {
			return errors.Wrap(err, "更新权限失败")
		}
		if pidChanged {
			if err := l.syncPermissionDescendantPidsTx(tx, req.ID); err != nil {
				return errors.Tag(err)
			}
		}
		return nil
	}); err != nil {
		if errors.Is(err, ErrPermissionUUIDAlreadyExists) || corelogic.IsMySQLDuplicateEntryError(err) {
			return PermissionUUIDAlreadyExistsResult(nextUUID, err)
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.Update 更新权限ID[%d]失败", req.ID).ToBizResult()
	}

	if err := l.refreshPermissionRelatedCache(); err != nil {
		return corelogic.CacheSyncPendingResult(l.Logger, codes.UpdateSuccess, i18n.MsgKeyAdminCacheInvalidationPending, err,
			"AdminPermissionLogic.Update RBAC缓存同步失败")
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// UpdateStatus 修改权限启用/禁用状态；禁用时同步禁用全部子孙权限。
func (l *AdminPermissionLogic) UpdateStatus(req *types.PermissionStatusReq) *types.BizResult {
	return l.withPermissionWriteLock("AdminPermissionLogic.UpdateStatus", func() *types.BizResult {
		return l.updateStatus(req)
	})
}

// updateStatus 在 RBAC 写锁内修改权限状态。
func (l *AdminPermissionLogic) updateStatus(req *types.PermissionStatusReq) *types.BizResult {
	var permission model.AdminPermission
	if err := l.Svc.WriteDB(svc.DatabaseMain).
		Select("id, pid").
		Where("id = ?", req.ID).
		First(&permission).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyNotFound, err,
				"AdminPermissionLogic.UpdateStatus 权限ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.UpdateStatus 查询权限ID[%d]失败", req.ID).ToBizResult()
	}
	if err := l.ensurePermissionsWithinManageScope([]int{req.ID}); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(err, "AdminPermissionLogic.UpdateStatus 权限ID[%d]超出可操作范围", req.ID))
	}
	status := req.StatusValue()
	if status == 1 {
		disabledPermissionID, err := l.disabledPermissionPathID(permission.Pid)
		if err != nil {
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminPermissionLogic.UpdateStatus 校验权限ID[%d]父级状态失败", req.ID).ToBizResult()
		}
		if disabledPermissionID > 0 {
			return types.NewBizResult(codes.Fail).
				SetI18nMessage(i18n.MsgKeyStatusChangeFail).
				WithError(errors.Errorf("AdminPermissionLogic.UpdateStatus 父级路径包含禁用权限ID[%d]", disabledPermissionID))
		}
	}
	permissionIDs := []int{req.ID}
	if status == 0 {
		subtreeIDs, err := l.permissionSubtreeIDs(req.ID)
		if err != nil {
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminPermissionLogic.UpdateStatus 查询权限ID[%d]子树失败", req.ID).ToBizResult()
		}
		permissionIDs = subtreeIDs
		if err := l.ensurePermissionsWithinManageScope(permissionIDs); err != nil {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "AdminPermissionLogic.UpdateStatus 权限ID[%d]级联权限超出可操作范围", req.ID))
		}
	}
	result := l.Svc.WriteDB(svc.DatabaseMain).Model(&model.AdminPermission{}).
		Where("id IN ?", permissionIDs).
		Updates(map[string]any{"status": status, "updated_at": time.Now()})
	if result.Error != nil {
		return types.DBError(i18n.MsgKeyDBError, result.Error,
			"AdminPermissionLogic.UpdateStatus 修改权限ID[%d]状态失败", req.ID).ToBizResult()
	}
	if err := l.refreshPermissionRelatedCache(); err != nil {
		return corelogic.CacheSyncPendingResult(l.Logger, codes.UpdateSuccess, i18n.MsgKeyAdminCacheInvalidationPending, err,
			"AdminPermissionLogic.UpdateStatus RBAC缓存同步失败")
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyStatusChangeOK)
}

// Delete 删除权限节点；删除时级联删除全部子孙权限，并清理角色权限关系。
func (l *AdminPermissionLogic) Delete(req *types.IDPathReq) *types.BizResult {
	return l.withPermissionWriteLock("AdminPermissionLogic.Delete", func() *types.BizResult {
		return l.delete(req)
	})
}

// delete 在 RBAC 写锁内删除权限子树。
func (l *AdminPermissionLogic) delete(req *types.IDPathReq) *types.BizResult {
	if err := l.ensurePermissionsWithinManageScope([]int{req.ID}); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(err, "AdminPermissionLogic.Delete 权限ID[%d]超出可操作范围", req.ID))
	}
	var affectedRoleIDs []int
	if err := l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		var permissionIDs []int
		if err := freshTxStatement(tx).Model(&model.AdminPermission{}).
			Where("id = ? OR FIND_IN_SET(?, pids)", req.ID, req.ID).
			Order("id ASC").
			Pluck("id", &permissionIDs).Error; err != nil {
			return errors.Wrapf(err, "查询权限ID[%d]子树失败", req.ID)
		}
		permissionIDs = types.UniquePositiveInts(permissionIDs)
		if len(permissionIDs) == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := l.ensurePermissionsWithinManageScope(permissionIDs); err != nil {
			return errors.Tag(err)
		}
		// 删除关系前记录受影响角色，事务提交后精确清理对应角色权限缓存。
		if err := freshTxStatement(tx).Model(&model.AdminRolePermissionRel{}).
			Where("permission_id IN ?", permissionIDs).
			Distinct("role_id").
			Pluck("role_id", &affectedRoleIDs).Error; err != nil {
			return errors.Wrap(err, "查询受影响角色失败")
		}
		affectedRoleIDs = types.UniquePositiveInts(affectedRoleIDs)
		if err := tx.Where("permission_id IN ?", permissionIDs).Delete(&model.AdminRolePermissionRel{}).Error; err != nil {
			return errors.Wrap(err, "清理角色权限关系失败")
		}
		result := tx.Where("id IN ?", permissionIDs).Delete(&model.AdminPermission{})
		if result.Error != nil {
			return errors.Wrap(result.Error, "删除权限失败")
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyNotFound, err,
				"AdminPermissionLogic.Delete 权限ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.Delete 删除权限ID[%d]失败", req.ID).ToBizResult()
	}

	if err := l.refreshPermissionRelatedCache(affectedRoleIDs...); err != nil {
		return corelogic.CacheSyncPendingResult(l.Logger, codes.DeleteSuccess, i18n.MsgKeyAdminCacheInvalidationPending, err,
			"AdminPermissionLogic.Delete RBAC缓存同步失败")
	}
	return types.NewBizResult(codes.DeleteSuccess).
		SetI18nMessage(i18n.MsgKeyDeleteSuccess)
}

// withPermissionWriteLock 把权限写链路切换到 RBAC 分布式锁生命周期上下文。
func (l *AdminPermissionLogic) withPermissionWriteLock(operation string, fn func() *types.BizResult) *types.BizResult {
	if l == nil {
		return WithRBACWriteLock(nil, operation, nil)
	}
	return WithRBACWriteLock(l.BaseLogic, operation, func(lockedBaseLogic *corelogic.BaseLogic) *types.BizResult {
		originalBaseLogic := l.BaseLogic
		l.BaseLogic = lockedBaseLogic
		defer func() {
			l.BaseLogic = originalBaseLogic
		}()
		return fn()
	})
}

// MaxUUID 返回下一个可用权限 UUID。
func (l *AdminPermissionLogic) MaxUUID() *types.BizResult {
	uuid, err := l.nextPermissionUUID()
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.MaxUUID 生成权限UUID失败").ToBizResult()
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.PermissionMaxUUIDResp{UUID: uuid})
}

// loadAllPermissions 加载全部权限，统一用于权限树和缓存重建。
func (l *AdminPermissionLogic) loadAllPermissions() ([]model.AdminPermission, error) {
	var permissions []model.AdminPermission
	err := l.Svc.WriteDB(svc.DatabaseMain).Order("id ASC").Find(&permissions).Error
	if err != nil {
		return nil, errors.Wrap(err, "AdminPermissionLogic.loadAllPermissions 查询全部权限失败")
	}
	return permissions, nil
}

// LoadPermissionTreeWithCache 优先读取权限树缓存，未命中时自动回源并重建。
func (l *AdminPermissionLogic) LoadPermissionTreeWithCache() ([]types.AdminPermissionItem, error) {
	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	var items []types.AdminPermissionItem
	_, err = manager.LoadThrough(l.Ctx, cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.PermissionTree), &items, nil)
	return items, errors.Tag(err)
}

// PermissionUUIDsByIDsWithCache 优先从权限 UUID 缓存读取启用权限码，缺失时自动回源重建。
func (l *AdminPermissionLogic) PermissionUUIDsByIDsWithCache(permissionIDs []int) ([]string, error) {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if len(permissionIDs) == 0 {
		return []string{}, nil
	}
	fields := make([]string, 0, len(permissionIDs))
	for _, permissionID := range permissionIDs {
		fields = append(fields, strconv.Itoa(permissionID))
	}
	cache, err := cachelogic.StringHashFieldsWithCache(l.BaseLogic, keys.PermissionUUID, fields)
	if err != nil {
		return nil, errors.Tag(err)
	}
	codesArr := make([]string, 0, len(permissionIDs))
	for _, permissionID := range permissionIDs {
		code := strings.TrimSpace(cache[strconv.Itoa(permissionID)])
		if code == "" || corelogic.CacheIsEmptyMarker(code) {
			continue
		}
		codesArr = append(codesArr, code)
	}
	return helper.UniqueNonEmptyStrings(codesArr), nil
}

// EnabledPermissionIDsWithCache 通过启用权限 UUID 索引过滤有效权限 ID。
func (l *AdminPermissionLogic) EnabledPermissionIDsWithCache(permissionIDs []int) ([]int, error) {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if len(permissionIDs) == 0 {
		return []int{}, nil
	}
	fields := make([]string, 0, len(permissionIDs))
	for _, permissionID := range permissionIDs {
		fields = append(fields, strconv.Itoa(permissionID))
	}
	cache, err := cachelogic.StringHashFieldsWithCache(l.BaseLogic, keys.PermissionUUID, fields)
	if err != nil {
		return nil, errors.Tag(err)
	}
	result := make([]int, 0, len(permissionIDs))
	for _, permissionID := range permissionIDs {
		if strings.TrimSpace(cache[strconv.Itoa(permissionID)]) != "" {
			result = append(result, permissionID)
		}
	}
	return types.UniquePositiveInts(result), nil
}

// AllEnabledPermissionIDsWithCache 返回全部启用权限 ID。
func (l *AdminPermissionLogic) AllEnabledPermissionIDsWithCache() ([]int, error) {
	cache, err := l.permissionUUIDMapWithCache()
	if err != nil {
		return nil, errors.Tag(err)
	}
	permissionIDs := make([]int, 0, len(cache))
	for field := range cache {
		permissionID, convErr := strconv.Atoi(strings.TrimSpace(field))
		if convErr == nil && permissionID > 0 {
			permissionIDs = append(permissionIDs, permissionID)
		}
	}
	return types.UniquePositiveInts(permissionIDs), nil
}

// AllEnabledPermissionUUIDsWithCache 返回全部启用权限码，供超级管理员初始化前端权限。
func (l *AdminPermissionLogic) AllEnabledPermissionUUIDsWithCache() ([]string, error) {
	cache, err := l.permissionUUIDMapWithCache()
	if err != nil {
		return nil, errors.Tag(err)
	}
	permissionIDs := make([]int, 0, len(cache))
	for field := range cache {
		permissionID, convErr := strconv.Atoi(strings.TrimSpace(field))
		if convErr == nil && permissionID > 0 {
			permissionIDs = append(permissionIDs, permissionID)
		}
	}
	sort.Ints(permissionIDs)
	result := make([]string, 0, len(permissionIDs))
	for _, permissionID := range permissionIDs {
		if uuid := strings.TrimSpace(cache[strconv.Itoa(permissionID)]); uuid != "" {
			result = append(result, uuid)
		}
	}
	return helper.UniqueNonEmptyStrings(result), nil
}

// permissionUUIDMapWithCache 读取全部启用权限 UUID 缓存，未命中时自动回源重建。
func (l *AdminPermissionLogic) permissionUUIDMapWithCache() (map[string]string, error) {
	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	cache := make(map[string]string)
	result, err := manager.LoadThrough(l.Ctx, cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.PermissionUUID), &cache, nil)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if result.State == tablecache.LookupStateEmpty {
		return map[string]string{}, nil
	}
	return cache, nil
}

// permissionModelToItem 把权限模型转换成接口响应项。
func permissionModelToItem(permission model.AdminPermission, checked bool, disabled bool, children []types.AdminPermissionItem) types.AdminPermissionItem {
	return types.AdminPermissionItem{
		ID:              permission.ID,
		UUID:            permission.UUID,
		Title:           permission.Title,
		Module:          permission.Module,
		Pid:             permission.Pid,
		Pids:            permission.Pids,
		Type:            permission.Type,
		Description:     permission.Description,
		Status:          permission.Status,
		Checked:         checked,
		Disabled:        disabled,
		DisableCheckbox: disabled,
		Selectable:      !disabled,
		HasChild:        len(children) > 0,
		Children:        children,
		CreatedAt:       corelogic.FormatDateTime(permission.CreatedAt),
		UpdatedAt:       corelogic.FormatDateTime(permission.UpdatedAt),
	}
}

// markPermissionTreeManageScope 按当前登录管理员可管理范围标记权限树节点可操作状态。
func markPermissionTreeManageScope(items []types.AdminPermissionItem, manageable map[int]struct{}) []types.AdminPermissionItem {
	return markPermissionTreeManageScopeWithPath(items, manageable, true)
}

// markPermissionTreeManageScopeWithPath 递归标记权限管理范围和父级路径可用状态。
func markPermissionTreeManageScopeWithPath(items []types.AdminPermissionItem, manageable map[int]struct{}, parentPathEnabled bool) []types.AdminPermissionItem {
	result := make([]types.AdminPermissionItem, 0, len(items))
	for _, item := range items {
		nextItem := item
		_, canManage := manageable[item.ID]
		pathEnabled := parentPathEnabled && nextItem.Status == 1
		nextItem.Manageable = canManage
		nextItem.CanCreateChild = canManage && pathEnabled
		nextItem.Disabled = nextItem.Status != 1 || !canManage
		nextItem.DisableCheckbox = nextItem.Disabled
		nextItem.Selectable = !nextItem.Disabled
		nextItem.Children = markPermissionTreeManageScopeWithPath(item.Children, manageable, pathEnabled)
		nextItem.HasChild = len(nextItem.Children) > 0
		result = append(result, nextItem)
	}
	return result
}

// markPermissionTreeChecked 在已缓存的权限树上补充 checked 和可操作状态，避免角色授权树每次都回库重建。
func markPermissionTreeChecked(items []types.AdminPermissionItem, checked map[int]struct{}, assignable map[int]struct{}, lockAll bool) []types.AdminPermissionItem {
	result := make([]types.AdminPermissionItem, 0, len(items))
	for _, item := range items {
		nextItem := item
		_, nextItem.Checked = checked[item.ID]
		_, assignableByRole := assignable[item.ID]

		// 角色权限树展示始终以后端计算出的“当前可分配范围”为准：
		// 1. 只有仍在可分配范围内的节点允许继续勾选或取消；
		// 2. 已禁用权限统一禁止再分配；
		// 3. 超级管理员角色自身编辑场景整体锁定；
		// 4. 历史越权脏数据即使仍是 checked，也直接显示为不可勾选，等待后端级联收敛后清理。
		nodeUsable := assignableByRole
		nextItem.Disabled = lockAll || nextItem.Status != 1 || !nodeUsable
		nextItem.DisableCheckbox = nextItem.Disabled
		nextItem.Selectable = !nextItem.Disabled
		nextItem.Children = markPermissionTreeChecked(item.Children, checked, assignable, lockAll)
		nextItem.HasChild = len(nextItem.Children) > 0
		result = append(result, nextItem)
	}
	return result
}

// manageablePermissionItemIDSet 基于缓存权限树和角色权限缓存计算当前管理员可管理范围。
func (l *AdminPermissionLogic) manageablePermissionItemIDSet(items []types.AdminPermissionItem) (map[int]struct{}, error) {
	roleLogic := &AdminRoleLogic{BaseLogic: l.BaseLogic}
	roleIDs, err := roleLogic.CurrentOperatorEnabledRoleIDs()
	if err != nil {
		return nil, errors.Tag(err)
	}
	isSuperRole := roleIDsContainSuper(roleIDs)
	operatorPermissionSet := make(map[int]struct{})
	if !isSuperRole {
		for _, roleID := range roleIDs {
			permissionIDs, err := roleLogic.RolePermissionIDsWithCache(roleID)
			if err != nil {
				return nil, errors.Tag(err)
			}
			for _, permissionID := range permissionIDs {
				operatorPermissionSet[permissionID] = struct{}{}
			}
		}
	}
	result := make(map[int]struct{})
	var walk func([]types.AdminPermissionItem)
	walk = func(nodes []types.AdminPermissionItem) {
		for _, item := range nodes {
			if isSuperRole || permissionItemWithinScope(item, operatorPermissionSet) {
				result[item.ID] = struct{}{}
			}
			walk(item.Children)
		}
	}
	walk(items)
	return result, nil
}

// permissionItemWithinScope 判断权限自身或任一祖先是否属于当前管理员权限集合。
func permissionItemWithinScope(item types.AdminPermissionItem, permissionSet map[int]struct{}) bool {
	if _, ok := permissionSet[item.ID]; ok {
		return true
	}
	for _, parentIDText := range strings.Split(item.Pids, ",") {
		parentID, err := strconv.Atoi(strings.TrimSpace(parentIDText))
		if err != nil || parentID <= 0 {
			continue
		}
		if _, ok := permissionSet[parentID]; ok {
			return true
		}
	}
	return false
}

// permissionParentChanged 判断权限编辑是否真实发生父级迁移。
// 只有迁移父级时才校验目标父级可管理范围。
func permissionParentChanged(currentPid int, nextPid int) bool {
	return currentPid != nextPid
}

// manageablePermissionIDSet 计算当前登录管理员可管理的权限集合。
// 超级管理员可管理全部权限；普通管理员可管理自己已拥有权限及其全部子级权限。
func (l *AdminPermissionLogic) manageablePermissionIDSet() (map[int]struct{}, error) {
	permissions, err := l.loadAllPermissions()
	if err != nil {
		return nil, errors.Tag(err)
	}
	roleLogic := &AdminRoleLogic{BaseLogic: l.BaseLogic}
	isSuperRole, err := roleLogic.CurrentOperatorIsSuperRole()
	if err != nil {
		return nil, errors.Tag(err)
	}
	result := make(map[int]struct{}, len(permissions))
	if isSuperRole {
		for _, permission := range permissions {
			result[permission.ID] = struct{}{}
		}
		return result, nil
	}
	roleIDs, err := roleLogic.CurrentOperatorEnabledRoleIDs()
	if err != nil {
		return nil, errors.Tag(err)
	}
	operatorPermissionSet := make(map[int]struct{})
	for _, roleID := range roleIDs {
		permissionIDs, err := roleLogic.RolePermissionIDsWithCache(roleID)
		if err != nil {
			return nil, errors.Tag(err)
		}
		for _, permissionID := range permissionIDs {
			operatorPermissionSet[permissionID] = struct{}{}
		}
	}
	for _, permission := range permissions {
		for permissionID := range operatorPermissionSet {
			if permission.ID == permissionID || corelogic.ContainsTreeID(permission.Pids, permissionID) {
				result[permission.ID] = struct{}{}
				break
			}
		}
	}
	return result, nil
}

// ensurePermissionsWithinManageScope 校验目标权限是否都在当前登录管理员可管理范围内。
func (l *AdminPermissionLogic) ensurePermissionsWithinManageScope(permissionIDs []int) error {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if len(permissionIDs) == 0 {
		return nil
	}
	manageablePermissionSet, err := l.manageablePermissionIDSet()
	if err != nil {
		return errors.Tag(err)
	}
	var invalidPermissionIDs []int
	for _, permissionID := range permissionIDs {
		if _, ok := manageablePermissionSet[permissionID]; ok {
			continue
		}
		invalidPermissionIDs = append(invalidPermissionIDs, permissionID)
	}
	if len(invalidPermissionIDs) > 0 {
		return errors.Errorf("存在超出当前管理员可管理范围的权限: %v", invalidPermissionIDs)
	}
	return nil
}

// ensurePermissionParentWithinManageScope 校验目标父级权限是否在当前登录管理员可管理范围内。
func (l *AdminPermissionLogic) ensurePermissionParentWithinManageScope(parentPermissionID int) error {
	if parentPermissionID <= 0 {
		roleLogic := &AdminRoleLogic{BaseLogic: l.BaseLogic}
		isSuperRole, err := roleLogic.CurrentOperatorIsSuperRole()
		if err != nil {
			return errors.Tag(err)
		}
		if !isSuperRole {
			return errors.Errorf("仅超级管理员允许创建或移动到顶级权限")
		}
		return nil
	}
	return l.ensurePermissionsWithinManageScope([]int{parentPermissionID})
}

// loadPermissionHierarchyTx 在指定数据库上下文中读取权限轻量层级快照。
func (l *AdminPermissionLogic) loadPermissionHierarchyTx(tx *gorm.DB) ([]model.AdminPermission, error) {
	var permissions []model.AdminPermission
	if err := freshTxStatement(tx).Model(&model.AdminPermission{}).
		Select("id, pid, pids, status").
		Order("id ASC").
		Find(&permissions).Error; err != nil {
		return nil, errors.Wrap(err, "AdminPermissionLogic.loadPermissionHierarchyTx 查询权限层级失败")
	}
	return permissions, nil
}

// disabledPermissionPathID 按真实 pid 链返回目标权限路径上首个禁用节点。
func disabledPermissionPathID(permissions []model.AdminPermission, permissionID int) (int, error) {
	permissionByID := make(map[int]model.AdminPermission, len(permissions))
	for _, permission := range permissions {
		permissionByID[permission.ID] = permission
	}
	visited := make(map[int]struct{}, len(permissions))
	for currentID := permissionID; currentID > 0; {
		if _, ok := visited[currentID]; ok {
			return 0, errors.Errorf("权限父级链存在循环 ID[%d]", currentID)
		}
		visited[currentID] = struct{}{}
		permission, ok := permissionByID[currentID]
		if !ok {
			return 0, errors.Errorf("权限ID[%d]不存在", currentID)
		}
		if permission.Status != 1 {
			return permission.ID, nil
		}
		currentID = permission.Pid
	}
	return 0, nil
}

// disabledPermissionPathID 从主库返回目标权限路径上首个禁用节点。
func (l *AdminPermissionLogic) disabledPermissionPathID(permissionID int) (int, error) {
	if permissionID <= 0 {
		return 0, nil
	}
	permissions, err := l.loadPermissionHierarchyTx(l.Svc.WriteDB(svc.DatabaseMain))
	if err != nil {
		return 0, errors.Tag(err)
	}
	return disabledPermissionPathID(permissions, permissionID)
}

// permissionPidsFromHierarchy 根据真实 pid 链生成权限族谱，并拒绝自引用或循环父级。
func permissionPidsFromHierarchy(permissions []model.AdminPermission, pid int, selfID int) (string, error) {
	if pid <= 0 {
		return "", nil
	}
	permissionByID := make(map[int]model.AdminPermission, len(permissions))
	for _, permission := range permissions {
		permissionByID[permission.ID] = permission
	}
	ancestorIDs := make([]int, 0)
	visited := make(map[int]struct{}, len(permissions))
	for currentID := pid; currentID > 0; {
		if currentID == selfID {
			return "", errors.Errorf("父级权限不能是权限ID[%d]自身或其子级", selfID)
		}
		if _, ok := visited[currentID]; ok {
			return "", errors.Errorf("权限父级链存在循环 ID[%d]", currentID)
		}
		visited[currentID] = struct{}{}
		permission, ok := permissionByID[currentID]
		if !ok {
			return "", errors.Errorf("父级权限ID[%d]不存在", currentID)
		}
		ancestorIDs = append(ancestorIDs, currentID)
		currentID = permission.Pid
	}
	for left, right := 0, len(ancestorIDs)-1; left < right; left, right = left+1, right-1 {
		ancestorIDs[left], ancestorIDs[right] = ancestorIDs[right], ancestorIDs[left]
	}
	parts := make([]string, 0, len(ancestorIDs))
	for _, permissionID := range ancestorIDs {
		parts = append(parts, strconv.Itoa(permissionID))
	}
	return strings.Join(parts, ","), nil
}

// descendantPermissionPids 根据 pid 邻接关系重建指定权限全部子孙的族谱。
func descendantPermissionPids(permissions []model.AdminPermission, rootID int) (map[int]string, error) {
	permissionByID := make(map[int]model.AdminPermission, len(permissions))
	childrenByPID := make(map[int][]int, len(permissions))
	for _, permission := range permissions {
		permissionByID[permission.ID] = permission
		childrenByPID[permission.Pid] = append(childrenByPID[permission.Pid], permission.ID)
	}
	root, ok := permissionByID[rootID]
	if !ok {
		return nil, errors.Errorf("权限ID[%d]不存在", rootID)
	}
	result := make(map[int]string)
	visited := map[int]struct{}{rootID: {}}
	var walk func(parentID int, parentPids string) error
	walk = func(parentID int, parentPids string) error {
		for _, childID := range childrenByPID[parentID] {
			if _, ok := visited[childID]; ok {
				return errors.Errorf("权限子树存在循环 ID[%d]", childID)
			}
			visited[childID] = struct{}{}
			childPids := corelogic.BuildTreePids(parentID, parentPids)
			result[childID] = childPids
			if err := walk(childID, childPids); err != nil {
				return errors.Tag(err)
			}
		}
		return nil
	}
	if err := walk(root.ID, root.Pids); err != nil {
		return nil, errors.Tag(err)
	}
	return result, nil
}

// permissionPidsTx 在事务内按真实 pid 链计算权限族谱。
func (l *AdminPermissionLogic) permissionPidsTx(tx *gorm.DB, pid int, selfID int) (string, error) {
	if pid <= 0 {
		return "", nil
	}
	permissions, err := l.loadPermissionHierarchyTx(tx)
	if err != nil {
		return "", errors.Tag(err)
	}
	pids, err := permissionPidsFromHierarchy(permissions, pid, selfID)
	if err != nil {
		return "", errors.Wrapf(err, "AdminPermissionLogic.permissionPidsTx 计算父级权限ID[%d]族谱失败", pid)
	}
	return pids, nil
}

// syncPermissionDescendantPidsTx 在权限换父级后同步更新全部子孙族谱。
func (l *AdminPermissionLogic) syncPermissionDescendantPidsTx(tx *gorm.DB, permissionID int) error {
	permissions, err := l.loadPermissionHierarchyTx(tx)
	if err != nil {
		return errors.Tag(err)
	}
	nextPids, err := descendantPermissionPids(permissions, permissionID)
	if err != nil {
		return errors.Wrapf(err, "AdminPermissionLogic.syncPermissionDescendantPidsTx 重建权限ID[%d]子树族谱失败", permissionID)
	}
	currentPids := make(map[int]string, len(permissions))
	permissionIDs := make([]int, 0, len(nextPids))
	for _, permission := range permissions {
		currentPids[permission.ID] = permission.Pids
		if _, ok := nextPids[permission.ID]; ok {
			permissionIDs = append(permissionIDs, permission.ID)
		}
	}
	sort.Ints(permissionIDs)
	updatedAt := time.Now()
	for _, descendantPermissionID := range permissionIDs {
		pids := nextPids[descendantPermissionID]
		if currentPids[descendantPermissionID] == pids {
			continue
		}
		if err := freshTxStatement(tx).Model(&model.AdminPermission{}).
			Where("id = ?", descendantPermissionID).
			Updates(map[string]any{"pids": pids, "updated_at": updatedAt}).Error; err != nil {
			return errors.Wrapf(err, "AdminPermissionLogic.syncPermissionDescendantPidsTx 更新权限ID[%d]族谱失败", descendantPermissionID)
		}
	}
	return nil
}

// permissionSubtreeIDs 按真实 pid 邻接关系返回权限自身及全部子孙 ID。
func (l *AdminPermissionLogic) permissionSubtreeIDs(permissionID int) ([]int, error) {
	permissions, err := l.loadPermissionHierarchyTx(l.Svc.WriteDB(svc.DatabaseMain))
	if err != nil {
		return nil, errors.Tag(err)
	}
	descendantPids, err := descendantPermissionPids(permissions, permissionID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	permissionIDs := make([]int, 0, len(descendantPids)+1)
	permissionIDs = append(permissionIDs, permissionID)
	for descendantPermissionID := range descendantPids {
		permissionIDs = append(permissionIDs, descendantPermissionID)
	}
	sort.Ints(permissionIDs)
	return permissionIDs, nil
}

// ensurePermissionUUIDUniqueTx 校验权限 UUID 唯一。
func (l *AdminPermissionLogic) ensurePermissionUUIDUniqueTx(tx *gorm.DB, uuid string, ignoreID int) error {
	var count int64
	query := tx.Model(&model.AdminPermission{}).Where("uuid = ?", strings.TrimSpace(uuid))
	if ignoreID > 0 {
		query = query.Where("id <> ?", ignoreID)
	}
	if err := query.Count(&count).Error; err != nil {
		return errors.Wrapf(err, "AdminPermissionLogic.ensurePermissionUUIDUniqueTx 检查权限UUID[%s]唯一失败", strings.TrimSpace(uuid))
	}
	if count > 0 {
		return errors.Wrapf(ErrPermissionUUIDAlreadyExists, "AdminPermissionLogic.ensurePermissionUUIDUniqueTx 权限UUID[%s]已存在", strings.TrimSpace(uuid))
	}
	return nil
}

// nextPermissionUUID 根据当前最大数字 UUID 生成下一个权限 UUID。
func (l *AdminPermissionLogic) nextPermissionUUID() (string, error) {
	if l == nil || l.Svc == nil {
		return "", errors.Errorf("权限数据库未初始化")
	}
	db := l.Svc.WriteDB(svc.DatabaseMain)
	if db == nil {
		return "", errors.Errorf("权限数据库未初始化")
	}
	var uuids []string
	if err := db.Model(&model.AdminPermission{}).Pluck("uuid", &uuids).Error; err != nil {
		return "", errors.Wrap(err, "AdminPermissionLogic.nextPermissionUUID 查询最大UUID失败")
	}
	maxValue := 100000
	for _, uuid := range uuids {
		value, err := strconv.Atoi(strings.TrimSpace(uuid))
		if err != nil {
			continue
		}
		if value > maxValue {
			maxValue = value
		}
	}
	return fmt.Sprintf("%06d", maxValue+1), nil
}

// refreshPermissionRelatedCache 清理权限树、共享权限索引和受影响角色权限关系缓存。
func (l *AdminPermissionLogic) refreshPermissionRelatedCache(roleIDs ...int) error {
	coreKeys := []string{keys.PermissionTree, keys.RoutePermissionIDs, keys.PermissionUUID}
	for _, roleID := range types.UniquePositiveInts(roleIDs) {
		coreKeys = append(coreKeys, fmt.Sprintf(keys.RolePermission, roleID))
	}
	return cachelogic.DeleteTableCacheKeysExact(
		l.BaseLogic,
		"AdminPermissionLogic.refreshPermissionRelatedCache 删除权限缓存",
		cachelogic.TableCachePhysicalKeys(l.BaseLogic, coreKeys...),
	)
}

// RefreshPermissionRelatedCache 清理权限树、共享权限索引和指定角色权限关系缓存。
func (l *AdminPermissionLogic) RefreshPermissionRelatedCache(roleIDs ...int) error {
	return l.refreshPermissionRelatedCache(roleIDs...)
}
