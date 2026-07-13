package rbac

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"admin/common/codes"
	i18n "admin/common/i18n"
	keys "admin/common/rediskeys"
	redislock "admin/internal/infra/redsync"
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	tablecache "github.com/Is999/table-cache"
	"gorm.io/gorm"
)

// AdminRoleLogic 处理角色层级、授权关系和角色缓存。
type AdminRoleLogic struct {
	*corelogic.BaseLogic // 复用上下文、数据库和日志能力
}

const (
	// rbacWriteLockTTL 是 RBAC 写锁默认持有时长。
	rbacWriteLockTTL = 20 * time.Second
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

	items := make([]types.AdminRoleItem, 0, len(list))
	for _, role := range list {
		items = append(items, corelogic.AdminRoleModelToItem(role, nil))
	}
	roleTree, err := l.LoadRoleTreeWithCache()
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.List 查询角色树缓存失败").ToBizResult()
	}
	manageableRoleSet, parentRoleSet, err := l.roleItemScopeSets(roleTree)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.List 计算角色可操作范围失败").ToBizResult()
	}
	items = markRoleTreeScope(items, manageableRoleSet)
	items = markRoleTreeCreateScope(items, parentRoleSet, true)

	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.ListResp[types.AdminRoleItem]{List: items, Total: total})
}

// TreeList 查询角色树，供新增/编辑角色和用户分配角色时使用。
func (l *AdminRoleLogic) TreeList() *types.BizResult {
	items, err := l.LoadRoleTreeWithCache()
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

// ParentTreeOptions 查询角色父级下拉树，普通管理员可选择自身角色来创建下级角色。
func (l *AdminRoleLogic) ParentTreeOptions() *types.BizResult {
	items, err := l.LoadRoleTreeWithCache()
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.ParentTreeOptions 查询角色树失败").ToBizResult()
	}
	items, err = l.decorateRoleTreeParentScope(items)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.ParentTreeOptions 计算角色父级可选范围失败").ToBizResult()
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(items)
}

// Create 新增角色；角色权限统一由专用授权接口维护。
func (l *AdminRoleLogic) Create(req *types.CreateRoleReq) *types.BizResult {
	return l.withRolePermissionWriteLock("AdminRoleLogic.Create", func() *types.BizResult {
		if err := l.ensureRoleParentWithinManageScope(req.Pid); err != nil {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "AdminRoleLogic.Create 父级角色 ID[%d]超出可操作范围", req.Pid))
		}
		disabledRoleID, err := l.disabledRolePathID(req.Pid)
		if err != nil {
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminRoleLogic.Create 校验父级角色 ID[%d]状态失败", req.Pid).ToBizResult()
		}
		if disabledRoleID > 0 {
			return types.NewBizResult(codes.Fail).
				SetI18nMessage(i18n.MsgKeyStatusChangeFail).
				WithError(errors.Errorf("AdminRoleLogic.Create 父级路径包含禁用角色 ID[%d]", disabledRoleID))
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
			return nil
		}); err != nil {
			if errors.Is(err, ErrRoleTitleAlreadyExists) || corelogic.IsMySQLDuplicateEntryError(err) {
				return RoleTitleAlreadyExistsResult(req.Title, err)
			}
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminRoleLogic.Create 创建角色[%s]失败", req.Title).ToBizResult()
		}

		if err := l.refreshRoleRelatedCache(role.ID); err != nil {
			return corelogic.CacheSyncPendingResult(l.Logger, codes.AddSuccess, i18n.MsgKeyAdminCacheInvalidationPending, err,
				"AdminRoleLogic.Create RBAC缓存同步失败")
		}
		return types.NewBizResult(codes.AddSuccess).
			SetI18nMessage(i18n.MsgKeyAddSuccess)
	})
}

