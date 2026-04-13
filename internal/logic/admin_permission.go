package logic

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"admin_cron/helper"

	"github.com/Is999/go-utils/errors"
	tablecache "github.com/Is999/table-cache"

	"admin_cron/common/codes"
	i18n "admin_cron/common/i18n"
	keys "admin_cron/common/rediskeys"
	"admin_cron/internal/model"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"
	"net/http"

	"gorm.io/gorm"
)

// AdminPermissionLogic 预留权限领域逻辑入口，后续扩展权限维护能力时从这里收口。
type AdminPermissionLogic struct {
	*BaseLogic // 复用上下文、数据库和日志能力
}

// NewAdminPermissionLogic 创建权限业务逻辑对象。
func NewAdminPermissionLogic(r *http.Request, svcCtx *svc.ServiceContext) *AdminPermissionLogic {
	return &AdminPermissionLogic{
		BaseLogic: NewBaseLogic(r, svcCtx),
	}
}

// List 分页查询权限列表，支持按 UUID、名称、模块、类型和父级筛选。
func (l *AdminPermissionLogic) List(req *types.PermissionListReq) *types.BizResult {
	// 权限表数据量通常不大，但仍保持分页接口，方便后续扩展搜索和审计。
	dbq := l.svc.ReadDB(svc.DatabaseMain).Model(&model.AdminPermission{})
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
			dbq = applyGenealogyScopeFilter(dbq, "pids", *req.Pid)
		} else {
			dbq = dbq.Where("pid = ?", *req.Pid)
		}
	}

	orderBy := normalizedOrderField(req.OrderBy, "id")
	list, total, err := model.List[model.AdminPermission](dbq, req.Page, req.PageSize, orderBy, normalizedOrderDirection(req.Order))
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.List 查询权限列表失败").ToBizResult()
	}

	items := make([]types.AdminPermissionItem, 0, len(list))
	for _, permission := range list {
		items = append(items, permissionModelToItem(permission, false, false, nil))
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.ListResp[types.AdminPermissionItem]{List: items, Total: total})
}

// TreeList 查询权限树，供权限选择和角色授权使用。
func (l *AdminPermissionLogic) TreeList() *types.BizResult {
	items, err := l.loadPermissionTreeWithCache()
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.TreeList 查询权限树失败").ToBizResult()
	}
	manageablePermissionSet, err := l.manageablePermissionIDSet()
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
func (l *AdminPermissionLogic) Create(req *types.SavePermissionReq) *types.BizResult {
	if req.UUID == "" {
		req.UUID = l.nextPermissionUUID()
	}
	if err := l.ensurePermissionParentWithinManageScope(req.Pid); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(err, "AdminPermissionLogic.Create 父级权限ID[%d]超出可操作范围", req.Pid))
	}
	description := req.Description
	permission := model.AdminPermission{
		UUID:        req.UUID,
		Title:       req.Title,
		Module:      req.Module,
		Pid:         req.Pid,
		Type:        req.Type,
		Description: description,
		Status:      intPtrDefault(req.Status, 1),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := l.svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
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
		if errors.Is(err, errPermissionUUIDAlreadyExists) || isMySQLDuplicateEntryError(err) {
			return permissionUUIDAlreadyExistsResult(req.UUID, err)
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.Create 创建权限[%s]失败", req.Title).ToBizResult()
	}

	l.refreshPermissionRelatedCache(req.Module)
	return types.NewBizResult(codes.AddSuccess).
		SetI18nMessage(i18n.MsgKeyAddSuccess)
}

