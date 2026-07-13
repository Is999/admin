package cache

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	keys "admin/common/rediskeys"
	corelogic "admin/internal/logic"
	"admin/internal/model"
	"admin/internal/routealias"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	tablecache "github.com/Is999/table-cache"
	"gorm.io/gorm"
)

const (
	// authorizationCacheTTL 表示规范化鉴权缓存的基础 TTL。
	authorizationCacheTTL = 15 * time.Minute
	// authorizationCacheJitter 表示规范化鉴权缓存的最大 TTL 抖动。
	authorizationCacheJitter = 2 * time.Minute
	// TableCacheIndexRolePermission 表示角色路由权限关系缓存指标索引。
	TableCacheIndexRolePermission = "role_permission"
	// TableCacheIndexRoleDocPermission 表示角色文档权限关系缓存指标索引。
	TableCacheIndexRoleDocPermission = "role_doc_permission"
	// secretKeyCacheFieldAESKeyRef 表示 AES KEY 文件路径缓存字段。
	secretKeyCacheFieldAESKeyRef = "aes_key_ref"
	// secretKeyCacheFieldAESIVRef 表示 AES IV 文件路径缓存字段。
	secretKeyCacheFieldAESIVRef = "aes_iv_ref"
	// secretKeyCacheFieldRSAPublicKeyUserRef 表示用户 RSA 公钥文件路径缓存字段。
	secretKeyCacheFieldRSAPublicKeyUserRef = "rsa_public_key_user_ref"
	// secretKeyCacheFieldRSAPublicKeyServerRef 表示服务端 RSA 公钥文件路径缓存字段。
	secretKeyCacheFieldRSAPublicKeyServerRef = "rsa_public_key_server_ref"
	// secretKeyCacheFieldRSAPrivateKeyServerRef 表示服务端 RSA 私钥文件路径缓存字段。
	secretKeyCacheFieldRSAPrivateKeyServerRef = "rsa_private_key_server_ref"
	// secretKeyCacheFieldStableVersion 表示稳定版本缓存字段。
	secretKeyCacheFieldStableVersion = "stable_version"
	// secretKeyCacheFieldGrayVersion 表示灰度版本缓存字段。
	secretKeyCacheFieldGrayVersion = "gray_version"
	// secretKeyCacheFieldGrayPercent 表示灰度比例缓存字段。
	secretKeyCacheFieldGrayPercent = "gray_percent"
	// secretKeyCacheFieldGraySalt 表示灰度哈希盐值字段。
	secretKeyCacheFieldGraySalt = "gray_salt"
	// secretKeyCacheFieldSignStatus 表示签名验签状态缓存字段。
	secretKeyCacheFieldSignStatus = "sign_status"
	// secretKeyCacheFieldCryptoStatus 表示加密解密状态缓存字段。
	secretKeyCacheFieldCryptoStatus = "crypto_status"
)

