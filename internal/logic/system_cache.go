package logic

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"admin_cron/common/codes"
	i18n "admin_cron/common/i18n"
	keys "admin_cron/common/rediskeys"
	"admin_cron/helper"
	"admin_cron/internal/infra/loggerx"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

	tablecache "github.com/Is999/table-cache"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// cacheSearchMaxResults 限制单次缓存搜索最多确认的真实 Redis Key 数量，避免模板枚举异常放大 Redis 压力。
	cacheSearchMaxResults = 5000
	// cacheSearchDefaultPageSize 表示缓存搜索默认每页返回数量，避免管理页首屏一次拉取过多 key。
	cacheSearchDefaultPageSize = 20
	// cacheSearchMaxPageSize 表示缓存搜索单页最大返回数量，供前端分页/流式加载使用。
	cacheSearchMaxPageSize = 50
	// cacheSearchExistsBatchSize 限制模板候选 key 批量校验存在性的单批大小，避免一次 pipeline 过大。
	cacheSearchExistsBatchSize = 200
	// cacheSearchExistsConcurrency 限制模板候选 key Exists pipeline 的并发数，避免搜索接口串行等待过多批次。
	cacheSearchExistsConcurrency = 4
	// cacheSearchExistsTimeout 表示模板候选 key 批量校验的最大耗时，防止异常候选量拖慢管理接口。
	cacheSearchExistsTimeout = 3 * time.Second
	// cacheKeyInfoPreviewItems 限制缓存详情集合类值的预览条数，避免大 hash/set/list/zset 卡住接口。
	cacheKeyInfoPreviewItems = 100
	// cacheKeyInfoPreviewStringBytes 限制缓存详情 string 值的预览字节数，避免大字符串一次性拉回内存。
	cacheKeyInfoPreviewStringBytes = 64 * 1024
	// cacheMaskedValue 表示缓存详情中的敏感字段已做脱敏处理。
	cacheMaskedValue = "******"
)

var sensitiveCacheKeyPrefixes = []string{
	"admin:mfa:two_step:",
}

// cacheSearchStats 记录一次缓存搜索的关键过程指标，便于日志排查慢点到底在候选枚举还是 Exists 校验。
type cacheSearchStats struct {
	candidateCount int    // 模板枚举得到的候选 key 数量
	existingCount  int    // 当前 Redis 中真实存在的 key 数量
	scanRounds     int    // 兼容旧日志字段；当前缓存搜索不再执行 Redis SCAN，固定为 0。
	scanNodeCount  int    // 兼容旧日志字段；当前缓存搜索不再遍历 Redis Cluster 节点，固定为 0。
	latencyMS      int64  // 当前搜索阶段累计耗时（毫秒）
	limited        bool   // 是否命中最大搜索窗口保护，命中后仅返回已确认窗口内的数据。
	providerName   string // providerName 表示当前命中的模板搜索提供器名称。
	templateKey    string // templateKey 表示当前命中的模板键定义。
}

// logCacheInfo 使用统一结构化字段打印缓存管理信息日志，减少中文句子拼接造成的风格漂移。
func logCacheInfo(ctx context.Context, message string, fields ...logx.LogField) {
	loggerx.Infow(ctx, message, fields...)
}

// SystemCacheLogic 承载后台缓存管理页面的查询、搜索和刷新逻辑。
type SystemCacheLogic struct {
	*BaseLogic // 复用上下文、数据库、Redis 和日志能力
}

// NewSystemCacheLogic 创建缓存管理业务逻辑对象。
func NewSystemCacheLogic(r *http.Request, svcCtx *svc.ServiceContext) *SystemCacheLogic {
	return &SystemCacheLogic{
		BaseLogic: NewBaseLogic(r, svcCtx),
	}
}

// List 返回系统内置可刷新缓存目标列表。
func (l *SystemCacheLogic) List(req *types.CacheListReq) *types.BizResult {
	items := l.cacheItems()
	if req.Key != "" {
		items = l.filterCacheItems(items, req.Key)
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.ListResp[types.CacheItem]{List: items, Total: int64(len(items))})
}

// filterCacheItems 按“模板缓存支持前缀模糊、普通缓存仅精确匹配”的规则过滤缓存目标。
func (l *SystemCacheLogic) filterCacheItems(items []types.CacheItem, keyword string) []types.CacheItem {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return items
	}
	filtered := make([]types.CacheItem, 0, len(items))
	for _, item := range items {
		if matchCacheListItem(item, keyword) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// matchCacheListItem 判断单个缓存目标是否命中缓存管理页筛选条件。
// 规则如下：
// 1. 普通缓存仅支持按 index/key/keyTitle/exampleKey 精确匹配；
// 2. 模板缓存额外支持按模板固定前缀做“前缀模糊查询”，例如 `admin:in` 命中 `admin:info:{adminID}`。
func matchCacheListItem(item types.CacheItem, keyword string) bool {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return true
	}
	if equalsAny(keyword, item.Index, item.Key, item.KeyTitle, item.ExampleKey) {
		return true
	}
	if !item.IsTemplate {
		return false
	}
	prefix := strings.TrimSpace(cacheTemplatePrefix(item.Key))
	if prefix == "" {
		return false
	}
	return strings.HasPrefix(prefix, keyword)
}

