//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"strings"

	"github.com/Is999/go-utils/errors"
)

// CacheListReq 表示缓存管理列表请求。
type CacheListReq struct {
	Key string `json:"key,optional" form:"key,optional"` // 缓存键筛选
}

// Validate 校验缓存管理列表请求。
func (r *CacheListReq) Validate() error {
	r.Key = strings.TrimSpace(r.Key)
	return nil
}

// CacheKeyReq 表示缓存键查询请求。
type CacheKeyReq struct {
	Key        string `json:"key,optional" form:"key,optional"`       // Redis 缓存键
	Source     string `json:"source,optional" form:"source,optional"` // 请求来源：manual_search/route_jump/template_locate/template_drawer
	GetPageReq        // 复用分页参数
}

// Validate 校验缓存键查询请求。
func (r *CacheKeyReq) Validate() error {
	r.Key = strings.TrimSpace(r.Key)
	r.Source = strings.TrimSpace(r.Source)
	if r.Key == "" {
		return errors.Errorf("缓存key不能为空")
	}
	return nil
}

// CacheRenewReq 表示缓存刷新请求。
type CacheRenewReq struct {
	Key  string `json:"key,optional" form:"key,optional"`   // Redis 缓存键或缓存目标
	Type string `json:"type,optional" form:"type,optional"` // Redis 类型，用于选择刷新处理方式
}

// Validate 校验缓存刷新请求。
func (r *CacheRenewReq) Validate() error {
	r.Key = strings.TrimSpace(r.Key)
	r.Type = strings.TrimSpace(r.Type)
	if r.Key == "" {
		return errors.Errorf("缓存key不能为空")
	}
	return nil
}

// CacheWarmupReq 表示模板缓存预热请求。
// 预热用于“从无到有”批量构建缓存实例，解决模板 key 因 Redis 未命中而无法全量刷新的问题。
type CacheWarmupReq struct {
	TemplateKey string `json:"templateKey,optional" form:"templateKey,optional"` // 模板缓存键定义
	Limit       int    `json:"limit,optional" form:"limit,optional"`             // 限制本次最多预热多少个实例，避免一次性回源过大
}

// Validate 校验模板缓存预热请求。
func (r *CacheWarmupReq) Validate() error {
	r.TemplateKey = strings.TrimSpace(r.TemplateKey)
	if r.TemplateKey == "" {
		return errors.Errorf("模板缓存key不能为空")
	}
	if r.Limit < 0 {
		return errors.Errorf("预热数量限制不合法")
	}
	return nil
}

// CacheWarmupResp 表示模板缓存预热响应。
type CacheWarmupResp struct {
	TemplateKey string   `json:"templateKey"`          // 当前预热的模板缓存键定义
	Total       int      `json:"total"`                // 预热命中并处理的实例 key 总数
	Success     int      `json:"success"`              // 预热成功数量
	Failed      int      `json:"failed"`               // 预热失败数量
	FailedKeys  []string `json:"failedKeys,omitempty"` // 失败 key 采样列表（仅用于排查，避免返回过大）
	LatencyMS   int64    `json:"latencyMs"`            // 本次预热耗时（毫秒）
}

// CacheItem 表示可刷新的缓存目标。
type CacheItem struct {
	Index           string `json:"index"`           // 缓存目标索引
	Key             string `json:"key"`             // Redis 缓存键或模板
	KeyTitle        string `json:"keyTitle"`        // Redis 缓存键展示标题
	Type            string `json:"type"`            // Redis 数据类型
	Remark          string `json:"remark"`          // 缓存说明
	Category        string `json:"category"`        // 缓存分类：auth/permission/config/secret/session/system
	IsTemplate      bool   `json:"isTemplate"`      // 是否为模板型缓存 key
	ExampleKey      string `json:"exampleKey"`      // 模板型缓存示例 key，便于管理页直接复制查询
	WarmupSupported bool   `json:"warmupSupported"` // 是否支持按模板枚举真实 key 并预热
	AutoRebuild     bool   `json:"autoRebuild"`     // 查询详情 miss 时是否支持自动回源重建
	RefreshScope    string `json:"refreshScope"`    // 刷新粒度：single/all/prefix
}

// CacheKeyInfoResp 表示缓存键详情响应。
type CacheKeyInfoResp struct {
	Key   string     `json:"key"`            // Redis 缓存键
	Type  string     `json:"type"`           // Redis 数据类型
	TTL   int64      `json:"ttl"`            // Redis 剩余过期时间，单位秒
	Total int64      `json:"total"`          // 当前缓存值数量
	Value any        `json:"value"`          // 当前缓存值
	Item  *CacheItem `json:"item,omitempty"` // 当前 key 命中的缓存项元信息，便于前端展示说明与操作提示
}