// tableCacheTargets 声明所有通用表缓存目标，业务只在这里描述数据如何回源。
func tableCacheTargets(base *corelogic.BaseLogic) []tablecache.Target {
	return []tablecache.Target{
		{
			Index:      keys.RoleTree,
			Title:      "角色树",
			Key:        keys.RoleTree,
			KeyTitle:   keys.RoleTree,
			Type:       tablecache.TypeString,
			Remark:     "角色树缓存",
			RefreshAll: true,
			Loader:     loadRoleTreeTableCache(base),
		},
		{
			Index:      keys.RoleStatus,
			Title:      "角色状态",
			Key:        keys.RoleStatus,
			KeyTitle:   keys.RoleStatus,
			Type:       tablecache.TypeHash,
			Remark:     "角色状态缓存",
			TTL:        authorizationCacheTTL,
			Jitter:     authorizationCacheJitter,
			RefreshAll: true,
			Loader:     loadRoleStatusTableCache(base),
		},
		{
			Index:            TableCacheIndexRolePermission,
			Title:            "角色路由权限关系",
			Key:              CacheTemplatePrefix(keys.RolePermissionPattern),
			KeyTitle:         keys.RolePermissionPattern,
			Type:             tablecache.TypeSet,
			Remark:           "单个角色原始路由权限 ID 集合缓存",
			TTL:              authorizationCacheTTL,
			Jitter:           authorizationCacheJitter,
			AllowEmptyMarker: true,
			Loader:           loadRolePermissionTableCache(base),
		},
		{
			Index:            TableCacheIndexRoleDocPermission,
			Title:            "角色文档权限关系",
			Key:              CacheTemplatePrefix(keys.RoleDocPermissionPattern),
			KeyTitle:         keys.RoleDocPermissionPattern,
			Type:             tablecache.TypeSet,
			Remark:           "单个角色原始文档权限 ID 集合缓存",
			TTL:              authorizationCacheTTL,
			Jitter:           authorizationCacheJitter,
			AllowEmptyMarker: true,
			Loader:           loadRoleDocPermissionTableCache(base),
		},
		{
			Index:            "admin_role_ids",
			Title:            "管理员角色 ID",
			Key:              CacheTemplatePrefix(keys.AdminRoleIDsPattern),
			KeyTitle:         keys.AdminRoleIDsPattern,
			Type:             tablecache.TypeSet,
			Remark:           "管理员原始角色 ID 集合缓存",
			TTL:              authorizationCacheTTL,
			Jitter:           authorizationCacheJitter,
			AllowEmptyMarker: true,
			Loader:           loadAdminRoleIDsTableCache(base),
		},
		{
			Index:            "admin_roles_detail",
			Title:            "管理员角色详情",
			Key:              CacheTemplatePrefix(keys.AdminRolesDetailPattern),
			KeyTitle:         keys.AdminRolesDetailPattern,
			Type:             tablecache.TypeString,
			Remark:           "管理员角色名称列表缓存",
			TTL:              time.Hour,
			AllowEmptyMarker: true,
			Loader:           loadAdminRolesDetailTableCache(base),
		},
		{
			Index:      keys.PermissionTree,
			Title:      "权限树",
			Key:        keys.PermissionTree,
			KeyTitle:   keys.PermissionTree,
			Type:       tablecache.TypeString,
			Remark:     "权限树缓存",
			RefreshAll: true,
			Loader:     loadPermissionTreeTableCache(base),
		},
		{
			Index:      keys.DocPermissionList,
			Title:      "文档权限节点",
			Key:        keys.DocPermissionList,
			KeyTitle:   keys.DocPermissionList,
			Type:       tablecache.TypeString,
			Remark:     "全部文档权限节点缓存",
			TTL:        time.Hour,
			RefreshAll: true,
			Loader:     loadDocPermissionListTableCache(base),
		},
		{
			Index:      keys.RoutePermissionIDs,
			Title:      "路由权限索引",
			Key:        keys.RoutePermissionIDs,
			KeyTitle:   keys.RoutePermissionIDs,
			Type:       tablecache.TypeHash,
			Remark:     "启用路由别名到权限 ID 的反向索引",
			TTL:        authorizationCacheTTL,
			Jitter:     authorizationCacheJitter,
			RefreshAll: true,
			Loader:     loadRoutePermissionIDsTableCache(base),
		},
		{
			Index:      keys.PermissionUUID,
			Title:      "权限UUID",
			Key:        keys.PermissionUUID,
			KeyTitle:   keys.PermissionUUID,
			Type:       tablecache.TypeHash,
			Remark:     "权限UUID缓存",
			TTL:        authorizationCacheTTL,
			Jitter:     authorizationCacheJitter,
			RefreshAll: true,
			Loader:     loadPermissionUUIDTableCache(base),
		},
		{
			Index:      keys.DocResourcePermissionID,
			Title:      "文档资源权限索引",
			Key:        keys.DocResourcePermissionID,
			KeyTitle:   keys.DocResourcePermissionID,
			Type:       tablecache.TypeHash,
			Remark:     "启用文档资源到文档权限 ID 的反向索引",
			TTL:        authorizationCacheTTL,
			Jitter:     authorizationCacheJitter,
			RefreshAll: true,
			Loader:     loadDocResourcePermissionIDTableCache(base),
		},
		{
			Index:            "config_uuid",
			Title:            "系统常量配置",
			Key:              CacheTemplatePrefix(keys.SysConfigUUIDPattern),
			KeyTitle:         keys.SysConfigUUIDPattern,
			Type:             tablecache.TypeHash,
			Remark:           "系统常量配置缓存",
			TTL:              time.Hour,
			AllowEmptyMarker: true,
			Loader:           loadSysConfigTableCache(base),
		},
		{
			Index:            "runtime_config_state",
			Title:            "运行配置版本",
			Key:              CacheTemplatePrefix(keys.RuntimeConfigStatePattern),
			KeyTitle:         keys.RuntimeConfigStatePattern,
			Type:             tablecache.TypeHash,
			Remark:           "运行配置 active 版本状态缓存",
			TTL:              time.Minute,
			AllowEmptyMarker: true,
			Loader:           loadRuntimeConfigStateTableCache(base),
		},
		{
			Index:            "runtime_config_release",
			Title:            "运行配置发布快照",
			Key:              CacheTemplatePrefix(keys.RuntimeConfigReleasePattern),
			KeyTitle:         keys.RuntimeConfigReleasePattern,
			Type:             tablecache.TypeString,
			Remark:           "运行配置不可变发布快照缓存",
			TTL:              time.Hour,
			AllowEmptyMarker: true,
			Loader:           loadRuntimeConfigReleaseTableCache(base),
		},
		{
			Index:    "secret_key_route",
			Title:    "秘钥路由配置",
			Key:      CacheTemplatePrefix(keys.SecretKeyRoutePattern),
			KeyTitle: keys.SecretKeyRoutePattern,
			Type:     tablecache.TypeHash,
			Remark:   "秘钥稳定版与灰度版路由缓存",
			TTL:      time.Hour,
			Loader:   loadSecretKeyRouteTableCache(base),
		},
		{
			Index:    "secret_key_aes",
			Title:    "AES秘钥配置",
			Key:      CacheTemplatePrefix(keys.SecretKeyAESVersionPattern),
			KeyTitle: keys.SecretKeyAESVersionPattern,
			Type:     tablecache.TypeHash,
			Remark:   "版本化 AES 秘钥配置缓存",
			TTL:      time.Hour,
			Loader:   loadSecretKeyAESTableCache(base),
		},
		{
			Index:    "secret_key_rsa",
			Title:    "RSA秘钥配置",
			Key:      CacheTemplatePrefix(keys.SecretKeyRSAVersionPattern),
			KeyTitle: keys.SecretKeyRSAVersionPattern,
			Type:     tablecache.TypeHash,
			Remark:   "版本化 RSA 秘钥配置缓存",
			TTL:      time.Hour,
			Loader:   loadSecretKeyRSATableCache(base),
		},
	}
}

