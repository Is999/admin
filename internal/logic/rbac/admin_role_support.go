package rbac

import (
	"fmt"
	"strconv"
	"strings"
	"time"

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

// roleIDSetToSliceMap 把角色映射的 key 转成稳定切片，供批量查询角色权限关系。
func roleIDSetToSliceMap(roleMap map[int]model.AdminRole) []int {
	roleIDs := make([]int, 0, len(roleMap))
	for roleID := range roleMap {
		if roleID > 0 {
			roleIDs = append(roleIDs, roleID)
		}
	}
	return types.UniquePositiveInts(roleIDs)
}

// diffPermissionIDs 计算目标权限相对当前权限的新增项和删除项，便于按最小改动落库。
func diffPermissionIDs(currentPermissionIDs []int, nextPermissionIDs []int) ([]int, []int) {
	currentPermissionIDs = types.UniquePositiveInts(currentPermissionIDs)
	nextPermissionIDs = types.UniquePositiveInts(nextPermissionIDs)

	currentSet := make(map[int]struct{}, len(currentPermissionIDs))
	nextSet := make(map[int]struct{}, len(nextPermissionIDs))
	for _, permissionID := range currentPermissionIDs {
		currentSet[permissionID] = struct{}{}
	}
	for _, permissionID := range nextPermissionIDs {
		nextSet[permissionID] = struct{}{}
	}

	addedPermissionIDs := make([]int, 0)
	for _, permissionID := range nextPermissionIDs {
		if _, ok := currentSet[permissionID]; ok {
			continue
		}
		addedPermissionIDs = append(addedPermissionIDs, permissionID)
	}

	removedPermissionIDs := make([]int, 0)
	for _, permissionID := range currentPermissionIDs {
		if _, ok := nextSet[permissionID]; ok {
			continue
		}
		removedPermissionIDs = append(removedPermissionIDs, permissionID)
	}
	return addedPermissionIDs, removedPermissionIDs
}

// descendantRoleIDs 查询指定角色的全部未删除子孙角色 ID，供父权限收缩时批量清理下级越权权限。
func (l *AdminRoleLogic) descendantRoleIDs(roleID int) ([]int, error) {
	return l.descendantRoleIDsByDB(l.Svc.WriteDB(svc.DatabaseMain), roleID)
}

// descendantRoleIDsByDB 基于指定数据库句柄查询全部子孙角色，供事务内外统一复用。
func (l *AdminRoleLogic) descendantRoleIDsByDB(db *gorm.DB, roleID int) ([]int, error) {
	var roleIDs []int
	if roleID <= 0 {
		return []int{}, nil
	}
	err := freshTxStatement(db).Model(&model.AdminRole{}).
		Where("is_delete = 0").
		Where("FIND_IN_SET(?, pids)", roleID).
		Order("id ASC").
		Pluck("id", &roleIDs).Error
	if err != nil {
		return nil, errors.Wrapf(err, "AdminRoleLogic.descendantRoleIDs 查询角色 ID[%d]子孙角色失败", roleID)
	}
	return types.UniquePositiveInts(roleIDs), nil
}

// syncRolePermissionDelta 按“先清子级、后改父级”的顺序同步当前角色权限变更。
// 该方法会先做完整预检，再执行最小增量写入，兼顾安全边界、性能和可观测性。
func (l *AdminRoleLogic) syncRolePermissionDelta(db *gorm.DB, roleID int, nextPermissionIDs []int, affectedRoleSet map[int]struct{}) error {
	if err := l.ensureRoleExistsTx(db, roleID); err != nil {
		return errors.Tag(err)
	}
	if err := l.ensurePermissionsUsableTx(db, nextPermissionIDs); err != nil {
		return errors.Tag(err)
	}
	currentPermissionIDs, err := l.rolePermissionIDsTx(db, roleID)
	if err != nil {
		return errors.Tag(err)
	}
	addedPermissionIDs, removedPermissionIDs := diffPermissionIDs(currentPermissionIDs, nextPermissionIDs)
	if len(addedPermissionIDs) == 0 && len(removedPermissionIDs) == 0 {
		return nil
	}

	// 先清理所有子孙角色中被父角色取消的权限，保证父角色真正落库前，下级已经失去对应权限。
	if len(removedPermissionIDs) > 0 {
		descendantRoleIDs, err := l.descendantRoleIDsByDB(db, roleID)
		if err != nil {
			return errors.Tag(err)
		}
		if err := l.deleteRolePermissionsByRoleIDs(db, descendantRoleIDs, removedPermissionIDs); err != nil {
			return errors.Tag(err)
		}
		for _, descendantRoleID := range descendantRoleIDs {
			affectedRoleSet[descendantRoleID] = struct{}{}
		}
	}

	// 子孙角色先完成收缩后，再落当前父角色自身的权限删除，避免出现父已删而子未删的越权窗口。
	if err := l.deleteRolePermissionsByRoleIDs(db, []int{roleID}, removedPermissionIDs); err != nil {
		return errors.Tag(err)
	}
	if err := l.appendRolePermissions(db, roleID, addedPermissionIDs); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// deleteRolePermissionsByRoleIDs 批量删除指定角色集合上的指定权限，供父级权限收缩时快速下推到子孙角色。
func (l *AdminRoleLogic) deleteRolePermissionsByRoleIDs(db *gorm.DB, roleIDs []int, permissionIDs []int) error {
	roleIDs = types.UniquePositiveInts(roleIDs)
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if len(roleIDs) == 0 || len(permissionIDs) == 0 {
		return nil
	}
	if err := freshTxStatement(db).
		Where("role_id IN ? AND permission_id IN ?", roleIDs, permissionIDs).
		Delete(&model.AdminRolePermissionRel{}).Error; err != nil {
		return errors.Wrapf(err, "AdminRoleLogic.deleteRolePermissionsByRoleIDs 删除角色权限失败 roleIDs=%v permissionIDs=%v", roleIDs, permissionIDs)
	}
	return nil
}

// appendRolePermissions 按增量补写当前角色新增权限，避免每次保存都整表删除重建。
func (l *AdminRoleLogic) appendRolePermissions(db *gorm.DB, roleID int, permissionIDs []int) error {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if roleID <= 0 || len(permissionIDs) == 0 {
		return nil
	}
	if err := l.ensurePermissionsUsableTx(db, permissionIDs); err != nil {
		return errors.Tag(err)
	}
	rows := make([]model.AdminRolePermissionRel, 0, len(permissionIDs))
	now := time.Now()
	for _, permissionID := range permissionIDs {
		rows = append(rows, model.AdminRolePermissionRel{
			RoleID:       int64(roleID),
			PermissionID: int64(permissionID),
			CreatedAt:    now,
		})
	}
	if err := freshTxStatement(db).Create(&rows).Error; err != nil {
		return errors.Wrapf(err, "AdminRoleLogic.appendRolePermissions 写入角色 ID[%d]新增权限失败 permissionIDs=%v", roleID, permissionIDs)
	}
	return nil
}

// filterEnabledRoleIDsWithCache 使用角色状态缓存过滤出启用且未删除的角色 ID。
func (l *AdminRoleLogic) filterEnabledRoleIDsWithCache(roleIDs []int) ([]int, error) {
	roleIDs = types.UniquePositiveInts(roleIDs)
	if len(roleIDs) == 0 {
		return []int{}, nil
	}
	statusMap, err := l.roleStatusMapWithCache(roleIDs)
	if err != nil {
		return nil, errors.Tag(err)
	}
	result := make([]int, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		if statusMap[roleID] == 1 {
			result = append(result, roleID)
		}
	}
	return result, nil
}

// roleStatusMapWithCache 批量读取角色状态缓存，未命中时自动重建缓存后再回读。
func (l *AdminRoleLogic) roleStatusMapWithCache(roleIDs []int) (map[int]int, error) {
	roleIDs = types.UniquePositiveInts(roleIDs)
	result := make(map[int]int, len(roleIDs))
	if len(roleIDs) == 0 {
		return result, nil
	}
	if l.Redis() == nil {
		return l.roleStatusMap(roleIDs)
	}
	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	var cache map[string]string
	if _, err = manager.LoadThrough(l.Ctx, cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.RoleStatus), &cache, nil); err != nil {
		return nil, errors.Tag(err)
	}
	for _, roleID := range roleIDs {
		statusText := strings.TrimSpace(cache[strconv.Itoa(roleID)])
		if statusText == "" || corelogic.CacheIsEmptyMarker(statusText) {
			continue
		}
		status, convErr := strconv.Atoi(statusText)
		if convErr != nil {
			return nil, errors.Wrap(convErr, "解析角色状态缓存失败")
		}
		result[roleID] = status
	}
	return result, nil
}

// roleStatusMap 直接从数据库读取角色状态，作为缓存 miss 后的最终兜底。
func (l *AdminRoleLogic) roleStatusMap(roleIDs []int) (map[int]int, error) {
	roleIDs = types.UniquePositiveInts(roleIDs)
	result := make(map[int]int, len(roleIDs))
	if len(roleIDs) == 0 {
		return result, nil
	}
	type row struct {
		ID     int // 角色 ID
		Status int // 角色状态
	}
	rows := make([]row, 0, len(roleIDs))
	err := l.Svc.ReadDB(svc.DatabaseMain).Model(&model.AdminRole{}).
		Select("id, status").
		Where("id IN ? AND is_delete = 0", roleIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, errors.Tag(err)
	}
	for _, row := range rows {
		result[row.ID] = row.Status
	}
	return result, nil
}

// rolePermissionIDsWithCache 优先读取单角色权限集合缓存，未命中时自动回源并重建。
func (l *AdminRoleLogic) rolePermissionIDsWithCache(roleID int) ([]int, error) {
	if roleID <= 0 {
		return nil, nil
	}
	if l.Redis() == nil {
		return l.rolePermissionIDs(roleID)
	}
	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	cacheKey := fmt.Sprintf(keys.RolePermission, roleID)
	var values []string
	result, err := manager.LoadThrough(l.Ctx, cachelogic.TableCachePhysicalKey(l.BaseLogic, cacheKey), &values, nil)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if result.State == tablecache.LookupStateEmpty {
		return []int{}, nil
	}
	return cachelogic.ParsePositiveIntStrings(values, "角色权限缓存")
}

// RolePermissionIDsWithCache 查询角色启用权限 ID。
func (l *AdminRoleLogic) RolePermissionIDsWithCache(roleID int) ([]int, error) {
	return l.rolePermissionIDsWithCache(roleID)
}

// rolePidsTx 在事务内计算角色族谱。
func (l *AdminRoleLogic) rolePidsTx(tx *gorm.DB, pid int, selfID int) (string, error) {
	if pid <= 0 {
		return "", nil
	}
	if pid == selfID {
		return "", errors.Errorf("AdminRoleLogic.rolePidsTx 父级角色不能是自己 pid=%d selfID=%d", pid, selfID)
	}
	var parent model.AdminRole
	if err := tx.Where("id = ? AND is_delete = 0", pid).First(&parent).Error; err != nil {
		return "", errors.Wrapf(err, "AdminRoleLogic.rolePidsTx 查询父级角色 ID[%d]失败", pid)
	}
	if corelogic.ContainsTreeID(parent.Pids, selfID) {
		return "", errors.Errorf("AdminRoleLogic.rolePidsTx 不能把角色移动到自己的子级下面 pid=%d selfID=%d", pid, selfID)
	}
	return corelogic.BuildTreePids(parent.ID, parent.Pids), nil
}

// ensureRoleTitleUniqueTx 校验角色名称唯一。
func (l *AdminRoleLogic) ensureRoleTitleUniqueTx(tx *gorm.DB, title string, ignoreID int) error {
	var count int64
	query := tx.Model(&model.AdminRole{}).Where("title = ? AND is_delete = 0", strings.TrimSpace(title))
	if ignoreID > 0 {
		query = query.Where("id <> ?", ignoreID)
	}
	if err := query.Count(&count).Error; err != nil {
		return errors.Wrapf(err, "AdminRoleLogic.ensureRoleTitleUniqueTx 检查角色名称[%s]唯一失败", strings.TrimSpace(title))
	}
	if count > 0 {
		return errors.Wrapf(ErrRoleTitleAlreadyExists, "AdminRoleLogic.ensureRoleTitleUniqueTx 角色名称[%s]已存在", strings.TrimSpace(title))
	}
	return nil
}

// ensureRoleExistsTx 确认角色存在且未删除。
func (l *AdminRoleLogic) ensureRoleExistsTx(tx *gorm.DB, roleID int) error {
	var count int64
	if err := freshTxStatement(tx).Model(&model.AdminRole{}).Where("id = ? AND is_delete = 0", roleID).Count(&count).Error; err != nil {
		return errors.Wrapf(err, "AdminRoleLogic.ensureRoleExistsTx 检查角色 ID[%d]是否存在失败", roleID)
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// replaceRolePermissionsTx 在事务内覆盖角色权限关系。
func (l *AdminRoleLogic) replaceRolePermissionsTx(tx *gorm.DB, roleID int, permissionIDs []int) error {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if err := l.ensurePermissionsUsableTx(tx, permissionIDs); err != nil {
		return errors.Tag(err)
	}
	if err := tx.Where("role_id = ?", roleID).Delete(&model.AdminRolePermissionRel{}).Error; err != nil {
		return errors.Wrapf(err, "AdminRoleLogic.replaceRolePermissionsTx 清理角色 ID[%d]原权限失败", roleID)
	}
	if len(permissionIDs) == 0 {
		return nil
	}
	rows := make([]model.AdminRolePermissionRel, 0, len(permissionIDs))
	now := time.Now()
	for _, permissionID := range permissionIDs {
		rows = append(rows, model.AdminRolePermissionRel{
			RoleID:       int64(roleID),
			PermissionID: int64(permissionID),
			CreatedAt:    now,
		})
	}
	if err := tx.Create(&rows).Error; err != nil {
		return errors.Wrapf(err, "AdminRoleLogic.replaceRolePermissionsTx 写入角色 ID[%d]权限关系失败", roleID)
	}
	return nil
}

// ensurePermissionsUsableTx 确认权限 ID 均存在且未禁用。
func (l *AdminRoleLogic) ensurePermissionsUsableTx(tx *gorm.DB, permissionIDs []int) error {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if len(permissionIDs) == 0 {
		return nil
	}
	type permissionRow struct {
		ID int `gorm:"column:id"` // 权限 ID
	}
	rows := make([]permissionRow, 0, len(permissionIDs))
	if err := freshTxStatement(tx).Table(model.TableNameAdminPermission).
		Select("id").
		Where("id IN ?", permissionIDs).
		Where("status = ?", 1).
		Order("id ASC").
		Scan(&rows).Error; err != nil {
		return errors.Wrapf(err, "AdminRoleLogic.ensurePermissionsUsableTx 检查权限可用性失败 permissionIDs=%v", permissionIDs)
	}
	enabledPermissionIDs := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.ID > 0 {
			enabledPermissionIDs = append(enabledPermissionIDs, row.ID)
		}
	}
	if len(enabledPermissionIDs) != len(permissionIDs) {
		return errors.Wrapf(errRolePermissionUnusable,
			"AdminRoleLogic.ensurePermissionsUsableTx 存在不可用权限 invalidPermissionIDs=%v permissionIDs=%v",
			subtractSortedInts(permissionIDs, enabledPermissionIDs), permissionIDs,
		)
	}
	return nil
}

// subtractSortedInts 计算 left 相对 right 缺失的元素，供精确记录不可用权限 ID。
func subtractSortedInts(left []int, right []int) []int {
	rightSet := make(map[int]struct{}, len(right))
	for _, item := range right {
		rightSet[item] = struct{}{}
	}
	result := make([]int, 0)
	for _, item := range left {
		if _, ok := rightSet[item]; ok {
			continue
		}
		result = append(result, item)
	}
	return result
}

// refreshRoleRelatedCache 清理角色相关缓存，确保下一次读取能重建最新数据。
func (l *AdminRoleLogic) refreshRoleRelatedCache(roleIDs ...int) {
	roleIDs = types.UniquePositiveInts(roleIDs)
	adminIDs, err := l.adminIDsByRoleIDs(roleIDs)
	if err != nil {
		corelogic.LogWrappedError(l, err, "AdminRoleLogic.refreshRoleRelatedCache 查询受影响管理员失败 roleIDs=%v", roleIDs)
	}
	l.refreshRoleRelatedCacheByScope(roleIDs, adminIDs)
}

// refreshRoleRelatedCacheByScope 按角色与管理员影响范围精确清理缓存。
// 管理员角色/权限缓存必须按 adminID 精确删除，禁止前缀扫描。
func (l *AdminRoleLogic) refreshRoleRelatedCacheByScope(roleIDs []int, adminIDs []int) {
	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		corelogic.LogWrappedError(l, err, "AdminRoleLogic.refreshRoleRelatedCacheByScope 初始化表缓存管理器失败")
		manager = nil
	}
	roleCacheKeys := []string{keys.RoleTree, keys.RoleStatus}
	for _, roleID := range types.UniquePositiveInts(roleIDs) {
		roleCacheKeys = append(roleCacheKeys, fmt.Sprintf(keys.RolePermission, roleID))
	}
	if manager != nil {
		for _, cacheKey := range roleCacheKeys {
			physicalKey := cachelogic.TableCachePhysicalKey(l.BaseLogic, cacheKey)
			if err := manager.DeleteByKey(l.Ctx, physicalKey); err != nil && !cachelogic.IsTableCacheTargetNotFound(err) {
				corelogic.LogWrappedError(l, err, "AdminRoleLogic.refreshRoleRelatedCacheByScope 清理角色缓存key[%s]失败", cacheKey)
			}
		}
	}
	if l.Redis() != nil {
		if err := l.RdsDelKeys(cachelogic.TableCachePhysicalKeys(l.BaseLogic, roleCacheKeys...)...); err != nil {
			corelogic.LogWrappedError(l, err, "AdminRoleLogic.refreshRoleRelatedCacheByScope 兜底删除角色缓存失败 roleIDs=%v", roleIDs)
		}
	}
	cachelogic.InvalidateAdminRoleAndPermissionCacheByAdminIDs(l.BaseLogic, adminIDs...)
}
