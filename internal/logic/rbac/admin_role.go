package rbac

import (
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	"fmt"
	"sort"
	"time"

	"admin/common/codes"
	i18n "admin/common/i18n"
	keys "admin/common/rediskeys"
	redislock "admin/internal/infra/redsync"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"
	"net/http"

	"github.com/Is999/go-utils/errors"

	tablecache "github.com/Is999/table-cache"

	"gorm.io/gorm"
)

// AdminRoleLogic 预留角色领域逻辑入口，后续扩展角色管理能力时统一从这里收口。
type AdminRoleLogic struct {
	*corelogic.BaseLogic // 复用上下文、数据库和日志能力
}

const (
	// rolePermissionWriteLockTTL 是角色权限写锁默认持有时长。
	rolePermissionWriteLockTTL = 20 * time.Second
)

var (
	// errRolePermissionUnusable 表示提交的权限中包含已禁用或不存在的权限，属于业务约束失败而非数据库故障。
	errRolePermissionUnusable = errors.New("角色权限包含不可用节点")
	// errRoleManageScopeExceeded 表示目标角色超出当前登录管理员可管理范围。
	errRoleManageScopeExceeded = errors.New("角色超出当前管理员可管理范围")
	// ErrRoleManageScopeExceeded 表示目标角色超出当前登录管理员可管理范围。
	ErrRoleManageScopeExceeded = errRoleManageScopeExceeded
)

// freshTxStatement 基于当前事务创建干净语句上下文，避免不同模型查询之间残留条件相互污染。
func freshTxStatement(tx *gorm.DB) *gorm.DB {
	if tx == nil {
		return nil
	}
	return tx.Session(&gorm.Session{NewDB: true})
}

// NewAdminRoleLogic 创建角色业务逻辑对象。
func NewAdminRoleLogic(r *http.Request, svcCtx *svc.ServiceContext) *AdminRoleLogic {
	return &AdminRoleLogic{
		BaseLogic: corelogic.NewBaseLogic(r, svcCtx),
	}
}

// List 分页查询角色列表，支持按名称、状态和父级筛选。
func (l *AdminRoleLogic) List(req *types.RoleListReq) *types.BizResult {
	// 角色管理页面默认只展示未删除角色，软删除数据保留给审计和历史关系排查。
	dbq := l.Svc.ReadDB(svc.DatabaseMain).Model(&model.AdminRole{}).Where("is_delete = 0")
	if req.Title != "" {
		dbq = dbq.Where("title LIKE ?", "%"+req.Title+"%")
	}
	if req.Status != nil {
		dbq = dbq.Where("status = ?", *req.Status)
	}
	if req.Pid != nil {
		if req.IsGenealogy > 0 {
			// 角色层级筛选统一走 `FIND_IN_SET`，避免 `LIKE` 前导通配符导致全表模糊扫描。
			dbq = corelogic.ApplyGenealogyScopeFilter(dbq, "pids", *req.Pid)
		} else {
			dbq = dbq.Where("pid = ?", *req.Pid)
		}
	}

	// 排序字段前端小驼峰传参，默认按 ID 倒序展示最新角色。
	orderBy := corelogic.NormalizedOrderField(req.OrderBy, "id")
	list, total, err := model.List[model.AdminRole](dbq, req.Page, req.PageSize, orderBy, corelogic.NormalizedOrderDirection(req.Order))
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.List 查询角色列表失败").ToBizResult()
	}

	roleIDs := make([]int, 0, len(list))
	for _, role := range list {
		roleIDs = append(roleIDs, role.ID)
	}
	permissionMap, err := l.rolePermissionMap(roleIDs)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.List 查询角色权限失败").ToBizResult()
	}

	items := make([]types.AdminRoleItem, 0, len(list))
	for _, role := range list {
		items = append(items, roleModelToItem(role, permissionMap[role.ID], nil))
	}

	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.ListResp[types.AdminRoleItem]{List: items, Total: total})
}

// TreeList 查询角色树，供新增/编辑角色和用户分配角色时使用。
func (l *AdminRoleLogic) TreeList() *types.BizResult {
	items, err := l.loadRoleTreeWithCache()
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.TreeList 查询角色树失败").ToBizResult()
	}
	items, err = l.decorateRoleTreeScope(items)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.TreeList 计算角色树可操作范围失败").ToBizResult()
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(items)
}

// Create 新增角色，并在同一事务内绑定权限。
func (l *AdminRoleLogic) Create(req *types.SaveRoleReq) *types.BizResult {
	return l.withRolePermissionWriteLock("AdminRoleLogic.Create", func() *types.BizResult {
		if err := l.ensureRoleParentWithinManageScope(req.Pid); err != nil {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "AdminRoleLogic.Create 父级角色ID[%d]超出可操作范围", req.Pid))
		}
		// 创建角色时如果提交了越权权限，按父角色边界自动过滤，仅保留合法权限继续保存。
		filteredPermissionIDs, err := l.retainRolePermissionsWithinParentScope(req.Pid, req.Permissions)
		if err != nil {
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminRoleLogic.Create 计算父级角色ID[%d]可分配权限失败", req.Pid).ToBizResult()
		}
		role := model.AdminRole{
			Title:     req.Title,
			Pid:       req.Pid,
			Status:    corelogic.IntPtrDefault(req.Status, 1),
			Describe:  req.Description,
			IsDelete:  0,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if err := l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
			pids, err := l.rolePidsTx(tx, req.Pid, 0)
			if err != nil {
				return errors.Tag(err)
			}
			role.Pids = pids
			if err := l.ensureRoleTitleUniqueTx(tx, req.Title, 0); err != nil {
				return errors.Tag(err)
			}
			if err := tx.Create(&role).Error; err != nil {
				return errors.Wrap(err, "创建角色失败")
			}
			if err := l.replaceRolePermissionsTx(tx, role.ID, filteredPermissionIDs); err != nil {
				return errors.Tag(err)
			}
			return nil
		}); err != nil {
			if errors.Is(err, ErrRoleTitleAlreadyExists) || corelogic.IsMySQLDuplicateEntryError(err) {
				return RoleTitleAlreadyExistsResult(req.Title, err)
			}
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminRoleLogic.Create 创建角色[%s]失败", req.Title).ToBizResult()
		}

		l.refreshRoleRelatedCache(role.ID)
		return types.NewBizResult(codes.AddSuccess).
			SetI18nMessage(i18n.MsgKeyAddSuccess)
	})
}