// loadAdminRolesDetailTableCache 加载单个管理员角色名称列表缓存。
func loadAdminRolesDetailTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		adminID, err := tableCacheFirstIntPart(params, "管理员ID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		roleIDs, err := loadEnabledRoleIDsByUserForCache(base, adminID)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if len(roleIDs) == 0 {
			return nil, nil
		}
		var roles []string
		// 角色名称缓存失效后从主库回源，避免副本延迟把旧名称重新写回缓存。
		writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
		if err != nil {
			return nil, errors.Tag(err)
		}
		if err := writeDB.Model(&model.AdminRole{}).
			Where("id IN ? AND is_delete = 0", roleIDs).
			Order("id ASC").
			Pluck("title", &roles).Error; err != nil {
			return nil, errors.Tag(err)
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeString,
			Value: roles,
		}}, nil
	}
}

// loadRoleTreeTableCache 加载角色树缓存数据。
func loadRoleTreeTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		roles, err := loadAllRolesForCache(base)
		if err != nil {
			return nil, errors.Tag(err)
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeString,
			Value: corelogic.BuildAdminRoleTree(roles),
		}}, nil
	}
}

// loadRoleStatusTableCache 加载角色状态 Hash 缓存数据。
func loadRoleStatusTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		var roles []model.AdminRole
		// 角色状态缓存来源于 admin_role，统一从主库读取。
		writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
		if err != nil {
			return nil, errors.Tag(err)
		}
		if err := writeDB.Where("is_delete = 0").Find(&roles).Error; err != nil {
			return nil, errors.Tag(err)
		}
		cache := make(map[string]any, len(roles))
		for _, role := range roles {
			cache[strconv.Itoa(role.ID)] = role.Status
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeHash,
			Value: cache,
		}}, nil
	}
}

