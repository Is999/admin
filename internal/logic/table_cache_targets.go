package logic

import (
	"context"
	"strconv"
	"strings"
	"time"

	keys "admin_cron/common/rediskeys"
	"admin_cron/helper"
	"admin_cron/internal/model"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

	"github.com/Is999/go-utils/errors"
	tablecache "github.com/Is999/table-cache"
	"gorm.io/gorm"
)

const (
	// tableCacheRolePermissionPrefix 表示角色权限集合缓存前缀。
	tableCacheRolePermissionPrefix = "role_permission:"
	// tableCacheAdminRoleIDsPrefix 表示管理员启用角色 ID 集合缓存前缀。
	tableCacheAdminRoleIDsPrefix = "admin_role_ids:"
	// tableCacheAdminPermissionIDsPrefix 表示管理员聚合权限 ID 集合缓存前缀。
	tableCacheAdminPermissionIDsPrefix = "admin_permission_ids:"
	// tableCacheAdminPermissionUUIDsPrefix 表示管理员最终权限码集合缓存前缀。
	tableCacheAdminPermissionUUIDsPrefix = "admin_permission_uuids:"
	// tableCacheRoutePermissionIDsPrefix 表示路由别名候选权限 ID 集合缓存前缀。
	tableCacheRoutePermissionIDsPrefix = "route_permission_ids:"
	// tableCacheAdminProfilePrefix 表示管理员公开资料缓存前缀。
	tableCacheAdminProfilePrefix = "admin_profile:"
	// tableCacheAdminRolesDetailPrefix 表示管理员角色名称列表缓存前缀。
	tableCacheAdminRolesDetailPrefix = "admin_roles_detail:"
)

// tableCacheManager 创建 admin-cron 的表数据缓存管理器。
func tableCacheManager(base *BaseLogic) (*tablecache.Manager, error) {
	if base == nil || base.Redis() == nil {
		return nil, errors.Errorf("Redis未初始化")
	}
	return tablecache.NewManager(
		tablecache.NewRedisStore(base.Redis()),
		tableCacheTargets(base),
		tablecache.WithKeyPrefix(tableCacheKeyPrefix(base)),
		tablecache.WithEmptyMarker(keys.EmptyValueMarker, emptyCacheTTL()),
		tablecache.WithLockTTL(cacheRebuildLockTTL),
		tablecache.WithWait(cacheWaitStep, 3),
	)
}

// tableCacheKeyPrefix 返回当前站点 table-cache 托管缓存使用的 Redis Key 前缀。
// 前缀来源于运行期 app_id，确保多站点共用 Redis 时权限、配置和秘钥缓存不会相互覆盖。
func tableCacheKeyPrefix(base *BaseLogic) string {
	if base == nil || base.svc == nil {
		return keys.TableCachePrefix("")
	}
	return keys.TableCachePrefix(base.svc.CurrentConfig().AppID)
}

// tableCachePhysicalKey 把逻辑缓存 key 转换为 table-cache 新版本要求的真实 Redis key。
// 读穿缓存、刷新、删除和直接 Redis 兜底删除都统一使用带前缀的真实 key，确保 miss 后能回源写入新命名空间。
func tableCachePhysicalKey(base *BaseLogic, key string) string {
	key = strings.TrimSpace(key)
	prefix := tableCacheKeyPrefix(base)
	if key == "" || prefix == "" || strings.HasPrefix(key, prefix) || strings.HasPrefix(key, keys.TableCacheDataPrefix) {
		return key
	}
	return prefix + key
}

// tableCacheLogicalKey 去掉 table-cache 项目级前缀，供分类、脱敏和兼容旧模板判断使用。
func tableCacheLogicalKey(base *BaseLogic, key string) string {
	key = strings.TrimSpace(key)
	prefix := tableCacheKeyPrefix(base)
	if prefix != "" && strings.HasPrefix(key, prefix) {
		return strings.TrimPrefix(key, prefix)
	}
	return keys.TrimTableCachePrefix(key)
}

// tableCachePhysicalKeys 批量转换 table-cache 托管缓存 key，并过滤空值。
func tableCachePhysicalKeys(base *BaseLogic, cacheKeys ...string) []string {
	result := make([]string, 0, len(cacheKeys))
	for _, key := range cacheKeys {
		key = tableCachePhysicalKey(base, key)
		if key == "" {
			continue
		}
		result = append(result, key)
	}
	return result
}

