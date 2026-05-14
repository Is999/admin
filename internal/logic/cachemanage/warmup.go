package cachemanage

import (
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	"admin/internal/svc"
	"fmt"
	"strings"
	"time"

	"admin/common/codes"
	i18n "admin/common/i18n"
	keys "admin/common/rediskeys"
	"admin/internal/model"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// cacheWarmupMaxKeys 限制单次模板预热的最大实例数量，避免误操作把数据库与 Redis 打爆。
	cacheWarmupMaxKeys = 5000
	// cacheTemplateEnumerationLimit 限制模板候选枚举规模，预留一条用于判断是否超过安全上限。
	cacheTemplateEnumerationLimit = cacheWarmupMaxKeys + 1
	// cacheWarmupFailedKeySampleLimit 限制失败 key 的返回数量，避免失败过多时返回体过大。
	cacheWarmupFailedKeySampleLimit = 30
)

// WarmupTemplate 通过“模板 key → 枚举实例 key → 批量回源”预热缓存。
// 该接口只允许对白名单模板执行，且只对内置支持回源的 table-cache 目标生效。
func (l *SystemCacheLogic) WarmupTemplate(req *types.CacheWarmupReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(errors.Errorf("模板缓存key不能为空"))
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}

	target := l.matchWarmupTemplateTarget(req.TemplateKey)
	if target == nil {
		err := errors.Errorf("当前模板缓存key不支持预热：%s", req.TemplateKey)
		return types.ParamErrorResult(err).
			WithError(corelogic.WrapLogicError(err, "SystemCacheLogic.WarmupTemplate 不支持的模板缓存key"))
	}

	start := time.Now()
	instanceKeys, err := target.buildKeys(l)
	if err != nil {
		return types.ServerError(i18n.MsgKeyCacheInfoFail, err,
			"SystemCacheLogic.WarmupTemplate 枚举模板缓存实例失败 templateKey=%s", req.TemplateKey).ToBizResult()
	}

	if req.Limit > 0 && len(instanceKeys) > req.Limit {
		instanceKeys = instanceKeys[:req.Limit]
	}
	if len(instanceKeys) > cacheWarmupMaxKeys {
		err := errors.Errorf("单次预热实例过多：%d，已超过最大限制：%d", len(instanceKeys), cacheWarmupMaxKeys)
		return types.ParamErrorResult(err).
			WithError(corelogic.WrapLogicError(err, "SystemCacheLogic.WarmupTemplate 预热实例数量超限"))
	}

	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		return types.ServerError(i18n.MsgKeyCacheInfoFail, err,
			"SystemCacheLogic.WarmupTemplate 初始化表缓存管理器失败").ToBizResult()
	}

	successCount := 0
	failedCount := 0
	failedKeys := make([]string, 0)
	for _, key := range instanceKeys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		// table-cache 新版本把刷新视为写操作，这里显式转换成带项目命名空间的真实 Redis key，避免刷新未命名空间。
		physicalKey := cachelogic.TableCachePhysicalKey(l.BaseLogic, trimmed)
		if refreshErr := manager.RefreshByKey(l.Ctx, physicalKey); refreshErr != nil {
			failedCount += 1
			if len(failedKeys) < cacheWarmupFailedKeySampleLimit {
				failedKeys = append(failedKeys, physicalKey)
			}
			continue
		}
		successCount += 1
	}

	latency := time.Since(start).Milliseconds()
	logCacheInfo(l.Ctx, "cache.warmup.template.done",
		logx.Field("template_key", req.TemplateKey),
		logx.Field("total", len(instanceKeys)),
		logx.Field("success_count", successCount),
		logx.Field("failed_count", failedCount),
		logx.Field("latency_ms", latency),
	)

	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess).
		WithData(types.CacheWarmupResp{
			TemplateKey: req.TemplateKey,
			Total:       len(instanceKeys),
			Success:     successCount,
			Failed:      failedCount,
			FailedKeys:  failedKeys,
			LatencyMS:   latency,
		})
}

// warmupTemplateTarget 定义“模板 key → 如何枚举实例 key”的白名单配置。
type warmupTemplateTarget struct {
	templateKey string                                    // 允许预热的缓存模板 key
	buildKeys   func(*SystemCacheLogic) ([]string, error) // 实例 key 枚举函数
}