// Update 编辑权限节点。
func (l *AdminPermissionLogic) Update(req *types.SavePermissionReq) *types.BizResult {
	var permission model.AdminPermission
	if err := l.svc.WriteDB(svc.DatabaseMain).Where("id = ?", req.ID).First(&permission).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyNotFound, err,
				"AdminPermissionLogic.Update 权限ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.Update 查询权限ID[%d]失败", req.ID).ToBizResult()
	}

	nextPid := resolvePermissionUpdateParentID(permission.Pid, req.Pid)
	nextUUID := permission.UUID
	if req.UUID != "" {
		nextUUID = req.UUID
	}
	nextStatus := permission.Status
	if req.Status != nil {
		nextStatus = *req.Status
	}
	if err := l.ensurePermissionsWithinManageScope([]int{req.ID}); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(err, "AdminPermissionLogic.Update 权限ID[%d]超出可操作范围", req.ID))
	}
	// 仅在父级真实变化时校验目标父级范围。
	// 已有顶级目录保持 pid=0 编辑时，不按创建顶级权限处理。
	if permissionParentChanged(permission.Pid, nextPid) {
		if err := l.ensurePermissionParentWithinManageScope(nextPid); err != nil {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "AdminPermissionLogic.Update 父级权限ID[%d]超出可操作范围", nextPid))
		}
	}
	description := req.Description
	if description == "" {
		description = permission.Description
	}

	if err := l.svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
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
			"module":      req.Module,
			"pid":         nextPid,
			"pids":        pids,
			"type":        req.Type,
			"description": description,
			"status":      nextStatus,
			"updated_at":  time.Now(),
		}).Error; err != nil {
			return errors.Wrap(err, "更新权限失败")
		}
		return nil
	}); err != nil {
		if errors.Is(err, errPermissionUUIDAlreadyExists) || isMySQLDuplicateEntryError(err) {
			return permissionUUIDAlreadyExistsResult(nextUUID, err)
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.Update 更新权限ID[%d]失败", req.ID).ToBizResult()
	}

	l.refreshPermissionRelatedCache(permission.Module, req.Module)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// UpdateStatus 修改权限启用/禁用状态。
func (l *AdminPermissionLogic) UpdateStatus(req *types.PermissionStatusReq) *types.BizResult {
	if err := l.ensurePermissionsWithinManageScope([]int{req.ID}); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(err, "AdminPermissionLogic.UpdateStatus 权限ID[%d]超出可操作范围", req.ID))
	}
	var permission model.AdminPermission
	if err := l.svc.WriteDB(svc.DatabaseMain).
		Select("id", "module").
		Where("id = ?", req.ID).
		First(&permission).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyNotFound, err,
				"AdminPermissionLogic.UpdateStatus 权限ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminPermissionLogic.UpdateStatus 查询权限ID[%d]失败", req.ID).ToBizResult()
	}
	status := req.StatusValue()
	result := l.svc.WriteDB(svc.DatabaseMain).Model(&model.AdminPermission{}).
		Where("id = ?", req.ID).
		Updates(map[string]any{"status": status, "updated_at": time.Now()})
	if result.Error != nil {
		return types.DBError(i18n.MsgKeyDBError, result.Error,
			"AdminPermissionLogic.UpdateStatus 修改权限ID[%d]状态失败", req.ID).ToBizResult()
	}
	if result.RowsAffected == 0 {
		return types.NotFound(i18n.MsgKeyNotFound, gorm.ErrRecordNotFound,
			"AdminPermissionLogic.UpdateStatus 权限ID[%d]不存在", req.ID).ToBizResult()
	}

	l.refreshPermissionRelatedCache(permission.Module)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyStatusChangeOK)
}

// Delete 删除权限节点；删除时级联删除全部子孙权限，并清理角色权限关系。
func (l *AdminPermissionLogic) Delete(req *types.IDPathReq) *types.BizResult {
	if err := l.ensurePermissionsWithinManageScope([]int{req.ID}); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(err, "AdminPermissionLogic.Delete 权限ID[%d]超出可操作范围", req.ID))
	}
	routeAliases := make([]string, 0)
	if err := l.svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
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
		if err := freshTxStatement(tx).Model(&model.AdminPermission{}).
			Where("id IN ?", permissionIDs).
			Pluck("module", &routeAliases).Error; err != nil {
			return errors.Wrapf(err, "查询权限ID[%d]子树模块失败", req.ID)
		}
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

	l.refreshPermissionRelatedCache(routeAliases...)
	return types.NewBizResult(codes.DeleteSuccess).
		SetI18nMessage(i18n.MsgKeyDeleteSuccess)
}

// MaxUUID 返回下一个可用权限 UUID。
func (l *AdminPermissionLogic) MaxUUID() *types.BizResult {
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.PermissionMaxUUIDResp{UUID: l.nextPermissionUUID()})
}