// equalsAny 判断查询关键字是否与多个候选值中的任一项精确相等。
func equalsAny(keyword string, values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == keyword {
			return true
		}
	}
	return false
}

// ServerInfo 查询 Redis 服务器信息。
func (l *SystemCacheLogic) ServerInfo() *types.BizResult {
	if l.Redis() == nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyTaskDisabled).
			WithError(errors.Errorf("Redis未初始化"))
	}
	info, err := l.Redis().Info(l.Context()).Result()
	if err != nil {
		return types.ServerError(i18n.MsgKeyCacheInfoFail, err,
			"SystemCacheLogic.ServerInfo 查询Redis信息失败").ToBizResult()
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(parseRedisInfo(info))
}

// KeyInfo 查询指定 Redis key 的类型、TTL 和当前值。
func (l *SystemCacheLogic) KeyInfo(req *types.CacheKeyReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(errors.Errorf("缓存key不能为空"))
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	infoKey := l.cacheInfoLookupKey(req.Key)
	info, err := l.keyInfo(infoKey)
	if errorsIsRedisNil(err) && l.isBuiltinRefreshableKey(req.Key) {
		logCacheInfo(l.Context(), "cache.key.rebuild.start",
			logx.Field("key", req.Key),
			logx.Field("source", "key_info"),
		)
		if rebuildErr := l.RefreshCacheByKey(req.Key); rebuildErr == nil {
			logCacheInfo(l.Context(), "cache.key.rebuild.success",
				logx.Field("key", req.Key),
				logx.Field("source", "key_info"),
			)
			info, err = l.keyInfo(infoKey)
		} else {
			err = errors.Wrapf(rebuildErr, "SystemCacheLogic.KeyInfo 自动重建缓存key[%s]失败", req.Key)
		}
	}
	if err != nil {
		if errorsIsRedisNil(err) {
			return types.NotFound(i18n.MsgKeyCacheKeyNotFound, err, req.Key).ToBizResult()
		}
		return types.ServerError(i18n.MsgKeyCacheInfoFail, err,
			"SystemCacheLogic.KeyInfo 查询缓存key[%s]失败", req.Key).ToBizResult()
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(info)
}

// cacheInfoLookupKey 返回缓存详情接口实际读取的 Redis key。
// table-cache 托管项在管理页可兼容输入逻辑 key，但详情读取必须落到带项目命名空间的真实 key。
func (l *SystemCacheLogic) cacheInfoLookupKey(key string) string {
	item := l.matchCacheItem(key)
	if item == nil || !strings.HasPrefix(item.Key, keys.TableCacheDataPrefix) {
		return strings.TrimSpace(key)
	}
	return tableCachePhysicalKey(l.BaseLogic, key)
}

// SearchKey 搜索 Redis Key，仅允许精确 key 或已登记模板 key，避免生产大 Key 空间下使用 SCAN。
func (l *SystemCacheLogic) SearchKey(req *types.CacheKeyReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(errors.Errorf("缓存key不能为空"))
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	normalizeCacheSearchPaging(req)
	pattern, normalizeErr := normalizeCacheSearchPattern(req.Key)
	if normalizeErr != nil {
		return types.NewBizResult(codes.ParamError).
			SetI18nMessage(i18n.MsgKeyParamErrorFormat, normalizeErr.Error()).
			WithError(wrapLogicError(normalizeErr, "SystemCacheLogic.SearchKey 缓存搜索参数校验失败"))
	}
	if validateErr := l.validateSearchPattern(pattern); validateErr != nil {
		return types.NewBizResult(codes.ParamError).
			SetI18nMessage(i18n.MsgKeyParamErrorFormat, validateErr.Error()).
			WithError(wrapLogicError(validateErr, "SystemCacheLogic.SearchKey 缓存搜索模式校验失败"))
	}
	start := time.Now()
	keys, searchPath, stats, err := l.searchKeys(pattern, cacheSearchMaxResults)
	if err != nil {
		logCacheInfo(l.Context(), "缓存搜索 失败",
			logx.Field("pattern", pattern),
			logx.Field("request_source", req.Source),
			logx.Field("search_path", searchPath),
			logx.Field("provider_name", stats.providerName),
			logx.Field("template_key", stats.templateKey),
			logx.Field("latency_ms", time.Since(start).Milliseconds()),
			logx.Field("candidate_count", stats.candidateCount),
			logx.Field("existing_count", stats.existingCount),
			logx.Field("scan_rounds", stats.scanRounds),
			logx.Field("scan_node_count", stats.scanNodeCount),
			logx.Field("limited", stats.limited),
			logx.Field("error", err.Error()),
		)
		return types.ServerError(i18n.MsgKeyCacheInfoFail, err,
			"SystemCacheLogic.SearchKey 搜索缓存key[%s]失败", req.Key).ToBizResult()
	}
	pagedKeys, hasMore := paginateCacheSearchKeys(keys, req.Page, req.PageSize)
	logCacheInfo(l.Context(), "缓存搜索 完成",
		logx.Field("pattern", pattern),
		logx.Field("request_source", req.Source),
		logx.Field("search_path", searchPath),
		logx.Field("provider_name", stats.providerName),
		logx.Field("template_key", stats.templateKey),
		logx.Field("result_count", len(keys)),
		logx.Field("page", req.Page),
		logx.Field("page_size", req.PageSize),
		logx.Field("page_result_count", len(pagedKeys)),
		logx.Field("latency_ms", time.Since(start).Milliseconds()),
		logx.Field("candidate_count", stats.candidateCount),
		logx.Field("existing_count", stats.existingCount),
		logx.Field("scan_rounds", stats.scanRounds),
		logx.Field("scan_node_count", stats.scanNodeCount),
		logx.Field("limited", stats.limited),
	)
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.CacheSearchResp{
			List:           l.buildSearchItems(pagedKeys),
			Total:          int64(len(keys)),
			Page:           req.Page,
			PageSize:       req.PageSize,
			HasMore:        hasMore,
			NextPage:       nextCacheSearchPage(req.Page, hasMore),
			SearchPath:     searchPath,
			ProviderName:   stats.providerName,
			TemplateKey:    stats.templateKey,
			CandidateTotal: stats.candidateCount,
			ExistingTotal:  stats.existingCount,
			Limited:        stats.limited,
			MaxResults:     cacheSearchMaxResults,
		})
}