// Update 编辑角色基础信息；换父级时同步收敛整棵角色子树的权限边界。
func (l *AdminRoleLogic) Update(req *types.UpdateRoleReq) *types.BizResult {
	return l.withRolePermissionWriteLock("AdminRoleLogic.Update", func() *types.BizResult {
		affectedRoleSet := map[int]struct{}{req.ID: {}}
		var role model.AdminRole
		if err := l.Svc.WriteDB(svc.DatabaseMain).Where("id = ? AND is_delete = 0", req.ID).First(&role).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return types.NotFound(i18n.MsgKeyNotFound, err,
					"AdminRoleLogic.Update 角色 ID[%d]不存在", req.ID).ToBizResult()
			}
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminRoleLogic.Update 查询角色 ID[%d]失败", req.ID).ToBizResult()
		}

		nextPid, err := roleParentIDForUpdate(req.ID, req.ParentID())
		if err != nil {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "AdminRoleLogic.Update 角色 ID[%d]父级不允许修改", req.ID))
		}
		pidChanged := nextPid != role.Pid
		if err := l.EnsureRolesWithinManageScope([]int{req.ID}); err != nil {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "AdminRoleLogic.Update 角色 ID[%d]超出可操作范围", req.ID))
		}
		if err := l.ensureRoleParentWithinManageScope(nextPid); err != nil {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "AdminRoleLogic.Update 父级角色 ID[%d]超出可操作范围", nextPid))
		}
		if pidChanged {
			disabledRoleID, err := l.disabledRolePathID(nextPid)
			if err != nil {
				return types.DBError(i18n.MsgKeyDBError, err,
					"AdminRoleLogic.Update 校验父级角色 ID[%d]状态失败", nextPid).ToBizResult()
			}
			if disabledRoleID > 0 {
				return types.NewBizResult(codes.Fail).
					SetI18nMessage(i18n.MsgKeyStatusChangeFail).
					WithError(errors.Errorf("AdminRoleLogic.Update 父级路径包含禁用角色 ID[%d]", disabledRoleID))
			}
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
				"describe":   *req.Description,
				"updated_at": time.Now(),
			}).Error; err != nil {
				return errors.Wrap(err, "更新角色基础信息失败")
			}
			if pidChanged {
				if err := l.syncRoleDescendantPidsTx(tx, req.ID, affectedRoleSet); err != nil {
					return errors.Tag(err)
				}
				isSuperRole, err := l.currentOperatorIsSuperRoleTx(tx)
				if err != nil {
					return errors.Tag(err)
				}
				if err := l.reconcileRolePermissionScopeTreeTx(tx, req.ID, isSuperRole, affectedRoleSet); err != nil {
					return errors.Tag(err)
				}
				return l.reconcileRoleDocPermissionScopeTreeTx(tx, req.ID, isSuperRole, affectedRoleSet)
			}
			return nil
		}); err != nil {
			if errors.Is(err, ErrRoleTitleAlreadyExists) || corelogic.IsMySQLDuplicateEntryError(err) {
				return RoleTitleAlreadyExistsResult(req.Title, err)
			}
			if errors.Is(err, errRolePermissionUnusable) {
				return types.ServerError(i18n.MsgKeyUpdateFail, err,
					"AdminRoleLogic.Update 更新角色 ID[%d]权限失败", req.ID).ToBizResult()
			}
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminRoleLogic.Update 更新角色 ID[%d]失败", req.ID).ToBizResult()
		}

		if err := l.refreshRoleRelatedCache(roleIDSetToSlice(affectedRoleSet)...); err != nil {
			return corelogic.CacheSyncPendingResult(l.Logger, codes.UpdateSuccess, i18n.MsgKeyAdminCacheInvalidationPending, err,
				"AdminRoleLogic.Update RBAC缓存同步失败")
		}
		return types.NewBizResult(codes.UpdateSuccess).
			SetI18nMessage(i18n.MsgKeyUpdateSuccess)
	})
}

// roleParentIDForUpdate 采用请求中的明确父级；超级管理员角色固定为根角色。
func roleParentIDForUpdate(roleID int, requestedPid int) (int, error) {
	if roleID == corelogic.AdminSuperRoleID {
		if requestedPid > 0 {
			return 0, errors.Errorf("超级管理员角色必须保持为根角色")
		}
		return 0, nil
	}
	return requestedPid, nil
}

// Delete 软删除角色；删除时级联软删除全部子孙角色，并清理管理员绑定关系与角色权限关系。
func (l *AdminRoleLogic) Delete(req *types.IDPathReq) *types.BizResult {
	return l.withRolePermissionWriteLock("AdminRoleLogic.Delete", func() *types.BizResult {
		return l.delete(req)
	})
}

// delete 在角色写锁内执行角色子树删除。
func (l *AdminRoleLogic) delete(req *types.IDPathReq) *types.BizResult {
	if err := l.EnsureRolesWithinManageScope([]int{req.ID}); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(err, "AdminRoleLogic.Delete 角色 ID[%d]超出可操作范围", req.ID))
	}
	if req.ID == corelogic.AdminSuperRoleID {
		forbidErr := errors.Errorf("超级管理员角色不允许删除")
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(forbidErr, "AdminRoleLogic.Delete 角色 ID[%d]不允许删除", req.ID))
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
			return errors.Wrapf(err, "查询角色 ID[%d]子树失败", req.ID)
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
		if err := l.EnsureRolesWithinManageScope(roleIDs); err != nil {
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
		if err := tx.Where("role_id IN ?", roleIDs).Delete(&model.AdminRoleDocPermissionRel{}).Error; err != nil {
			return errors.Wrap(err, "清理角色文档权限关系失败")
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
				"AdminRoleLogic.Delete 角色 ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.Delete 删除角色 ID[%d]失败", req.ID).ToBizResult()
	}

	if len(deletedRoleIDs) == 0 {
		deletedRoleIDs = []int{req.ID}
	}
	cacheErr := l.refreshRoleRelatedCacheByScope(deletedRoleIDs, nil)
	adminCacheErr := cachelogic.InvalidateAdminRoleCacheByAdminIDs(l.BaseLogic, affectedAdminIDs...)
	cacheErr = errors.Join(cacheErr, adminCacheErr)
	if cacheErr != nil {
		return corelogic.CacheSyncPendingResult(l.Logger, codes.DeleteSuccess, i18n.MsgKeyAdminCacheInvalidationPending, cacheErr,
			"AdminRoleLogic.Delete RBAC缓存同步失败")
	}
	return types.NewBizResult(codes.DeleteSuccess).
		SetI18nMessage(i18n.MsgKeyDeleteSuccess)
}

