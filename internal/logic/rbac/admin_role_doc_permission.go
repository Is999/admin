package rbac

import (
	"fmt"
	"time"

	keys "admin/common/rediskeys"
	"admin/helper"
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	"admin/internal/model"
	"admin/internal/routealias"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	tablecache "github.com/Is999/table-cache"
	"gorm.io/gorm"
)

// rolePermissionSelection 表示一次角色保存中的两类权限选择。
type rolePermissionSelection struct {
	RoutePermissionIDs []int // 正常路由权限 ID
	DocPermissionIDs   []int // 文档权限 ID
}

// retainRolePermissionSelectionInScopeTx 在事务内按目标角色父级边界裁剪两类权限。
func (l *AdminRoleLogic) retainRolePermissionSelectionInScopeTx(tx *gorm.DB, roleID int, routePermissionIDs []int, docPermissionIDs []int) (rolePermissionSelection, error) {
	var role model.AdminRole
	if err := freshTxStatement(tx).Where("id = ? AND is_delete = 0", roleID).First(&role).Error; err != nil {
		return rolePermissionSelection{}, errors.Tag(err)
	}
	return l.retainRolePermissionSelectionWithinParentScopeTx(tx, role.Pid, routePermissionIDs, docPermissionIDs)
}

// retainRolePermissionSelectionWithinParentScopeTx 在事务内按直接父角色边界裁剪两类权限。
func (l *AdminRoleLogic) retainRolePermissionSelectionWithinParentScopeTx(tx *gorm.DB, parentRoleID int, routePermissionIDs []int, docPermissionIDs []int) (rolePermissionSelection, error) {
	isSuperRole, err := l.currentOperatorIsSuperRoleTx(tx)
	if err != nil {
		return rolePermissionSelection{}, errors.Tag(err)
	}
	allowedRoutePermissionIDs, err := l.allowedPermissionIDsForParentRoleTx(tx, parentRoleID, isSuperRole)
	if err != nil {
		return rolePermissionSelection{}, errors.Tag(err)
	}
	allowedDocPermissionIDs, err := l.allowedDocPermissionIDsForParentRoleTx(tx, parentRoleID, isSuperRole)
	if err != nil {
		return rolePermissionSelection{}, errors.Tag(err)
	}
	return l.normalizeRolePermissionSelection(
		tx,
		routePermissionIDs,
		docPermissionIDs,
		allowedRoutePermissionIDs,
		allowedDocPermissionIDs,
	)
}

// normalizeRolePermissionSelection 补齐文档入口路由，再分别按两类权限边界裁剪。
func (l *AdminRoleLogic) normalizeRolePermissionSelection(db *gorm.DB, routePermissionIDs []int, docPermissionIDs []int, allowedRoutePermissionIDs []int, allowedDocPermissionIDs []int) (rolePermissionSelection, error) {
	docPermissionIDs = retainAssignablePermissionIDs(docPermissionIDs, allowedDocPermissionIDs)
	docSites, err := l.enabledDocPermissionSites(db, docPermissionIDs)
	if err != nil {
		return rolePermissionSelection{}, errors.Tag(err)
	}
	entryPermissionIDs, err := l.enabledDocEntryPermissionIDs(db, docSites)
	if err != nil {
		return rolePermissionSelection{}, errors.Tag(err)
	}
	for _, permissionID := range entryPermissionIDs {
		routePermissionIDs = append(routePermissionIDs, permissionID)
	}
	routePermissionIDs, err = l.normalizeAssignablePermissionIDs(db, routePermissionIDs, allowedRoutePermissionIDs)
	if err != nil {
		return rolePermissionSelection{}, errors.Tag(err)
	}

	return rolePermissionSelection{
		RoutePermissionIDs: types.UniquePositiveInts(routePermissionIDs),
		DocPermissionIDs:   retainDocPermissionsWithEntries(docPermissionIDs, docSites, entryPermissionIDs, routePermissionIDs),
	}, nil
}

