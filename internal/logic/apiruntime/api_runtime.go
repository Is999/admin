package apiruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	"admin/common/codes"
	i18n "admin/common/i18n"
	"admin/internal/config"
	adminlogic "admin/internal/logic/admin"
	securitylogic "admin/internal/logic/security"
	"admin/internal/svc"
	"admin/internal/types"
)

const (
	// apiRuntimeDefaultTimeoutSeconds 表示调用 API 内网接口的默认超时时间。
	apiRuntimeDefaultTimeoutSeconds = 5
	// apiRuntimeMaxTimeoutSeconds 表示调用 API 内网接口的最大超时时间。
	apiRuntimeMaxTimeoutSeconds = 30
	// apiRuntimeOpsTokenHeader 表示 API 运维接口令牌请求头。
	apiRuntimeOpsTokenHeader = "X-Ops-Token"
	// apiRuntimeConfigReloadStatusPath 表示 API 配置热加载状态内网路径。
	apiRuntimeConfigReloadStatusPath = "/internal/system/config-reload/status"
	// apiRuntimeConfigReloadItemsPath 表示 API 运行态配置项内网路径。
	apiRuntimeConfigReloadItemsPath = "/internal/system/config-reload/items"
	// apiRuntimeConfigReloadRunPath 表示 API 配置热加载触发内网路径。
	apiRuntimeConfigReloadRunPath = "/internal/system/config-reload/run"
	// userRuntimeSyncPathPrefix 表示前台用户 API 运行态同步内网路径前缀。
	userRuntimeSyncPathPrefix = "/internal/users/"
	// apiDocsInternalPathPrefix 表示 API 内网文档资源路径前缀。
	apiDocsInternalPathPrefix = "/internal/docs"
	// apiDocsMaxResponseBytes 限制单个 API 文档资源最大响应，避免代理异常大文件。
	apiDocsMaxResponseBytes = 4 << 20
	// apiRuntimeMaxResponseBytes 限制 API 运维接口 JSON 响应，配置快照允许超过 1MiB。
	apiRuntimeMaxResponseBytes = 8 << 20
)

// Logic 负责调用必须落在 API 进程内的运行态能力。
type Logic struct {
	*adminlogic.AdminLogic // 复用后台登录态、MFA 和审计公共能力
}

// Client 是 admin 调用 API 内网运维接口的轻量客户端。
type Client struct {
	baseURL string       // API 内网服务地址
	token   string       // X-Ops-Token 运维令牌
	client  *http.Client // 独立 HTTP 客户端，限制超时避免阻塞后台请求
}

// DocsAsset 表示从 API 内网读取到的文档资源。
type DocsAsset struct {
	StatusCode   int    // API 内网响应状态码
	ContentType  string // 文档资源 Content-Type
	CacheControl string // 文档资源缓存策略
	Body         []byte // 文档资源内容
}

// DocsAssetError 表示 API 文档资源代理失败。
type DocsAssetError struct {
	StatusCode int    // API 内网响应状态码
	Message    string // API 内网失败响应摘要
}

// Error 返回文档资源代理错误说明。
func (e *DocsAssetError) Error() string {
	if e == nil {
		return ""
	}
	return "API 文档资源读取失败 HTTP " + strconv.Itoa(e.StatusCode) + ": " + strings.TrimSpace(e.Message)
}

// apiResponse 表示 API 服务统一响应结构。
type apiResponse[T any] struct {
	Status  bool   `json:"status"`  // 是否成功
	Code    int    `json:"code"`    // 业务码
	Message string `json:"message"` // 响应文案
	Data    T      `json:"data"`    // 响应数据
	TraceID string `json:"traceId"` // API 侧 trace id
}

// userRuntimeSyncPayload 表示发给 API 的用户运行态同步载荷。
type userRuntimeSyncPayload struct {
	Profile  bool   `json:"profile"`  // 是否失效资料缓存
	Sessions bool   `json:"sessions"` // 是否失效登录态
	Reason   string `json:"reason"`   // 同步原因
}

// NewLogic 创建 API 运行态管理逻辑对象。
func NewLogic(r *http.Request, svcCtx *svc.ServiceContext) *Logic {
	return &Logic{AdminLogic: adminlogic.NewAdminLogic(r, svcCtx)}
}