// UpdateStatus 修改角色启用/禁用状态；禁用时级联禁用全部子孙角色。
func (l *AdminRoleLogic) UpdateStatus(req *types.RoleStatusReq) *types.BizResult {
	return l.withRolePermissionWriteLock("AdminRoleLogic.UpdateStatus", func() *types.BizResult {
		return l.updateStatus(req)
	})
}

// updateStatus 在角色写锁内执行状态修改与子树禁用。
func (l *AdminRoleLogic) updateStatus(req *types.RoleStatusReq) *types.BizResult {
	var role model.AdminRole
	if err := l.Svc.WriteDB(svc.DatabaseMain).
		Select("id, pid").
		Where("id = ? AND is_delete = 0", req.ID).
		First(&role).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyNotFound, err,
				"AdminRoleLogic.UpdateStatus 角色 ID[%d]不存在", req.ID).ToBizResult()
		}
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.UpdateStatus 查询角色 ID[%d]失败", req.ID).ToBizResult()
	}
	if err := l.EnsureRolesWithinManageScope([]int{req.ID}); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(err, "AdminRoleLogic.UpdateStatus 角色 ID[%d]超出可操作范围", req.ID))
	}
	status := req.StatusValue()
	if req.ID == corelogic.AdminSuperRoleID && status == 0 {
		forbidErr := errors.Errorf("超级管理员角色状态不允许禁用")
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(forbidErr, "AdminRoleLogic.UpdateStatus 角色 ID[%d]不允许禁用", req.ID))
	}
	if status == 1 {
		disabledRoleID, err := l.disabledRolePathID(role.Pid)
		if err != nil {
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminRoleLogic.UpdateStatus 校验角色 ID[%d]父级状态失败", req.ID).ToBizResult()
		}
		if disabledRoleID > 0 {
			return types.NewBizResult(codes.Fail).
				SetI18nMessage(i18n.MsgKeyStatusChangeFail).
				WithError(errors.Errorf("AdminRoleLogic.UpdateStatus 父级路径包含禁用角色 ID[%d]", disabledRoleID))
		}
	}

	roleIDs := []int{req.ID}
	if status == 0 {
		descendantRoleIDs, err := l.descendantRoleIDs(req.ID)
		if err != nil {
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminRoleLogic.UpdateStatus 查询角色 ID[%d]子孙角色失败", req.ID).ToBizResult()
		}
		roleIDs = append(roleIDs, descendantRoleIDs...)
		roleIDs = types.UniquePositiveInts(roleIDs)
		if err := l.EnsureRolesWithinManageScope(roleIDs); err != nil {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "AdminRoleLogic.UpdateStatus 角色 ID[%d]级联角色超出可操作范围", req.ID))
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
			"AdminRoleLogic.UpdateStatus 修改角色 ID[%d]状态失败", req.ID).ToBizResult()
	}
	if err := l.refreshRoleRelatedCache(roleIDs...); err != nil {
		return corelogic.CacheSyncPendingResult(l.Logger, codes.UpdateSuccess, i18n.MsgKeyAdminCacheInvalidationPending, err,
			"AdminRoleLogic.UpdateStatus RBAC缓存同步失败")
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyStatusChangeOK)
}

// PermissionTree 查询角色权限树，节点 checked 表示当前角色已拥有权限。
func (l *AdminRoleLogic) PermissionTree(req *types.IDPathReq) *types.BizResult {
	rolePermissionIDs, err := l.RolePermissionIDsWithCache(req.ID)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.PermissionTree 查询角色 ID[%d]权限失败", req.ID).ToBizResult()
	}
	items, err := l.LoadPermissionTreeWithCache()
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.PermissionTree 查询权限树失败").ToBizResult()
	}

	assignableIDs, lockAll, err := l.permissionTreeAssignScope(req.ID)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.PermissionTree 计算角色 ID[%d]权限可分配范围失败", req.ID).ToBizResult()
	}
	rolePermissionIDs = effectiveRolePermissionIDs(req.ID, rolePermissionIDs, assignableIDs)
	checked := make(map[int]struct{}, len(rolePermissionIDs))
	for _, permissionID := range rolePermissionIDs {
		checked[permissionID] = struct{}{}
	}
	assignable := make(map[int]struct{}, len(assignableIDs))
	for _, permissionID := range assignableIDs {
		assignable[permissionID] = struct{}{}
	}
	docPermissionIDs, err := l.roleDocPermissionIDs(req.ID)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.PermissionTree 查询角色 ID[%d]文档权限失败", req.ID).ToBizResult()
	}
	docAssignableIDs, docLockAll, err := l.docPermissionTreeAssignScope(req.ID)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.PermissionTree 计算角色 ID[%d]文档权限可分配范围失败", req.ID).ToBizResult()
	}
	docPermissionIDs = effectiveRolePermissionIDs(req.ID, docPermissionIDs, docAssignableIDs)
	docItems, err := l.loadDocPermissionItems(docPermissionIDs, docAssignableIDs, docLockAll)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err,
			"AdminRoleLogic.PermissionTree 查询文档权限列表失败").ToBizResult()
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.RolePermissionTreeResp{
			RoutePermissions: markPermissionTreeChecked(items, checked, assignable, lockAll),
			DocPermissions:   docItems,
			Writable:         !lockAll && !docLockAll,
		})
}