// tableCachePhysicalAndLegacyKeys 返回 table-cache 新物理 key 与升级前裸 key。
// 删除链路用它精确清理新旧命名空间 key。
func tableCachePhysicalAndLegacyKeys(base *BaseLogic, cacheKeys ...string) []string {
	result := make([]string, 0, len(cacheKeys)*2)
	for _, cacheKey := range cacheKeys {
		cacheKey = strings.TrimSpace(cacheKey)
		if cacheKey == "" {
			continue
		}
		physicalKey := tableCachePhysicalKey(base, cacheKey)
		if physicalKey != "" {
			result = append(result, physicalKey)
		}
		logicalKey := keys.TrimTableCachePrefix(cacheKey)
		if logicalKey != "" && logicalKey != physicalKey {
			result = append(result, logicalKey)
		}
	}
	return helper.UniqueNonEmptyStrings(result)
}

// tableCacheReadDB 统一获取表缓存回源所需的读库连接，缺失时返回明确错误，避免直接触发 GORM 空指针。
func tableCacheReadDB(base *BaseLogic, database svc.DbName, databaseLabel string) (*gorm.DB, error) {
	if base == nil || base.svc == nil {
		return nil, errors.Errorf("服务上下文未初始化")
	}
	readDB := base.svc.ReadDB(database)
	if readDB == nil {
		return nil, errors.Errorf("%s读库未初始化", strings.TrimSpace(databaseLabel))
	}
	return readDB, nil
}

// tableCacheWriteDB 统一获取表缓存回源所需的主库连接，缺失时返回明确错误，避免直接触发 GORM 空指针。
func tableCacheWriteDB(base *BaseLogic, database svc.DbName, databaseLabel string) (*gorm.DB, error) {
	if base == nil || base.svc == nil {
		return nil, errors.Errorf("服务上下文未初始化")
	}
	writeDB := base.svc.WriteDB(database)
	if writeDB == nil {
		return nil, errors.Errorf("%s主库未初始化", strings.TrimSpace(databaseLabel))
	}
	return writeDB, nil
}

// tableCacheItems 返回缓存管理页使用的通用表缓存目标列表。
func tableCacheItems(base *BaseLogic) []types.CacheItem {
	targets := tableCacheTargets(base)
	items := make([]types.CacheItem, 0, len(targets))
	for _, target := range targets {
		keyTitle := target.KeyTitle
		if keyTitle == "" {
			keyTitle = target.Key
		}
		keyTitle = tableCachePhysicalKey(base, keyTitle)
		logicalKeyTitle := tableCacheLogicalKey(base, keyTitle)
		items = append(items, types.CacheItem{
			Index:        target.Index,
			Key:          keyTitle,
			KeyTitle:     keyTitle,
			Type:         string(target.Type),
			Remark:       target.Remark,
			Category:     tableCacheCategory(target.Index, logicalKeyTitle),
			IsTemplate:   isTemplateCachePattern(keyTitle),
			ExampleKey:   tableCacheExampleKey(keyTitle),
			AutoRebuild:  true,
			RefreshScope: tableCacheRefreshScope(target),
		})
	}
	return items
}

// tableCacheCategory 根据缓存目标索引和 key 模板归类缓存用途，供管理页分组展示。
func tableCacheCategory(index string, key string) string {
	switch {
	case strings.HasPrefix(key, "admin:info:"):
		return "session"
	case strings.HasPrefix(key, "secret_key_"):
		return "secret"
	case strings.HasPrefix(key, "config_uuid:"):
		return "config"
	case strings.HasPrefix(key, "admin_"), strings.HasPrefix(key, "route_permission_"), strings.Contains(index, "permission"), strings.Contains(index, "role"):
		return "auth"
	default:
		return "system"
	}
}

// tableCacheExampleKey 为模板型 key 生成管理页可直接使用的示例 key。
func tableCacheExampleKey(key string) string {
	replacer := strings.NewReplacer(
		"%d", "1",
		"%s", "demo",
		"%v", "value",
		"{adminID}", "1",
		"{roleID}", "1",
		"{uuid}", "demo",
		"{keyVersion}", "v1",
		"{routeAlias}", "admin.list",
	)
	return replacer.Replace(key)
}

// tableCacheRefreshScope 返回缓存目标刷新粒度描述。
func tableCacheRefreshScope(target tablecache.Target) string {
	switch {
	case target.RefreshAll:
		return "all"
	case isTemplateCachePattern(target.KeyTitle):
		return "single"
	case strings.HasSuffix(target.Key, ":"):
		return "prefix"
	default:
		return "single"
	}
}