// retainDocPermissionsWithEntries 只保留入口路由仍在最终授权中的文档权限。
func retainDocPermissionsWithEntries(docPermissionIDs []int, docSites map[int]string, entryPermissionIDs map[string]int, routePermissionIDs []int) []int {
	routePermissionSet := make(map[int]struct{}, len(routePermissionIDs))
	for _, permissionID := range routePermissionIDs {
		routePermissionSet[permissionID] = struct{}{}
	}
	result := make([]int, 0, len(docPermissionIDs))
	for _, docPermissionID := range types.UniquePositiveInts(docPermissionIDs) {
		entryPermissionID := entryPermissionIDs[docSites[docPermissionID]]
		if entryPermissionID <= 0 {
			continue
		}
		if _, ok := routePermissionSet[entryPermissionID]; ok {
			result = append(result, docPermissionID)
		}
	}
	return types.UniquePositiveInts(result)
}

// enabledDocPermissionSites 查询启用文档权限所属站点，返回 docPermissionID 到 site 的映射。
func (l *AdminRoleLogic) enabledDocPermissionSites(db *gorm.DB, docPermissionIDs []int) (map[int]string, error) {
	docPermissionIDs = types.UniquePositiveInts(docPermissionIDs)
	result := make(map[int]string, len(docPermissionIDs))
	if len(docPermissionIDs) == 0 {
		return result, nil
	}
	type docSiteRow struct {
		ID   int    `gorm:"column:id"`   // 文档权限 ID
		Site string `gorm:"column:site"` // 文档站点
	}
	var rows []docSiteRow
	if err := freshTxStatement(db).
		Model(&model.AdminDocPermission{}).
		Select("id, site").
		Where("id IN ? AND status = ?", docPermissionIDs, 1).
		Order("id ASC").
		Scan(&rows).Error; err != nil {
		return nil, errors.Wrapf(err, "AdminRoleLogic.enabledDocPermissionSites 查询文档权限站点失败 docPermissionIDs=%v", docPermissionIDs)
	}
	for _, row := range rows {
		result[row.ID] = row.Site
	}
	return result, nil
}

// enabledDocEntryPermissionIDs 查询每个文档站点对应的启用入口路由权限 ID。
func (l *AdminRoleLogic) enabledDocEntryPermissionIDs(db *gorm.DB, docSites map[int]string) (map[string]int, error) {
	modules := make([]string, 0, 2)
	for _, site := range docSites {
		switch site {
		case routealias.DocSiteAdmin:
			modules = append(modules, string(routealias.DocsIndex))
		case routealias.DocSiteAPI:
			modules = append(modules, string(routealias.DocsAPIServiceIndex))
		}
	}
	modules = helper.UniqueNonEmptyStrings(modules)
	result := make(map[string]int, len(modules))
	if len(modules) == 0 {
		return result, nil
	}
	type entryPermissionRow struct {
		ID     int    `gorm:"column:id"`     // 入口权限 ID
		Module string `gorm:"column:module"` // 入口权限模块
	}
	var rows []entryPermissionRow
	if err := freshTxStatement(db).
		Model(&model.AdminPermission{}).
		Select("id, module").
		Where("module IN ? AND status = ?", modules, 1).
		Order("id ASC").
		Scan(&rows).Error; err != nil {
		return nil, errors.Wrap(err, "AdminRoleLogic.enabledDocEntryPermissionIDs 查询文档入口权限失败")
	}
	for _, row := range rows {
		switch routealias.Alias(row.Module) {
		case routealias.DocsIndex:
			result[routealias.DocSiteAdmin] = row.ID
		case routealias.DocsAPIServiceIndex:
			result[routealias.DocSiteAPI] = row.ID
		}
	}
	return result, nil
}

// allowedDocPermissionIDsForParentRole 根据父角色计算可分配的文档权限集合。
func (l *AdminRoleLogic) allowedDocPermissionIDsForParentRole(parentRoleID int) ([]int, error) {
	isSuperRole, err := l.CurrentOperatorIsSuperRole()
	if err != nil {
		return nil, errors.Tag(err)
	}
	if parentRoleUsesFullPermissionScope(parentRoleID, isSuperRole) {
		return l.allEnabledDocPermissionIDs()
	}
	return l.roleDocPermissionIDs(parentRoleID)
}

