package rbac

import (
	"fmt"
	"sort"
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

// permissionPathRow 表示权限节点及其祖先路径，供保存角色权限时补齐菜单父级。
type permissionPathRow struct {
	ID   int    `gorm:"column:id"`   // 权限 ID
	Pids string `gorm:"column:pids"` // 祖先权限 ID 串
}

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

// permissionPathIDs 解析权限祖先 ID 串。
func permissionPathIDs(pids string) []int {
	parts := strings.Split(strings.TrimSpace(pids), ",")
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		permissionID, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || permissionID <= 0 {
			continue
		}
		result = append(result, permissionID)
	}
	return types.UniquePositiveInts(result)
}

// expandPermissionAncestorIDsFromRows 根据权限族谱补齐祖先权限。
func expandPermissionAncestorIDsFromRows(permissionIDs []int, rows []permissionPathRow) []int {
	result := types.UniquePositiveInts(permissionIDs)
	seen := make(map[int]struct{}, len(result))
	for _, permissionID := range result {
		seen[permissionID] = struct{}{}
	}
	for _, row := range rows {
		for _, ancestorID := range permissionPathIDs(row.Pids) {
			if _, ok := seen[ancestorID]; ok {
				continue
			}
			seen[ancestorID] = struct{}{}
			result = append(result, ancestorID)
		}
	}
	sort.Ints(result)
	return result
}