// isTemplateCachePattern 判断缓存管理页中的 key 是否属于模板型缓存键。
// 这里同时兼容 `{id}` 与 `%s/%d/%v` 两类占位写法，保证前后端识别规则一致。
func isTemplateCachePattern(key string) bool {
	return strings.Contains(key, "{") || strings.Contains(key, "%")
}

// cacheTemplatePrefix 返回模板型缓存键的固定前缀部分。
// 管理页在匹配真实 Redis Key 与模板缓存项时，会基于该前缀做快速归类。
func cacheTemplatePrefix(key string) string {
	braceIndex := strings.Index(key, "{")
	percentIndex := strings.Index(key, "%")
	switch {
	case braceIndex >= 0 && percentIndex >= 0:
		if braceIndex < percentIndex {
			return key[:braceIndex]
		}
		return key[:percentIndex]
	case braceIndex >= 0:
		return key[:braceIndex]
	case percentIndex >= 0:
		return key[:percentIndex]
	default:
		return key
	}
}

// tableCacheTargets 声明所有通用表缓存目标，业务只在这里描述数据如何回源。
func tableCacheTargets(base *BaseLogic) []tablecache.Target {
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
			Key:              tableCacheRolePermissionPrefix,
			KeyTitle:         "role_permission:{roleID}",
			Type:             tablecache.TypeSet,
			Remark:           "单个角色权限集合缓存",
			AllowEmptyMarker: true,
			Loader:           loadRolePermissionTableCache(base),
		},
		{
			Index:            "admin_role_ids",
			Title:            "管理员角色ID",
			Key:              tableCacheAdminRoleIDsPrefix,
			KeyTitle:         "admin_role_ids:{adminID}",
			Type:             tablecache.TypeSet,
			Remark:           "管理员启用角色ID集合缓存",
			TTL:              time.Hour,
			AllowEmptyMarker: true,
			Loader:           loadAdminRoleIDsTableCache(base),
		},
		{
			Index:            "admin_permission_ids",
			Title:            "管理员权限ID",
			Key:              tableCacheAdminPermissionIDsPrefix,
			KeyTitle:         "admin_permission_ids:{adminID}",
			Type:             tablecache.TypeSet,
			Remark:           "管理员聚合权限ID集合缓存",
			TTL:              time.Hour,
			AllowEmptyMarker: true,
			Loader:           loadAdminPermissionIDsTableCache(base),
		},
		{
			Index:            "admin_permission_uuids",
			Title:            "管理员权限码",
			Key:              tableCacheAdminPermissionUUIDsPrefix,
			KeyTitle:         "admin_permission_uuids:{adminID}",
			Type:             tablecache.TypeSet,
			Remark:           "管理员最终权限码集合缓存",
			TTL:              time.Hour,
			AllowEmptyMarker: true,
			Loader:           loadAdminPermissionUUIDsTableCache(base),
		},
		{
			Index:            "admin_profile",
			Title:            "管理员公开资料",
			Key:              tableCacheAdminProfilePrefix,
			KeyTitle:         "admin_profile:{adminID}",
			Type:             tablecache.TypeString,
			Remark:           "管理员公开资料缓存",
			TTL:              time.Hour,
			AllowEmptyMarker: true,
			Loader:           loadAdminProfileTableCache(base),
		},
		{
			Index:            "admin_roles_detail",
			Title:            "管理员角色详情",
			Key:              tableCacheAdminRolesDetailPrefix,
			KeyTitle:         "admin_roles_detail:{adminID}",
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
			Key:              tableCacheRoutePermissionIDsPrefix,
			KeyTitle:         "route_permission_ids:{routeAlias}",
			Type:             tablecache.TypeSet,
			Remark:           "路由别名候选权限ID集合缓存",
			TTL:              time.Hour,
			AllowEmptyMarker: true,
			Loader:           loadRoutePermissionIDsTableCache(base),
		},
		{
			Index:            "config_uuid",
			Title:            "系统常量配置",
			Key:              "config_uuid:",
			KeyTitle:         "config_uuid:{uuid}",
			Type:             tablecache.TypeHash,
			Remark:           "系统常量配置缓存",
			TTL:              time.Hour,
			AllowEmptyMarker: true,
			Loader:           loadSysConfigTableCache(base),
		},
		{
			Index:    "secret_key_route",
			Title:    "秘钥路由配置",
			Key:      "secret_key_route:",
			KeyTitle: "secret_key_route:{uuid}",
			Type:     tablecache.TypeHash,
			Remark:   "秘钥稳定版与灰度版路由缓存",
			TTL:      time.Hour,
			Loader:   loadSecretKeyRouteTableCache(base),
		},
		{
			Index:    "secret_key_aes",
			Title:    "AES秘钥配置",
			Key:      "secret_key_aes:",
			KeyTitle: "secret_key_aes:{uuid}:{keyVersion}",
			Type:     tablecache.TypeHash,
			Remark:   "版本化 AES 秘钥配置缓存",
			TTL:      time.Hour,
			Loader:   loadSecretKeyAESTableCache(base),
		},
		{
			Index:    "secret_key_rsa",
			Title:    "RSA秘钥配置",
			Key:      "secret_key_rsa:",
			KeyTitle: "secret_key_rsa:{uuid}:{keyVersion}",
			Type:     tablecache.TypeHash,
			Remark:   "版本化 RSA 秘钥配置缓存",
			TTL:      time.Hour,
			Loader:   loadSecretKeyRSATableCache(base),
		},
	}
}

