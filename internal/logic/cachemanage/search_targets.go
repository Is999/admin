package cachemanage

import (
	cachelogic "admin/internal/logic/cache"
	"fmt"
	"strings"

	keys "admin/common/rediskeys"
	"admin/internal/model"
	"admin/internal/svc"

	"github.com/Is999/go-utils/errors"
)

// templateSearchTarget 定义“模板 key -> 如何枚举候选实例 key”的搜索快路径白名单。
// 搜索场景不要求模板一定支持预热，只要求候选实例可以稳定、低成本地枚举出来。
type templateSearchTarget struct {
	providerName string                                    // providerName 表示当前模板搜索提供器名称，便于日志定位具体枚举策略。
	templateKey  string                                    // templateKey 表示当前 provider 负责的模板缓存键定义。
	tableCache   bool                                      // tableCache 表示候选 key 来自 table-cache 托管目标。
	appPrefix    bool                                      // appPrefix 表示候选 key 来自普通 app_id 前缀缓存。
	buildKeys    func(*SystemCacheLogic) ([]string, error) // buildKeys 负责枚举当前模板缓存的候选实例 key。
}

// searchTemplateTargets 返回缓存搜索支持的模板枚举策略。
// 这里把“可预热模板”和“仅可搜索模板”统一收口，后续新增模板 key 只需补一个枚举器。
func (l *SystemCacheLogic) searchTemplateTargets() []templateSearchTarget {
	warmupTargets := l.warmupTemplateTargets()
	targets := make([]templateSearchTarget, 0, len(warmupTargets)+6)
	for _, target := range warmupTargets {
		targets = append(targets, templateSearchTarget{
			providerName: "warmup_template",
			templateKey:  target.templateKey,
			tableCache:   true,
			buildKeys:    target.buildKeys,
		})
	}
	targets = append(targets,
		templateSearchTarget{
			providerName: "admin_enabled_ids",
			templateKey:  keys.AdminInfoPatternRedisKey(),
			appPrefix:    true,
			buildKeys: func(l *SystemCacheLogic) ([]string, error) {
				return l.buildAppAdminKeys(keys.AdminInfoRedisKey)
			},
		},
		templateSearchTarget{
			providerName: "admin_enabled_ids",
			templateKey:  cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.AdminRoleIDsPattern),
			tableCache:   true,
			buildKeys: func(l *SystemCacheLogic) ([]string, error) {
				return l.buildTableAdminKeys(keys.AdminRoleIDs)
			},
		},
		templateSearchTarget{
			providerName: "admin_enabled_ids",
			templateKey:  cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.AdminPermissionIDsPattern),
			tableCache:   true,
			buildKeys: func(l *SystemCacheLogic) ([]string, error) {
				return l.buildTableAdminKeys(keys.AdminPermissionIDs)
			},
		},
		templateSearchTarget{
			providerName: "admin_enabled_ids",
			templateKey:  cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.AdminPermissionUUIDsPattern),
			tableCache:   true,
			buildKeys: func(l *SystemCacheLogic) ([]string, error) {
				return l.buildTableAdminKeys(keys.AdminPermissionUUIDs)
			},
		},
		templateSearchTarget{
			providerName: "admin_enabled_ids",
			templateKey:  cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.AdminProfilePattern),
			tableCache:   true,
			buildKeys: func(l *SystemCacheLogic) ([]string, error) {
				return l.buildTableAdminKeys(keys.AdminProfile)
			},
		},
		templateSearchTarget{
			providerName: "admin_enabled_ids",
			templateKey:  cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.AdminRolesDetailPattern),
			tableCache:   true,
			buildKeys: func(l *SystemCacheLogic) ([]string, error) {
				return l.buildTableAdminKeys(keys.AdminRolesDetail)
			},
		},
	)
	return targets
}

// matchSearchTemplateTarget 根据搜索模式找到可以直接枚举实例的模板目标。
// 只要搜索模式命中模板固定前缀，就优先走白名单枚举逻辑，避免扫描整个 Redis。
func (l *SystemCacheLogic) matchSearchTemplateTarget(pattern string) *templateSearchTarget {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil
	}
	targets := l.searchTemplateTargets()
	for i := range targets {
		prefix := strings.TrimSpace(cachelogic.CacheTemplatePrefix(targets[i].templateKey))
		logicalPrefix := strings.TrimSpace(cachelogic.CacheTemplatePrefix(cachelogic.TableCacheLogicalKey(l.BaseLogic, targets[i].templateKey)))
		appLogicalPrefix := strings.TrimSpace(cachelogic.CacheTemplatePrefix(keys.TrimPrefix(targets[i].templateKey)))
		if prefix == "" && logicalPrefix == "" && appLogicalPrefix == "" {
			continue
		}
		// 管理页展示的是带命名空间的物理 key；这里支持入口传入逻辑 key，避免升级后搜索按钮失效。
		if (prefix != "" && strings.HasPrefix(pattern, prefix)) ||
			(logicalPrefix != "" && strings.HasPrefix(pattern, logicalPrefix)) ||
			(appLogicalPrefix != "" && strings.HasPrefix(pattern, appLogicalPrefix)) {
			return &targets[i]
		}
	}
	return nil
}

// listEnabledAdminIDs 枚举启用状态的管理员 ID，供管理员相关模板缓存搜索使用。
func (l *SystemCacheLogic) listEnabledAdminIDs() ([]int, error) {
	readDB, err := cachelogic.TableCacheReadDB(l.BaseLogic, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var ids []int
	if err := readDB.
		Model(&model.Admin{}).
		Select("id").
		Where("status = 1").
		Order("id ASC").
		Limit(cacheTemplateEnumerationLimit).
		Pluck("id", &ids).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return ids, nil
}

// buildAdminKeys 基于启用管理员 ID 列表批量拼出管理员维度模板缓存实例 key。
// 这样新增管理员模板缓存时只需传入 key format，避免在注册表里重复写同一套循环逻辑。
func (l *SystemCacheLogic) buildAdminKeys(keyFormat string) ([]string, error) {
	adminIDs, err := l.listEnabledAdminIDs()
	if err != nil {
		return nil, errors.Tag(err)
	}
	keysList := make([]string, 0, len(adminIDs))
	for _, adminID := range adminIDs {
		if adminID <= 0 {
			continue
		}
		keysList = append(keysList, fmt.Sprintf(keyFormat, adminID))
	}
	return keysList, nil
}

// buildAppAdminKeys 基于启用管理员 ID 列表批量生成 app_id 前缀实例 key。
func (l *SystemCacheLogic) buildAppAdminKeys(buildKey func(int) string) ([]string, error) {
	adminIDs, err := l.listEnabledAdminIDs()
	if err != nil {
		return nil, errors.Tag(err)
	}
	keysList := make([]string, 0, len(adminIDs))
	for _, adminID := range adminIDs {
		if adminID <= 0 {
			continue
		}
		keysList = append(keysList, buildKey(adminID))
	}
	return keysList, nil
}

// buildTableAdminKeys 基于启用管理员 ID 列表批量拼出 table-cache 真实实例 key。
// 管理后台搜索会直接校验 Redis 物理 key 是否存在，因此这里统一补 table-cache 项目前缀。
func (l *SystemCacheLogic) buildTableAdminKeys(keyFormat string) ([]string, error) {
	keysList, err := l.buildAdminKeys(keyFormat)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return cachelogic.TableCachePhysicalKeys(l.BaseLogic, keysList...), nil
}