// searchKeys 按“精确 key -> 模板快路径”顺序搜索缓存键。
// 未登记模板的通配符搜索会在参数校验阶段拒绝，避免直接对整个 Redis 做全量模式扫描。
func (l *SystemCacheLogic) searchKeys(pattern string, maxResults int) ([]string, string, cacheSearchStats, error) {
	if l.Redis() == nil {
		return nil, "redis_uninitialized", cacheSearchStats{}, errors.Errorf("Redis未初始化")
	}
	if !isWildcardCacheSearchPattern(pattern) {
		for _, cacheKey := range l.cacheSearchExactKeys(pattern) {
			exists, err := l.keyExists(cacheKey)
			if err != nil {
				return nil, "exact_exists", cacheSearchStats{}, errors.Tag(err)
			}
			if exists {
				return []string{cacheKey}, "exact_exists", cacheSearchStats{existingCount: 1}, nil
			}
		}
		return []string{}, "exact_exists", cacheSearchStats{}, nil
	}
	if cacheKeys, stats, handled, err := l.searchTemplateKeys(pattern, maxResults); handled {
		if err != nil {
			return cacheKeys, "template_candidates", stats, errors.Wrapf(err, "缓存模板 key 搜索失败 pattern=%s max_results=%d", pattern, maxResults)
		}
		return cacheKeys, "template_candidates", stats, nil
	}
	return nil, "unsupported_wildcard", cacheSearchStats{}, errors.Errorf("缓存搜索仅支持精确key或已登记模板key，未知通配符禁止搜索")
}

// cacheSearchExactKeys 返回精确搜索时需要按顺序校验的 Redis key。
// 先保留用户输入的真实 key；若命中 table-cache 托管目标，再补充新命名空间 key，兼容旧逻辑 key 输入。
func (l *SystemCacheLogic) cacheSearchExactKeys(key string) []string {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	cacheKeys := []string{key}
	item := l.matchCacheItem(key)
	if item != nil && strings.HasPrefix(item.Key, keys.TableCacheDataPrefix) {
		physicalKey := tableCachePhysicalKey(l.BaseLogic, key)
		if physicalKey != "" && physicalKey != key {
			cacheKeys = append(cacheKeys, physicalKey)
		}
	}
	return helper.UniqueNonEmptyStrings(cacheKeys)
}

