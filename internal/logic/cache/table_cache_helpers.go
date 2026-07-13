package cache

import (
	"fmt"
	"strconv"
	"strings"

	keys "admin/common/rediskeys"
	"admin/helper"
	corelogic "admin/internal/logic"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	tablecache "github.com/Is999/table-cache"
)

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

// StringHashFieldsWithCache 批量读取完整快照 Hash 的指定字段，Key 不存在时通过 table-cache 回源。
func StringHashFieldsWithCache(base *corelogic.BaseLogic, cacheKey string, fields []string) (map[string]string, error) {
	result := make(map[string]string, len(fields))
	if base == nil || base.Redis() == nil {
		return nil, WrapRedisUnavailable(nil, "Hash缓存读取失败")
	}
	fields = helper.UniqueNonEmptyStrings(fields)
	if len(fields) == 0 {
		return result, nil
	}
	physicalKey := TableCachePhysicalKey(base, cacheKey)
	if physicalKey == "" {
		return nil, WrapRedisUnavailable(nil, "Hash缓存 Key 未初始化")
	}
	pipe := base.Redis().Pipeline()
	valuesCmd := pipe.HMGet(base.Ctx, physicalKey, fields...)
	existsCmd := pipe.Exists(base.Ctx, physicalKey)
	if _, err := pipe.Exec(base.Ctx); err != nil {
		return nil, WrapRedisUnavailable(err, "批量读取Hash缓存失败")
	}
	values, err := valuesCmd.Result()
	if err != nil {
		return nil, errors.Tag(err)
	}
	for index, value := range values {
		if value == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" && !corelogic.CacheIsEmptyMarker(text) {
			result[fields[index]] = text
		}
	}
	exists, err := existsCmd.Result()
	if err != nil {
		return nil, errors.Tag(err)
	}
	if exists > 0 {
		RecordTableCacheHit(base, cacheKey)
		return result, nil
	}
	manager, err := TableCacheManager(base)
	if err != nil {
		return nil, errors.Tag(err)
	}
	cache := make(map[string]string)
	lookup, err := manager.LoadThrough(base.Ctx, physicalKey, &cache, nil)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if lookup.State == tablecache.LookupStateEmpty {
		return result, nil
	}
	for _, field := range fields {
		value := strings.TrimSpace(cache[field])
		if value != "" && !corelogic.CacheIsEmptyMarker(value) {
			result[field] = value
		}
	}
	return result, nil
}

// RecordTableCacheHit 记录直接 Redis 点查命中的 table-cache 指标。
func RecordTableCacheHit(base *corelogic.BaseLogic, index string) {
	if base == nil || base.Svc == nil || base.Svc.TableCacheMetrics == nil {
		return
	}
	base.Svc.TableCacheMetrics.RecordCacheHit(base.Ctx, index)
	if metrics, ok := base.Svc.TableCacheMetrics.(tablecache.LookupMetrics); ok {
		metrics.RecordLookupState(base.Ctx, index, tablecache.LookupStateHit)
	}
}

// DeleteTableCacheKeysExact 通过 table-cache 精确失效缓存，避免并发回源把旧数据重新写回。
func DeleteTableCacheKeysExact(base *corelogic.BaseLogic, title string, cacheKeys []string) error {
	return executeSecurityCacheSync(base, title, securityCacheSyncPlan{TableKeys: cacheKeys})
}

// InvalidateAdminSessionCache 精确删除指定管理员的登录态缓存。
func InvalidateAdminSessionCache(base *corelogic.BaseLogic, adminIDs ...int) error {
	plan, err := adminSessionCacheSyncPlan(base, adminIDs)
	if err != nil {
		return errors.Tag(err)
	}
	return executeSecurityCacheSync(base, "管理员会话缓存失效", plan)
}

// InvalidateAdminSecurityCache 删除管理员会话和 MFA 状态缓存。
func InvalidateAdminSecurityCache(base *corelogic.BaseLogic, adminIDs ...int) error {
	plan, err := adminSessionCacheSyncPlan(base, adminIDs)
	if err != nil {
		return errors.Tag(err)
	}
	plan.MFAAdminIDs = append(plan.MFAAdminIDs, types.UniquePositiveInts(adminIDs)...)
	return executeSecurityCacheSync(base, "管理员安全缓存失效", plan)
}

// InvalidateDeletedAdminCache 删除已删除管理员的会话、角色关系和 MFA 状态缓存。
func InvalidateDeletedAdminCache(base *corelogic.BaseLogic, adminIDs ...int) error {
	plan, err := adminSessionCacheSyncPlan(base, adminIDs)
	if err != nil {
		return errors.Tag(err)
	}
	roleCacheKeys, err := adminRoleTableCacheKeys(base, adminIDs)
	if err != nil {
		return errors.Tag(err)
	}
	plan.TableKeys = append(plan.TableKeys, roleCacheKeys...)
	plan.MFAAdminIDs = append(plan.MFAAdminIDs, types.UniquePositiveInts(adminIDs)...)
	return executeSecurityCacheSync(base, "已删除管理员缓存失效", plan)
}