// loadRolePermissionTableCache 加载单角色权限集合缓存数据。
func loadRolePermissionTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		roleID, err := tableCacheFirstIntPart(params, "角色 ID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		permissionIDs, err := loadRolePermissionIDsForCache(base, roleID)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if len(permissionIDs) == 0 {
			return nil, nil
		}
		values := make([]any, 0, len(permissionIDs))
		for _, permissionID := range permissionIDs {
			values = append(values, permissionID)
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeSet,
			Value: values,
		}}, nil
	}
}

// loadRoleDocPermissionTableCache 加载单角色文档权限集合缓存数据。
func loadRoleDocPermissionTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		roleID, err := tableCacheFirstIntPart(params, "角色 ID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		permissionIDs, err := loadRoleDocPermissionIDsForCache(base, roleID)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if len(permissionIDs) == 0 {
			return nil, nil
		}
		values := make([]any, 0, len(permissionIDs))
		for _, permissionID := range permissionIDs {
			values = append(values, permissionID)
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeSet,
			Value: values,
		}}, nil
	}
}

// loadAdminRoleIDsTableCache 加载单个管理员原始角色关系缓存。
func loadAdminRoleIDsTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		adminID, err := tableCacheFirstIntPart(params, "管理员ID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		roleIDs, err := loadAssignedRoleIDsByUserForCache(base, adminID)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if len(roleIDs) == 0 {
			return nil, nil
		}
		values := make([]any, 0, len(roleIDs))
		for _, roleID := range roleIDs {
			values = append(values, roleID)
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeSet,
			Value: values,
		}}, nil
	}
}

// loadPermissionTreeTableCache 加载权限树缓存数据。
func loadPermissionTreeTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		permissions, err := loadAllPermissionsForCache(base)
		if err != nil {
			return nil, errors.Tag(err)
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeString,
			Value: corelogic.BuildAdminPermissionTree(permissions, nil, nil),
		}}, nil
	}
}

// loadDocPermissionListTableCache 加载全部文档权限节点缓存。
func loadDocPermissionListTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
		if err != nil {
			return nil, errors.Tag(err)
		}
		var permissions []model.AdminDocPermission
		if err := writeDB.Order("site ASC, path ASC, id ASC").Find(&permissions).Error; err != nil {
			return nil, errors.Tag(err)
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeString,
			Value: permissions,
		}}, nil
	}
}

// loadRoutePermissionIDsTableCache 加载启用路由别名到权限 ID 的反向索引。
func loadRoutePermissionIDsTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
		if err != nil {
			return nil, errors.Tag(err)
		}
		var permissions []model.AdminPermission
		if err := writeDB.
			Select("id", "module").
			Where("status = 1 AND module <> ''").
			Order("id ASC").
			Find(&permissions).Error; err != nil {
			return nil, errors.Tag(err)
		}
		permissionIDsByRoute := make(map[string][]int, len(permissions))
		for _, permission := range permissions {
			routeAlias := strings.TrimSpace(permission.Module)
			if permission.ID <= 0 || routeAlias == "" {
				continue
			}
			// 纯数字 module 只用于前端权限树目录分组，不是后端路由别名。
			if _, err := strconv.Atoi(routeAlias); err == nil {
				continue
			}
			permissionIDsByRoute[routeAlias] = append(permissionIDsByRoute[routeAlias], permission.ID)
		}
		cache := make(map[string]any, len(permissionIDsByRoute))
		for routeAlias, permissionIDs := range permissionIDsByRoute {
			permissionIDs = types.UniquePositiveInts(permissionIDs)
			sort.Ints(permissionIDs)
			values := make([]string, 0, len(permissionIDs))
			for _, permissionID := range permissionIDs {
				values = append(values, strconv.Itoa(permissionID))
			}
			cache[routeAlias] = strings.Join(values, ",")
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeHash,
			Value: cache,
		}}, nil
	}
}