// effectiveRolePermissionIDs 返回角色权限树应展示的授权集合；超级角色自身按隐式全权限展示。
func effectiveRolePermissionIDs(roleID int, assignedPermissionIDs []int, assignablePermissionIDs []int) []int {
	if roleID == corelogic.AdminSuperRoleID {
		return types.UniquePositiveInts(assignablePermissionIDs)
	}
	return types.UniquePositiveInts(assignedPermissionIDs)
}

// LoadPermissionTreeWithCache 优先读取权限树缓存，未命中时自动回源。
func (l *AdminRoleLogic) LoadPermissionTreeWithCache() ([]types.AdminPermissionItem, error) {
	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	var items []types.AdminPermissionItem
	_, err = manager.LoadThrough(l.Ctx, cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.PermissionTree), &items, nil)
	return items, errors.Tag(err)
}

// SavePermissions 覆盖保存角色权限关系。
func (l *AdminRoleLogic) SavePermissions(req *types.RolePermissionSaveReq) *types.BizResult {
	return l.withRolePermissionWriteLock("AdminRoleLogic.SavePermissions", func() *types.BizResult {
		affectedRoleSet := map[int]struct{}{req.ID: {}}
		if err := l.EnsureRolesWithinManageScope([]int{req.ID}); err != nil {
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(err, "AdminRoleLogic.SavePermissions 角色 ID[%d]超出可操作范围", req.ID))
		}
		if req.ID == corelogic.AdminSuperRoleID {
			forbidErr := errors.Errorf("超级管理员角色权限不允许在此处修改")
			return types.Forbidden(i18n.MsgKeyForbidden).
				ToBizResult().
				WithError(errors.Wrapf(forbidErr, "AdminRoleLogic.SavePermissions 角色 ID[%d]不允许在当前入口修改权限", req.ID))
		}
		err := l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
			// 权限配置保存时在主库事务内裁剪越权权限，避免读取副本延迟后回写旧授权。
			selection, err := l.retainRolePermissionSelectionInScopeTx(tx, req.ID, req.RoutePermissionIDs, req.DocPermissionIDs)
			if err != nil {
				return errors.Wrapf(err, "计算角色 ID[%d]可分配权限失败", req.ID)
			}
			if err := l.syncRolePermissionDelta(tx, req.ID, selection.RoutePermissionIDs, affectedRoleSet); err != nil {
				return errors.Tag(err)
			}
			return l.syncRoleDocPermissionDelta(tx, req.ID, selection.DocPermissionIDs, affectedRoleSet)
		})
		if err != nil {
			if errors.Is(err, errRolePermissionUnusable) {
				return types.ServerError(i18n.MsgKeyUpdateFail, err,
					"AdminRoleLogic.SavePermissions 保存角色 ID[%d]权限失败", req.ID).ToBizResult()
			}
			return types.DBError(i18n.MsgKeyDBError, err,
				"AdminRoleLogic.SavePermissions 保存角色 ID[%d]权限失败", req.ID).ToBizResult()
		}

		if err := l.refreshRolePermissionCache(roleIDSetToSlice(affectedRoleSet)...); err != nil {
			return corelogic.CacheSyncPendingResult(l.Logger, codes.UpdateSuccess, i18n.MsgKeyAdminCacheInvalidationPending, err,
				"AdminRoleLogic.SavePermissions RBAC缓存同步失败")
		}
		return types.NewBizResult(codes.UpdateSuccess).
			SetI18nMessage(i18n.MsgKeyUpdateSuccess)
	})
}

// withRolePermissionWriteLock 用全局分布式锁保护角色层级与权限写链路。
// 锁丢失会取消派生上下文，事务和缓存操作都通过该上下文尽快停止。
func (l *AdminRoleLogic) withRolePermissionWriteLock(operation string, fn func() *types.BizResult) *types.BizResult {
	if l == nil {
		return WithRBACWriteLock(nil, operation, nil)
	}
	return WithRBACWriteLock(l.BaseLogic, operation, func(lockedBaseLogic *corelogic.BaseLogic) *types.BizResult {
		// AdminRoleLogic 是单请求对象，临界区内临时切换到锁生命周期上下文，退出后恢复原上下文。
		originalBaseLogic := l.BaseLogic
		l.BaseLogic = lockedBaseLogic
		defer func() {
			l.BaseLogic = originalBaseLogic
		}()
		return fn()
	})
}

