package cache

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	keys "admin/common/rediskeys"
	"admin/helper"
	corelogic "admin/internal/logic"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	tablecache "github.com/Is999/table-cache"
	"github.com/redis/go-redis/v9"
)

const (
	// redisExactDeleteBatchSize 表示精确 DEL 时单批 key 数量，避免一次命令过大影响 Redis。
	redisExactDeleteBatchSize = 200
	// rolePermissionInvalidateQueryBatchSize 表示权限定义变更后枚举角色 ID 的单批数量。
	rolePermissionInvalidateQueryBatchSize = 500
	// adminPermissionInvalidateQueryBatchSize 表示权限定义变更后枚举管理员 ID 的单批数量，避免一次性加载全量管理员。
	adminPermissionInvalidateQueryBatchSize = 500
)

// redisPipelinedClient 表示支持管道执行的 Redis 客户端。
// redis.UniversalClient 接口未暴露 Pipelined，但实际单机和 Cluster 客户端都支持该能力。
type redisPipelinedClient interface {
	Pipelined(ctx context.Context, fn func(redis.Pipeliner) error) ([]redis.Cmder, error)
}

// ParsePositiveIntStrings 把字符串切片转换成去重后的正整数切片。
func ParsePositiveIntStrings(values []string, title string) ([]int, error) {
	result := make([]int, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || corelogic.CacheIsEmptyMarker(value) {
			continue
		}
		id, err := strconv.Atoi(value)
		if err != nil || id <= 0 {
			if err != nil {
				return nil, errors.Wrapf(err, "ParsePositiveIntStrings 解析%s失败 value=%s", title, value)
			}
			return nil, errors.Errorf("ParsePositiveIntStrings 解析%s失败，非正整数 value=%s", title, value)
		}
		result = append(result, id)
	}
	return types.UniquePositiveInts(result), nil
}

// DeleteRedisKeysExactBatches 按精确 key 批量删除 Redis 缓存，禁止使用 SCAN/通配符兜底。
func DeleteRedisKeysExactBatches(base *corelogic.BaseLogic, title string, cacheKeys []string) {
	if base == nil || base.Redis() == nil {
		return
	}
	cacheKeys = helper.UniqueNonEmptyStrings(cacheKeys)
	ctx := base.Ctx
	client := base.Redis()
	pipelinedClient, canPipeline := client.(redisPipelinedClient)
	for start := 0; start < len(cacheKeys); start += redisExactDeleteBatchSize {
		end := start + redisExactDeleteBatchSize
		if end > len(cacheKeys) {
			end = len(cacheKeys)
		}
		batch := cacheKeys[start:end]
		if len(batch) == 0 {
			continue
		}
		// Redis Cluster 要求单条 DEL 的所有 key 位于同一 slot。
		// 这里使用 pipeline 执行多条单 key DEL，既保留批量往返优化，又避免跨 slot 命令失败。
		if canPipeline {
			if _, err := pipelinedClient.Pipelined(ctx, func(pipe redis.Pipeliner) error {
				for _, key := range batch {
					pipe.Del(ctx, key)
				}
				return nil
			}); err != nil {
				corelogic.LogWrappedError(base, err, "%s 精确删除Redis缓存失败 batch_start=%d batch_size=%d", strings.TrimSpace(title), start, len(batch))
			}
			continue
		}
		// 极少数自定义 UniversalClient 可能不暴露 Pipelined；退化为单 key DEL，保持 Redis Cluster 安全。
		for offset, key := range batch {
			if err := client.Del(ctx, key).Err(); err != nil {
				corelogic.LogWrappedError(base, err, "%s 精确删除Redis缓存失败 batch_start=%d batch_offset=%d key=%s", strings.TrimSpace(title), start, offset, key)
			}
		}
	}
}

// TrackRoutePermissionAliasCache 登记已访问的路由权限候选缓存别名，权限变更时可按索引精确删除。
func TrackRoutePermissionAliasCache(base *corelogic.BaseLogic, routeAlias string) {
	if base == nil || base.Redis() == nil {
		return
	}
	routeAlias = strings.TrimSpace(routeAlias)
	if routeAlias == "" {
		return
	}
	indexKey := TableCachePhysicalKey(base, keys.RoutePermissionAliasIndex)
	if err := base.Redis().SAdd(base.Ctx, indexKey, routeAlias).Err(); err != nil {
		corelogic.LogWrappedError(base, err, "TrackRoutePermissionAliasCache 登记路由权限候选缓存索引失败 route_alias=%s", routeAlias)
	}
}

// InvalidateAdminRelationCache 删除管理员关系缓存，并清理登录态触发资料重建。
func InvalidateAdminRelationCache(base *corelogic.BaseLogic, adminIDs ...int) {
	invalidateAdminRelationCacheWithOptions(base, false, adminIDs...)
}