// Update 编辑角色基础信息，并按需覆盖角色权限。
func (l *AdminRoleLogic) Update(req *types.SaveRoleReq) *types.BizResult {
	return l.withRolePermissionWriteLock("AdminRoleLogic.Update", func() *types.BizResult {
		affectedRoleSet := map[int]struct{}{req.ID: {}}
		var role model.AdminRole
		if err := l.Svc.WriteDB(svc.DatabaseMain).Where("id = ? AND is_delete = 0", req.ID).First(&role).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return types.NotFound(i18n.MsgKeyNotFound, err,
					"AdminRoleLogic.Update 角色ID[%d]不存在", req.ID).ToBizResult()
			}
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminRoleLogic.Update 查询角色ID[%d]失败", req.ID).ToBizResult()
		}

		// 编辑接口沿用 laravel-admin：未提交 pid 时保留原父级，避免误把角色移动到根节点。
		nextPid := role.Pid
		if req.Pid > 0 || role.Pid == 0 {
			nextPid = req.Pid
		}
		pidChanged := nextPid != role.Pid
		nextStatus := role.Status
		if req.Status != nil {
			nextStatus = *req.Status
		}
		if req.ID == corelogic.AdminSuperRoleID && nextStatus == 0 {
			forbidErr := errors.Errorf("超级管理员角色状态不允许禁用")
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(forbidErr, "AdminRoleLogic.Update 角色ID[%d]不允许禁用", req.ID))
		}
		if err := l.ensureRolesWithinManageScope([]int{req.ID}); err != nil {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "AdminRoleLogic.Update 角色ID[%d]超出可操作范围", req.ID))
		}
		if err := l.ensureRoleParentWithinManageScope(nextPid); err != nil {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "AdminRoleLogic.Update 父级角色ID[%d]超出可操作范围", nextPid))
		}
		filteredPermissionIDs := req.Permissions
		if req.Permissions != nil {
			// 编辑角色时自动过滤超出父角色边界的权限，避免单个越权节点导致整次保存失败。
			filtered, filterErr := l.retainRolePermissionsWithinParentScope(nextPid, req.Permissions)
			if filterErr != nil {
				return types.DBError(i18n.MsgKeyDBError, filterErr,
					"AdminRoleLogic.Update 计算角色ID[%d]可分配权限失败", req.ID).ToBizResult()
			}
			filteredPermissionIDs = filtered
		}

		if err := l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
			pids, err := l.rolePidsTx(tx, nextPid, req.ID)
			if err != nil {
				return errors.Tag(err)
			}
			if err := l.ensureRoleTitleUniqueTx(tx, req.Title, req.ID); err != nil {
				return errors.Tag(err)
			}
			if err := tx.Model(&model.AdminRole{}).Where("id = ?", req.ID).Updates(map[string]any{
				"title":      req.Title,
				"pid":        nextPid,
				"pids":       pids,
				"status":     nextStatus,
				"describe":   req.Description,
				"updated_at": time.Now(),
			}).Error; err != nil {
				return errors.Wrap(err, "更新角色基础信息失败")
			}
			if filteredPermissionIDs != nil {
				err := l.syncRolePermissionDelta(tx, req.ID, filteredPermissionIDs, affectedRoleSet)
				if err != nil {
					return errors.Tag(err)
				}
			}
			if pidChanged && req.Permissions == nil {
				return l.reconcileRolePermissionScopeTreeTx(tx, req.ID, affectedRoleSet)
			}
			return nil
		}); err != nil {
			if errors.Is(err, ErrRoleTitleAlreadyExists) || corelogic.IsMySQLDuplicateEntryError(err) {
				return RoleTitleAlreadyExistsResult(req.Title, err)
			}
			if errors.Is(err, errRolePermissionUnusable) {
				return types.ServerError(i18n.MsgKeyUpdateFail, err,
					"AdminRoleLogic.Update 更新角色ID[%d]权限失败", req.ID).ToBizResult()
			}
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminRoleLogic.Update 更新角色ID[%d]失败", req.ID).ToBizResult()
		}

		l.refreshRoleRelatedCache(roleIDSetToSlice(affectedRoleSet)...)
		return types.NewBizResult(codes.UpdateSuccess).
			SetI18nMessage(i18n.MsgKeyUpdateSuccess)
	})
}

// Delete 软删除角色；删除时级联软删除全部子孙角色，并清理管理员绑定关系与角色权限关系。
func (l *AdminRoleLogic) Delete(req *types.IDPathReq) *types.BizResult {
	if err := l.ensureRolesWithinManageScope([]int{req.ID}); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(err, "AdminRoleLogic.Delete 角色ID[%d]超出可操作范围", req.ID))
	}
	if req.ID == corelogic.AdminSuperRoleID {
		forbidErr := errors.Errorf("超级管理员角色不允许删除")
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(forbidErr, "AdminRoleLogic.Delete 角色ID[%d]不允许删除", req.ID))
	}

	var deletedRoleIDs []int
	var affectedAdminIDs []int
	if err := l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		var roleIDs []int
		if err := freshTxStatement(tx).Model(&model.AdminRole{}).
			Where("is_delete = 0").
			Where("id = ? OR FIND_IN_SET(?, pids)", req.ID, req.ID).
			Order("id ASC").
			Pluck("id", &roleIDs).Error; err != nil {
			return errors.Wrapf(err, "查询角色ID[%d]子树失败", req.ID)
		}
		roleIDs = types.UniquePositiveInts(roleIDs)
		if len(roleIDs) == 0 {
			return gorm.ErrRecordNotFound
		}
		for _, roleID := range roleIDs {
			if roleID == corelogic.AdminSuperRoleID {
				return errors.Errorf("超级管理员角色不允许删除")
			}
		}
		if err := l.ensureRolesWithinManageScope(roleIDs); err != nil {
			return errors.Tag(err)
		}
		now := time.Now()
		result := tx.Model(&model.AdminRole{}).
			Where("id IN ? AND is_delete = 0", roleIDs).
			Updates(map[string]any{"is_delete": 1, "updated_at": now})
		if result.Error != nil {
			return errors.Wrap(result.Error, "软删除角色失败")
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Where("role_id IN ?", roleIDs).Delete(&model.AdminRolePermissionRel{}).Error; err != nil {
			return errors.Wrap(err, "清理角色权限关系失败")
		}
		// 删除角色关系前先捕获受影响管理员，后续缓存失效才能精确删除对应 admin_* key。
		adminIDs, err := l.adminIDsByRoleIDsTx(tx, roleIDs)
		if err != nil {
			return errors.Tag(err)
		}
		if err := tx.Where("role_id IN ?", roleIDs).Delete(&model.AdminRoleRel{}).Error; err != nil {
			return errors.Wrap(err, "清理管理员角色关系失败")
		}
		deletedRoleIDs = roleIDs
		affectedAdminIDs = adminIDs
		return nil
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyNotFound, err,
				"AdminRoleLogic.Delete 角色ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.Delete 删除角色ID[%d]失败", req.ID).ToBizResult()
	}

	if len(deletedRoleIDs) == 0 {
		deletedRoleIDs = []int{req.ID}
	}
	l.refreshRoleRelatedCacheByScope(deletedRoleIDs, affectedAdminIDs)
	return types.NewBizResult(codes.DeleteSuccess).
		SetI18nMessage(i18n.MsgKeyDeleteSuccess)
}