// Renew 刷新指定缓存目标。
func (l *SystemCacheLogic) Renew(req *types.CacheRenewReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(errors.Errorf("缓存key不能为空"))
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	if !l.isBuiltinRefreshableKey(req.Key) {
		err := errors.Errorf("非内置缓存key禁止刷新")
		return types.Forbidden(i18n.MsgKeyForbidden).ToBizResult().
			WithError(wrapLogicError(err, "SystemCacheLogic.Renew 禁止刷新非内置缓存key[%s]", req.Key))
	}
	if err := l.RefreshCacheByKey(req.Key); err != nil {
		return types.ServerError(i18n.MsgKeyCacheInfoFail, err,
			"SystemCacheLogic.Renew 刷新缓存key[%s]失败", req.Key).ToBizResult()
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// RenewAll 刷新全部系统内置缓存目标。
func (l *SystemCacheLogic) RenewAll() *types.BizResult {
	manager, err := tableCacheManager(l.BaseLogic)
	if err != nil {
		return types.ServerError(i18n.MsgKeyCacheInfoFail, err,
			"SystemCacheLogic.RenewAll 初始化表缓存管理器失败").ToBizResult()
	}
	if err := manager.RefreshAll(l.Context()); err != nil {
		return types.ServerError(i18n.MsgKeyCacheInfoFail, err,
			"SystemCacheLogic.RenewAll 刷新全部表缓存失败").ToBizResult()
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess)
}

// RefreshCacheByKey 根据缓存键重建或删除指定缓存。
func (l *SystemCacheLogic) RefreshCacheByKey(key string) error {
	if l.Redis() == nil {
		return errors.Errorf("Redis未初始化")
	}
	key = strings.TrimSpace(key)
	manager, err := tableCacheManager(l.BaseLogic)
	if err != nil {
		return errors.Tag(err)
	}
	physicalKey := tableCachePhysicalKey(l.BaseLogic, key)
	if err := manager.RefreshByKey(l.Context(), physicalKey); err == nil {
		logCacheInfo(l.Context(), "cache.refresh.success",
			logx.Field("key", physicalKey),
			logx.Field("target", "table_cache"),
		)
		return nil
	} else if !errors.Is(err, tablecache.ErrTargetNotFound) {
		return errors.Wrapf(err, "SystemCacheLogic.RefreshCacheByKey 自动刷新表缓存失败 key=%s", key)
	}
	if strings.HasPrefix(key, "admin:info:") {
		rebuildErr := NewCacheLogic(l.Context(), l.svc).RebuildAdminInfoByKey(key)
		if rebuildErr == nil {
			logCacheInfo(l.Context(), "cache.refresh.success",
				logx.Field("key", key),
				logx.Field("target", "admin_session"),
			)
			return nil
		}
		return errors.Wrapf(rebuildErr, "SystemCacheLogic.RefreshCacheByKey 自动重建登录态缓存失败 key=%s", key)
	}
	deleteErr := l.RdsDelKeys(key)
	if deleteErr == nil {
		logCacheInfo(l.Context(), "cache.refresh.success",
			logx.Field("key", key),
			logx.Field("target", "plain_cache_delete"),
		)
		return nil
	}
	return errors.Wrapf(deleteErr, "SystemCacheLogic.RefreshCacheByKey 删除普通缓存失败 key=%s", key)
}

// cacheItems 返回内置缓存目标定义。
func (l *SystemCacheLogic) cacheItems() []types.CacheItem {
	tableItems := tableCacheItems(l.BaseLogic)
	items := make([]types.CacheItem, 0, len(tableItems)+1)
	items = append(items, tableItems...)
	items = append(items, types.CacheItem{
		Index:        "admin_info",
		Key:          "admin:info:{adminID}",
		KeyTitle:     "admin:info:{adminID}",
		Type:         "hash",
		Remark:       "管理员登录态缓存",
		Category:     "session",
		IsTemplate:   true,
		ExampleKey:   "admin:info:1",
		AutoRebuild:  true,
		RefreshScope: "single",
	})
	return items
}

// isBuiltinCacheKey 判断当前 key 是否属于后台缓存管理内置目标，内置目标 miss 时允许自动回源重建后再查看。
func (l *SystemCacheLogic) isBuiltinCacheKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	physicalKey := tableCachePhysicalKey(l.BaseLogic, key)
	for _, item := range l.cacheItems() {
		if isTemplateCachePattern(item.Key) {
			continue
		}
		if item.Key == key || item.Key == physicalKey {
			return true
		}
	}
	return false
}

// isBuiltinRefreshableKey 判断当前 key 是否属于支持自动回源/回填的内置缓存目标。
// 固定 key 直接精确匹配；模板 key 按占位符前缀匹配，且必须显式标记 AutoRebuild=true 才允许刷新。
func (l *SystemCacheLogic) isBuiltinRefreshableKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	item := l.matchCacheItem(key)
	return item != nil && item.AutoRebuild
}

// keyInfo 查询 Redis key 详情。
func (l *SystemCacheLogic) keyInfo(key string) (*types.CacheKeyInfoResp, error) {
	if l.Redis() == nil {
		return nil, errors.Errorf("Redis未初始化")
	}
	ctx := l.Context()
	typ, err := l.Redis().Type(ctx, key).Result()
	if err != nil {
		return nil, errors.Tag(err)
	}
	if typ == "none" {
		return nil, redis.Nil
	}
	ttl, err := l.Redis().TTL(ctx, key).Result()
	if err != nil {
		return nil, errors.Tag(err)
	}

	total := int64(1)
	var value any
	switch typ {
	case "string":
		total, value, err = l.previewStringValue(ctx, key, cacheKeyInfoPreviewStringBytes)
	case "hash":
		value, total, err = l.previewHashValue(ctx, key, cacheKeyInfoPreviewItems)
	case "set":
		value, total, err = l.previewSetValue(ctx, key, cacheKeyInfoPreviewItems)
	case "list":
		total, err = l.Redis().LLen(ctx, key).Result()
		if err == nil {
			value, err = l.Redis().LRange(ctx, key, 0, cacheKeyInfoPreviewItems-1).Result()
		}
	case "zset":
		total, err = l.Redis().ZCard(ctx, key).Result()
		if err == nil {
			value, err = l.Redis().ZRangeWithScores(ctx, key, 0, cacheKeyInfoPreviewItems-1).Result()
		}
	default:
		value = fmt.Sprintf("暂不支持展示类型: %s", typ)
	}
	if err != nil {
		return nil, errors.Tag(err)
	}
	return &types.CacheKeyInfoResp{
		Key:   key,
		Type:  typ,
		TTL:   int64(ttl.Seconds()),
		Total: total,
		Value: maskCacheValue(key, value),
		Item:  l.matchCacheItem(key),
	}, nil
}