// docPermissionTreeAssignScope 计算角色文档权限列表的可操作范围。
func (l *AdminRoleLogic) docPermissionTreeAssignScope(roleID int) ([]int, bool, error) {
	if roleID == corelogic.AdminSuperRoleID {
		permissionIDs, err := l.allEnabledDocPermissionIDs()
		return permissionIDs, true, errors.Tag(err)
	}
	if err := l.EnsureRolesWithinManageScope([]int{roleID}); err != nil {
		if errors.Is(err, ErrRoleManageScopeExceeded) {
			return []int{}, true, nil
		}
		return nil, false, errors.Tag(err)
	}
	var role model.AdminRole
	if err := l.Svc.ReadDB(svc.DatabaseMain).Where("id = ? AND is_delete = 0", roleID).First(&role).Error; err != nil {
		return nil, false, errors.Tag(err)
	}
	permissionIDs, err := l.allowedDocPermissionIDsForParentRole(role.Pid)
	return permissionIDs, false, errors.Tag(err)
}

// loadDocPermissionItems 加载角色授权页的全部文档权限节点。
func (l *AdminRoleLogic) loadDocPermissionItems(checkedPermissionIDs []int, assignablePermissionIDs []int, lockAll bool) ([]types.AdminDocPermissionItem, error) {
	permissions, err := l.loadDocPermissionsWithCache()
	if err != nil {
		return nil, errors.Tag(err)
	}
	checkedSet := make(map[int]struct{}, len(checkedPermissionIDs))
	for _, permissionID := range checkedPermissionIDs {
		checkedSet[permissionID] = struct{}{}
	}
	assignableSet := make(map[int]struct{}, len(assignablePermissionIDs))
	for _, permissionID := range assignablePermissionIDs {
		assignableSet[permissionID] = struct{}{}
	}
	items := make([]types.AdminDocPermissionItem, 0, len(permissions))
	for _, permission := range permissions {
		_, checked := checkedSet[permission.ID]
		_, assignable := assignableSet[permission.ID]
		usable := permission.Status == 1 && assignable && !lockAll
		items = append(items, types.AdminDocPermissionItem{
			ID:              permission.ID,
			Site:            permission.Site,
			Path:            permission.Path,
			Title:           permission.Title,
			Description:     permission.Description,
			Status:          permission.Status,
			Checked:         checked,
			Disabled:        !usable,
			DisableCheckbox: !usable,
			Selectable:      usable,
		})
	}
	return items, nil
}

// loadDocPermissionsWithCache 复用文档权限管理与鉴权共用的缓存读取。
func (l *AdminRoleLogic) loadDocPermissionsWithCache() ([]model.AdminDocPermission, error) {
	return loadDocPermissionsWithCache(l.BaseLogic)
}

