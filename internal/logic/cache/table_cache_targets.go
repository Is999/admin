package cache

import (
	"context"
	"strconv"
	"strings"
	"time"

	keys "admin/common/rediskeys"
	corelogic "admin/internal/logic"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	tablecache "github.com/Is999/table-cache"
	"gorm.io/gorm"
)

const (
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
			RefreshAll: true,
			Loader:     loadRoleStatusTableCache(base),
		},
		{
			Index:            "role_permission",
			Title:            "角色权限",
			Key:              cacheTemplatePrefix(keys.RolePermissionPattern),
			KeyTitle:         keys.RolePermissionPattern,
			Type:             tablecache.TypeSet,
			Remark:           "单个角色权限集合缓存",
			AllowEmptyMarker: true,
			Loader:           loadRolePermissionTableCache(base),
		},
		{
			Index:            "admin_role_ids",
			Title:            "管理员角色ID",
			Key:              cacheTemplatePrefix(keys.AdminRoleIDsPattern),
			KeyTitle:         keys.AdminRoleIDsPattern,
			Type:             tablecache.TypeSet,
			Remark:           "管理员启用角色ID集合缓存",
			TTL:              time.Hour,
			AllowEmptyMarker: true,
			Loader:           loadAdminRoleIDsTableCache(base),
		},
		{
			Index:            "admin_permission_ids",
			Title:            "管理员权限ID",
			Key:              cacheTemplatePrefix(keys.AdminPermissionIDsPattern),
			KeyTitle:         keys.AdminPermissionIDsPattern,
			Type:             tablecache.TypeSet,
			Remark:           "管理员聚合权限ID集合缓存",
			TTL:              time.Hour,
			AllowEmptyMarker: true,
			Loader:           loadAdminPermissionIDsTableCache(base),
		},
		{
			Index:            "admin_permission_uuids",
			Title:            "管理员权限码",
			Key:              cacheTemplatePrefix(keys.AdminPermissionUUIDsPattern),
			KeyTitle:         keys.AdminPermissionUUIDsPattern,
			Type:             tablecache.TypeSet,
			Remark:           "管理员最终权限码集合缓存",
			TTL:              time.Hour,
			AllowEmptyMarker: true,
			Loader:           loadAdminPermissionUUIDsTableCache(base),
		},
		{
			Index:            "admin_profile",
			Title:            "管理员公开资料",
			Key:              cacheTemplatePrefix(keys.AdminProfilePattern),
			KeyTitle:         keys.AdminProfilePattern,
			Type:             tablecache.TypeString,
			Remark:           "管理员公开资料缓存",
			TTL:              time.Hour,
			AllowEmptyMarker: true,
			Loader:           loadAdminProfileTableCache(base),
		},
		{
			Index:            "admin_roles_detail",
			Title:            "管理员角色详情",
			Key:              cacheTemplatePrefix(keys.AdminRolesDetailPattern),
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
			Index:      keys.PermissionModule,
			Title:      "权限模块",
			Key:        keys.PermissionModule,
			KeyTitle:   keys.PermissionModule,
			Type:       tablecache.TypeHash,
			Remark:     "权限模块缓存",
			RefreshAll: true,
			Loader:     loadPermissionModuleTableCache(base),
		},
		{
			Index:      keys.PermissionUUID,
			Title:      "权限UUID",
			Key:        keys.PermissionUUID,
			KeyTitle:   keys.PermissionUUID,
			Type:       tablecache.TypeHash,
			Remark:     "权限UUID缓存",
			RefreshAll: true,
			Loader:     loadPermissionUUIDTableCache(base),
		},
		{
			Index:            "route_permission_ids",
			Title:            "路由权限候选ID",
			Key:              cacheTemplatePrefix(keys.RoutePermissionIDsPattern),
			KeyTitle:         keys.RoutePermissionIDsPattern,
			Type:             tablecache.TypeSet,
			Remark:           "路由别名候选权限ID集合缓存",
			TTL:              time.Hour,
			AllowEmptyMarker: true,
			Loader:           loadRoutePermissionIDsTableCache(base),
		},
		{
			Index:            "config_uuid",
			Title:            "系统常量配置",
			Key:              cacheTemplatePrefix(keys.SysConfigUUIDPattern),
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
			Key:              cacheTemplatePrefix(keys.RuntimeConfigStatePattern),
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
			Key:              cacheTemplatePrefix(keys.RuntimeConfigReleasePattern),
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
			Key:      cacheTemplatePrefix(keys.SecretKeyRoutePattern),
			KeyTitle: keys.SecretKeyRoutePattern,
			Type:     tablecache.TypeHash,
			Remark:   "秘钥稳定版与灰度版路由缓存",
			TTL:      time.Hour,
			Loader:   loadSecretKeyRouteTableCache(base),
		},
		{
			Index:    "secret_key_aes",
			Title:    "AES秘钥配置",
			Key:      cacheTemplatePrefix(keys.SecretKeyAESVersionPattern),
			KeyTitle: keys.SecretKeyAESVersionPattern,
			Type:     tablecache.TypeHash,
			Remark:   "版本化 AES 秘钥配置缓存",
			TTL:      time.Hour,
			Loader:   loadSecretKeyAESTableCache(base),
		},
		{
			Index:    "secret_key_rsa",
			Title:    "RSA秘钥配置",
			Key:      cacheTemplatePrefix(keys.SecretKeyRSAVersionPattern),
			KeyTitle: keys.SecretKeyRSAVersionPattern,
			Type:     tablecache.TypeHash,
			Remark:   "版本化 RSA 秘钥配置缓存",
			TTL:      time.Hour,
			Loader:   loadSecretKeyRSATableCache(base),
		},
	}
}