// NewClient 根据后台配置创建 API 内网客户端。
func NewClient(cfg config.APIServiceConfig) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.InternalBaseURL), "/")
	token := strings.TrimSpace(cfg.OpsToken)
	if baseURL == "" || token == "" {
		return nil, errors.New("api_service.internal_base_url 或 api_service.ops_token 未配置")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.Errorf("api_service.internal_base_url 配置不合法")
	}
	timeoutSeconds := cfg.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = apiRuntimeDefaultTimeoutSeconds
	}
	if timeoutSeconds > apiRuntimeMaxTimeoutSeconds {
		timeoutSeconds = apiRuntimeMaxTimeoutSeconds
	}
	return &Client{
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
	}, nil
}

// Configured 判断 API 内网客户端是否具备基础调用配置。
func Configured(cfg config.APIServiceConfig) bool {
	return strings.TrimSpace(cfg.InternalBaseURL) != "" && strings.TrimSpace(cfg.OpsToken) != ""
}

// Status 查询 API 配置热加载状态。
func (l *Logic) Status() *types.BizResult {
	client, err := NewClient(l.Svc.CurrentConfig().APIService)
	if err != nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyQuerySuccess).
			WithData(&types.APIRuntimeConfigReloadResp{Connected: false, Message: err.Error()})
	}
	status, err := client.ConfigReloadStatus(l.Ctx)
	if err != nil {
		return types.ServerError(i18n.MsgKeyTaskQueryFail, err, "APIRuntimeLogic.Status 查询 API 热加载状态失败").ToBizResult()
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(&types.APIRuntimeConfigReloadResp{Connected: true, Status: status, Message: "API 热加载状态已获取"})
}

// Items 查询 API 当前运行态配置项。
func (l *Logic) Items(req *types.TaskConfigItemQueryReq) *types.BizResult {
	if req == nil {
		req = &types.TaskConfigItemQueryReq{}
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	client, err := NewClient(l.Svc.CurrentConfig().APIService)
	if err != nil {
		return types.NewBizResult(codes.Success).
			SetI18nMessage(i18n.MsgKeyQuerySuccess).
			WithData(&types.APIRuntimeConfigItemsResp{Connected: false, Message: err.Error()})
	}
	items, err := client.ConfigReloadItems(l.Ctx, req)
	if err != nil {
		return types.ServerError(i18n.MsgKeyTaskQueryFail, err, "APIRuntimeLogic.Items 查询 API 运行态配置项失败").ToBizResult()
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(&types.APIRuntimeConfigItemsResp{Connected: true, Items: items, Message: "API 运行态配置项已获取"})
}

// Reload 手动触发 API 配置热加载。
func (l *Logic) Reload(req *types.APIRuntimeConfigReloadReq) *types.BizResult {
	if err := l.RequireOperateMFATwoStep(securitylogic.MFAScenarioAPIRuntimeManage, req.TwoStepKey, req.TwoStepValue); err != nil {
		return l.MFABizResult(err)
	}
	client, err := NewClient(l.Svc.CurrentConfig().APIService)
	if err != nil {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyFail).
			WithError(errors.Wrap(err, "APIRuntimeLogic.Reload API 内网客户端未配置"))
	}
	status, err := client.RunConfigReload(l.Ctx)
	if err != nil {
		return types.ServerError(i18n.MsgKeyTaskTriggerFail, err, "APIRuntimeLogic.Reload 触发 API 热加载失败").ToBizResult()
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess).
		WithData(&types.APIRuntimeConfigReloadResp{Connected: true, Status: status, Message: "API 热加载已触发"})
}

// ConfigReloadStatus 查询 API 配置热加载状态。
func (c *Client) ConfigReloadStatus(ctx context.Context) (*types.TaskConfigReloadStatusResp, error) {
	return requestAPI[types.TaskConfigReloadStatusResp](ctx, c, http.MethodGet, apiRuntimeConfigReloadStatusPath, nil)
}

// ConfigReloadItems 查询 API 运行态配置项。
func (c *Client) ConfigReloadItems(ctx context.Context, req *types.TaskConfigItemQueryReq) (*types.TaskConfigItemQueryResp, error) {
	query := url.Values{}
	if req != nil {
		if strings.TrimSpace(req.Keyword) != "" {
			query.Set("keyword", strings.TrimSpace(req.Keyword))
		}
		if req.SensitiveOnly {
			query.Set("sensitiveOnly", "true")
		}
		if req.Page > 0 {
			query.Set("page", strconv.Itoa(req.Page))
		}
		if req.PageSize > 0 {
			query.Set("pageSize", strconv.Itoa(req.PageSize))
		}
	}
	path := apiRuntimeConfigReloadItemsPath
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return requestAPI[types.TaskConfigItemQueryResp](ctx, c, http.MethodGet, path, nil)
}

// RunConfigReload 手动触发 API 配置热加载。
func (c *Client) RunConfigReload(ctx context.Context) (*types.TaskConfigReloadStatusResp, error) {
	return requestAPI[types.TaskConfigReloadStatusResp](ctx, c, http.MethodPost, apiRuntimeConfigReloadRunPath, nil)
}

// SyncUserRuntime 同步前台用户在 API 进程内的资料缓存或登录态。
func (c *Client) SyncUserRuntime(ctx context.Context, userID int64, profile bool, sessions bool, reason string) (*types.UserRuntimeSyncResp, error) {
	if userID <= 0 {
		return nil, errors.New("用户ID不能为空")
	}
	if !profile && !sessions {
		profile = true
	}
	path := userRuntimeSyncPathPrefix + strconv.FormatInt(userID, 10) + "/runtime-sync"
	data, err := requestAPI[types.UserRuntimeSyncResp](ctx, c, http.MethodPost, path, userRuntimeSyncPayload{
		Profile:  profile,
		Sessions: sessions,
		Reason:   strings.TrimSpace(reason),
	})
	if err != nil {
		return nil, errors.Tag(err)
	}
	data.Enabled = true
	data.Success = true
	if data.Message == "" {
		data.Message = "API 运行态已同步"
	}
	return data, nil
}

// DocsAsset 通过 API 内网接口读取可在后台展示的接口文档资源。
func (c *Client) DocsAsset(ctx context.Context, docsPath string) (*DocsAsset, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("API 内网客户端未初始化")
	}
	docsPath = "/" + strings.TrimLeft(strings.TrimSpace(docsPath), "/")
	req, err := buildAPIRequest(ctx, c, http.MethodGet, apiDocsInternalPathPrefix+docsPath, nil)
	if err != nil {
		return nil, errors.Tag(err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "请求 API 文档资源失败")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, apiDocsMaxResponseBytes+1))
	if err != nil {
		return nil, errors.Wrap(err, "读取 API 文档资源失败")
	}
	if len(body) > apiDocsMaxResponseBytes {
		return nil, errors.Errorf("API 文档资源超过大小限制")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &DocsAssetError{StatusCode: resp.StatusCode, Message: string(body)}
	}
	return &DocsAsset{
		StatusCode:   resp.StatusCode,
		ContentType:  resp.Header.Get("Content-Type"),
		CacheControl: resp.Header.Get("Cache-Control"),
		Body:         body,
	}, nil
}