// previewStringValue 读取 string 缓存的有限字节预览，并返回完整字节长度。
func (l *SystemCacheLogic) previewStringValue(ctx context.Context, key string, maxBytes int64) (int64, string, error) {
	total, err := l.Redis().StrLen(ctx, key).Result()
	if err != nil {
		return 0, "", errors.Tag(err)
	}
	if maxBytes <= 0 || total <= maxBytes {
		value, getErr := l.Redis().Get(ctx, key).Result()
		return total, value, errors.Tag(getErr)
	}
	value, err := l.Redis().GetRange(ctx, key, 0, maxBytes-1).Result()
	return total, value, errors.Tag(err)
}

// previewHashValue 使用 HRANDFIELD 抽样读取 hash 缓存预览，并返回完整字段数。
func (l *SystemCacheLogic) previewHashValue(ctx context.Context, key string, maxItems int) (map[string]string, int64, error) {
	total, err := l.Redis().HLen(ctx, key).Result()
	if err != nil {
		return nil, 0, errors.Tag(err)
	}
	if maxItems <= 0 {
		return map[string]string{}, total, nil
	}
	count := min(int(total), maxItems)
	values, err := l.Redis().HRandFieldWithValues(ctx, key, count).Result()
	if err != nil {
		// 抽样预览属于辅助信息；命令不兼容或抽样失败时只返回总数，禁止回退 HSCAN/HGETALL 放大 Redis 压力。
		return map[string]string{}, total, nil
	}
	out := make(map[string]string, len(values))
	for _, item := range values {
		out[item.Key] = item.Value
	}
	return out, total, nil
}

// previewSetValue 使用 SRANDMEMBER 抽样读取 set 缓存预览，并返回完整成员数。
func (l *SystemCacheLogic) previewSetValue(ctx context.Context, key string, maxItems int) ([]string, int64, error) {
	total, err := l.Redis().SCard(ctx, key).Result()
	if err != nil {
		return nil, 0, errors.Tag(err)
	}
	if maxItems <= 0 {
		return []string{}, total, nil
	}
	count := int64(min(int(total), maxItems))
	values, err := l.Redis().SRandMemberN(ctx, key, count).Result()
	if err != nil {
		// Set 预览失败时保持详情接口可用，只返回总量，避免为展示功能引入扫描或全量读取。
		return []string{}, total, nil
	}
	sort.Strings(values)
	return values, total, nil
}

// matchCacheItem 根据具体缓存 key 匹配缓存管理页的缓存项元信息。
// 优先精确匹配固定 key，再匹配模板 key 前缀，便于详情接口和自动重建逻辑复用同一套规则。
func (l *SystemCacheLogic) matchCacheItem(key string) *types.CacheItem {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	physicalKey := tableCachePhysicalKey(l.BaseLogic, key)
	logicalKey := tableCacheLogicalKey(l.BaseLogic, key)
	items := l.cacheItems()
	for i := range items {
		if items[i].Key == key || items[i].Key == physicalKey || tableCacheLogicalKey(l.BaseLogic, items[i].Key) == logicalKey {
			return &items[i]
		}
	}
	for i := range items {
		if !items[i].IsTemplate {
			continue
		}
		prefix := cacheTemplatePrefix(items[i].Key)
		logicalPrefix := cacheTemplatePrefix(tableCacheLogicalKey(l.BaseLogic, items[i].Key))
		if (prefix != "" && (strings.HasPrefix(key, prefix) || strings.HasPrefix(physicalKey, prefix))) ||
			(logicalPrefix != "" && strings.HasPrefix(logicalKey, logicalPrefix)) {
			return &items[i]
		}
	}
	return nil
}

// buildSearchItems 把搜索出的真实 Redis Key 转换成带缓存项元信息的结果集。
// 这样前端检索结果可以直接展示分类、模板实例和刷新粒度，不需要再自行匹配。
func (l *SystemCacheLogic) buildSearchItems(keys []string) []types.CacheSearchItem {
	items := make([]types.CacheSearchItem, 0, len(keys))
	for _, key := range keys {
		items = append(items, types.CacheSearchItem{
			Key:  key,
			Item: l.matchCacheItem(key),
		})
	}
	return items
}