// loadPermissionUUIDTableCache 加载启用权限 ID 到 UUID 的索引。
func loadPermissionUUIDTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
		if err != nil {
			return nil, errors.Tag(err)
		}
		var permissions []model.AdminPermission
		if err := writeDB.
			Select("id", "uuid").
			Where("status = 1").
			Order("id ASC").
			Find(&permissions).Error; err != nil {
			return nil, errors.Tag(err)
		}
		cache := make(map[string]any, len(permissions))
		for _, permission := range permissions {
			uuid := strings.TrimSpace(permission.UUID)
			if permission.ID > 0 && uuid != "" {
				cache[strconv.Itoa(permission.ID)] = uuid
			}
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeHash,
			Value: cache,
		}}, nil
	}
}

// loadDocResourcePermissionIDTableCache 加载启用文档资源到文档权限 ID 的反向索引。
func loadDocResourcePermissionIDTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
		if err != nil {
			return nil, errors.Tag(err)
		}
		var permissions []model.AdminDocPermission
		if err := writeDB.
			Select("id", "site", "path").
			Where("status = 1").
			Order("id ASC").
			Find(&permissions).Error; err != nil {
			return nil, errors.Tag(err)
		}
		cache := make(map[string]any, len(permissions))
		for _, permission := range permissions {
			resourceKey := (routealias.DocResource{Site: permission.Site, Path: permission.Path}).Key()
			if permission.ID > 0 && resourceKey != "" {
				cache[resourceKey] = permission.ID
			}
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeHash,
			Value: cache,
		}}, nil
	}
}

// loadAllRolesForCache 读取角色树缓存所需的全部有效角色。
func loadAllRolesForCache(base *corelogic.BaseLogic) ([]model.AdminRole, error) {
	writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var roles []model.AdminRole
	if err := writeDB.Where("is_delete = 0").Order("id ASC").Find(&roles).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return roles, nil
}

// loadAllPermissionsForCache 从主库读取权限树缓存所需的全部权限。
func loadAllPermissionsForCache(base *corelogic.BaseLogic) ([]model.AdminPermission, error) {
	writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var permissions []model.AdminPermission
	if err := writeDB.Order("id ASC").Find(&permissions).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return permissions, nil
}

// loadEnabledRoleIDsByUserForCache 从主库读取管理员绑定的启用角色 ID，避免失效后重建旧授权。
func loadEnabledRoleIDsByUserForCache(base *corelogic.BaseLogic, adminID int) ([]int, error) {
	if adminID <= 0 {
		return []int{}, nil
	}
	writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var roleIDs []int
	if err := writeDB.Table(model.TableNameAdminRoleRel+" AS rel").
		Joins("JOIN "+model.TableNameAdminRole+" AS role ON role.id = rel.role_id AND role.status = 1 AND role.is_delete = 0").
		Where("rel.user_id = ?", adminID).
		Order("rel.role_id ASC").
		Pluck("rel.role_id", &roleIDs).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return types.UniquePositiveInts(roleIDs), nil
}

// loadAssignedRoleIDsByUserForCache 从主库读取管理员的原始角色关系。
func loadAssignedRoleIDsByUserForCache(base *corelogic.BaseLogic, adminID int) ([]int, error) {
	if adminID <= 0 {
		return []int{}, nil
	}
	writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var roleIDs []int
	if err := writeDB.Model(&model.AdminRoleRel{}).
		Where("user_id = ?", adminID).
		Order("role_id ASC").
		Pluck("role_id", &roleIDs).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return types.UniquePositiveInts(roleIDs), nil
}