// InvalidateAdminRelationCachePreserveSession 删除管理员关系缓存，但保留登录态。
// 适用于个人中心自助更新场景，避免刚更新完资料就把当前会话自身打成未登录。
func InvalidateAdminRelationCachePreserveSession(base *corelogic.BaseLogic, adminIDs ...int) {
	invalidateAdminRelationCacheWithOptions(base, true, adminIDs...)
}

// InvalidateAdminRoleAndPermissionCacheByAdminIDs 精确删除指定管理员的角色与权限聚合缓存。
// 角色变更只影响绑定了相关角色的管理员，不能在接口链路里按前缀扫描 Redis 高基数 key。
func InvalidateAdminRoleAndPermissionCacheByAdminIDs(base *corelogic.BaseLogic, adminIDs ...int) {
	if base == nil {
		return
	}
	cacheKeys := make([]string, 0, len(adminIDs)*4)
	for _, adminID := range types.UniquePositiveInts(adminIDs) {
		cacheKeys = append(cacheKeys,
			fmt.Sprintf(keys.AdminRoleIDs, adminID),
			fmt.Sprintf(keys.AdminRolesDetail, adminID),
			fmt.Sprintf(keys.AdminPermissionIDs, adminID),
			fmt.Sprintf(keys.AdminPermissionUUIDs, adminID),
		)
	}
	DeleteRedisKeysExactBatches(base, "InvalidateAdminRoleAndPermissionCacheByAdminIDs 删除管理员关系权限缓存", TableCachePhysicalKeys(base, cacheKeys...))
}

// InvalidateAdminPermissionCacheByAdminIDs 精确删除指定管理员的聚合权限与最终权限码缓存。
func InvalidateAdminPermissionCacheByAdminIDs(base *corelogic.BaseLogic, adminIDs ...int) {
	if base == nil {
		return
	}
	cacheKeys := make([]string, 0, len(adminIDs)*2)
	for _, adminID := range types.UniquePositiveInts(adminIDs) {
		cacheKeys = append(cacheKeys,
			fmt.Sprintf(keys.AdminPermissionIDs, adminID),
			fmt.Sprintf(keys.AdminPermissionUUIDs, adminID),
		)
	}
	DeleteRedisKeysExactBatches(base, "InvalidateAdminPermissionCacheByAdminIDs 删除管理员权限缓存", TableCachePhysicalKeys(base, cacheKeys...))
}

// InvalidateRolePermissionCacheByRoleIDs 精确删除指定角色的权限关系缓存。
func InvalidateRolePermissionCacheByRoleIDs(base *corelogic.BaseLogic, roleIDs ...int) {
	if base == nil {
		return
	}
	cacheKeys := make([]string, 0, len(roleIDs))
	for _, roleID := range types.UniquePositiveInts(roleIDs) {
		cacheKeys = append(cacheKeys, fmt.Sprintf(keys.RolePermission, roleID))
	}
	DeleteRedisKeysExactBatches(base, "InvalidateRolePermissionCacheByRoleIDs 删除角色权限缓存", TableCachePhysicalKeys(base, cacheKeys...))
}

// InvalidateAllRolePermissionCache 精确删除全量角色权限关系缓存，适用于权限定义整体变更或迁移补权场景。
func InvalidateAllRolePermissionCache(base *corelogic.BaseLogic) {
	if base == nil {
		return
	}
	readDB, err := TableCacheReadDB(base, svc.DatabaseMain, "main")
	if err != nil {
		corelogic.LogWrappedError(base, err, "InvalidateAllRolePermissionCache 获取admin读库失败")
		return
	}
	lastRoleID := 0
	for {
		roleIDs := make([]int, 0, rolePermissionInvalidateQueryBatchSize)
		// 角色权限缓存按 role_id 精确失效，避免 Redis 前缀扫描。
		if err := readDB.WithContext(base.Ctx).
			Model(&model.AdminRole{}).
			Where("id > ?", lastRoleID).
			Order("id ASC").
			Limit(rolePermissionInvalidateQueryBatchSize).
			Pluck("id", &roleIDs).Error; err != nil {
			corelogic.LogWrappedError(base, err, "InvalidateAllRolePermissionCache 查询全量角色ID失败 last_role_id=%d", lastRoleID)
			return
		}
		if len(roleIDs) == 0 {
			return
		}
		InvalidateRolePermissionCacheByRoleIDs(base, roleIDs...)
		lastRoleID = roleIDs[len(roleIDs)-1]
		if len(roleIDs) < rolePermissionInvalidateQueryBatchSize {
			return
		}
	}
}