// loadAllPermissions 加载全部权限，统一用于权限树和缓存重建。
func (l *AdminPermissionLogic) loadAllPermissions() ([]model.AdminPermission, error) {
	var permissions []model.AdminPermission
	err := l.svc.ReadDB(svc.DatabaseMain).Order("id ASC").Find(&permissions).Error
	if err != nil {
		return nil, errors.Wrap(err, "AdminPermissionLogic.loadAllPermissions 查询全部权限失败")
	}
	return permissions, nil
}

// loadPermissionTreeWithCache 优先读取权限树缓存，未命中时自动回源并重建。
func (l *AdminPermissionLogic) loadPermissionTreeWithCache() ([]types.AdminPermissionItem, error) {
	if l.Redis() == nil {
		permissions, err := l.loadAllPermissions()
		if err != nil {
			return nil, errors.Tag(err)
		}
		return buildPermissionTree(permissions, nil, nil), nil
	}
	manager, err := tableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	var items []types.AdminPermissionItem
	_, err = manager.LoadThrough(l.Context(), tableCachePhysicalKey(l.BaseLogic, keys.PermissionTree), &items, nil)
	return items, errors.Tag(err)
}

// permissionUUIDsByIDsWithCache 优先从权限 UUID 缓存读取启用权限码，缺失时自动回源重建。
func (l *AdminPermissionLogic) permissionUUIDsByIDsWithCache(permissionIDs []int) ([]string, error) {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if len(permissionIDs) == 0 {
		return []string{}, nil
	}
	if l.Redis() == nil {
		return l.permissionUUIDsByIDs(permissionIDs)
	}
	cache, err := l.permissionFieldMapWithCache(keys.PermissionUUID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	codesArr := make([]string, 0, len(permissionIDs))
	for _, permissionID := range permissionIDs {
		code := strings.TrimSpace(cache[strconv.Itoa(permissionID)])
		if code == "" || cacheIsEmptyMarker(code) {
			continue
		}
		codesArr = append(codesArr, code)
	}
	return helper.UniqueNonEmptyStrings(codesArr), nil
}

// permissionUUIDsByIDs 直接从数据库读取启用权限码，作为缓存 miss 后的最终兜底。
func (l *AdminPermissionLogic) permissionUUIDsByIDs(permissionIDs []int) ([]string, error) {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if len(permissionIDs) == 0 {
		return []string{}, nil
	}
	var codesArr []string
	err := l.svc.WriteDB(svc.DatabaseMain).Session(&gorm.Session{NewDB: true}).
		Model(&model.AdminPermission{}).
		Distinct("uuid").
		Where("id IN ? AND status = 1", permissionIDs).
		Pluck("uuid", &codesArr).Error
	if err != nil {
		return nil, errors.Tag(err)
	}
	return helper.UniqueNonEmptyStrings(codesArr), nil
}

// permissionFieldMapWithCache 读取指定权限字段缓存，未命中时自动回源重建。
func (l *AdminPermissionLogic) permissionFieldMapWithCache(cacheKey string) (map[string]string, error) {
	if l.Redis() == nil {
		return l.permissionFieldMap(cacheKey)
	}
	manager, err := tableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	cache := make(map[string]string)
	result, err := manager.LoadThrough(l.Context(), tableCachePhysicalKey(l.BaseLogic, cacheKey), &cache, nil)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if result.State == tablecache.LookupStateEmpty {
		return map[string]string{}, nil
	}
	return cache, nil
}

// permissionFieldMap 直接从数据库构造权限字段缓存，作为 Redis 不可用时的兜底读取。
func (l *AdminPermissionLogic) permissionFieldMap(cacheKey string) (map[string]string, error) {
	var permissions []model.AdminPermission
	if err := l.svc.ReadDB(svc.DatabaseMain).Where("status = 1").Find(&permissions).Error; err != nil {
		return nil, errors.Tag(err)
	}
	cache := make(map[string]string, len(permissions))
	for _, permission := range permissions {
		switch cacheKey {
		case keys.PermissionModule:
			cache[strconv.Itoa(permission.ID)] = permission.Module
		default:
			cache[strconv.Itoa(permission.ID)] = permission.UUID
		}
	}
	return cache, nil
}

// permissionCandidateIDsWithCache 使用权限 module 缓存解析候选权限 ID，供鉴权链路优先走缓存。
func (l *AdminPermissionLogic) permissionCandidateIDsWithCache(candidates []string) ([]int, error) {
	candidateSet := make(map[string]struct{}, len(candidates))
	for _, candidate := range helper.UniqueNonEmptyStrings(candidates) {
		candidateSet[candidate] = struct{}{}
	}
	if len(candidateSet) == 0 {
		return []int{}, nil
	}
	moduleMap, err := l.permissionFieldMapWithCache(keys.PermissionModule)
	if err != nil {
		return nil, errors.Tag(err)
	}
	permissionIDs := make([]int, 0)
	for permissionIDText, module := range moduleMap {
		if _, ok := candidateSet[strings.TrimSpace(module)]; !ok {
			continue
		}
		permissionID, convErr := strconv.Atoi(permissionIDText)
		if convErr != nil || permissionID <= 0 {
			return nil, errors.Wrap(convErr, "解析权限模块缓存ID失败")
		}
		permissionIDs = append(permissionIDs, permissionID)
	}
	return types.UniquePositiveInts(permissionIDs), nil
}

// rebuildPermissionUUIDCache 把全部启用权限 UUID 重建到 Redis Hash，保持与旧版后台权限语义一致。
func (l *AdminPermissionLogic) rebuildPermissionUUIDCache() error {
	if l.Redis() == nil {
		return nil
	}
	cacheKey := tableCachePhysicalKey(l.BaseLogic, keys.PermissionUUID)
	var permissions []model.AdminPermission
	if err := l.svc.ReadDB(svc.DatabaseMain).Where("status = 1").Find(&permissions).Error; err != nil {
		return errors.Tag(err)
	}
	cache := make(map[string]any, len(permissions))
	for _, permission := range permissions {
		cache[strconv.Itoa(permission.ID)] = permission.UUID
	}
	pipe := l.Redis().Pipeline()
	pipe.Del(l.Context(), cacheKey)
	if len(cache) > 0 {
		pipe.HSet(l.Context(), cacheKey, cache)
	}
	_, err := pipe.Exec(l.Context())
	return errors.Tag(err)
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
		Children:        children,
		CreatedAt:       formatDateTime(permission.CreatedAt),
		UpdatedAt:       formatDateTime(permission.UpdatedAt),
	}
}