// loadRoutePermissionIDsTableCache 加载单个路由别名候选权限 ID 集合缓存。
func loadRoutePermissionIDsTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		routeAlias, err := tableCacheFirstStringPart(params, "路由别名")
		if err != nil {
			return nil, errors.Tag(err)
		}
		permissionIDs, err := loadRoutePermissionIDsForCache(base, routeAlias)
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

// loadAdminPermissionUUIDsTableCache 加载单个管理员最终权限码集合缓存。
func loadAdminPermissionUUIDsTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		adminID, err := tableCacheFirstIntPart(params, "管理员ID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		permissionIDs, err := loadAdminPermissionIDsForCache(base, adminID)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if len(permissionIDs) == 0 {
			return nil, nil
		}
		codesArr, err := loadPermissionUUIDsByIDsForCache(base, permissionIDs)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if len(codesArr) == 0 {
			return nil, nil
		}
		values := make([]any, 0, len(codesArr))
		for _, code := range codesArr {
			values = append(values, code)
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeSet,
			Value: values,
		}}, nil
	}
}

// loadAdminProfileTableCache 加载单个管理员公开资料缓存。
func loadAdminProfileTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		adminID, err := tableCacheFirstIntPart(params, "管理员ID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		admin, err := loadAdminByIDForCache(base, adminID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, errors.Tag(err)
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeString,
			Value: BuildAdminProfileCache(admin),
		}}, nil
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
		// 角色名称缓存回源使用主库，空连接时返回明确错误而不是触发 GORM panic。
		readDB, err := tableCacheReadDB(base, svc.DatabaseMain, "main")
		if err != nil {
			return nil, errors.Tag(err)
		}
		if err := readDB.Model(&model.AdminRole{}).
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
			Value: buildTableCacheRoleTree(roles, nil),
		}}, nil
	}
}

// loadRoleStatusTableCache 加载角色状态 Hash 缓存数据。
func loadRoleStatusTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		var roles []model.AdminRole
		// 角色状态缓存来源于 admin_role，统一从主库读取。
		readDB, err := tableCacheReadDB(base, svc.DatabaseMain, "main")
		if err != nil {
			return nil, errors.Tag(err)
		}
		if err := readDB.Where("is_delete = 0").Find(&roles).Error; err != nil {
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
		roleID, err := tableCacheFirstIntPart(params, "角色ID")
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

// loadAdminRoleIDsTableCache 加载单个管理员启用角色 ID 集合缓存。
func loadAdminRoleIDsTableCache(base *corelogic.BaseLogic) tablecache.Loader {
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

// loadAdminPermissionIDsTableCache 加载单个管理员聚合权限 ID 集合缓存。
func loadAdminPermissionIDsTableCache(base *corelogic.BaseLogic) tablecache.Loader {
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
		permissionIDs := make([]int, 0)
		for _, roleID := range roleIDs {
			currentPermissionIDs, currentErr := loadRolePermissionIDsForCache(base, roleID)
			if currentErr != nil {
				return nil, errors.Wrapf(currentErr, "加载角色权限缓存失败 role_id=%d", roleID)
			}
			permissionIDs = append(permissionIDs, currentPermissionIDs...)
		}
		permissionIDs = types.UniquePositiveInts(permissionIDs)
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
			Value: buildTableCachePermissionTree(permissions, nil, nil),
		}}, nil
	}
}

// loadPermissionModuleTableCache 加载权限 module Hash 缓存数据。
func loadPermissionModuleTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return loadPermissionFieldTableCache(base, keys.PermissionModule, "module")
}

// loadPermissionUUIDTableCache 加载权限 uuid Hash 缓存数据。
func loadPermissionUUIDTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return loadPermissionFieldTableCache(base, keys.PermissionUUID, "uuid")
}