// loadRolePermissionIDsForCache 从主库读取单个角色的原始路由权限关系。
func loadRolePermissionIDsForCache(base *corelogic.BaseLogic, roleID int) ([]int, error) {
	if roleID <= 0 {
		return []int{}, nil
	}
	writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var permissionIDs []int
	if err := writeDB.Model(&model.AdminRolePermissionRel{}).
		Where("role_id = ?", roleID).
		Order("permission_id ASC").
		Pluck("permission_id", &permissionIDs).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return types.UniquePositiveInts(permissionIDs), nil
}

// loadRoleDocPermissionIDsForCache 从主库读取单个角色的原始文档权限关系。
func loadRoleDocPermissionIDsForCache(base *corelogic.BaseLogic, roleID int) ([]int, error) {
	if roleID <= 0 {
		return []int{}, nil
	}
	writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var permissionIDs []int
	if err := writeDB.Model(&model.AdminRoleDocPermissionRel{}).
		Where("role_id = ?", roleID).
		Order("doc_permission_id ASC").
		Pluck("doc_permission_id", &permissionIDs).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return types.UniquePositiveInts(permissionIDs), nil
}

// loadSysConfigTableCache 加载单个系统常量配置 Hash 缓存数据。
func loadSysConfigTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		uuid, err := tableCacheFirstStringPart(params, "配置UUID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		// 系统配置缓存需要读取最新配置值，回源时走主库。
		writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
		if err != nil {
			return nil, errors.Tag(err)
		}
		var cfg model.SysConfig
		if err := writeDB.Where("uuid = ?", uuid).First(&cfg).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, errors.Tag(err)
		}
		cache := map[string]any{
			"id":        cfg.ID,
			"uuid":      cfg.UUID,
			"title":     cfg.Title,
			"type":      cfg.Type,
			"value":     cfg.Value,
			"example":   cfg.Example,
			"remark":    cfg.Remark,
			"page":      cfg.Page,
			"pid":       cfg.Pid,
			"pids":      cfg.Pids,
			"version":   cfg.Version,
			"updatedAt": corelogic.FormatDateTime(cfg.UpdatedAt),
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeHash,
			Value: cache,
		}}, nil
	}
}

// loadRuntimeConfigStateTableCache 加载当前 MySQL 库的运行配置 active 版本状态。
func loadRuntimeConfigStateTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
		if err != nil {
			return nil, errors.Tag(err)
		}
		var state model.RuntimeConfigState
		if err = writeDB.First(&state).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, errors.Tag(err)
		}
		return []tablecache.Entry{{
			Key:  params.Key,
			Type: tablecache.TypeHash,
			Value: map[string]any{
				"active_release_id": state.ActiveReleaseID,
				"active_version":    state.ActiveVersion,
				"active_checksum":   state.ActiveChecksum,
				"published_at_unix": state.PublishedAt.Unix(),
			},
		}}, nil
	}
}

// loadRuntimeConfigReleaseTableCache 加载指定发布 ID 的运行配置快照。
func loadRuntimeConfigReleaseTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		releaseID, err := tableCacheFirstIntPart(params, "发布ID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
		if err != nil {
			return nil, errors.Tag(err)
		}
		var release model.RuntimeConfigRelease
		if err = writeDB.Where("id = ?", releaseID).First(&release).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, errors.Tag(err)
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeString,
			Value: release.SnapshotJSON,
		}}, nil
	}
}

// loadSecretKeyRouteTableCache 加载指定 AppID 的秘钥版本路由缓存。
func loadSecretKeyRouteTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		appID, err := tableCacheFirstStringPart(params, "AppID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		// 秘钥路由缓存回源主库，确保启停状态和灰度配置即时生效。
		writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
		if err != nil {
			return nil, errors.Tag(err)
		}
		var row model.SecretKey
		if err := writeDB.Where("uuid = ?", appID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return []tablecache.Entry{tableCacheSecretKeyRouteEmptyEntry(params.Key)}, nil
			}
			return nil, errors.Tag(err)
		}
		return []tablecache.Entry{{
			Key:  params.Key,
			Type: tablecache.TypeHash,
			Value: map[string]any{
				secretKeyCacheFieldStableVersion: row.StableVersion,
				secretKeyCacheFieldGrayVersion:   row.GrayVersion,
				secretKeyCacheFieldGrayPercent:   row.GrayPercent,
				secretKeyCacheFieldGraySalt:      row.GraySalt,
				secretKeyCacheFieldSignStatus:    row.SignStatus,
				secretKeyCacheFieldCryptoStatus:  row.CryptoStatus,
				"status":                         row.Status,
			},
		}}, nil
	}
}