// buildPermissionTree 把平铺权限列表转换成树结构。
func buildPermissionTree(permissions []model.AdminPermission, checked map[int]struct{}, disabled map[int]struct{}) []types.AdminPermissionItem {
	children := make(map[int][]model.AdminPermission, len(permissions))
	for _, permission := range permissions {
		children[permission.Pid] = append(children[permission.Pid], permission)
	}
	var walk func(pid int) []types.AdminPermissionItem
	walk = func(pid int) []types.AdminPermissionItem {
		nodes := children[pid]
		result := make([]types.AdminPermissionItem, 0, len(nodes))
		for _, permission := range nodes {
			_, isChecked := checked[permission.ID]
			_, isDisabled := disabled[permission.ID]
			result = append(result, permissionModelToItem(permission, isChecked, isDisabled, walk(permission.ID)))
		}
		return result
	}
	return walk(0)
}

// markPermissionTreeManageScope 按当前登录管理员可管理范围标记权限树节点可操作状态。
func markPermissionTreeManageScope(items []types.AdminPermissionItem, manageable map[int]struct{}) []types.AdminPermissionItem {
	result := make([]types.AdminPermissionItem, 0, len(items))
	for _, item := range items {
		nextItem := item
		_, canManage := manageable[item.ID]
		nextItem.Disabled = nextItem.Status != 1 || !canManage
		nextItem.DisableCheckbox = nextItem.Disabled
		nextItem.Selectable = !nextItem.Disabled
		nextItem.Children = markPermissionTreeManageScope(item.Children, manageable)
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
		result = append(result, nextItem)
	}
	return result
}