// invalidateAdminRelationCacheWithOptions 按需清理管理员关系缓存。
func invalidateAdminRelationCacheWithOptions(base *corelogic.BaseLogic, preserveSession bool, adminIDs ...int) {
	if base == nil {
		return
	}
	cacheLogic := NewCacheLogic(base.Ctx, base.Svc)
	manager, err := TableCacheManager(base)
	if err != nil {
		corelogic.LogWrappedError(base, err, "invalidateAdminRelationCache 初始化表缓存管理器失败")
		manager = nil
	}
	for _, adminID := range types.UniquePositiveInts(adminIDs) {
		if !preserveSession {
			_ = cacheLogic.DeleteAdminInfo(adminID)
		}
		profileKey := fmt.Sprintf(keys.AdminProfile, adminID)
		roleKey := fmt.Sprintf(keys.AdminRoleIDs, adminID)
		roleDetailKey := fmt.Sprintf(keys.AdminRolesDetail, adminID)
		permissionKey := fmt.Sprintf(keys.AdminPermissionIDs, adminID)
		permissionUUIDKey := fmt.Sprintf(keys.AdminPermissionUUIDs, adminID)
		if manager != nil {
			if err := manager.DeleteByKey(base.Ctx, TableCachePhysicalKey(base, profileKey)); err != nil && !IsTableCacheTargetNotFound(err) {
				corelogic.LogWrappedError(base, err, "invalidateAdminRelationCache 删除管理员ID[%d]资料缓存失败", adminID)
			}
			if err := manager.DeleteByKey(base.Ctx, TableCachePhysicalKey(base, roleKey)); err != nil && !IsTableCacheTargetNotFound(err) {
				corelogic.LogWrappedError(base, err, "invalidateAdminRelationCache 删除管理员ID[%d]角色缓存失败", adminID)
			}
			if err := manager.DeleteByKey(base.Ctx, TableCachePhysicalKey(base, roleDetailKey)); err != nil && !IsTableCacheTargetNotFound(err) {
				corelogic.LogWrappedError(base, err, "invalidateAdminRelationCache 删除管理员ID[%d]角色详情缓存失败", adminID)
			}
			if err := manager.DeleteByKey(base.Ctx, TableCachePhysicalKey(base, permissionKey)); err != nil && !IsTableCacheTargetNotFound(err) {
				corelogic.LogWrappedError(base, err, "invalidateAdminRelationCache 删除管理员ID[%d]聚合权限缓存失败", adminID)
			}
			if err := manager.DeleteByKey(base.Ctx, TableCachePhysicalKey(base, permissionUUIDKey)); err != nil && !IsTableCacheTargetNotFound(err) {
				corelogic.LogWrappedError(base, err, "invalidateAdminRelationCache 删除管理员ID[%d]最终权限码缓存失败", adminID)
			}
		}
		if base.Redis() != nil {
			if err := base.RdsDelKeys(TableCachePhysicalKeys(base, profileKey, roleKey, roleDetailKey, permissionKey, permissionUUIDKey)...); err != nil {
				corelogic.LogWrappedError(base, err, "invalidateAdminRelationCache 删除管理员ID[%d]Redis关系缓存失败", adminID)
			}
		}
	}
}

// InvalidateAllAdminPermissionCache 精确删除全量管理员聚合权限缓存，适用于权限定义整体变更场景。
func InvalidateAllAdminPermissionCache(base *corelogic.BaseLogic) {
	if base == nil {
		return
	}
	readDB, err := TableCacheReadDB(base, svc.DatabaseMain, "main")
	if err != nil {
		corelogic.LogWrappedError(base, err, "InvalidateAllAdminPermissionCache 获取admin读库失败")
		return
	}
	lastAdminID := 0
	for {
		adminIDs := make([]int, 0, adminPermissionInvalidateQueryBatchSize)
		// 权限定义变更后按管理员 ID 分批精确清理，避免 Redis SCAN。
		if err := readDB.WithContext(base.Ctx).
			Model(&model.Admin{}).
			Where("id > ?", lastAdminID).
			Order("id ASC").
			Limit(adminPermissionInvalidateQueryBatchSize).
			Pluck("id", &adminIDs).Error; err != nil {
			corelogic.LogWrappedError(base, err, "InvalidateAllAdminPermissionCache 查询全量管理员ID失败 last_admin_id=%d", lastAdminID)
			return
		}
		if len(adminIDs) == 0 {
			return
		}
		InvalidateAdminPermissionCacheByAdminIDs(base, adminIDs...)
		lastAdminID = adminIDs[len(adminIDs)-1]
		if len(adminIDs) < adminPermissionInvalidateQueryBatchSize {
			return
		}
	}
}

// IsTableCacheTargetNotFound 判断当前错误是否为表缓存目标未注册。
func IsTableCacheTargetNotFound(err error) bool {
	return errors.Is(err, tablecache.ErrTargetNotFound)
}