// matchWarmupTemplateTarget 根据模板 key 找到对应的预热枚举策略。
func (l *SystemCacheLogic) matchWarmupTemplateTarget(templateKey string) *warmupTemplateTarget {
	templateKey = strings.TrimSpace(templateKey)
	if templateKey == "" {
		return nil
	}
	physicalTemplateKey := cachelogic.TableCachePhysicalKey(l.BaseLogic, templateKey)
	logicalTemplateKey := cachelogic.TableCacheLogicalKey(l.BaseLogic, templateKey)
	targets := l.warmupTemplateTargets()
	for i := range targets {
		if targets[i].templateKey == templateKey ||
			targets[i].templateKey == physicalTemplateKey ||
			cachelogic.TableCacheLogicalKey(l.BaseLogic, targets[i].templateKey) == logicalTemplateKey {
			return &targets[i]
		}
	}
	return nil
}

// warmupTemplateTargets 返回当前支持预热的模板缓存白名单。
// 白名单只放“可枚举、规模可控、回源链路明确”的模板，避免误预热造成数据库与 Redis 瞬时压力过高。
func (l *SystemCacheLogic) warmupTemplateTargets() []warmupTemplateTarget {
	return []warmupTemplateTarget{
		{
			templateKey: cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.SysConfigUUIDPattern),
			buildKeys: func(l *SystemCacheLogic) ([]string, error) {
				uuids, err := l.listSysConfigUUIDs()
				if err != nil {
					return nil, errors.Tag(err)
				}
				keysList := make([]string, 0, len(uuids))
				for _, uuid := range uuids {
					uuid = strings.TrimSpace(uuid)
					if uuid == "" {
						continue
					}
					keysList = append(keysList, cachelogic.TableCachePhysicalKey(l.BaseLogic, fmt.Sprintf(keys.SysConfigUUID, uuid)))
				}
				return keysList, nil
			},
		},
		{
			templateKey: cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.SecretKeyRoutePattern),
			buildKeys: func(l *SystemCacheLogic) ([]string, error) {
				uuids, err := l.listEnabledSecretKeyUUIDs()
				if err != nil {
					return nil, errors.Tag(err)
				}
				keysList := make([]string, 0, len(uuids))
				for _, uuid := range uuids {
					uuid = strings.TrimSpace(uuid)
					if uuid == "" {
						continue
					}
					keysList = append(keysList, cachelogic.TableCachePhysicalKey(l.BaseLogic, fmt.Sprintf(keys.SecretKeyRoute, uuid)))
				}
				return keysList, nil
			},
		},
		{
			templateKey: cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.SecretKeyAESVersionPattern),
			buildKeys: func(l *SystemCacheLogic) ([]string, error) {
				versions, err := l.listEnabledSecretKeyVersions()
				if err != nil {
					return nil, errors.Tag(err)
				}
				keysList := make([]string, 0, len(versions))
				for _, row := range versions {
					uuid := strings.TrimSpace(row.UUID)
					keyVersion := strings.TrimSpace(row.KeyVersion)
					if uuid == "" || keyVersion == "" {
						continue
					}
					keysList = append(keysList, cachelogic.TableCachePhysicalKey(l.BaseLogic, fmt.Sprintf(keys.SecretKeyAESVersion, uuid, keyVersion)))
				}
				return keysList, nil
			},
		},
		{
			templateKey: cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.SecretKeyRSAVersionPattern),
			buildKeys: func(l *SystemCacheLogic) ([]string, error) {
				versions, err := l.listEnabledSecretKeyVersions()
				if err != nil {
					return nil, errors.Tag(err)
				}
				keysList := make([]string, 0, len(versions))
				for _, row := range versions {
					uuid := strings.TrimSpace(row.UUID)
					keyVersion := strings.TrimSpace(row.KeyVersion)
					if uuid == "" || keyVersion == "" {
						continue
					}
					keysList = append(keysList, cachelogic.TableCachePhysicalKey(l.BaseLogic, fmt.Sprintf(keys.SecretKeyRSAVersion, uuid, keyVersion)))
				}
				return keysList, nil
			},
		},
		{
			templateKey: cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.RolePermissionPattern),
			buildKeys: func(l *SystemCacheLogic) ([]string, error) {
				roleIDs, err := l.listEnabledRoleIDs()
				if err != nil {
					return nil, errors.Tag(err)
				}
				keysList := make([]string, 0, len(roleIDs))
				for _, roleID := range roleIDs {
					if roleID <= 0 {
						continue
					}
					keysList = append(keysList, cachelogic.TableCachePhysicalKey(l.BaseLogic, fmt.Sprintf(keys.RolePermission, roleID)))
				}
				return keysList, nil
			},
		},
		{
			templateKey: cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.RoutePermissionIDsPattern),
			buildKeys: func(l *SystemCacheLogic) ([]string, error) {
				aliases, err := l.listEnabledRouteAliases()
				if err != nil {
					return nil, errors.Tag(err)
				}
				keysList := make([]string, 0, len(aliases))
				for _, alias := range aliases {
					alias = strings.TrimSpace(alias)
					if alias == "" {
						continue
					}
					keysList = append(keysList, cachelogic.TableCachePhysicalKey(l.BaseLogic, fmt.Sprintf(keys.RoutePermissionIDs, alias)))
				}
				return keysList, nil
			},
		},
	}
}