// roleDocPermissionIDs 查询单个角色当前启用的文档权限 ID。
func (l *AdminRoleLogic) roleDocPermissionIDs(roleID int) ([]int, error) {
	permissionIDs, err := l.roleDocPermissionRelationIDsWithCache(roleID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	permissions, err := l.loadDocPermissionsWithCache()
	if err != nil {
		return nil, errors.Tag(err)
	}
	enabled := make(map[int]struct{}, len(permissions))
	for _, permission := range permissions {
		if permission.ID > 0 && permission.Status == 1 {
			enabled[permission.ID] = struct{}{}
		}
	}
	result := make([]int, 0, len(permissionIDs))
	for _, permissionID := range permissionIDs {
		if _, ok := enabled[permissionID]; ok {
			result = append(result, permissionID)
		}
	}
	return types.UniquePositiveInts(result), nil
}

// roleDocPermissionRelationIDsWithCache 优先读取单个角色的原始文档权限关系缓存。
func (l *AdminRoleLogic) roleDocPermissionRelationIDsWithCache(roleID int) ([]int, error) {
	if roleID <= 0 {
		return []int{}, nil
	}
	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	cacheKey := fmt.Sprintf(keys.RoleDocPermission, roleID)
	var values []string
	result, err := manager.LoadThrough(l.Ctx, cachelogic.TableCachePhysicalKey(l.BaseLogic, cacheKey), &values, nil)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if result.State == tablecache.LookupStateEmpty {
		return []int{}, nil
	}
	return cachelogic.ParsePositiveIntStrings(values, "角色文档权限缓存")
}

// roleDocPermissionIDsTx 在事务内读取角色当前的文档权限关系。
func (l *AdminRoleLogic) roleDocPermissionIDsTx(tx *gorm.DB, roleID int) ([]int, error) {
	if roleID <= 0 {
		return []int{}, nil
	}
	var permissionIDs []int
	if err := freshTxStatement(tx).
		Model(&model.AdminRoleDocPermissionRel{}).
		Where("role_id = ?", roleID).
		Order("doc_permission_id ASC").
		Pluck("doc_permission_id", &permissionIDs).Error; err != nil {
		return nil, errors.Wrapf(err, "AdminRoleLogic.roleDocPermissionIDsTx 查询角色 ID[%d]文档权限失败", roleID)
	}
	return types.UniquePositiveInts(permissionIDs), nil
}

// allEnabledDocPermissionIDs 查询全部启用文档权限 ID。
func (l *AdminRoleLogic) allEnabledDocPermissionIDs() ([]int, error) {
	permissions, err := l.loadDocPermissionsWithCache()
	if err != nil {
		return nil, errors.Tag(err)
	}
	permissionIDs := make([]int, 0, len(permissions))
	for _, permission := range permissions {
		if permission.Status == 1 {
			permissionIDs = append(permissionIDs, permission.ID)
		}
	}
	return types.UniquePositiveInts(permissionIDs), nil
}

// syncRoleDocPermissionDelta 增量保存文档权限，并把父角色撤销项同步清理到全部子孙角色。
func (l *AdminRoleLogic) syncRoleDocPermissionDelta(db *gorm.DB, roleID int, nextPermissionIDs []int, affectedRoleSet map[int]struct{}) error {
	if err := l.ensureRoleExistsTx(db, roleID); err != nil {
		return errors.Tag(err)
	}
	if err := l.ensureDocPermissionsUsableTx(db, nextPermissionIDs); err != nil {
		return errors.Tag(err)
	}
	currentPermissionIDs, err := l.roleDocPermissionIDsTx(db, roleID)
	if err != nil {
		return errors.Tag(err)
	}
	addedPermissionIDs, removedPermissionIDs := diffPermissionIDs(currentPermissionIDs, nextPermissionIDs)
	if len(addedPermissionIDs) == 0 && len(removedPermissionIDs) == 0 {
		return nil
	}
	if len(removedPermissionIDs) > 0 {
		descendantRoleIDs, err := l.descendantRoleIDsByDB(db, roleID)
		if err != nil {
			return errors.Tag(err)
		}
		if err := l.deleteRoleDocPermissionsByRoleIDs(db, descendantRoleIDs, removedPermissionIDs); err != nil {
			return errors.Tag(err)
		}
		for _, descendantRoleID := range descendantRoleIDs {
			affectedRoleSet[descendantRoleID] = struct{}{}
		}
	}
	if err := l.deleteRoleDocPermissionsByRoleIDs(db, []int{roleID}, removedPermissionIDs); err != nil {
		return errors.Tag(err)
	}
	return l.appendRoleDocPermissions(db, roleID, addedPermissionIDs)
}

// deleteRoleDocPermissionsByRoleIDs 批量删除角色文档权限关系。
func (l *AdminRoleLogic) deleteRoleDocPermissionsByRoleIDs(db *gorm.DB, roleIDs []int, permissionIDs []int) error {
	roleIDs = types.UniquePositiveInts(roleIDs)
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if len(roleIDs) == 0 || len(permissionIDs) == 0 {
		return nil
	}
	if err := freshTxStatement(db).
		Where("role_id IN ? AND doc_permission_id IN ?", roleIDs, permissionIDs).
		Delete(&model.AdminRoleDocPermissionRel{}).Error; err != nil {
		return errors.Wrapf(err, "AdminRoleLogic.deleteRoleDocPermissionsByRoleIDs 删除角色文档权限失败 roleIDs=%v docPermissionIDs=%v", roleIDs, permissionIDs)
	}
	return nil
}

// appendRoleDocPermissions 增量写入角色新增的文档权限关系。
func (l *AdminRoleLogic) appendRoleDocPermissions(db *gorm.DB, roleID int, permissionIDs []int) error {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if roleID <= 0 || len(permissionIDs) == 0 {
		return nil
	}
	if err := l.ensureDocPermissionsUsableTx(db, permissionIDs); err != nil {
		return errors.Tag(err)
	}
	rows := make([]model.AdminRoleDocPermissionRel, 0, len(permissionIDs))
	now := time.Now()
	for _, permissionID := range permissionIDs {
		rows = append(rows, model.AdminRoleDocPermissionRel{RoleID: roleID, DocPermissionID: permissionID, CreatedAt: now})
	}
	if err := freshTxStatement(db).Create(&rows).Error; err != nil {
		return errors.Wrapf(err, "AdminRoleLogic.appendRoleDocPermissions 写入角色 ID[%d]新增文档权限失败 docPermissionIDs=%v", roleID, permissionIDs)
	}
	return nil
}

// replaceRoleDocPermissionsTx 在事务内覆盖角色文档权限关系。
func (l *AdminRoleLogic) replaceRoleDocPermissionsTx(tx *gorm.DB, roleID int, permissionIDs []int) error {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if err := l.ensureDocPermissionsUsableTx(tx, permissionIDs); err != nil {
		return errors.Tag(err)
	}
	if err := freshTxStatement(tx).Where("role_id = ?", roleID).Delete(&model.AdminRoleDocPermissionRel{}).Error; err != nil {
		return errors.Wrapf(err, "AdminRoleLogic.replaceRoleDocPermissionsTx 清理角色 ID[%d]文档权限失败", roleID)
	}
	return l.appendRoleDocPermissions(tx, roleID, permissionIDs)
}

// ensureDocPermissionsUsableTx 确认文档权限 ID 均存在且启用。
func (l *AdminRoleLogic) ensureDocPermissionsUsableTx(tx *gorm.DB, permissionIDs []int) error {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if len(permissionIDs) == 0 {
		return nil
	}
	var enabledPermissionIDs []int
	if err := freshTxStatement(tx).
		Model(&model.AdminDocPermission{}).
		Where("id IN ? AND status = ?", permissionIDs, 1).
		Order("id ASC").
		Pluck("id", &enabledPermissionIDs).Error; err != nil {
		return errors.Wrapf(err, "AdminRoleLogic.ensureDocPermissionsUsableTx 检查文档权限可用性失败 docPermissionIDs=%v", permissionIDs)
	}
	enabledPermissionIDs = types.UniquePositiveInts(enabledPermissionIDs)
	if len(enabledPermissionIDs) != len(permissionIDs) {
		return errors.Wrapf(errRolePermissionUnusable,
			"AdminRoleLogic.ensureDocPermissionsUsableTx 存在不可用文档权限 invalidDocPermissionIDs=%v docPermissionIDs=%v",
			subtractSortedInts(permissionIDs, enabledPermissionIDs), permissionIDs,
		)
	}
	return nil
}

// reconcileRoleDocPermissionScopeTreeTx 递归收敛目标角色及其全部子孙角色的文档权限范围。
func (l *AdminRoleLogic) reconcileRoleDocPermissionScopeTreeTx(tx *gorm.DB, roleID int, isSuperRole bool, affectedRoleSet map[int]struct{}) error {
	if roleID <= 0 {
		return nil
	}
	roleTree, childRoleMap, err := l.roleScopeTreeTx(tx, roleID)
	if err != nil {
		return errors.Tag(err)
	}
	rootRole, ok := roleTree[roleID]
	if !ok {
		return errors.Errorf("AdminRoleLogic.reconcileRoleDocPermissionScopeTreeTx 角色 ID[%d]不存在", roleID)
	}
	permissionMap, err := l.roleDocPermissionMapTx(tx, roleIDSetToSliceMap(roleTree))
	if err != nil {
		return errors.Tag(err)
	}
	rootAllowedPermissionIDs, err := l.allowedDocPermissionIDsForParentRoleTx(tx, rootRole.Pid, isSuperRole)
	if err != nil {
		return errors.Tag(err)
	}
	var reconcile func(currentRoleID int, allowedPermissionIDs []int) error
	reconcile = func(currentRoleID int, allowedPermissionIDs []int) error {
		currentPermissionIDs := types.UniquePositiveInts(permissionMap[currentRoleID])
		nextPermissionIDs := retainAssignablePermissionIDs(currentPermissionIDs, allowedPermissionIDs)
		if !intSlicesEqual(currentPermissionIDs, nextPermissionIDs) {
			if err := l.replaceRoleDocPermissionsTx(tx, currentRoleID, nextPermissionIDs); err != nil {
				return errors.Tag(err)
			}
			permissionMap[currentRoleID] = nextPermissionIDs
			affectedRoleSet[currentRoleID] = struct{}{}
		}
		for _, childRoleID := range childRoleMap[currentRoleID] {
			if err := reconcile(childRoleID, permissionMap[currentRoleID]); err != nil {
				return errors.Tag(err)
			}
		}
		return nil
	}
	return reconcile(roleID, rootAllowedPermissionIDs)
}

// roleDocPermissionMapTx 在事务内批量读取角色文档权限关系。
func (l *AdminRoleLogic) roleDocPermissionMapTx(tx *gorm.DB, roleIDs []int) (map[int][]int, error) {
	result := make(map[int][]int, len(roleIDs))
	roleIDs = types.UniquePositiveInts(roleIDs)
	if len(roleIDs) == 0 {
		return result, nil
	}
	var rows []model.AdminRoleDocPermissionRel
	if err := freshTxStatement(tx).
		Where("role_id IN ?", roleIDs).
		Order("role_id ASC, doc_permission_id ASC").
		Find(&rows).Error; err != nil {
		return nil, errors.Wrapf(err, "AdminRoleLogic.roleDocPermissionMapTx 查询角色文档权限失败 roleIDs=%v", roleIDs)
	}
	for _, row := range rows {
		result[row.RoleID] = append(result[row.RoleID], row.DocPermissionID)
	}
	for _, roleID := range roleIDs {
		result[roleID] = types.UniquePositiveInts(result[roleID])
	}
	return result, nil
}

// allowedDocPermissionIDsForParentRoleTx 在事务内计算父角色允许子角色保留的文档权限范围。
func (l *AdminRoleLogic) allowedDocPermissionIDsForParentRoleTx(tx *gorm.DB, parentRoleID int, isSuperRole bool) ([]int, error) {
	if parentRoleUsesFullPermissionScope(parentRoleID, isSuperRole) {
		var permissionIDs []int
		if err := freshTxStatement(tx).
			Model(&model.AdminDocPermission{}).
			Where("status = ?", 1).
			Order("id ASC").
			Pluck("id", &permissionIDs).Error; err != nil {
			return nil, errors.Tag(err)
		}
		return types.UniquePositiveInts(permissionIDs), nil
	}
	var permissionIDs []int
	if parentRoleID <= 0 {
		return permissionIDs, nil
	}
	if err := freshTxStatement(tx).
		Table(model.TableNameAdminRoleDocPermissionRel+" AS rel").
		Select("rel.doc_permission_id").
		Joins("JOIN "+model.TableNameAdminDocPermission+" AS doc ON doc.id = rel.doc_permission_id AND doc.status = 1").
		Where("rel.role_id = ?", parentRoleID).
		Order("rel.doc_permission_id ASC").
		Pluck("rel.doc_permission_id", &permissionIDs).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return types.UniquePositiveInts(permissionIDs), nil
}