// DocsAssetHTTPStatus 返回 API 文档资源错误中的 HTTP 状态码。
func DocsAssetHTTPStatus(err error) int {
	if assetErr, ok := err.(*DocsAssetError); ok {
		return assetErr.StatusCode
	}
	return 0
}

// requestAPI 执行 API 内网请求并解析统一响应。
func requestAPI[T any](ctx context.Context, c *Client, method string, path string, payload any) (*T, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("API 内网客户端未初始化")
	}
	req, err := buildAPIRequest(ctx, c, method, path, payload)
	if err != nil {
		return nil, errors.Tag(err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "请求 API 内网接口失败")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, apiRuntimeMaxResponseBytes+1))
	if err != nil {
		return nil, errors.Wrap(err, "读取 API 内网响应失败")
	}
	if len(body) > apiRuntimeMaxResponseBytes {
		return nil, errors.Errorf("API 内网响应超过大小限制 max_bytes=%d", apiRuntimeMaxResponseBytes)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, errors.Errorf("API 内网接口 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded apiResponse[T]
	if err = json.Unmarshal(body, &decoded); err != nil {
		return nil, errors.Wrap(err, "解析 API 内网响应失败")
	}
	if !decoded.Status {
		return nil, errors.Errorf("API 内网接口返回失败 code=%d message=%s traceId=%s", decoded.Code, decoded.Message, decoded.TraceID)
	}
	return &decoded.Data, nil
}

// buildAPIRequest 构造带运维令牌的 API 内网 HTTP 请求。
func buildAPIRequest(ctx context.Context, c *Client, method string, path string, payload any) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, errors.Wrap(err, "序列化 API 内网请求失败")
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, errors.Wrap(err, "创建 API 内网请求失败")
	}
	req.Header.Set(apiRuntimeOpsTokenHeader, c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}