// searchTemplateKeys 尝试把模板型缓存检索转换为“枚举候选 key + 批量校验存在性”。
// 仅对白名单模板生效，未命中白名单时由上层拒绝，不再回退通用 Redis SCAN。
func (l *SystemCacheLogic) searchTemplateKeys(pattern string, maxResults int) ([]string, cacheSearchStats, bool, error) {
	target := l.matchSearchTemplateTarget(pattern)
	if target == nil {
		return nil, cacheSearchStats{}, false, nil
	}
	start := time.Now()
	candidates, err := target.buildKeys(l)
	if err != nil {
		return nil, cacheSearchStats{}, true, errors.Tag(err)
	}
	matchedCandidates := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || !l.cacheSearchCandidateMatches(pattern, candidate, target) {
			continue
		}
		matchedCandidates = append(matchedCandidates, candidate)
	}
	keys, stats, err := l.filterExistingKeys(matchedCandidates, maxResults)
	if err != nil {
		return nil, stats, true, errors.Tag(err)
	}
	stats.candidateCount = len(matchedCandidates)
	stats.providerName = target.providerName
	stats.templateKey = target.templateKey
	stats.latencyMS = time.Since(start).Milliseconds()
	return keys, stats, true, nil
}

// cacheSearchCandidateMatches 判断候选实例 key 是否匹配用户搜索模板。
// table-cache 托管模板允许用户输入旧逻辑模板，同时按新物理模板匹配枚举出的真实 Redis key。
func (l *SystemCacheLogic) cacheSearchCandidateMatches(pattern string, candidate string, target *templateSearchTarget) bool {
	pattern = strings.TrimSpace(pattern)
	candidate = strings.TrimSpace(candidate)
	if pattern == "" || candidate == "" {
		return false
	}
	patterns := []string{pattern}
	if target != nil && strings.HasPrefix(target.templateKey, keys.TableCacheDataPrefix) {
		physicalPattern := tableCachePhysicalKey(l.BaseLogic, pattern)
		if physicalPattern != "" {
			patterns = append(patterns, physicalPattern)
		}
	}
	for _, currentPattern := range helper.UniqueNonEmptyStrings(patterns) {
		if cacheSearchPatternMatch(currentPattern, candidate) {
			return true
		}
	}
	return false
}

// filterExistingKeys 通过 Redis pipeline 批量过滤候选 key 是否真实存在。
// 模板快路径只返回当前 Redis 中实际存在的 key，避免前端拿到“可枚举但尚未构建”的假结果。
func (l *SystemCacheLogic) filterExistingKeys(candidates []string, maxResults int) ([]string, cacheSearchStats, error) {
	if len(candidates) == 0 {
		return []string{}, cacheSearchStats{}, nil
	}
	if l.Redis() == nil {
		return nil, cacheSearchStats{}, errors.Errorf("Redis未初始化")
	}
	ctx, cancel := context.WithTimeout(l.Context(), cacheSearchExistsTimeout)
	defer cancel()

	batchCount := (len(candidates) + cacheSearchExistsBatchSize - 1) / cacheSearchExistsBatchSize
	workerCount := cacheSearchExistsConcurrency
	if workerCount > batchCount {
		workerCount = batchCount
	}
	if workerCount <= 0 {
		workerCount = 1
	}

	resultCap := cacheSearchResultCap(len(candidates), maxResults)
	resultSet := make(map[string]struct{}, resultCap)
	result := make([]string, 0, resultCap)
	stats := cacheSearchStats{}
	jobs := make(chan []string, workerCount)
	var (
		mu        sync.Mutex
		waitGroup sync.WaitGroup
		firstErr  error
		errOnce   sync.Once
		maxHit    bool
	)
	setErr := func(err error) {
		if err == nil {
			return
		}
		errOnce.Do(func() {
			firstErr = err
			cancel()
		})
	}
	for i := 0; i < workerCount; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for batch := range jobs {
				if err := ctx.Err(); err != nil {
					mu.Lock()
					hit := maxHit
					mu.Unlock()
					if !hit {
						setErr(errors.Wrap(err, "缓存模板候选 key 校验超时"))
					}
					return
				}
				existingKeys, err := l.existingKeysInBatch(ctx, batch)
				if err != nil {
					mu.Lock()
					hit := maxHit
					mu.Unlock()
					if hit {
						return
					}
					setErr(err)
					return
				}
				mu.Lock()
				for _, key := range existingKeys {
					if _, ok := resultSet[key]; ok {
						continue
					}
					resultSet[key] = struct{}{}
					result = append(result, key)
					stats.existingCount += 1
					if maxResults > 0 && len(result) >= maxResults {
						maxHit = true
						stats.limited = true
						break
					}
				}
				hit := maxHit
				mu.Unlock()
				if hit {
					cancel()
					return
				}
			}
		}()
	}
	for start := 0; start < len(candidates); start += cacheSearchExistsBatchSize {
		mu.Lock()
		hit := maxHit
		mu.Unlock()
		if hit {
			break
		}
		end := start + cacheSearchExistsBatchSize
		if end > len(candidates) {
			end = len(candidates)
		}
		select {
		case jobs <- candidates[start:end]:
		case <-ctx.Done():
			mu.Lock()
			hit = maxHit
			mu.Unlock()
			if !hit {
				setErr(errors.Wrap(ctx.Err(), "缓存模板候选 key 校验超时"))
			}
			start = len(candidates)
		}
	}
	close(jobs)
	waitGroup.Wait()
	if firstErr != nil {
		return nil, stats, errors.Tag(firstErr)
	}
	sort.Strings(result)
	if maxResults > 0 && len(result) > maxResults {
		return result[:maxResults], stats, nil
	}
	return result, stats, nil
}