// resolvePermissionUpdateParentID 计算编辑权限时最终使用的父级权限 ID。
// 编辑接口的 pid 是历史 int 字段，无法区分“未提交 pid”和“显式提交 0”；
// 因此非顶级权限未提交 pid=0 时保留原父级，已有顶级权限继续保持 pid=0。
func resolvePermissionUpdateParentID(currentPid int, requestPid int) int {
	if requestPid > 0 || currentPid == 0 {
		return requestPid
	}
	return currentPid
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
	isSuperRole, err := roleLogic.currentOperatorIsSuperRole()
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
	roleIDs, err := roleLogic.currentOperatorEnabledRoleIDs()
	if err != nil {
		return nil, errors.Tag(err)
	}
	operatorPermissionSet := make(map[int]struct{})
	for _, roleID := range roleIDs {
		permissionIDs, err := roleLogic.rolePermissionIDsWithCache(roleID)
		if err != nil {
			return nil, errors.Tag(err)
		}
		for _, permissionID := range permissionIDs {
			operatorPermissionSet[permissionID] = struct{}{}
		}
	}
	for _, permission := range permissions {
		for permissionID := range operatorPermissionSet {
			if permission.ID == permissionID || containsTreeID(permission.Pids, permissionID) {
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
		isSuperRole, err := roleLogic.currentOperatorIsSuperRole()
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

// permissionPidsTx 在事务内计算权限族谱。
func (l *AdminPermissionLogic) permissionPidsTx(tx *gorm.DB, pid int, selfID int) (string, error) {
	if pid <= 0 {
		return "", nil
	}
	if pid == selfID {
		return "", errors.Errorf("AdminPermissionLogic.permissionPidsTx 父级权限不能是自己 pid=%d selfID=%d", pid, selfID)
	}
	var parent model.AdminPermission
	if err := tx.Where("id = ?", pid).First(&parent).Error; err != nil {
		return "", errors.Wrapf(err, "AdminPermissionLogic.permissionPidsTx 查询父级权限ID[%d]失败", pid)
	}
	if containsTreeID(parent.Pids, selfID) {
		return "", errors.Errorf("AdminPermissionLogic.permissionPidsTx 不能把权限移动到自己的子级下面 pid=%d selfID=%d", pid, selfID)
	}
	return buildTreePids(parent.ID, parent.Pids), nil
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
		return errors.Wrapf(errPermissionUUIDAlreadyExists, "AdminPermissionLogic.ensurePermissionUUIDUniqueTx 权限UUID[%s]已存在", strings.TrimSpace(uuid))
	}
	return nil
}

// ensurePermissionNoChildrenTx 确认权限节点没有子节点。
func (l *AdminPermissionLogic) ensurePermissionNoChildrenTx(tx *gorm.DB, permissionID int) error {
	var count int64
	if err := tx.Model(&model.AdminPermission{}).Where("pid = ?", permissionID).Count(&count).Error; err != nil {
		return errors.Wrapf(err, "AdminPermissionLogic.ensurePermissionNoChildrenTx 检查权限ID[%d]子权限失败", permissionID)
	}
	if count > 0 {
		return errors.Errorf("AdminPermissionLogic.ensurePermissionNoChildrenTx 权限ID[%d]存在子权限，不能删除", permissionID)
	}
	return nil
}

// ensurePermissionNoRolesTx 确认权限没有被角色绑定。
func (l *AdminPermissionLogic) ensurePermissionNoRolesTx(tx *gorm.DB, permissionID int) error {
	var count int64
	if err := tx.Model(&model.AdminRolePermissionRel{}).Where("permission_id = ?", permissionID).Count(&count).Error; err != nil {
		return errors.Wrapf(err, "AdminPermissionLogic.ensurePermissionNoRolesTx 检查权限ID[%d]绑定角色失败", permissionID)
	}
	if count > 0 {
		return errors.Errorf("AdminPermissionLogic.ensurePermissionNoRolesTx 权限ID[%d]已被角色绑定，不能删除", permissionID)
	}
	return nil
}

// nextPermissionUUID 根据当前最大数字 UUID 生成下一个权限 UUID。
func (l *AdminPermissionLogic) nextPermissionUUID() string {
	var uuids []string
	if err := l.svc.ReadDB(svc.DatabaseMain).Model(&model.AdminPermission{}).Pluck("uuid", &uuids).Error; err != nil {
		logWrappedError(l, err, "AdminPermissionLogic.nextPermissionUUID 查询最大UUID失败")
		return "100001"
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
	return fmt.Sprintf("%06d", maxValue+1)
}

// refreshPermissionRelatedCache 清理权限相关缓存，确保下次读取重建最新权限数据。
func (l *AdminPermissionLogic) refreshPermissionRelatedCache(routeAliases ...string) {
	manager, err := tableCacheManager(l.BaseLogic)
	if err != nil {
		logWrappedError(l, err, "AdminPermissionLogic.refreshPermissionRelatedCache 初始化表缓存管理器失败")
		manager = nil
	}
	coreKeys := []string{keys.PermissionTree, keys.PermissionModule, keys.PermissionUUID}
	if manager != nil {
		for _, key := range coreKeys {
			physicalKey := tableCachePhysicalKey(l.BaseLogic, key)
			if err := manager.DeleteByKey(l.Context(), physicalKey); err != nil && !isTableCacheTargetNotFound(err) {
				logWrappedError(l, err, "AdminPermissionLogic.refreshPermissionRelatedCache 清理权限缓存key[%s]失败", key)
			}
		}
	}
	deleteRedisKeysExactBatches(l.BaseLogic, "AdminPermissionLogic.refreshPermissionRelatedCache 删除权限核心缓存", tableCachePhysicalAndLegacyKeys(l.BaseLogic, coreKeys...))
	l.deleteRoutePermissionCandidateCache(routeAliases...)
	invalidateAllAdminPermissionCache(l.BaseLogic)
}

// deleteRoutePermissionCandidateCache 精确删除路由候选权限缓存，避免使用 route_permission_ids:* 前缀 SCAN。
func (l *AdminPermissionLogic) deleteRoutePermissionCandidateCache(routeAliases ...string) {
	aliases := l.routePermissionCandidateAliases(routeAliases...)
	cacheKeys := make([]string, 0, len(aliases)+1)
	for _, alias := range aliases {
		cacheKeys = append(cacheKeys, fmt.Sprintf(keys.RoutePermissionIDs, alias))
	}
	cacheKeys = append(cacheKeys, keys.RoutePermissionAliasIndex)
	deleteRedisKeysExactBatches(l.BaseLogic, "AdminPermissionLogic.deleteRoutePermissionCandidateCache 删除路由候选权限缓存", tableCachePhysicalAndLegacyKeys(l.BaseLogic, cacheKeys...))
}

// routePermissionCandidateAliases 合并显式变更模块、已访问索引和当前权限模块，覆盖正向与空值缓存。
func (l *AdminPermissionLogic) routePermissionCandidateAliases(routeAliases ...string) []string {
	aliases := make([]string, 0, len(routeAliases))
	aliases = append(aliases, routeAliases...)
	if l.Redis() != nil {
		for _, indexKey := range tableCachePhysicalAndLegacyKeys(l.BaseLogic, keys.RoutePermissionAliasIndex) {
			indexedAliases, err := l.Redis().SMembers(l.Context(), indexKey).Result()
			if err != nil {
				logWrappedError(l, err, "AdminPermissionLogic.routePermissionCandidateAliases 读取路由候选权限索引失败 key=%s", indexKey)
				continue
			}
			aliases = append(aliases, indexedAliases...)
		}
	}
	readDB, err := tableCacheReadDB(l.BaseLogic, svc.DatabaseMain, "main")
	if err != nil {
		logWrappedError(l, err, "AdminPermissionLogic.routePermissionCandidateAliases 获取admin读库失败")
		return helper.UniqueNonEmptyStrings(aliases)
	}
	var modules []string
	// 当前启用权限 module 可枚举出正在使用的路由候选缓存；显式参数负责覆盖被删除或被禁用的旧 module。
	if err := readDB.WithContext(l.Context()).
		Model(&model.AdminPermission{}).
		Select("module").
		Where("status = 1").
		Where("module <> ''").
		Order("id ASC").
		Pluck("module", &modules).Error; err != nil {
		logWrappedError(l, err, "AdminPermissionLogic.routePermissionCandidateAliases 查询启用权限模块失败")
		return helper.UniqueNonEmptyStrings(aliases)
	}
	aliases = append(aliases, modules...)
	return helper.UniqueNonEmptyStrings(aliases)
}