// listSysConfigUUIDs 枚举系统常量配置 UUID 列表，供 SysConfigUUIDPattern 模板预热使用。
func (l *SystemCacheLogic) listSysConfigUUIDs() ([]string, error) {
	readDB, err := cachelogic.TableCacheReadDB(l.BaseLogic, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var uuids []string
	if err := readDB.
		Model(&model.SysConfig{}).
		Select("uuid").
		Where("uuid <> ''").
		Order("id ASC").
		Limit(cacheTemplateEnumerationLimit).
		Pluck("uuid", &uuids).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return uuids, nil
}

// listEnabledSecretKeyUUIDs 枚举启用状态的秘钥主配置 UUID 列表，供 SecretKeyRoutePattern 模板预热使用。
func (l *SystemCacheLogic) listEnabledSecretKeyUUIDs() ([]string, error) {
	readDB, err := cachelogic.TableCacheReadDB(l.BaseLogic, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var uuids []string
	if err := readDB.
		Model(&model.SecretKey{}).
		Select("uuid").
		Where("status = 1").
		Order("id ASC").
		Limit(cacheTemplateEnumerationLimit).
		Pluck("uuid", &uuids).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return uuids, nil
}

// listEnabledSecretKeyVersions 枚举启用状态的秘钥版本列表，供 secret_key_aes/secret_key_rsa 模板预热使用。
func (l *SystemCacheLogic) listEnabledSecretKeyVersions() ([]model.SecretKeyVersion, error) {
	readDB, err := cachelogic.TableCacheReadDB(l.BaseLogic, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var rows []model.SecretKeyVersion
	if err := readDB.
		Model(&model.SecretKeyVersion{}).
		Select("uuid, key_version").
		Where("status = 1").
		Order("id ASC").
		Limit(cacheTemplateEnumerationLimit).
		Find(&rows).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return rows, nil
}

// listEnabledRoleIDs 枚举启用且未删除的角色 ID 列表，供 RolePermissionPattern 模板预热使用。
func (l *SystemCacheLogic) listEnabledRoleIDs() ([]int, error) {
	readDB, err := cachelogic.TableCacheReadDB(l.BaseLogic, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var ids []int
	if err := readDB.
		Model(&model.AdminRole{}).
		Select("id").
		Where("status = 1").
		Where("is_delete = 0").
		Order("id ASC").
		Limit(cacheTemplateEnumerationLimit).
		Pluck("id", &ids).Error; err != nil {
		return nil, errors.Tag(err)
	}
	return ids, nil
}

// listEnabledRouteAliases 枚举启用权限点中的 module（路由别名）集合，供 RoutePermissionIDsPattern 模板预热使用。
func (l *SystemCacheLogic) listEnabledRouteAliases() ([]string, error) {
	readDB, err := cachelogic.TableCacheReadDB(l.BaseLogic, svc.DatabaseMain, "main")
	if err != nil {
		return nil, errors.Tag(err)
	}
	var modules []string
	if err := readDB.
		Model(&model.AdminPermission{}).
		Select("module").
		Where("status = 1").
		Where("module <> ''").
		Order("id ASC").
		Limit(cacheTemplateEnumerationLimit).
		Pluck("module", &modules).Error; err != nil {
		return nil, errors.Tag(err)
	}
	seen := make(map[string]struct{}, len(modules))
	result := make([]string, 0, len(modules))
	for _, module := range modules {
		module = strings.TrimSpace(module)
		if module == "" {
			continue
		}
		if _, ok := seen[module]; ok {
			continue
		}
		seen[module] = struct{}{}
		result = append(result, module)
	}
	return result, nil
}