// UpdateStatus 修改角色启用/禁用状态；禁用时级联禁用全部子孙角色。
func (l *AdminRoleLogic) UpdateStatus(req *types.RoleStatusReq) *types.BizResult {
	if err := l.ensureRolesWithinManageScope([]int{req.ID}); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(err, "AdminRoleLogic.UpdateStatus 角色ID[%d]超出可操作范围", req.ID))
	}
	status := req.StatusValue()
	if req.ID == corelogic.AdminSuperRoleID && status == 0 {
		forbidErr := errors.Errorf("超级管理员角色状态不允许禁用")
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(forbidErr, "AdminRoleLogic.UpdateStatus 角色ID[%d]不允许禁用", req.ID))
	}

	roleIDs := []int{req.ID}
	if status == 0 {
		descendantRoleIDs, err := l.descendantRoleIDs(req.ID)
		if err != nil {
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminRoleLogic.UpdateStatus 查询角色ID[%d]子孙角色失败", req.ID).ToBizResult()
		}
		roleIDs = append(roleIDs, descendantRoleIDs...)
		roleIDs = types.UniquePositiveInts(roleIDs)
		if err := l.ensureRolesWithinManageScope(roleIDs); err != nil {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "AdminRoleLogic.UpdateStatus 角色ID[%d]级联角色超出可操作范围", req.ID))
		}
		for _, roleID := range roleIDs {
			if roleID == corelogic.AdminSuperRoleID {
				forbidErr := errors.Errorf("超级管理员角色状态不允许禁用")
				return types.Forbidden(i18n.MsgKeyForbidden).
					ToBizResult().
					WithError(errors.Wrapf(forbidErr, "AdminRoleLogic.UpdateStatus 级联角色包含超级管理员角色"))
			}
		}
	}
	result := l.Svc.WriteDB(svc.DatabaseMain).Model(&model.AdminRole{}).
		Where("id IN ? AND is_delete = 0", roleIDs).
		Updates(map[string]any{"status": status, "updated_at": time.Now()})
	if result.Error != nil {
		return types.DBError(i18n.MsgKeyDBError, result.Error,
			"AdminRoleLogic.UpdateStatus 修改角色ID[%d]状态失败", req.ID).ToBizResult()
	}
	if result.RowsAffected == 0 {
		return types.NotFound(i18n.MsgKeyNotFound, gorm.ErrRecordNotFound,
			"AdminRoleLogic.UpdateStatus 角色ID[%d]不存在", req.ID).ToBizResult()
	}

	l.refreshRoleRelatedCache(roleIDs...)
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyStatusChangeOK)
}

// PermissionTree 查询角色权限树，节点 checked 表示当前角色已拥有权限。
func (l *AdminRoleLogic) PermissionTree(req *types.RolePermissionReq) *types.BizResult {
	rolePermissionIDs, err := l.rolePermissionIDsWithCache(req.ID)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.PermissionTree 查询角色ID[%d]权限失败", req.ID).ToBizResult()
	}
	items, err := l.loadPermissionTreeWithCache()
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.PermissionTree 查询权限树失败").ToBizResult()
	}

	checked := make(map[int]struct{}, len(rolePermissionIDs))
	for _, permissionID := range rolePermissionIDs {
		checked[permissionID] = struct{}{}
	}

	assignableIDs, lockAll, err := l.permissionTreeAssignScope(req)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.PermissionTree 计算角色ID[%d]权限可分配范围失败", req.ID).ToBizResult()
	}
	assignable := make(map[int]struct{}, len(assignableIDs))
	for _, permissionID := range assignableIDs {
		assignable[permissionID] = struct{}{}
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(markPermissionTreeChecked(items, checked, assignable, lockAll))
}

// loadPermissionTreeWithCache 优先读取权限树缓存，未命中时自动回源。
func (l *AdminRoleLogic) loadPermissionTreeWithCache() ([]types.AdminPermissionItem, error) {
	if l.Redis() == nil {
		var permissions []model.AdminPermission
		if err := l.Svc.ReadDB(svc.DatabaseMain).Order("id ASC").Find(&permissions).Error; err != nil {
			return nil, errors.Tag(err)
		}
		return buildRolePermissionTree(permissions, nil, nil), nil
	}
	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	var items []types.AdminPermissionItem
	_, err = manager.LoadThrough(l.Ctx, cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.PermissionTree), &items, nil)
	return items, errors.Tag(err)
}

// buildRolePermissionTree 把平铺权限列表转换成角色授权用权限树。
func buildRolePermissionTree(permissions []model.AdminPermission, checked map[int]struct{}, disabled map[int]struct{}) []types.AdminPermissionItem {
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
			result = append(result, rolePermissionModelToItem(permission, isChecked, isDisabled, walk(permission.ID)))
		}
		return result
	}
	return walk(0)
}