// WithRBACWriteLock 用全局分布式锁串行化角色层级、权限定义和授权关系写入。
func WithRBACWriteLock(baseLogic *corelogic.BaseLogic, operation string, fn func(*corelogic.BaseLogic) *types.BizResult) *types.BizResult {
	if fn == nil {
		return types.ServerError(i18n.MsgKeyServerError, errors.New("RBAC写操作为空"),
			"%s RBAC写操作为空", operation).ToBizResult()
	}
	if baseLogic == nil || baseLogic.Redis() == nil {
		redisErr := errors.New("Redis 客户端未初始化")
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyServiceBusy).
			WithError(corelogic.WrapLogicError(redisErr, "%s RBAC分布式锁未初始化", operation))
	}

	var result *types.BizResult
	err := redislock.WithLock(baseLogic.Ctx, baseLogic.Redis(), baseLogic.AppRedisKey(keys.RBACWriteLock), rbacWriteLockTTL, func(lockCtx context.Context) error {
		result = fn(corelogic.NewBaseLogicWithContext(lockCtx, baseLogic.Svc))
		return nil
	})
	if err != nil {
		// 业务已经返回明确结果时保留真实结果，避免数据库已提交却因释放锁失败误报可重试。
		if result != nil {
			corelogic.LogWrappedError(baseLogic, err, "%s RBAC分布式锁执行异常", operation)
			return result
		}
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyServiceBusy).
			WithError(corelogic.WrapLogicError(err, "%s 获取或持有RBAC分布式锁失败", operation))
	}
	if result == nil {
		return types.ServerError(i18n.MsgKeyServerError, errors.New("RBAC写操作未返回结果"),
			"%s RBAC写操作未返回结果", operation).ToBizResult()
	}
	return result
}

// loadAllRoles 加载全部未删除角色，统一用于树结构和缓存重建。
func (l *AdminRoleLogic) loadAllRoles() ([]model.AdminRole, error) {
	var roles []model.AdminRole
	err := l.Svc.WriteDB(svc.DatabaseMain).Where("is_delete = 0").Order("id ASC").Find(&roles).Error
	if err != nil {
		return nil, errors.Wrap(err, "AdminRoleLogic.loadAllRoles 查询全部角色失败")
	}
	return roles, nil
}

// UserRoleIDs 查询管理员绑定的全部角色 ID，不在这里过滤状态，统一交给角色状态缓存判断。
func (l *AdminRoleLogic) UserRoleIDs(userID int) ([]int, error) {
	if userID <= 0 {
		return []int{}, nil
	}
	var roleIDs []int
	err := l.Svc.WriteDB(svc.DatabaseMain).Table(model.TableNameAdminRoleRel).
		Where("user_id = ?", userID).
		Order("role_id ASC").
		Pluck("role_id", &roleIDs).Error
	if err != nil {
		return nil, errors.Wrapf(err, "AdminRoleLogic.UserRoleIDs 查询管理员ID[%d]角色关系失败", userID)
	}
	return types.UniquePositiveInts(roleIDs), nil
}

// adminIDsByRoleIDs 从主库查询绑定指定角色的管理员，避免副本延迟导致漏删鉴权缓存。
func (l *AdminRoleLogic) adminIDsByRoleIDs(roleIDs []int) ([]int, error) {
	roleIDs = types.UniquePositiveInts(roleIDs)
	if len(roleIDs) == 0 {
		return []int{}, nil
	}
	var adminIDs []int
	err := l.Svc.WriteDB(svc.DatabaseMain).
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

// LoadRoleTreeWithCache 优先从 Redis 读取角色树缓存，未命中时自动回源数据库并重建。
func (l *AdminRoleLogic) LoadRoleTreeWithCache() ([]types.AdminRoleItem, error) {
	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	var items []types.AdminRoleItem
	_, err = manager.LoadThrough(l.Ctx, cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.RoleTree), &items, nil)
	return items, errors.Tag(err)
}