// retainCompletePermissionPathIDsFromRows 移除缺少祖先权限的半截授权。
func retainCompletePermissionPathIDsFromRows(permissionIDs []int, rows []permissionPathRow) []int {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	requestedSet := make(map[int]struct{}, len(permissionIDs))
	for _, permissionID := range permissionIDs {
		requestedSet[permissionID] = struct{}{}
	}
	enabledSet := make(map[int]struct{}, len(rows))
	pidsByID := make(map[int]string, len(rows))
	for _, row := range rows {
		if row.ID > 0 {
			pidsByID[row.ID] = row.Pids
			if _, requested := requestedSet[row.ID]; requested {
				enabledSet[row.ID] = struct{}{}
			}
		}
	}

	result := make([]int, 0, len(permissionIDs))
	for _, permissionID := range permissionIDs {
		pids, ok := pidsByID[permissionID]
		if !ok {
			continue
		}
		complete := true
		for _, ancestorID := range permissionPathIDs(pids) {
			if _, ok := enabledSet[ancestorID]; !ok {
				complete = false
				break
			}
		}
		if complete {
			result = append(result, permissionID)
		}
	}
	sort.Ints(result)
	return result
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
	fields := make([]string, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		fields = append(fields, strconv.Itoa(roleID))
	}
	cache, err := cachelogic.StringHashFieldsWithCache(l.BaseLogic, keys.RoleStatus, fields)
	if err != nil {
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

// RolePermissionIDsWithCache 查询单个角色当前启用的路由权限 ID。
func (l *AdminRoleLogic) RolePermissionIDsWithCache(roleID int) ([]int, error) {
	permissionIDs, err := l.rolePermissionRelationIDsWithCache(roleID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return (&AdminPermissionLogic{BaseLogic: l.BaseLogic}).EnabledPermissionIDsWithCache(permissionIDs)
}

// rolePermissionRelationIDsWithCache 优先读取单个角色的原始路由权限关系缓存。
func (l *AdminRoleLogic) rolePermissionRelationIDsWithCache(roleID int) ([]int, error) {
	if roleID <= 0 {
		return nil, nil
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

// loadRoleHierarchyTx 在事务内读取未删除角色的轻量层级快照。
func (l *AdminRoleLogic) loadRoleHierarchyTx(tx *gorm.DB) ([]model.AdminRole, error) {
	var roles []model.AdminRole
	if err := freshTxStatement(tx).Model(&model.AdminRole{}).
		Select("id, pid, pids, status").
		Where("is_delete = 0").
		Order("id ASC").
		Find(&roles).Error; err != nil {
		return nil, errors.Wrap(err, "AdminRoleLogic.loadRoleHierarchyTx 查询角色层级失败")
	}
	return roles, nil
}

// disabledRolePathID 按真实 pid 链返回目标角色路径上首个禁用节点。
func disabledRolePathID(roles []model.AdminRole, roleID int) (int, error) {
	roleByID := make(map[int]model.AdminRole, len(roles))
	for _, role := range roles {
		roleByID[role.ID] = role
	}
	visited := make(map[int]struct{}, len(roles))
	for currentID := roleID; currentID > 0; {
		if _, ok := visited[currentID]; ok {
			return 0, errors.Errorf("角色父级链存在循环 ID[%d]", currentID)
		}
		visited[currentID] = struct{}{}
		role, ok := roleByID[currentID]
		if !ok {
			return 0, errors.Errorf("角色 ID[%d]不存在", currentID)
		}
		if role.Status != 1 {
			return role.ID, nil
		}
		currentID = role.Pid
	}
	return 0, nil
}

// disabledRolePathID 从主库返回目标角色路径上首个禁用节点。
func (l *AdminRoleLogic) disabledRolePathID(roleID int) (int, error) {
	if roleID <= 0 {
		return 0, nil
	}
	roles, err := l.loadRoleHierarchyTx(l.Svc.WriteDB(svc.DatabaseMain))
	if err != nil {
		return 0, errors.Tag(err)
	}
	return disabledRolePathID(roles, roleID)
}

// rolePidsFromHierarchy 根据真实 pid 链生成角色族谱，并拒绝自引用或循环父级。
func rolePidsFromHierarchy(roles []model.AdminRole, pid int, selfID int) (string, error) {
	if pid <= 0 {
		return "", nil
	}
	roleByID := make(map[int]model.AdminRole, len(roles))
	for _, role := range roles {
		roleByID[role.ID] = role
	}
	ancestorIDs := make([]int, 0)
	visited := make(map[int]struct{}, len(roles))
	for currentID := pid; currentID > 0; {
		if currentID == selfID {
			return "", errors.Errorf("父级角色不能是角色 ID[%d]自身或其子级", selfID)
		}
		if _, ok := visited[currentID]; ok {
			return "", errors.Errorf("角色父级链存在循环 ID[%d]", currentID)
		}
		visited[currentID] = struct{}{}
		role, ok := roleByID[currentID]
		if !ok {
			return "", errors.Errorf("父级角色 ID[%d]不存在", currentID)
		}
		ancestorIDs = append(ancestorIDs, currentID)
		currentID = role.Pid
	}
	for left, right := 0, len(ancestorIDs)-1; left < right; left, right = left+1, right-1 {
		ancestorIDs[left], ancestorIDs[right] = ancestorIDs[right], ancestorIDs[left]
	}
	parts := make([]string, 0, len(ancestorIDs))
	for _, roleID := range ancestorIDs {
		parts = append(parts, strconv.Itoa(roleID))
	}
	return strings.Join(parts, ","), nil
}

// descendantRolePids 根据 pid 邻接关系重建指定角色全部子孙的族谱。
func descendantRolePids(roles []model.AdminRole, rootID int) (map[int]string, error) {
	roleByID := make(map[int]model.AdminRole, len(roles))
	childrenByPID := make(map[int][]int, len(roles))
	for _, role := range roles {
		roleByID[role.ID] = role
		childrenByPID[role.Pid] = append(childrenByPID[role.Pid], role.ID)
	}
	root, ok := roleByID[rootID]
	if !ok {
		return nil, errors.Errorf("角色 ID[%d]不存在", rootID)
	}
	result := make(map[int]string)
	visited := map[int]struct{}{rootID: {}}
	var walk func(parentID int, parentPids string) error
	walk = func(parentID int, parentPids string) error {
		for _, childID := range childrenByPID[parentID] {
			if _, ok := visited[childID]; ok {
				return errors.Errorf("角色子树存在循环 ID[%d]", childID)
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

// rolePidsTx 在事务内按真实 pid 链计算角色族谱。
func (l *AdminRoleLogic) rolePidsTx(tx *gorm.DB, pid int, selfID int) (string, error) {
	if pid <= 0 {
		return "", nil
	}
	roles, err := l.loadRoleHierarchyTx(tx)
	if err != nil {
		return "", errors.Tag(err)
	}
	pids, err := rolePidsFromHierarchy(roles, pid, selfID)
	if err != nil {
		return "", errors.Wrapf(err, "AdminRoleLogic.rolePidsTx 计算父级角色 ID[%d]族谱失败", pid)
	}
	return pids, nil
}

// syncRoleDescendantPidsTx 在角色换父级后同步更新全部子孙族谱。
func (l *AdminRoleLogic) syncRoleDescendantPidsTx(tx *gorm.DB, roleID int, affectedRoleSet map[int]struct{}) error {
	roles, err := l.loadRoleHierarchyTx(tx)
	if err != nil {
		return errors.Tag(err)
	}
	nextPids, err := descendantRolePids(roles, roleID)
	if err != nil {
		return errors.Wrapf(err, "AdminRoleLogic.syncRoleDescendantPidsTx 重建角色 ID[%d]子树族谱失败", roleID)
	}
	currentPids := make(map[int]string, len(roles))
	roleIDs := make([]int, 0, len(nextPids))
	for _, role := range roles {
		currentPids[role.ID] = role.Pids
		if _, ok := nextPids[role.ID]; ok {
			roleIDs = append(roleIDs, role.ID)
		}
	}
	sort.Ints(roleIDs)
	updatedAt := time.Now()
	for _, descendantRoleID := range roleIDs {
		pids := nextPids[descendantRoleID]
		if currentPids[descendantRoleID] == pids {
			continue
		}
		if err := freshTxStatement(tx).Model(&model.AdminRole{}).
			Where("id = ? AND is_delete = 0", descendantRoleID).
			Updates(map[string]any{"pids": pids, "updated_at": updatedAt}).Error; err != nil {
			return errors.Wrapf(err, "AdminRoleLogic.syncRoleDescendantPidsTx 更新角色 ID[%d]族谱失败", descendantRoleID)
		}
		affectedRoleSet[descendantRoleID] = struct{}{}
	}
	return nil
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

// permissionPathRows 查询启用权限的祖先路径。
func (l *AdminRoleLogic) permissionPathRows(db *gorm.DB, permissionIDs []int) ([]permissionPathRow, error) {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if len(permissionIDs) == 0 {
		return []permissionPathRow{}, nil
	}
	rows := make([]permissionPathRow, 0, len(permissionIDs))
	if err := freshTxStatement(db).Model(&model.AdminPermission{}).
		Select("id, pids").
		Where("id IN ? AND status = ?", permissionIDs, 1).
		Order("id ASC").
		Scan(&rows).Error; err != nil {
		return nil, errors.Wrapf(err, "AdminRoleLogic.permissionPathRows 查询权限祖先失败 permissionIDs=%v", permissionIDs)
	}
	return rows, nil
}

// expandPermissionAncestorIDs 补齐已选权限的启用祖先节点，保证菜单路由父级真实授权。
func (l *AdminRoleLogic) expandPermissionAncestorIDs(db *gorm.DB, permissionIDs []int) ([]int, error) {
	rows, err := l.permissionPathRows(db, permissionIDs)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return expandPermissionAncestorIDsFromRows(permissionIDs, rows), nil
}

// retainCompletePermissionPathIDs 移除缺少启用祖先节点的半截授权。
func (l *AdminRoleLogic) retainCompletePermissionPathIDs(db *gorm.DB, permissionIDs []int) ([]int, error) {
	rows, err := l.permissionPathRows(db, permissionIDs)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return retainCompletePermissionPathIDsFromRows(permissionIDs, rows), nil
}

// normalizeAssignablePermissionIDs 补齐菜单祖先，再按父角色边界裁剪半截授权。
func (l *AdminRoleLogic) normalizeAssignablePermissionIDs(db *gorm.DB, permissionIDs []int, allowedPermissionIDs []int) ([]int, error) {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if len(permissionIDs) == 0 {
		return []int{}, nil
	}
	var err error
	permissionIDs, err = l.expandPermissionAncestorIDs(db, permissionIDs)
	if err != nil {
		return nil, errors.Tag(err)
	}
	allowedPermissionIDs, err = l.expandPermissionAncestorIDs(db, allowedPermissionIDs)
	if err != nil {
		return nil, errors.Tag(err)
	}
	permissionIDs = retainAssignablePermissionIDs(permissionIDs, allowedPermissionIDs)
	permissionIDs, err = l.retainCompletePermissionPathIDs(db, permissionIDs)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return permissionIDs, nil
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

// refreshRoleRelatedCache 清理角色定义及其关联展示缓存。
func (l *AdminRoleLogic) refreshRoleRelatedCache(roleIDs ...int) error {
	roleIDs = types.UniquePositiveInts(roleIDs)
	adminIDs, err := l.adminIDsByRoleIDs(roleIDs)
	if err != nil {
		return errors.Wrapf(err, "查询受影响管理员失败 roleIDs=%v", roleIDs)
	}
	return l.refreshRoleRelatedCacheByScope(roleIDs, adminIDs)
}

// refreshRoleRelatedCacheByScope 按角色与管理员影响范围精确清理角色定义缓存。
func (l *AdminRoleLogic) refreshRoleRelatedCacheByScope(roleIDs []int, adminIDs []int) error {
	roleCacheKeys := []string{keys.RoleTree, keys.RoleStatus}
	for _, roleID := range types.UniquePositiveInts(roleIDs) {
		roleCacheKeys = append(roleCacheKeys,
			fmt.Sprintf(keys.RolePermission, roleID),
			fmt.Sprintf(keys.RoleDocPermission, roleID),
		)
	}
	roleCacheErr := cachelogic.DeleteTableCacheKeysExact(
		l.BaseLogic,
		"AdminRoleLogic.refreshRoleRelatedCacheByScope 删除角色缓存",
		cachelogic.TableCachePhysicalKeys(l.BaseLogic, roleCacheKeys...),
	)
	adminDetailErr := cachelogic.InvalidateAdminRoleDetailCacheByAdminIDs(l.BaseLogic, adminIDs...)
	return errors.Join(roleCacheErr, adminDetailErr)
}

// refreshRolePermissionCache 精确清理角色路由和文档权限关系缓存。
func (l *AdminRoleLogic) refreshRolePermissionCache(roleIDs ...int) error {
	cacheKeys := make([]string, 0, len(roleIDs)*2)
	for _, roleID := range types.UniquePositiveInts(roleIDs) {
		cacheKeys = append(cacheKeys,
			fmt.Sprintf(keys.RolePermission, roleID),
			fmt.Sprintf(keys.RoleDocPermission, roleID),
		)
	}
	return cachelogic.DeleteTableCacheKeysExact(
		l.BaseLogic,
		"AdminRoleLogic.refreshRolePermissionCache 删除角色权限关系缓存",
		cachelogic.TableCachePhysicalKeys(l.BaseLogic, cacheKeys...),
	)
}