// rolePermissionModelToItem 把权限模型转换成角色授权树节点。
func rolePermissionModelToItem(permission model.AdminPermission, checked bool, disabled bool, children []types.AdminPermissionItem) types.AdminPermissionItem {
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
		CreatedAt:       corelogic.FormatDateTime(permission.CreatedAt),
		UpdatedAt:       corelogic.FormatDateTime(permission.UpdatedAt),
	}
}

// SavePermissions 覆盖保存角色权限关系。
func (l *AdminRoleLogic) SavePermissions(req *types.RolePermissionSaveReq) *types.BizResult {
	return l.withRolePermissionWriteLock("AdminRoleLogic.SavePermissions", func() *types.BizResult {
		affectedRoleSet := map[int]struct{}{req.ID: {}}
		if err := l.ensureRolesWithinManageScope([]int{req.ID}); err != nil {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "AdminRoleLogic.SavePermissions 角色ID[%d]超出可操作范围", req.ID))
		}
		if req.ID == corelogic.AdminSuperRoleID {
			forbidErr := errors.Errorf("超级管理员角色权限不允许在此处修改")
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(forbidErr, "AdminRoleLogic.SavePermissions 角色ID[%d]不允许在当前入口修改权限", req.ID))
		}
		// 角色权限配置保存时自动裁剪越权权限，只保留当前角色允许分配的部分继续后续写链路。
		filteredPermissionIDs, err := l.retainRolePermissionsInScope(req.ID, req.Permissions)
		if err != nil {
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminRoleLogic.SavePermissions 计算角色ID[%d]可分配权限失败", req.ID).ToBizResult()
		}

		writeDB := l.Svc.WriteDB(svc.DatabaseMain)
		err = l.syncRolePermissionDelta(writeDB, req.ID, filteredPermissionIDs, affectedRoleSet)
		if err != nil {
			if errors.Is(err, errRolePermissionUnusable) {
				return types.ServerError(i18n.MsgKeyUpdateFail, err,
					"AdminRoleLogic.SavePermissions 保存角色ID[%d]权限失败", req.ID).ToBizResult()
			}
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminRoleLogic.SavePermissions 保存角色ID[%d]权限失败", req.ID).ToBizResult()
		}

		l.refreshRoleRelatedCache(roleIDSetToSlice(affectedRoleSet)...)
		return types.NewBizResult(codes.UpdateSuccess).
			SetI18nMessage(i18n.MsgKeyUpdateSuccess)
	})
}

// withRolePermissionWriteLock 用全局分布式锁保护角色权限写链路，避免多人并发修改导致权限范围校验与落库交叉。
func (l *AdminRoleLogic) withRolePermissionWriteLock(operation string, fn func() *types.BizResult) *types.BizResult {
	if fn == nil {
		return types.ServerError(i18n.MsgKeyServerError, errors.New("角色权限写操作为空"),
			"%s 角色权限写操作为空", operation).ToBizResult()
	}
	if l == nil || l.Redis() == nil {
		redisErr := errors.New("Redis 客户端未初始化")
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyServiceBusy).
			WithError(corelogic.WrapLogicError(redisErr, "%s 角色权限分布式锁未初始化", operation))
	}

	lock := redislock.NewLock(l.Redis(), l.AppRedisKey(keys.RolePermissionWriteLock))
	if err := lock.TryLock(l.Ctx, rolePermissionWriteLockTTL); err != nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyServiceBusy).
			WithError(corelogic.WrapLogicError(err, "%s 获取角色权限分布式锁失败", operation))
	}

	result := fn()

	select {
	case lostErr, ok := <-lock.Lost():
		if ok && lostErr != nil {
			if unlockErr := lock.Unlock(); unlockErr != nil {
				corelogic.LogWrappedError(l, unlockErr, "%s 角色权限分布式锁丢失后释放失败", operation)
			}
			return types.NewBizResult(codes.ServiceBusy).
				SetI18nMessage(i18n.MsgKeyServiceBusy).
				WithError(corelogic.WrapLogicError(lostErr, "%s 执行期间角色权限分布式锁丢失", operation))
		}
	default:
	}

	if err := lock.Unlock(); err != nil {
		corelogic.LogWrappedError(l, err, "%s 释放角色权限分布式锁失败", operation)
	}
	if result == nil {
		return types.ServerError(i18n.MsgKeyServerError, errors.New("角色权限写操作未返回结果"),
			"%s 角色权限写操作未返回结果", operation).ToBizResult()
	}
	return result
}

// loadAllRoles 加载全部未删除角色，统一用于树结构和缓存重建。
func (l *AdminRoleLogic) loadAllRoles() ([]model.AdminRole, error) {
	var roles []model.AdminRole
	err := l.Svc.ReadDB(svc.DatabaseMain).Where("is_delete = 0").Order("id ASC").Find(&roles).Error
	if err != nil {
		return nil, errors.Wrap(err, "AdminRoleLogic.loadAllRoles 查询全部角色失败")
	}
	return roles, nil
}

// userRoleIDs 查询管理员绑定的全部角色 ID，不在这里过滤状态，统一交给角色状态缓存判断。
func (l *AdminRoleLogic) userRoleIDs(userID int) ([]int, error) {
	if userID <= 0 {
		return []int{}, nil
	}
	var roleIDs []int
	err := l.Svc.ReadDB(svc.DatabaseMain).Table(model.TableNameAdminRoleRel).
		Where("user_id = ?", userID).
		Order("role_id ASC").
		Pluck("role_id", &roleIDs).Error
	if err != nil {
		return nil, errors.Wrapf(err, "AdminRoleLogic.userRoleIDs 查询管理员ID[%d]角色关系失败", userID)
	}
	return types.UniquePositiveInts(roleIDs), nil
}

// UserRoleIDs 查询管理员绑定的全部角色 ID。
func (l *AdminRoleLogic) UserRoleIDs(userID int) ([]int, error) {
	return l.userRoleIDs(userID)
}