// normalizeCacheSearchPaging 归一化缓存搜索分页参数。
// 详情接口复用 CacheKeyReq 时也会带入分页字段，这里只在搜索入口调用，避免详情查询被无关分页参数影响。
func normalizeCacheSearchPaging(req *types.CacheKeyReq) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = cacheSearchDefaultPageSize
	}
	if req.PageSize > cacheSearchMaxPageSize {
		req.PageSize = cacheSearchMaxPageSize
	}
}

// paginateCacheSearchKeys 按页切分已确认存在的 Redis Key。
// 后端先完成受控模板枚举和批量 Exists 校验，再只把当前页返回给前端，避免管理页一次渲染过多 key。
func paginateCacheSearchKeys(keys []string, page int, pageSize int) ([]string, bool) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = cacheSearchDefaultPageSize
	}
	start := (page - 1) * pageSize
	if start < 0 || start >= len(keys) {
		return []string{}, false
	}
	end := start + pageSize
	if end >= len(keys) {
		return keys[start:], false
	}
	return keys[start:end], true
}

// nextCacheSearchPage 返回下一页页码；没有下一页时返回 0，前端可据此禁用“继续加载”。
func nextCacheSearchPage(page int, hasMore bool) int {
	if !hasMore {
		return 0
	}
	if page <= 0 {
		return 2
	}
	return page + 1
}

// existingKeysInBatch 使用单个 Redis pipeline 校验一批候选 key 是否存在。
// 该函数只做精确 Exists，不接受通配符，避免缓存管理页绕过模板白名单扫描生产高基数 key。
func (l *SystemCacheLogic) existingKeysInBatch(ctx context.Context, batch []string) ([]string, error) {
	pipe := l.Redis().Pipeline()
	existsCmds := make([]*redis.IntCmd, 0, len(batch))
	for _, key := range batch {
		existsCmds = append(existsCmds, pipe.Exists(ctx, key))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, errors.Tag(err)
	}
	keys := make([]string, 0, len(batch))
	for idx, key := range batch {
		if existsCmds[idx].Val() > 0 {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// cacheSearchResultCap 计算搜索结果容器的初始容量，避免候选量很大时按候选总数分配内存。
func cacheSearchResultCap(candidateCount int, maxResults int) int {
	if candidateCount <= 0 {
		return 0
	}
	if maxResults > 0 && maxResults < candidateCount {
		return maxResults
	}
	return candidateCount
}

// keyExists 判断单个精确缓存 key 是否已存在于 Redis。
func (l *SystemCacheLogic) keyExists(key string) (bool, error) {
	count, err := l.Redis().Exists(l.Context(), strings.TrimSpace(key)).Result()
	if err != nil {
		return false, errors.Tag(err)
	}
	return count > 0, nil
}

// isWildcardCacheSearchPattern 判断当前搜索条件是否包含 Redis glob 通配符。
func isWildcardCacheSearchPattern(pattern string) bool {
	return strings.ContainsAny(pattern, "*?")
}

// cacheSearchPatternMatch 使用 Redis glob 兼容规则匹配候选 key。
// 当前管理页只依赖 `*` 与 `?`，这里按字节匹配即可满足缓存 key 场景。
func cacheSearchPatternMatch(pattern string, value string) bool {
	pattern = strings.TrimSpace(pattern)
	value = strings.TrimSpace(value)
	patternIndex := 0
	valueIndex := 0
	starIndex := -1
	matchIndex := 0
	for valueIndex < len(value) {
		if patternIndex < len(pattern) && (pattern[patternIndex] == value[valueIndex] || pattern[patternIndex] == '?') {
			patternIndex += 1
			valueIndex += 1
			continue
		}
		if patternIndex < len(pattern) && pattern[patternIndex] == '*' {
			starIndex = patternIndex
			matchIndex = valueIndex
			patternIndex += 1
			continue
		}
		if starIndex >= 0 {
			patternIndex = starIndex + 1
			matchIndex += 1
			valueIndex = matchIndex
			continue
		}
		return false
	}
	for patternIndex < len(pattern) && pattern[patternIndex] == '*' {
		patternIndex += 1
	}
	return patternIndex == len(pattern)
}

// normalizeCacheSearchPattern 归一化缓存 key 搜索条件，避免空条件或全量通配符枚举 Redis。
func normalizeCacheSearchPattern(pattern string) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "*" {
		return "", errors.Errorf("缓存key搜索必须输入至少3个非通配符字符")
	}
	meaningfulCount := 0
	for _, ch := range pattern {
		if ch == '*' || ch == '?' {
			continue
		}
		meaningfulCount++
	}
	if meaningfulCount < 3 {
		return "", errors.Errorf("缓存key搜索必须输入至少3个非通配符字符")
	}
	return pattern, nil
}

// validateSearchPattern 限制通配符搜索只能命中已登记模板，彻底避免回退到 Redis SCAN。
func (l *SystemCacheLogic) validateSearchPattern(pattern string) error {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return errors.Errorf("缓存key不能为空")
	}
	if !isWildcardCacheSearchPattern(pattern) {
		return nil
	}
	if l.matchSearchTemplateTarget(pattern) != nil {
		return nil
	}
	return errors.Errorf("缓存搜索仅支持精确key或已登记模板key，未知通配符禁止搜索")
}

// maskCacheValue 按缓存 key 和字段名脱敏敏感信息，避免缓存管理页暴露 token 与秘钥。
func maskCacheValue(key string, value any) any {
	if isAdminInfoCacheKey(key) {
		switch typed := value.(type) {
		case map[string]string:
			return maskAdminInfoStringMap(typed)
		case map[string]any:
			return maskAdminInfoAnyMap(typed)
		default:
			return value
		}
	}
	if isSensitiveCacheKey(key) {
		return maskWholeCacheValue(value)
	}
	switch typed := value.(type) {
	case map[string]string:
		return maskCacheStringMap(typed, false)
	case map[string]any:
		return maskCacheAnyMap(typed, false)
	default:
		return value
	}
}

// isAdminInfoCacheKey 判断当前缓存 key 是否为管理员登录态缓存。
func isAdminInfoCacheKey(key string) bool {
	return strings.HasPrefix(strings.TrimSpace(key), "admin:info:")
}

// maskAdminInfoStringMap 对管理员登录态缓存做字段级脱敏。
// 缓存管理页需要保留管理员资料字段用于排障，仅保留通用敏感字段规则。
func maskAdminInfoStringMap(value map[string]string) map[string]string {
	result := make(map[string]string, len(value))
	for field, item := range value {
		if !isAdminInfoAllowedPlainField(field) && isSensitiveCacheField(field) {
			result[field] = cacheMaskedValue
			continue
		}
		result[field] = item
	}
	return result
}

// maskAdminInfoAnyMap 对管理员登录态缓存的通用 map 做字段级脱敏。
func maskAdminInfoAnyMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for field, item := range value {
		if !isAdminInfoAllowedPlainField(field) && isSensitiveCacheField(field) {
			result[field] = cacheMaskedValue
			continue
		}
		result[field] = item
	}
	return result
}