// loadSecretKeyAESTableCache 加载指定 AppID + 版本的 AES 秘钥缓存。
func loadSecretKeyAESTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		_, _, row, err := loadSecretKeyVersionTableCacheRow(base, params)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if row == nil {
			return []tablecache.Entry{tableCacheSecretKeyVersionEmptyEntry(params.Key)}, nil
		}
		return []tablecache.Entry{{
			Key:  params.Key,
			Type: tablecache.TypeHash,
			Value: map[string]any{
				secretKeyCacheFieldAESKeyRef: row.AESKeyRef,
				secretKeyCacheFieldAESIVRef:  row.AESIVRef,
				"status":                     row.Status,
			},
		}}, nil
	}
}

// loadSecretKeyRSATableCache 加载指定 AppID + 版本的 RSA 秘钥缓存。
func loadSecretKeyRSATableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		_, _, row, err := loadSecretKeyVersionTableCacheRow(base, params)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if row == nil {
			return []tablecache.Entry{tableCacheSecretKeyVersionEmptyEntry(params.Key)}, nil
		}
		return []tablecache.Entry{{
			Key:  params.Key,
			Type: tablecache.TypeHash,
			Value: map[string]any{
				secretKeyCacheFieldRSAPublicKeyUserRef:    row.RSAPublicKeyUserRef,
				secretKeyCacheFieldRSAPublicKeyServerRef:  row.RSAPublicKeyServerRef,
				secretKeyCacheFieldRSAPrivateKeyServerRef: row.RSAPrivateKeyServerRef,
				"status": row.Status,
			},
		}}, nil
	}
}

// loadSecretKeyVersionTableCacheRow 读取版本化秘钥缓存所需的版本材料记录。
func loadSecretKeyVersionTableCacheRow(base *corelogic.BaseLogic, params tablecache.LoadParams) (string, string, *model.SecretKeyVersion, error) {
	appID, err := tableCacheFirstStringPart(params, "AppID")
	if err != nil {
		return "", "", nil, errors.Tag(err)
	}
	keyVersion, err := tableCacheStringPart(params, 1, "秘钥版本")
	if err != nil {
		return "", "", nil, errors.Tag(err)
	}
	// 版本材料缓存回源主库，避免从读库拿到滞后的秘钥文件引用。
	writeDB, err := TableCacheWriteDB(base, svc.DatabaseMain, "main")
	if err != nil {
		return "", "", nil, errors.Tag(err)
	}
	var row model.SecretKeyVersion
	if err := writeDB.Where("uuid = ? AND key_version = ?", appID, keyVersion).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return appID, keyVersion, nil, nil
		}
		return "", "", nil, errors.Tag(err)
	}
	return appID, keyVersion, &row, nil
}

// tableCacheSecretKeyRouteEmptyEntry 构造秘钥路由不存在时的空值占位缓存。
func tableCacheSecretKeyRouteEmptyEntry(cacheKey string) tablecache.Entry {
	return tablecache.Entry{
		Key:   strings.TrimSpace(cacheKey),
		Type:  tablecache.TypeHash,
		Value: map[string]any{"value": keys.EmptyValueMarker},
		TTL:   corelogic.EmptyCacheTTL(),
	}
}

// tableCacheSecretKeyVersionEmptyEntry 构造秘钥版本不存在时的空值占位缓存。
func tableCacheSecretKeyVersionEmptyEntry(cacheKey string) tablecache.Entry {
	return tablecache.Entry{
		Key:   strings.TrimSpace(cacheKey),
		Type:  tablecache.TypeHash,
		Value: map[string]any{"value": keys.EmptyValueMarker},
		TTL:   corelogic.EmptyCacheTTL(),
	}
}