// adminIDsByRoleIDs 查询绑定了指定角色集合的管理员 ID，用于角色变更后精确失效管理员权限缓存。
func (l *AdminRoleLogic) adminIDsByRoleIDs(roleIDs []int) ([]int, error) {
	roleIDs = types.UniquePositiveInts(roleIDs)
	if len(roleIDs) == 0 {
		return []int{}, nil
	}
	var adminIDs []int
	err := l.Svc.ReadDB(svc.DatabaseMain).
		Model(&model.AdminRoleRel{}).
		Where("role_id IN ?", roleIDs).
		Order("user_id ASC").
		Pluck("user_id", &adminIDs).Error
	if err != nil {
		return nil, errors.Wrapf(err, "AdminRoleLogic.adminIDsByRoleIDs 查询角色绑定管理员失败 roleIDs=%v", roleIDs)
	}
	return types.UniquePositiveInts(adminIDs), nil
}

// adminIDsByRoleIDsTx 在事务内查询指定角色集合绑定的管理员 ID，删除角色关系前必须使用该方法保留影响范围。
func (l *AdminRoleLogic) adminIDsByRoleIDsTx(tx *gorm.DB, roleIDs []int) ([]int, error) {
	roleIDs = types.UniquePositiveInts(roleIDs)
	if len(roleIDs) == 0 {
		return []int{}, nil
	}
	var adminIDs []int
	err := freshTxStatement(tx).
		Model(&model.AdminRoleRel{}).
		Where("role_id IN ?", roleIDs).
		Order("user_id ASC").
		Pluck("user_id", &adminIDs).Error
	if err != nil {
		return nil, errors.Wrapf(err, "AdminRoleLogic.adminIDsByRoleIDsTx 查询角色绑定管理员失败 roleIDs=%v", roleIDs)
	}
	return types.UniquePositiveInts(adminIDs), nil
}

// loadRoleTreeWithCache 优先从 Redis 读取角色树缓存，未命中时自动回源数据库并重建。
func (l *AdminRoleLogic) loadRoleTreeWithCache() ([]types.AdminRoleItem, error) {
	if l.Redis() == nil {
		roles, err := l.loadAllRoles()
		if err != nil {
			return nil, errors.Tag(err)
		}
		return buildRoleTree(roles, nil), nil
	}
	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	var items []types.AdminRoleItem
	_, err = manager.LoadThrough(l.Ctx, cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.RoleTree), &items, nil)
	return items, errors.Tag(err)
}

// LoadRoleTreeWithCache 查询角色树缓存，未命中时自动回源。
func (l *AdminRoleLogic) LoadRoleTreeWithCache() ([]types.AdminRoleItem, error) {
	return l.loadRoleTreeWithCache()
}

// enabledRoleIDsByUserWithCache 查询管理员绑定的启用角色 ID，优先使用角色状态缓存做过滤。
func (l *AdminRoleLogic) enabledRoleIDsByUserWithCache(userID int) ([]int, error) {
	if userID <= 0 {
		return []int{}, nil
	}
	if l.Redis() == nil {
		roleIDs, err := l.userRoleIDs(userID)
		if err != nil {
			return nil, errors.Tag(err)
		}
		return l.filterEnabledRoleIDsWithCache(roleIDs)
	}
	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	cacheKey := fmt.Sprintf(keys.AdminRoleIDs, userID)
	var values []string
	result, err := manager.LoadThrough(l.Ctx, cachelogic.TableCachePhysicalKey(l.BaseLogic, cacheKey), &values, nil)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if result.State == tablecache.LookupStateEmpty {
		return []int{}, nil
	}
	return cachelogic.ParsePositiveIntStrings(values, "管理员角色ID缓存")
}

// EnabledRoleIDsByUserWithCache 查询管理员绑定的启用角色 ID。
func (l *AdminRoleLogic) EnabledRoleIDsByUserWithCache(userID int) ([]int, error) {
	return l.enabledRoleIDsByUserWithCache(userID)
}

// currentOperatorEnabledRoleIDs 查询当前登录管理员拥有的全部启用角色 ID。
func (l *AdminRoleLogic) currentOperatorEnabledRoleIDs() ([]int, error) {
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin == nil || ctxAdmin.ID <= 0 {
		return nil, errors.Errorf("未获取到当前登录管理员信息")
	}
	return l.enabledRoleIDsByUserWithCache(ctxAdmin.ID)
}

// CurrentOperatorEnabledRoleIDs 查询当前登录管理员启用角色 ID。
func (l *AdminRoleLogic) CurrentOperatorEnabledRoleIDs() ([]int, error) {
	return l.currentOperatorEnabledRoleIDs()
}

// currentOperatorIsSuperRole 判断当前登录管理员是否拥有超级管理员角色。
func (l *AdminRoleLogic) currentOperatorIsSuperRole() (bool, error) {
	roleIDs, err := l.currentOperatorEnabledRoleIDs()
	if err != nil {
		return false, errors.Tag(err)
	}
	for _, roleID := range roleIDs {
		if roleID == corelogic.AdminSuperRoleID {
			return true, nil
		}
	}
	return false, nil
}

// CurrentOperatorIsSuperRole 判断当前登录管理员是否拥有超级管理员角色。
func (l *AdminRoleLogic) CurrentOperatorIsSuperRole() (bool, error) {
	return l.currentOperatorIsSuperRole()
}

// manageableRoleIDSet 计算当前登录管理员可管理的角色集合。
// 超级管理员可管理全部未删除角色；普通管理员可管理自己拥有的角色及其全部后代角色。
func (l *AdminRoleLogic) manageableRoleIDSet() (map[int]struct{}, error) {
	roles, err := l.loadAllRoles()
	if err != nil {
		return nil, errors.Tag(err)
	}
	isSuperRole, err := l.currentOperatorIsSuperRole()
	if err != nil {
		return nil, errors.Tag(err)
	}
	result := make(map[int]struct{}, len(roles))
	if isSuperRole {
		for _, role := range roles {
			result[role.ID] = struct{}{}
		}
		return result, nil
	}
	roleIDs, err := l.currentOperatorEnabledRoleIDs()
	if err != nil {
		return nil, errors.Tag(err)
	}
	operatorRoleSet := make(map[int]struct{}, len(roleIDs))
	for _, roleID := range roleIDs {
		operatorRoleSet[roleID] = struct{}{}
		result[roleID] = struct{}{}
	}
	for _, role := range roles {
		for roleID := range operatorRoleSet {
			if role.ID == roleID || corelogic.ContainsTreeID(role.Pids, roleID) {
				result[role.ID] = struct{}{}
				break
			}
		}
	}
	return result, nil
}