// EnabledRoleIDsByUserWithCache 查询管理员绑定的启用角色 ID。
func (l *AdminRoleLogic) EnabledRoleIDsByUserWithCache(userID int) ([]int, error) {
	if userID <= 0 {
		return []int{}, nil
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
	roleIDs, err := cachelogic.ParsePositiveIntStrings(values, "管理员角色关系缓存")
	if err != nil {
		return nil, errors.Tag(err)
	}
	return l.filterEnabledRoleIDsWithCache(roleIDs)
}

// CurrentOperatorEnabledRoleIDs 查询当前登录管理员拥有的全部启用角色 ID。
func (l *AdminRoleLogic) CurrentOperatorEnabledRoleIDs() ([]int, error) {
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin == nil || ctxAdmin.ID <= 0 {
		return nil, errors.Errorf("未获取到当前登录管理员信息")
	}
	return l.EnabledRoleIDsByUserWithCache(ctxAdmin.ID)
}

// CurrentOperatorIsSuperRole 判断当前登录管理员是否拥有超级管理员角色。
func (l *AdminRoleLogic) CurrentOperatorIsSuperRole() (bool, error) {
	roleIDs, err := l.CurrentOperatorEnabledRoleIDs()
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

// currentOperatorIsSuperRoleTx 在事务主库中确认当前管理员是否拥有启用的超级管理员角色。
func (l *AdminRoleLogic) currentOperatorIsSuperRoleTx(tx *gorm.DB) (bool, error) {
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin == nil || ctxAdmin.ID <= 0 {
		return false, errors.Errorf("未获取到当前登录管理员信息")
	}
	var matchedRoleID int
	if err := freshTxStatement(tx).
		Table(model.TableNameAdminRoleRel+" AS rel").
		Select("rel.role_id").
		Joins("JOIN "+model.TableNameAdminRole+" AS role ON role.id = rel.role_id AND role.status = 1 AND role.is_delete = 0").
		Where("rel.user_id = ? AND rel.role_id = ?", ctxAdmin.ID, corelogic.AdminSuperRoleID).
		Limit(1).
		Scan(&matchedRoleID).Error; err != nil {
		return false, errors.Wrapf(err, "AdminRoleLogic.currentOperatorIsSuperRoleTx 查询管理员 ID[%d]超级角色失败", ctxAdmin.ID)
	}
	return matchedRoleID == corelogic.AdminSuperRoleID, nil
}

// manageableRoleIDSet 计算当前登录管理员可管理的角色集合。
// 超级管理员可管理全部未删除角色；普通管理员只能管理自己角色的后代角色。
func (l *AdminRoleLogic) manageableRoleIDSet() (map[int]struct{}, error) {
	manageableRoleSet, _, err := l.roleScopeIDSets()
	return manageableRoleSet, errors.Tag(err)
}

// roleScopeIDSets 一次计算角色管理范围和可作为父级的范围。
func (l *AdminRoleLogic) roleScopeIDSets() (map[int]struct{}, map[int]struct{}, error) {
	roles, err := l.loadAllRoles()
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	roleIDs, err := l.CurrentOperatorEnabledRoleIDs()
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	isSuperRole := roleIDsContainSuper(roleIDs)
	return manageableRoleSetFrom(roles, roleIDs, isSuperRole), parentRoleSetFrom(roles, roleIDs, isSuperRole), nil
}

// ManageableRoleIDs 返回当前登录管理员可管理的角色 ID，供管理员列表批量标记操作范围。
func (l *AdminRoleLogic) ManageableRoleIDs() ([]int, error) {
	items, err := l.LoadRoleTreeWithCache()
	if err != nil {
		return nil, errors.Tag(err)
	}
	roleIDSet, _, err := l.roleItemScopeSets(items)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return roleIDSetToSlice(roleIDSet), nil
}

// manageableRoleSetFrom 基于角色树计算可管理范围；普通管理员不能管理自身角色。
func manageableRoleSetFrom(roles []model.AdminRole, operatorRoleIDs []int, isSuperRole bool) map[int]struct{} {
	return roleScopeSetFrom(roles, operatorRoleIDs, isSuperRole, false)
}

// parentRoleSetFrom 计算可作为父级的角色范围；普通管理员可在自身角色下创建后代。
func parentRoleSetFrom(roles []model.AdminRole, operatorRoleIDs []int, isSuperRole bool) map[int]struct{} {
	return roleScopeSetFrom(roles, operatorRoleIDs, isSuperRole, true)
}

// roleScopeSetFrom 基于角色树计算范围，includeOperator 控制是否包含操作者自身角色。
func roleScopeSetFrom(roles []model.AdminRole, operatorRoleIDs []int, isSuperRole bool, includeOperator bool) map[int]struct{} {
	result := make(map[int]struct{}, len(roles))
	if isSuperRole {
		for _, role := range roles {
			if role.ID > 0 {
				result[role.ID] = struct{}{}
			}
		}
		return result
	}
	operatorRoleSet := make(map[int]struct{}, len(operatorRoleIDs))
	for _, roleID := range types.UniquePositiveInts(operatorRoleIDs) {
		operatorRoleSet[roleID] = struct{}{}
	}
	for _, role := range roles {
		for roleID := range operatorRoleSet {
			if (includeOperator && role.ID == roleID) || corelogic.ContainsTreeID(role.Pids, roleID) {
				result[role.ID] = struct{}{}
				break
			}
		}
	}
	return result
}

// EnsureRolesWithinManageScope 校验目标角色是否都在当前登录管理员可管理范围内。
func (l *AdminRoleLogic) EnsureRolesWithinManageScope(roleIDs []int) error {
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
	// 只有当前操作人是超级管理员时，顶级或超级管理员父级才允许使用全量权限范围。
	isSuperRole, err := l.CurrentOperatorIsSuperRole()
	if err != nil {
		return nil, errors.Tag(err)
	}
	if parentRoleUsesFullPermissionScope(parentRoleID, isSuperRole) {
		return l.allEnabledPermissionIDs()
	}
	return l.RolePermissionIDsWithCache(parentRoleID)
}

// parentRoleUsesFullPermissionScope 判断父角色是否使用全部启用权限作为子角色可分配范围。
func parentRoleUsesFullPermissionScope(parentRoleID int, isSuperRole bool) bool {
	return isSuperRole && (parentRoleID <= 0 || parentRoleID == corelogic.AdminSuperRoleID)
}

// permissionTreeAssignScope 计算目标角色权限树的可操作范围。
func (l *AdminRoleLogic) permissionTreeAssignScope(roleID int) ([]int, bool, error) {
	// 超级管理员角色自身不允许在此入口修改，前后端统一整树锁定。
	if roleID == corelogic.AdminSuperRoleID {
		assignableIDs, err := l.allEnabledPermissionIDs()
		return assignableIDs, true, errors.Tag(err)
	}

	if err := l.EnsureRolesWithinManageScope([]int{roleID}); err != nil {
		if errors.Is(err, ErrRoleManageScopeExceeded) {
			return []int{}, true, nil
		}
		return nil, false, errors.Tag(err)
	}

	assignableIDs, err := l.allowedPermissionIDsForRole(roleID)
	return assignableIDs, false, errors.Tag(err)
}

// allEnabledPermissionIDs 查询全部启用权限 ID，供超级管理员角色权限树只读展示复用。
func (l *AdminRoleLogic) allEnabledPermissionIDs() ([]int, error) {
	return (&AdminPermissionLogic{BaseLogic: l.BaseLogic}).AllEnabledPermissionIDsWithCache()
}

// ensureRoleParentWithinManageScope 校验目标父级角色是否在当前登录管理员可管理范围内。
func (l *AdminRoleLogic) ensureRoleParentWithinManageScope(parentRoleID int) error {
	if parentRoleID <= 0 {
		isSuperRole, err := l.CurrentOperatorIsSuperRole()
		if err != nil {
			return errors.Tag(err)
		}
		if !isSuperRole {
			return errors.Wrap(errRoleManageScopeExceeded, "仅超级管理员允许创建或移动到顶级角色")
		}
		return nil
	}
	roles, err := l.loadAllRoles()
	if err != nil {
		return errors.Tag(err)
	}
	isSuperRole, err := l.CurrentOperatorIsSuperRole()
	if err != nil {
		return errors.Tag(err)
	}
	roleIDs, err := l.CurrentOperatorEnabledRoleIDs()
	if err != nil {
		return errors.Tag(err)
	}
	if _, ok := parentRoleSetFrom(roles, roleIDs, isSuperRole)[parentRoleID]; !ok {
		return errors.Wrapf(errRoleManageScopeExceeded, "父级角色 ID[%d]超出当前管理员可管理范围", parentRoleID)
	}
	return nil
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

// roleIDSetToSlice 把角色 ID集合转成稳定切片，便于统一清理缓存。
func roleIDSetToSlice(roleIDSet map[int]struct{}) []int {
	roleIDs := make([]int, 0, len(roleIDSet))
	for roleID := range roleIDSet {
		if roleID > 0 {
			roleIDs = append(roleIDs, roleID)
		}
	}
	return types.UniquePositiveInts(roleIDs)
}

// decorateRoleTreeScope 在角色树上补充当前登录管理员可操作范围，便于前端直接按后端裁剪后的语义展示。
func (l *AdminRoleLogic) decorateRoleTreeScope(items []types.AdminRoleItem) ([]types.AdminRoleItem, error) {
	manageableRoleSet, parentRoleSet, err := l.roleItemScopeSets(items)
	if err != nil {
		return nil, errors.Tag(err)
	}
	items = markRoleTreeScope(items, manageableRoleSet)
	return markRoleTreeCreateScope(items, parentRoleSet, true), nil
}

// decorateRoleTreeParentScope 在角色树上补充新增/编辑角色时允许选择的父级范围。
func (l *AdminRoleLogic) decorateRoleTreeParentScope(items []types.AdminRoleItem) ([]types.AdminRoleItem, error) {
	_, parentRoleSet, err := l.roleItemScopeSets(items)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return markRoleTreeParentScope(items, parentRoleSet, true), nil
}

// roleItemScopeSets 基于缓存角色树一次计算管理范围和可作为父级的范围。
func (l *AdminRoleLogic) roleItemScopeSets(items []types.AdminRoleItem) (map[int]struct{}, map[int]struct{}, error) {
	roleIDs, err := l.CurrentOperatorEnabledRoleIDs()
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	isSuperRole := roleIDsContainSuper(roleIDs)
	return roleItemScopeSetFrom(items, roleIDs, isSuperRole, false),
		roleItemScopeSetFrom(items, roleIDs, isSuperRole, true),
		nil
}

// roleIDsContainSuper 判断角色集合是否包含超级管理员角色。
func roleIDsContainSuper(roleIDs []int) bool {
	for _, roleID := range roleIDs {
		if roleID == corelogic.AdminSuperRoleID {
			return true
		}
	}
	return false
}

// roleItemScopeSetFrom 基于已缓存角色树计算可操作范围，includeOperator 控制是否包含操作者自身角色。
func roleItemScopeSetFrom(items []types.AdminRoleItem, operatorRoleIDs []int, isSuperRole bool, includeOperator bool) map[int]struct{} {
	result := make(map[int]struct{})
	operatorRoleSet := make(map[int]struct{}, len(operatorRoleIDs))
	for _, roleID := range types.UniquePositiveInts(operatorRoleIDs) {
		operatorRoleSet[roleID] = struct{}{}
	}
	var walk func([]types.AdminRoleItem)
	walk = func(nodes []types.AdminRoleItem) {
		for _, item := range nodes {
			if isSuperRole {
				result[item.ID] = struct{}{}
			} else {
				for roleID := range operatorRoleSet {
					if (includeOperator && item.ID == roleID) || corelogic.ContainsTreeID(item.Pids, roleID) {
						result[item.ID] = struct{}{}
						break
					}
				}
			}
			walk(item.Children)
		}
	}
	walk(items)
	return result
}

// markRoleTreeScope 递归写入角色树节点的管理范围和选择语义。
func markRoleTreeScope(items []types.AdminRoleItem, roleScopeSet map[int]struct{}) []types.AdminRoleItem {
	result := make([]types.AdminRoleItem, 0, len(items))
	for _, item := range items {
		nextItem := item
		_, inScope := roleScopeSet[item.ID]
		nodeUsable := inScope && item.Status == 1 && item.IsDelete == 0
		nextItem.Manageable = inScope
		nextItem.Disabled = !nodeUsable
		nextItem.DisableCheckbox = !nodeUsable
		nextItem.Selectable = nodeUsable
		nextItem.Children = markRoleTreeScope(item.Children, roleScopeSet)
		result = append(result, nextItem)
	}
	return result
}

// markRoleTreeCreateScope 递归标记可新增子角色的节点，并要求整条父级路径启用。
func markRoleTreeCreateScope(items []types.AdminRoleItem, parentRoleSet map[int]struct{}, parentPathEnabled bool) []types.AdminRoleItem {
	result := make([]types.AdminRoleItem, 0, len(items))
	for _, item := range items {
		nextItem := item
		pathEnabled := parentPathEnabled && item.Status == 1 && item.IsDelete == 0
		_, inParentScope := parentRoleSet[item.ID]
		nextItem.CanCreateChild = inParentScope && pathEnabled
		nextItem.Children = markRoleTreeCreateScope(item.Children, parentRoleSet, pathEnabled)
		result = append(result, nextItem)
	}
	return result
}

// markRoleTreeParentScope 按可新增范围和整条启用路径标记父级下拉节点。
func markRoleTreeParentScope(items []types.AdminRoleItem, parentRoleSet map[int]struct{}, parentPathEnabled bool) []types.AdminRoleItem {
	result := make([]types.AdminRoleItem, 0, len(items))
	for _, item := range items {
		nextItem := item
		pathEnabled := parentPathEnabled && item.Status == 1 && item.IsDelete == 0
		_, inParentScope := parentRoleSet[item.ID]
		selectable := inParentScope && pathEnabled
		nextItem.Manageable = inParentScope
		nextItem.CanCreateChild = selectable
		nextItem.Disabled = !selectable
		nextItem.DisableCheckbox = !selectable
		nextItem.Selectable = selectable
		nextItem.Children = markRoleTreeParentScope(item.Children, parentRoleSet, pathEnabled)
		result = append(result, nextItem)
	}
	return result
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
		return nil, errors.Wrapf(err, "AdminRoleLogic.rolePermissionIDsTx 查询角色 ID[%d]权限失败", roleID)
	}
	return types.UniquePositiveInts(permissionIDs), nil
}

// allowedPermissionIDsForParentRoleTx 按角色继承关系计算父角色允许子角色保留的权限范围。
func (l *AdminRoleLogic) allowedPermissionIDsForParentRoleTx(tx *gorm.DB, parentRoleID int, isSuperRole bool) ([]int, error) {
	if parentRoleUsesFullPermissionScope(parentRoleID, isSuperRole) {
		var permissionIDs []int
		if err := freshTxStatement(tx).
			Model(&model.AdminPermission{}).
			Where("status = ?", 1).
			Order("id ASC").
			Pluck("id", &permissionIDs).Error; err != nil {
			return nil, errors.Tag(err)
		}
		return types.UniquePositiveInts(permissionIDs), nil
	}
	return l.enabledRolePermissionIDsTx(tx, parentRoleID)
}

// reconcileRolePermissionScopeTreeTx 递归收敛目标角色及其全部子孙角色的权限范围。
// 为避免深层角色树出现 N+1 查询，这里会先在事务内批量加载整棵子树和权限关系，再在内存中完成收敛。
func (l *AdminRoleLogic) reconcileRolePermissionScopeTreeTx(tx *gorm.DB, roleID int, isSuperRole bool, affectedRoleSet map[int]struct{}) error {
	if roleID <= 0 {
		return nil
	}
	roleTree, childRoleMap, err := l.roleScopeTreeTx(tx, roleID)
	if err != nil {
		return errors.Tag(err)
	}
	rootRole, ok := roleTree[roleID]
	if !ok {
		return errors.Errorf("AdminRoleLogic.reconcileRolePermissionScopeTreeTx 角色 ID[%d]不存在", roleID)
	}
	rolePermissionMap, err := l.rolePermissionMapTx(tx, roleIDSetToSliceMap(roleTree))
	if err != nil {
		return errors.Tag(err)
	}
	rootAllowedPermissionIDs, err := l.allowedPermissionIDsForParentRoleTx(tx, rootRole.Pid, isSuperRole)
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
		return nil, nil, errors.Wrapf(err, "AdminRoleLogic.roleScopeTreeTx 查询角色 ID[%d]子树失败", roleID)
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
		return nil, errors.Wrapf(err, "AdminRoleLogic.enabledRolePermissionIDsTx 查询角色 ID[%d]启用权限失败", roleID)
	}
	return types.UniquePositiveInts(permissionIDs), nil
}