// maskWholeCacheValue 对敏感 key 的值做整体脱敏，同时保留基础结构便于排查缓存形态。
func maskWholeCacheValue(value any) any {
	switch typed := value.(type) {
	case map[string]string:
		return maskCacheStringMap(typed, true)
	case map[string]any:
		return maskCacheAnyMap(typed, true)
	case []string:
		if len(typed) == 0 {
			return typed
		}
		return []string{cacheMaskedValue}
	default:
		if value == nil {
			return nil
		}
		return cacheMaskedValue
	}
}

// maskCacheStringMap 对 Redis Hash 的字符串字段做脱敏。
func maskCacheStringMap(value map[string]string, maskAll bool) map[string]string {
	result := make(map[string]string, len(value))
	for field, item := range value {
		if maskAll || isSensitiveCacheField(field) {
			result[field] = cacheMaskedValue
			continue
		}
		result[field] = item
	}
	return result
}

// maskCacheAnyMap 对通用 map 字段做脱敏。
func maskCacheAnyMap(value map[string]any, maskAll bool) map[string]any {
	result := make(map[string]any, len(value))
	for field, item := range value {
		if maskAll || isSensitiveCacheField(field) {
			result[field] = cacheMaskedValue
			continue
		}
		result[field] = item
	}
	return result
}

// isSensitiveCacheKey 判断 Redis Key 是否属于秘钥、登录态或 MFA 票据等敏感缓存。
func isSensitiveCacheKey(key string) bool {
	key = keys.TrimTableCachePrefix(strings.TrimSpace(key))
	for _, prefix := range sensitiveCacheKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// isAdminInfoAllowedPlainField 判断管理员登录态缓存中允许明文展示的字段。
// `needResetPassword` 虽命中通用 password 关键词，但属于状态位，不应在缓存管理页被误遮罩。
func isAdminInfoAllowedPlainField(field string) bool {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "needresetpassword":
		return true
	default:
		return false
	}
}

// isSensitiveCacheField 判断 Redis Hash 字段名是否需要脱敏。
func isSensitiveCacheField(field string) bool {
	field = strings.ToLower(strings.TrimSpace(field))
	sensitiveParts := []string{"token", "password", "private", "public_key", "secret", "mfa_secure_key", "two_step", "aes_key", "aes_iv"}
	for _, part := range sensitiveParts {
		if strings.Contains(field, part) {
			return true
		}
	}
	return false
}

// parseRedisInfo 把 Redis INFO 文本转换成分段 map，方便前端直接展示。
func parseRedisInfo(info string) map[string]map[string]string {
	result := make(map[string]map[string]string)
	section := "default"
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			section = strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if section == "" {
				section = "default"
			}
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if result[section] == nil {
			result[section] = make(map[string]string)
		}
		result[section][parts[0]] = parts[1]
	}
	return result
}

// errorsIsRedisNil 判断错误是否为 Redis 空值。
func errorsIsRedisNil(err error) bool {
	return err == redis.Nil
}