// ensureRolesWithinManageScope 校验目标角色是否都在当前登录管理员可管理范围内。
func (l *AdminRoleLogic) ensureRolesWithinManageScope(roleIDs []int) error {
	roleIDs = types.UniquePositiveInts(roleIDs)
	if len(roleIDs) == 0 {
		return nil
	}
	manageableRoleSet, err := l.manageableRoleIDSet()
	if err != nil {
		return errors.Tag(err)
	}
	var invalidRoleIDs []int
	for _, roleID := range roleIDs {
		if _, ok := manageableRoleSet[roleID]; !ok {
			invalidRoleIDs = append(invalidRoleIDs, roleID)
		}
	}
	if len(invalidRoleIDs) > 0 {
		return errors.Wrapf(errRoleManageScopeExceeded, "存在超出当前管理员可管理范围的角色: %v", invalidRoleIDs)
	}
	return nil
}

// EnsureRolesWithinManageScope 校验角色集合是否在当前管理员可管理范围内。
func (l *AdminRoleLogic) EnsureRolesWithinManageScope(roleIDs []int) error {
	return l.ensureRolesWithinManageScope(roleIDs)
}

// retainRolePermissionsInScope 过滤当前角色权限配置请求中的越权权限，仅保留允许继续写入的部分。
func (l *AdminRoleLogic) retainRolePermissionsInScope(roleID int, permissionIDs []int) ([]int, error) {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if len(permissionIDs) == 0 {
		return []int{}, nil
	}
	allowedPermissionIDs, err := l.allowedPermissionIDsForRole(roleID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return retainAssignablePermissionIDs(permissionIDs, allowedPermissionIDs), nil
}

// retainRolePermissionsWithinParentScope 过滤父角色边界外的权限，供新增/编辑角色时复用。
func (l *AdminRoleLogic) retainRolePermissionsWithinParentScope(parentRoleID int, permissionIDs []int) ([]int, error) {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if len(permissionIDs) == 0 {
		return []int{}, nil
	}
	allowedPermissionIDs, err := l.allowedPermissionIDsForParentRole(parentRoleID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return retainAssignablePermissionIDs(permissionIDs, allowedPermissionIDs), nil
}

// allowedPermissionIDsForRole 计算当前登录管理员给目标角色可分配的权限集合。
// 超级管理员可分配全部启用权限；普通角色只能分配父级角色已拥有的权限。
func (l *AdminRoleLogic) allowedPermissionIDsForRole(roleID int) ([]int, error) {
	var role model.AdminRole
	if err := l.Svc.ReadDB(svc.DatabaseMain).Where("id = ? AND is_delete = 0", roleID).First(&role).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return l.allowedPermissionIDsForParentRole(role.Pid)
}

// allowedPermissionIDsForParentRole 根据父级角色计算当前登录管理员可分配的权限集合。
func (l *AdminRoleLogic) allowedPermissionIDsForParentRole(parentRoleID int) ([]int, error) {
	// 角色继承边界始终以“目标角色的直接父角色”实际拥有的权限为准；
	// 即使当前操作人是超级管理员，也不能绕过父角色范围直接给子角色越权授权。
	if parentRoleUsesFullPermissionScope(parentRoleID) {
		return l.allEnabledPermissionIDs()
	}
	return l.rolePermissionIDsWithCache(parentRoleID)
}

// parentRoleUsesFullPermissionScope 判断父角色是否使用全部启用权限作为子角色可分配范围。
func parentRoleUsesFullPermissionScope(parentRoleID int) bool {
	return parentRoleID <= 0 || parentRoleID == corelogic.AdminSuperRoleID
}

// permissionTreeAssignScope 计算角色权限树的可操作范围，并支持 isPid 参数语义。
func (l *AdminRoleLogic) permissionTreeAssignScope(req *types.RolePermissionReq) ([]int, bool, error) {
	// 沿用 laravel-admin 的父级权限查询语义：isPid=y 时展示当前角色已有权限，供子角色继承参考。
	if req.IsPid == "y" {
		if parentRoleUsesFullPermissionScope(req.ID) {
			assignableIDs, err := l.allEnabledPermissionIDs()
			return assignableIDs, false, errors.Tag(err)
		}
		assignableIDs, err := l.rolePermissionIDsWithCache(req.ID)
		return assignableIDs, false, errors.Tag(err)
	}

	// 超级管理员角色自身不允许在此入口修改，前后端统一整树锁定。
	if req.ID == corelogic.AdminSuperRoleID {
		assignableIDs, err := l.allEnabledPermissionIDs()
		return assignableIDs, true, errors.Tag(err)
	}

	assignableIDs, err := l.allowedPermissionIDsForRole(req.ID)
	return assignableIDs, false, errors.Tag(err)
}

// allEnabledPermissionIDs 查询全部启用权限 ID，供超级管理员角色权限树只读展示复用。
func (l *AdminRoleLogic) allEnabledPermissionIDs() ([]int, error) {
	var permissionIDs []int
	err := l.Svc.ReadDB(svc.DatabaseMain).Model(&model.AdminPermission{}).
		Where("status = 1").
		Order("id ASC").
		Pluck("id", &permissionIDs).Error
	if err != nil {
		return nil, errors.Tag(err)
	}
	return types.UniquePositiveInts(permissionIDs), nil
}

// ensureRoleParentWithinManageScope 校验目标父级角色是否在当前登录管理员可管理范围内。
func (l *AdminRoleLogic) ensureRoleParentWithinManageScope(parentRoleID int) error {
	if parentRoleID <= 0 {
		isSuperRole, err := l.currentOperatorIsSuperRole()
		if err != nil {
			return errors.Tag(err)
		}
		if !isSuperRole {
			return errors.Errorf("仅超级管理员允许创建或移动到顶级角色")
		}
		return nil
	}
	return l.ensureRolesWithinManageScope([]int{parentRoleID})
}

// retainAssignablePermissionIDs 保留仍在允许范围内的权限 ID。
func retainAssignablePermissionIDs(permissionIDs []int, allowedPermissionIDs []int) []int {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if len(permissionIDs) == 0 {
		return []int{}
	}
	allowedSet := make(map[int]struct{}, len(allowedPermissionIDs))
	for _, permissionID := range types.UniquePositiveInts(allowedPermissionIDs) {
		allowedSet[permissionID] = struct{}{}
	}
	result := make([]int, 0, len(permissionIDs))
	for _, permissionID := range permissionIDs {
		if _, ok := allowedSet[permissionID]; ok {
			result = append(result, permissionID)
		}
	}
	sort.Ints(result)
	return result
}

// intSlicesEqual 判断两个已排序整数切片是否完全一致，避免不必要的权限关系重写。
func intSlicesEqual(left []int, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// roleIDSetToSlice 把角色ID集合转成稳定切片，便于统一清理缓存。
func roleIDSetToSlice(roleIDSet map[int]struct{}) []int {
	roleIDs := make([]int, 0, len(roleIDSet))
	for roleID := range roleIDSet {
		if roleID > 0 {
			roleIDs = append(roleIDs, roleID)
		}
	}
	return types.UniquePositiveInts(roleIDs)
}

// roleModelToItem 把角色模型转换成接口响应项。
func roleModelToItem(role model.AdminRole, permissionIDs []int, children []types.AdminRoleItem) types.AdminRoleItem {
	return types.AdminRoleItem{
		ID:              role.ID,
		Title:           role.Title,
		Pid:             role.Pid,
		Pids:            role.Pids,
		Status:          role.Status,
		Description:     role.Describe,
		IsDelete:        role.IsDelete,
		Disabled:        role.Status != 1 || role.IsDelete != 0,
		DisableCheckbox: role.Status != 1 || role.IsDelete != 0,
		Selectable:      role.Status == 1 && role.IsDelete == 0,
		Permissions:     permissionIDs,
		Children:        children,
		CreatedAt:       corelogic.FormatDateTime(role.CreatedAt),
		UpdatedAt:       corelogic.FormatDateTime(role.UpdatedAt),
	}
}

// buildRoleTree 把平铺角色列表转换成树结构。
func buildRoleTree(roles []model.AdminRole, permissionMap map[int][]int) []types.AdminRoleItem {
	children := make(map[int][]model.AdminRole, len(roles))
	for _, role := range roles {
		children[role.Pid] = append(children[role.Pid], role)
	}
	var walk func(pid int) []types.AdminRoleItem
	walk = func(pid int) []types.AdminRoleItem {
		nodes := children[pid]
		result := make([]types.AdminRoleItem, 0, len(nodes))
		for _, role := range nodes {
			result = append(result, roleModelToItem(role, permissionMap[role.ID], walk(role.ID)))
		}
		return result
	}
	return walk(0)
}

// decorateRoleTreeScope 在角色树上补充当前登录管理员可操作范围，便于前端直接按后端裁剪后的语义展示。
func (l *AdminRoleLogic) decorateRoleTreeScope(items []types.AdminRoleItem) ([]types.AdminRoleItem, error) {
	manageableRoleSet, err := l.manageableRoleIDSet()
	if err != nil {
		return nil, errors.Tag(err)
	}
	return markRoleTreeScope(items, manageableRoleSet), nil
}

// markRoleTreeScope 递归写入角色树节点的 disabled/selectable 语义。
func markRoleTreeScope(items []types.AdminRoleItem, manageableRoleSet map[int]struct{}) []types.AdminRoleItem {
	result := make([]types.AdminRoleItem, 0, len(items))
	for _, item := range items {
		nextItem := item
		_, inScope := manageableRoleSet[item.ID]
		nodeUsable := inScope && item.Status == 1 && item.IsDelete == 0
		nextItem.Disabled = !nodeUsable
		nextItem.DisableCheckbox = !nodeUsable
		nextItem.Selectable = nodeUsable
		nextItem.Children = markRoleTreeScope(item.Children, manageableRoleSet)
		result = append(result, nextItem)
	}
	return result
}

// rolePermissionMap 批量查询角色权限关系。
func (l *AdminRoleLogic) rolePermissionMap(roleIDs []int) (map[int][]int, error) {
	result := make(map[int][]int, len(roleIDs))
	roleIDs = types.UniquePositiveInts(roleIDs)
	if len(roleIDs) == 0 {
		return result, nil
	}
	type rolePermissionRow struct {
		RoleID       int `gorm:"column:role_id"`
		PermissionID int `gorm:"column:permission_id"`
	}
	var rows []rolePermissionRow
	if err := l.Svc.ReadDB(svc.DatabaseMain).
		Table(model.TableNameAdminRolePermissionRel+" AS rel").
		Select("rel.role_id, rel.permission_id").
		Joins("JOIN "+model.TableNameAdminPermission+" AS permission ON permission.id = rel.permission_id AND permission.status = 1").
		Where("rel.role_id IN ?", roleIDs).
		Order("rel.permission_id ASC").
		Scan(&rows).Error; err != nil {
		return nil, errors.Tag(err)
	}
	for _, row := range rows {
		result[row.RoleID] = append(result[row.RoleID], row.PermissionID)
	}
	return result, nil
}

// rolePermissionIDs 查询单个角色绑定的权限 ID。
func (l *AdminRoleLogic) rolePermissionIDs(roleID int) ([]int, error) {
	permissionMap, err := l.rolePermissionMap([]int{roleID})
	if err != nil {
		return nil, errors.Tag(err)
	}
	return permissionMap[roleID], nil
}

// rolePermissionIDsTx 在事务内读取单个角色当前已绑定的权限 ID。
func (l *AdminRoleLogic) rolePermissionIDsTx(tx *gorm.DB, roleID int) ([]int, error) {
	var permissionIDs []int
	if roleID <= 0 {
		return []int{}, nil
	}
	if err := freshTxStatement(tx).Model(&model.AdminRolePermissionRel{}).
		Where("role_id = ?", roleID).
		Order("permission_id ASC").
		Pluck("permission_id", &permissionIDs).Error; err != nil {
		return nil, errors.Wrapf(err, "AdminRoleLogic.rolePermissionIDsTx 查询角色ID[%d]权限失败", roleID)
	}
	return types.UniquePositiveInts(permissionIDs), nil
}

// allowedPermissionIDsForParentRoleTx 按角色继承关系计算父角色允许子角色保留的权限范围。
func (l *AdminRoleLogic) allowedPermissionIDsForParentRoleTx(tx *gorm.DB, parentRoleID int) ([]int, error) {
	if parentRoleUsesFullPermissionScope(parentRoleID) {
		return l.allEnabledPermissionIDs()
	}
	return l.enabledRolePermissionIDsTx(tx, parentRoleID)
}

// reconcileRolePermissionScopeTreeTx 递归收敛目标角色及其全部子孙角色的权限范围。
// 为避免深层角色树出现 N+1 查询，这里会先在事务内批量加载整棵子树和权限关系，再在内存中完成收敛。
func (l *AdminRoleLogic) reconcileRolePermissionScopeTreeTx(tx *gorm.DB, roleID int, affectedRoleSet map[int]struct{}) error {
	if roleID <= 0 {
		return nil
	}
	roleTree, childRoleMap, err := l.roleScopeTreeTx(tx, roleID)
	if err != nil {
		return errors.Tag(err)
	}
	rootRole, ok := roleTree[roleID]
	if !ok {
		return errors.Errorf("AdminRoleLogic.reconcileRolePermissionScopeTreeTx 角色ID[%d]不存在", roleID)
	}
	rolePermissionMap, err := l.rolePermissionMapTx(tx, roleIDSetToSliceMap(roleTree))
	if err != nil {
		return errors.Tag(err)
	}
	rootAllowedPermissionIDs, err := l.allowedPermissionIDsForParentRoleTx(tx, rootRole.Pid)
	if err != nil {
		return errors.Tag(err)
	}

	var reconcile func(currentRoleID int, allowedPermissionIDs []int) error
	reconcile = func(currentRoleID int, allowedPermissionIDs []int) error {
		currentPermissionIDs := types.UniquePositiveInts(rolePermissionMap[currentRoleID])
		nextPermissionIDs := retainAssignablePermissionIDs(currentPermissionIDs, allowedPermissionIDs)
		if !intSlicesEqual(currentPermissionIDs, nextPermissionIDs) {
			if err := l.replaceRolePermissionsTx(tx, currentRoleID, nextPermissionIDs); err != nil {
				return errors.Tag(err)
			}
			rolePermissionMap[currentRoleID] = nextPermissionIDs
			affectedRoleSet[currentRoleID] = struct{}{}
		} else {
			rolePermissionMap[currentRoleID] = currentPermissionIDs
		}
		for _, childRoleID := range childRoleMap[currentRoleID] {
			if err := reconcile(childRoleID, rolePermissionMap[currentRoleID]); err != nil {
				return errors.Tag(err)
			}
		}
		return nil
	}

	return reconcile(roleID, rootAllowedPermissionIDs)
}

// roleScopeTreeTx 一次性加载指定角色及其全部未删除子孙角色，供权限收敛在内存中遍历角色树。
func (l *AdminRoleLogic) roleScopeTreeTx(tx *gorm.DB, roleID int) (map[int]model.AdminRole, map[int][]int, error) {
	roleMap := make(map[int]model.AdminRole)
	childRoleMap := make(map[int][]int)
	var roles []model.AdminRole
	if err := tx.Where("is_delete = 0").
		Where("id = ? OR FIND_IN_SET(?, pids)", roleID, roleID).
		Order("id ASC").
		Find(&roles).Error; err != nil {
		return nil, nil, errors.Wrapf(err, "AdminRoleLogic.roleScopeTreeTx 查询角色ID[%d]子树失败", roleID)
	}
	for _, role := range roles {
		roleMap[role.ID] = role
		if role.ID != roleID {
			childRoleMap[role.Pid] = append(childRoleMap[role.Pid], role.ID)
		}
	}
	return roleMap, childRoleMap, nil
}

// rolePermissionMapTx 在事务内批量读取角色权限关系，避免递归收敛时逐节点反复查库。
func (l *AdminRoleLogic) rolePermissionMapTx(tx *gorm.DB, roleIDs []int) (map[int][]int, error) {
	result := make(map[int][]int, len(roleIDs))
	roleIDs = types.UniquePositiveInts(roleIDs)
	if len(roleIDs) == 0 {
		return result, nil
	}
	var rows []model.AdminRolePermissionRel
	if err := tx.Where("role_id IN ?", roleIDs).
		Order("role_id ASC, permission_id ASC").
		Find(&rows).Error; err != nil {
		return nil, errors.Wrapf(err, "AdminRoleLogic.rolePermissionMapTx 查询角色权限失败 roleIDs=%v", roleIDs)
	}
	for _, row := range rows {
		roleID := int(row.RoleID)
		result[roleID] = append(result[roleID], int(row.PermissionID))
	}
	for _, roleID := range roleIDs {
		result[roleID] = types.UniquePositiveInts(result[roleID])
	}
	return result, nil
}

// enabledRolePermissionIDsTx 在事务内读取角色当前仍启用的权限 ID，供父子继承范围计算使用。
func (l *AdminRoleLogic) enabledRolePermissionIDsTx(tx *gorm.DB, roleID int) ([]int, error) {
	var permissionIDs []int
	if roleID <= 0 {
		return []int{}, nil
	}
	if err := tx.Table(model.TableNameAdminRolePermissionRel+" AS rel").
		Select("rel.permission_id").
		Joins("JOIN "+model.TableNameAdminPermission+" AS permission ON permission.id = rel.permission_id AND permission.status = 1").
		Where("rel.role_id = ?", roleID).
		Order("rel.permission_id ASC").
		Pluck("rel.permission_id", &permissionIDs).Error; err != nil {
		return nil, errors.Wrapf(err, "AdminRoleLogic.enabledRolePermissionIDsTx 查询角色ID[%d]启用权限失败", roleID)
	}
	return types.UniquePositiveInts(permissionIDs), nil
}