// InvalidateAdminRoleCacheByAdminIDs 精确删除指定管理员的角色关系与角色名称缓存。
func InvalidateAdminRoleCacheByAdminIDs(base *corelogic.BaseLogic, adminIDs ...int) error {
	cacheKeys, err := adminRoleTableCacheKeys(base, adminIDs)
	if err != nil {
		return errors.Tag(err)
	}
	return DeleteTableCacheKeysExact(base, "InvalidateAdminRoleCacheByAdminIDs 删除管理员角色缓存", cacheKeys)
}

// InvalidateAdminRoleDetailCacheByAdminIDs 精确删除指定管理员的角色名称缓存。
func InvalidateAdminRoleDetailCacheByAdminIDs(base *corelogic.BaseLogic, adminIDs ...int) error {
	cacheKeys := make([]string, 0, len(adminIDs))
	for _, adminID := range types.UniquePositiveInts(adminIDs) {
		cacheKeys = append(cacheKeys, fmt.Sprintf(keys.AdminRolesDetail, adminID))
	}
	if len(cacheKeys) > 0 && !securityCacheNamespaceReady(base) {
		return securityCacheKeyUnavailable(base, "管理员角色名称缓存 Key 未初始化")
	}
	physicalKeys := TableCachePhysicalKeys(base, cacheKeys...)
	if len(physicalKeys) != len(cacheKeys) {
		return securityCacheKeyUnavailable(base, "管理员角色名称缓存 Key 未初始化")
	}
	return DeleteTableCacheKeysExact(base, "InvalidateAdminRoleDetailCacheByAdminIDs 删除管理员角色名称缓存", physicalKeys)
}

// adminSessionCacheSyncPlan 构造指定管理员的会话失效计划，并拒绝缺失应用命名空间。
func adminSessionCacheSyncPlan(base *corelogic.BaseLogic, adminIDs []int) (securityCacheSyncPlan, error) {
	plan := securityCacheSyncPlan{}
	adminIDs = types.UniquePositiveInts(adminIDs)
	if len(adminIDs) > 0 && !securityCacheNamespaceReady(base) {
		return securityCacheSyncPlan{}, securityCacheKeyUnavailable(base, "管理员会话缓存 Key 未初始化")
	}
	for _, adminID := range adminIDs {
		cacheKey := base.AppRedisKey(keys.AdminSessionLogicalKey(adminID))
		if cacheKey == "" {
			return securityCacheSyncPlan{}, securityCacheKeyUnavailable(base,
				fmt.Sprintf("管理员ID[%d]会话缓存 Key 未初始化", adminID))
		}
		plan.RedisKeys = append(plan.RedisKeys, cacheKey)
	}
	return plan, nil
}

// adminRoleTableCacheKeys 返回指定管理员的角色关系和角色名称物理缓存 Key。
func adminRoleTableCacheKeys(base *corelogic.BaseLogic, adminIDs []int) ([]string, error) {
	cacheKeys := make([]string, 0, len(adminIDs)*2)
	for _, adminID := range types.UniquePositiveInts(adminIDs) {
		cacheKeys = append(cacheKeys,
			fmt.Sprintf(keys.AdminRoleIDs, adminID),
			fmt.Sprintf(keys.AdminRolesDetail, adminID),
		)
	}
	if len(cacheKeys) > 0 && !securityCacheNamespaceReady(base) {
		return nil, securityCacheKeyUnavailable(base, "管理员角色缓存 Key 未初始化")
	}
	physicalKeys := TableCachePhysicalKeys(base, cacheKeys...)
	if len(physicalKeys) != len(cacheKeys) {
		return nil, securityCacheKeyUnavailable(base, "管理员角色缓存 Key 未初始化")
	}
	return physicalKeys, nil
}

// securityCacheNamespaceReady 校验服务配置与运行时 Redis 命名空间一致。
func securityCacheNamespaceReady(base *corelogic.BaseLogic) bool {
	return base != nil && base.AppRedisKey(keys.AdminSessionLogicalKey(1)) != ""
}

// securityCacheKeyUnavailable 在无法构造精确 Key 时立即关闭当前进程缓存鉴权。
func securityCacheKeyUnavailable(base *corelogic.BaseLogic, message string) error {
	if base != nil && base.Svc != nil {
		base.Svc.SetSecurityCacheSyncPending(true)
	}
	return WrapRedisUnavailable(nil, message)
}

// IsTableCacheTargetNotFound 判断当前错误是否为表缓存目标未注册。
func IsTableCacheTargetNotFound(err error) bool {
	return errors.Is(err, tablecache.ErrTargetNotFound)
}