// loadRoutePermissionIDsTableCache 加载单个路由别名候选权限 ID 集合缓存。
func loadRoutePermissionIDsTableCache(base *BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		routeAlias, err := tableCacheFirstStringPart(params, "路由别名")
		if err != nil {
			return nil, errors.Tag(err)
		}
		permissionLogic := &AdminPermissionLogic{BaseLogic: base}
		permissionIDs, err := permissionLogic.permissionCandidateIDsWithCache([]string{routeAlias})
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
func loadAdminPermissionUUIDsTableCache(base *BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		adminID, err := tableCacheFirstIntPart(params, "管理员ID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		securityLogic := NewSecurityLogic(ctx, base.svc)
		permissionIDs, err := securityLogic.userPermissionIDsWithCache(adminID)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if len(permissionIDs) == 0 {
			return nil, nil
		}
		permissionLogic := &AdminPermissionLogic{BaseLogic: base}
		codesArr, err := permissionLogic.permissionUUIDsByIDsWithCache(permissionIDs)
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
func loadAdminProfileTableCache(base *BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		adminID, err := tableCacheFirstIntPart(params, "管理员ID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		adminLogic := &AdminLogic{BaseLogic: base}
		admin, err := adminLogic.GetAdminByID(adminID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, errors.Tag(err)
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeString,
			Value: buildAdminProfileCache(admin),
		}}, nil
	}
}

// loadAdminRolesDetailTableCache 加载单个管理员角色名称列表缓存。
func loadAdminRolesDetailTableCache(base *BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		adminID, err := tableCacheFirstIntPart(params, "管理员ID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		roleLogic := &AdminRoleLogic{BaseLogic: base}
		roleIDs, err := roleLogic.enabledRoleIDsByUserWithCache(adminID)
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
func loadRoleTreeTableCache(base *BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		roleLogic := &AdminRoleLogic{BaseLogic: base}
		roles, err := roleLogic.loadAllRoles()
		if err != nil {
			return nil, errors.Tag(err)
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeString,
			Value: buildRoleTree(roles, nil),
		}}, nil
	}
}

// loadRoleStatusTableCache 加载角色状态 Hash 缓存数据。
func loadRoleStatusTableCache(base *BaseLogic) tablecache.Loader {
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
func loadRolePermissionTableCache(base *BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		roleID, err := tableCacheFirstIntPart(params, "角色ID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		roleLogic := &AdminRoleLogic{BaseLogic: base}
		permissionIDs, err := roleLogic.rolePermissionIDs(roleID)
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
func loadAdminRoleIDsTableCache(base *BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		adminID, err := tableCacheFirstIntPart(params, "管理员ID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		roleLogic := &AdminRoleLogic{BaseLogic: base}
		roleIDs, err := roleLogic.userRoleIDs(adminID)
		if err != nil {
			return nil, errors.Tag(err)
		}
		roleIDs, err = roleLogic.filterEnabledRoleIDsWithCache(roleIDs)
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
func loadAdminPermissionIDsTableCache(base *BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		adminID, err := tableCacheFirstIntPart(params, "管理员ID")
		if err != nil {
			return nil, errors.Tag(err)
		}
		roleLogic := &AdminRoleLogic{BaseLogic: base}
		roleIDs, err := roleLogic.userRoleIDs(adminID)
		if err != nil {
			return nil, errors.Tag(err)
		}
		roleIDs, err = roleLogic.filterEnabledRoleIDsWithCache(roleIDs)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if len(roleIDs) == 0 {
			return nil, nil
		}
		permissionIDs := make([]int, 0)
		for _, roleID := range roleIDs {
			currentPermissionIDs, currentErr := roleLogic.rolePermissionIDsWithCache(roleID)
			if currentErr != nil {
				return nil, currentErr
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
func loadPermissionTreeTableCache(base *BaseLogic) tablecache.Loader {
	return func(ctx context.Context, params tablecache.LoadParams) ([]tablecache.Entry, error) {
		permissionLogic := &AdminPermissionLogic{BaseLogic: base}
		permissions, err := permissionLogic.loadAllPermissions()
		if err != nil {
			return nil, errors.Tag(err)
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeString,
			Value: buildPermissionTree(permissions, nil, nil),
		}}, nil
	}
}

// loadPermissionModuleTableCache 加载权限 module Hash 缓存数据。
func loadPermissionModuleTableCache(base *BaseLogic) tablecache.Loader {
	return loadPermissionFieldTableCache(base, keys.PermissionModule, "module")
}

// loadPermissionUUIDTableCache 加载权限 uuid Hash 缓存数据。
func loadPermissionUUIDTableCache(base *BaseLogic) tablecache.Loader {
	return loadPermissionFieldTableCache(base, keys.PermissionUUID, "uuid")
}

// loadPermissionFieldTableCache 加载权限表指定字段缓存数据。
func loadPermissionFieldTableCache(base *BaseLogic, cacheKey string, field string) tablecache.Loader {
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

// loadSysConfigTableCache 加载单个系统常量配置 Hash 缓存数据。
func loadSysConfigTableCache(base *BaseLogic) tablecache.Loader {
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
			"updatedAt": formatDateTime(cfg.UpdatedAt),
		}
		return []tablecache.Entry{{
			Key:   params.Key,
			Type:  tablecache.TypeHash,
			Value: cache,
		}}, nil
	}
}

// loadSecretKeyRouteTableCache 加载指定 AppID 的秘钥版本路由缓存。
func loadSecretKeyRouteTableCache(base *BaseLogic) tablecache.Loader {
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
func loadSecretKeyAESTableCache(base *BaseLogic) tablecache.Loader {
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
func loadSecretKeyRSATableCache(base *BaseLogic) tablecache.Loader {
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
func loadSecretKeyVersionTableCacheRow(base *BaseLogic, params tablecache.LoadParams) (string, string, *model.SecretKeyVersion, error) {
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
		TTL:   emptyCacheTTL(),
	}
}

// tableCacheSecretKeyVersionEmptyEntry 构造秘钥版本不存在时的空值占位缓存。
func tableCacheSecretKeyVersionEmptyEntry(cacheKey string) tablecache.Entry {
	return tablecache.Entry{
		Key:   strings.TrimSpace(cacheKey),
		Type:  tablecache.TypeHash,
		Value: map[string]any{"value": keys.EmptyValueMarker},
		TTL:   emptyCacheTTL(),
	}
}

// tableCacheFirstStringPart 读取前缀型缓存 key 的第一个参数。
func tableCacheFirstStringPart(params tablecache.LoadParams, title string) (string, error) {
	return tableCacheStringPart(params, 0, title)
}

// tableCacheStringPart 读取前缀型缓存 key 的指定字符串参数。
func tableCacheStringPart(params tablecache.LoadParams, index int, title string) (string, error) {
	if index < 0 || len(params.KeyParts) <= index || strings.TrimSpace(params.KeyParts[index]) == "" {
		return "", errors.Errorf("%s不能为空", title)
	}
	return strings.TrimSpace(params.KeyParts[index]), nil
}

// tableCacheFirstIntPart 读取前缀型缓存 key 的第一个整数参数。
func tableCacheFirstIntPart(params tablecache.LoadParams, title string) (int, error) {
	value, err := tableCacheFirstStringPart(params, title)
	if err != nil {
		return 0, errors.Tag(err)
	}
	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 {
		return 0, errors.Errorf("%s不合法: %s", title, value)
	}
	return id, nil
}