// CacheSearchItem 表示缓存键搜索结果项。
type CacheSearchItem struct {
	Key  string     `json:"key"`            // 真实 Redis 缓存键
	Item *CacheItem `json:"item,omitempty"` // 当前缓存键命中的缓存项元信息，便于搜索结果直接展示分类与提示
}

// CacheSearchResp 表示缓存键搜索分页响应。
type CacheSearchResp struct {
	List           []CacheSearchItem `json:"list"`                     // 当前页真实 Redis Key 列表
	Total          int64             `json:"total"`                    // 当前搜索条件下已确认存在的 Redis Key 总数
	Page           int               `json:"page"`                     // 当前页码，从 1 开始
	PageSize       int               `json:"pageSize"`                 // 当前每页数量
	HasMore        bool              `json:"hasMore"`                  // 是否还有下一页，供前端流式加载或分页加载使用
	NextPage       int               `json:"nextPage,omitempty"`       // 下一页页码；没有下一页时为空
	SearchPath     string            `json:"searchPath,omitempty"`     // 本次搜索命中的后端链路：exact_exists/template_candidates
	ProviderName   string            `json:"providerName,omitempty"`   // 模板搜索 provider 名称，便于排查模板枚举来源
	TemplateKey    string            `json:"templateKey,omitempty"`    // 命中的模板 key 定义
	CandidateTotal int               `json:"candidateTotal,omitempty"` // 模板 provider 枚举出的候选 key 数量
	ExistingTotal  int               `json:"existingTotal,omitempty"`  // Redis 中已确认存在的 key 数量
	Limited        bool              `json:"limited"`                  // 是否触发后端最大搜索窗口保护
	MaxResults     int               `json:"maxResults"`               // 后端单次搜索最多确认的真实 key 数量
}

// CacheMetricSummary 表示当前进程的表缓存运行指标汇总。
type CacheMetricSummary struct {
	HitTotal            int64   `json:"hitTotal"`            // 缓存命中次数
	MissTotal           int64   `json:"missTotal"`           // 缓存未命中次数
	HitRate             float64 `json:"hitRate"`             // 缓存命中率，范围 0-100
	RefreshSuccessTotal int64   `json:"refreshSuccessTotal"` // 刷新成功次数
	RefreshErrorTotal   int64   `json:"refreshErrorTotal"`   // 刷新失败次数
	LoaderErrorTotal    int64   `json:"loaderErrorTotal"`    // 回源失败次数
	LockFailedTotal     int64   `json:"lockFailedTotal"`     // 获取重建锁失败次数
	WaitTimeoutTotal    int64   `json:"waitTimeoutTotal"`    // 等待其它实例重建超时次数
	ScanFallbackTotal   int64   `json:"scanFallbackTotal"`   // 前缀删除降级 SCAN 次数
	BatchSuccessTotal   int64   `json:"batchSuccessTotal"`   // 批量刷新成功条目数
	BatchFailedTotal    int64   `json:"batchFailedTotal"`    // 批量刷新失败条目数
}

// CacheMetricTarget 表示单个表缓存目标的当前进程指标。
type CacheMetricTarget struct {
	Index               string  `json:"index"`               // 缓存目标索引
	KeyTitle            string  `json:"keyTitle"`            // 缓存键或模板
	Remark              string  `json:"remark"`              // 缓存说明
	Category            string  `json:"category"`            // 缓存分类
	HitTotal            int64   `json:"hitTotal"`            // 缓存命中次数
	MissTotal           int64   `json:"missTotal"`           // 缓存未命中次数
	HitRate             float64 `json:"hitRate"`             // 缓存命中率，范围 0-100
	RefreshSuccessTotal int64   `json:"refreshSuccessTotal"` // 刷新成功次数
	RefreshErrorTotal   int64   `json:"refreshErrorTotal"`   // 刷新失败次数
	LoaderErrorTotal    int64   `json:"loaderErrorTotal"`    // 回源失败次数
	LockFailedTotal     int64   `json:"lockFailedTotal"`     // 获取重建锁失败次数
	WaitTimeoutTotal    int64   `json:"waitTimeoutTotal"`    // 等待其它实例重建超时次数
	ScanFallbackTotal   int64   `json:"scanFallbackTotal"`   // 前缀删除降级 SCAN 次数
}

// CacheMetricsResp 表示当前管理进程的表缓存运行观测快照。
type CacheMetricsResp struct {
	Scope       string              `json:"scope"`       // 指标范围，固定为 current_process
	InstanceID  string              `json:"instanceId"`  // 当前进程实例标识
	StartedAt   string              `json:"startedAt"`   // 当前进程指标起始时间
	GeneratedAt string              `json:"generatedAt"` // 快照生成时间
	Summary     CacheMetricSummary  `json:"summary"`     // 当前进程汇总指标
	Targets     []CacheMetricTarget `json:"targets"`     // 按缓存目标拆分的指标
}