// loadPermissionFieldTableCache 加载权限表指定字段缓存数据。
func loadPermissionFieldTableCache(base *corelogic.BaseLogic, cacheKey string, field string) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		var permissions []model.AdminPermission
		// 权限字段缓存来源于 admin_permission，统一从主库读取。
		readDB, err := tableCacheReadDB(base, svc.DatabaseMain, "main")
		if err != nil {
			return nil, errors.Tag(err)
		}
		if err := readDB.Where("status = 1").Find(&permissions).Error; err != nil {
			return nil, errors.Tag(err)
		}
		cache := make(map[string]any, len(permissions))
		for _, permission := range permissions {
			switch field {
			case "module":
				cache[strconv.Itoa(permission.ID)] = permission.Module
			default:
				cache[strconv.Itoa(permission.ID)] = permission.UUID
			}
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeHash,
			Value: cache,
		}}, nil
	}
}

// loadAdminByIDForCache 读取管理员公开资料缓存所需的管理员模型。
func loadAdminByIDForCache(base *corelogic.BaseLogic, adminID int) (*model.Admin, error) {
	writeDB, err := tableCacheWriteDB(base, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var admin model.Admin
	if err := writeDB.Where("id = ?", adminID).First(&admin).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return &admin, nil
}

// loadAllRolesForCache 读取角色树缓存所需的全部有效角色。
func loadAllRolesForCache(base *corelogic.BaseLogic) ([]model.AdminRole, error) {
	readDB, err := tableCacheReadDB(base, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var roles []model.AdminRole
	if err := readDB.Where("is_delete = 0").Order("id ASC").Find(&roles).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return roles, nil
}

// loadAllPermissionsForCache 读取权限树缓存所需的全部权限。
func loadAllPermissionsForCache(base *corelogic.BaseLogic) ([]model.AdminPermission, error) {
	readDB, err := tableCacheReadDB(base, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var permissions []model.AdminPermission
	if err := readDB.Order("id ASC").Find(&permissions).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return permissions, nil
}

// loadRoutePermissionIDsForCache 读取路由别名对应的启用权限 ID。
func loadRoutePermissionIDsForCache(base *corelogic.BaseLogic, routeAlias string) ([]int, error) {
	routeAlias = strings.TrimSpace(routeAlias)
	if routeAlias == "" {
		return []int{}, nil
	}
	readDB, err := tableCacheReadDB(base, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var permissionIDs []int
	if err := readDB.Model(&model.AdminPermission{}).
		Where("status = 1 AND module = ?", routeAlias).
		Order("id ASC").
		Pluck("id", &permissionIDs).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return types.UniquePositiveInts(permissionIDs), nil
}

// loadEnabledRoleIDsByUserForCache 读取管理员绑定的启用角色 ID。
func loadEnabledRoleIDsByUserForCache(base *corelogic.BaseLogic, adminID int) ([]int, error) {
	if adminID <= 0 {
		return []int{}, nil
	}
	readDB, err := tableCacheReadDB(base, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var roleIDs []int
	if err := readDB.Table(model.TableNameAdminRoleRel+" AS rel").
		Joins("JOIN "+model.TableNameAdminRole+" AS role ON role.id = rel.role_id AND role.status = 1 AND role.is_delete = 0").
		Where("rel.user_id = ?", adminID).
		Order("rel.role_id ASC").
		Pluck("rel.role_id", &roleIDs).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return types.UniquePositiveInts(roleIDs), nil
}

// loadRolePermissionIDsForCache 读取单个角色绑定的启用权限 ID。
func loadRolePermissionIDsForCache(base *corelogic.BaseLogic, roleID int) ([]int, error) {
	if roleID <= 0 {
		return []int{}, nil
	}
	readDB, err := tableCacheReadDB(base, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var permissionIDs []int
	if err := readDB.Table(model.TableNameAdminRolePermissionRel+" AS rel").
		Joins("JOIN "+model.TableNameAdminPermission+" AS permission ON permission.id = rel.permission_id AND permission.status = 1").
		Where("rel.role_id = ?", roleID).
		Order("rel.permission_id ASC").
		Pluck("rel.permission_id", &permissionIDs).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return types.UniquePositiveInts(permissionIDs), nil
}

// loadAdminPermissionIDsForCache 读取管理员聚合启用权限 ID。
func loadAdminPermissionIDsForCache(base *corelogic.BaseLogic, adminID int) ([]int, error) {
	roleIDs, err := loadEnabledRoleIDsByUserForCache(base, adminID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if len(roleIDs) == 0 {
		return []int{}, nil
	}
	readDB, err := tableCacheReadDB(base, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var permissionIDs []int
	if err := readDB.Table(model.TableNameAdminRolePermissionRel+" AS rel").
		Joins("JOIN "+model.TableNameAdminPermission+" AS permission ON permission.id = rel.permission_id AND permission.status = 1").
		Where("rel.role_id IN ?", roleIDs).
		Order("rel.permission_id ASC").
		Distinct("rel.permission_id").
		Pluck("rel.permission_id", &permissionIDs).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return types.UniquePositiveInts(permissionIDs), nil
}

// loadPermissionUUIDsByIDsForCache 读取启用权限码集合。
func loadPermissionUUIDsByIDsForCache(base *corelogic.BaseLogic, permissionIDs []int) ([]string, error) {
	permissionIDs = types.UniquePositiveInts(permissionIDs)
	if len(permissionIDs) == 0 {
		return []string{}, nil
	}
	readDB, err := tableCacheReadDB(base, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var uuids []string
	if err := readDB.Model(&model.AdminPermission{}).
		Distinct("uuid").
		Where("id IN ? AND status = 1", permissionIDs).
		Order("id ASC").
		Pluck("uuid", &uuids).Error; err != nil {
		return nil, errors.Tag(err)
	}
	seen := make(map[string]struct{}, len(uuids))
	result := make([]string, 0, len(uuids))
	for _, uuid := range uuids {
		uuid = strings.TrimSpace(uuid)
		if uuid == "" {
			continue
		}
		if _, ok := seen[uuid]; ok {
			continue
		}
		seen[uuid] = struct{}{}
		result = append(result, uuid)
	}
	return result, nil
}

// buildTableCacheRoleTree 把平铺角色列表转换成缓存用角色树。
func buildTableCacheRoleTree(roles []model.AdminRole, permissionMap map[int][]int) []types.AdminRoleItem {
	children := make(map[int][]model.AdminRole, len(roles))
	for _, role := range roles {
		children[role.Pid] = append(children[role.Pid], role)
	}
	var walk func(pid int) []types.AdminRoleItem
	walk = func(pid int) []types.AdminRoleItem {
		nodes := children[pid]
		result := make([]types.AdminRoleItem, 0, len(nodes))
		for _, role := range nodes {
			result = append(result, tableCacheRoleModelToItem(role, permissionMap[role.ID], walk(role.ID)))
		}
		return result
	}
	return walk(0)
}

// tableCacheRoleModelToItem 把角色模型转换成缓存树节点。
func tableCacheRoleModelToItem(role model.AdminRole, permissionIDs []int, children []types.AdminRoleItem) types.AdminRoleItem {
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

// buildTableCachePermissionTree 把平铺权限列表转换成缓存用权限树。
func buildTableCachePermissionTree(permissions []model.AdminPermission, checked map[int]struct{}, disabled map[int]struct{}) []types.AdminPermissionItem {
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
			result = append(result, tableCachePermissionModelToItem(permission, isChecked, isDisabled, walk(permission.ID)))
		}
		return result
	}
	return walk(0)
}

// tableCachePermissionModelToItem 把权限模型转换成缓存树节点。
func tableCachePermissionModelToItem(permission model.AdminPermission, checked bool, disabled bool, children []types.AdminPermissionItem) types.AdminPermissionItem {
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

// loadSysConfigTableCache 加载单个系统常量配置 Hash 缓存数据。
func loadSysConfigTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		uuid, err := tableCacheFirstStringPart(params, "配置UUID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		// 系统配置缓存需要读取最新配置值，回源时走主库。
		writeDB, err := tableCacheWriteDB(base, svc.DatabaseMain, "main")
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

// loadRuntimeConfigStateTableCache 加载指定环境的运行配置 active 版本状态。
func loadRuntimeConfigStateTableCache(base *corelogic.BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		env, err := tableCacheFirstStringPart(params, "运行环境")
		if err != nil {
			return nil, errors.Tag(err)
		}
		writeDB, err := tableCacheWriteDB(base, svc.DatabaseMain, "main")
		if err != nil {
			return nil, errors.Tag(err)
		}
		var state model.RuntimeConfigState
		if err = writeDB.Where("app_id = ? AND env = ?", base.AppID(), env).First(&state).Error; err != nil {
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
		writeDB, err := tableCacheWriteDB(base, svc.DatabaseMain, "main")
		if err != nil {
			return nil, errors.Tag(err)
		}
		var release model.RuntimeConfigRelease
		if err = writeDB.Where("id = ? AND app_id = ?", releaseID, base.AppID()).First(&release).Error; err != nil {
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
		writeDB, err := tableCacheWriteDB(base, svc.DatabaseMain, "main")
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
	writeDB, err := tableCacheWriteDB(base, svc.DatabaseMain, "main")
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
