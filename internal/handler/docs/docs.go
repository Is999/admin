package docs

import (
	"net/http"
	"net/url"
	pathpkg "path"
	"strings"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	sitedocs "admin/docs"
	"admin/helper"
	"admin/internal/logic/apiruntime"
	"admin/internal/middleware"
	"admin/internal/requestctx"
	"admin/internal/svc"
)

const (
	// docsSessionMaxAgeSeconds 控制文档 cookie 最长有效期，实际访问仍受 JWT 与 Redis 会话约束。
	docsSessionMaxAgeSeconds = 3600
	// apiDocsProxyPathPrefix 表示 Admin 文档站内的 API 文档代理前缀。
	apiDocsProxyPathPrefix = "/api/docs/api"
	// apiDocsProxyDefaultPath 表示访问 API 文档代理根路径时返回的默认文档。
	apiDocsProxyDefaultPath = "/接口文档/前台系统/系统接口.md"
)

// DocsSessionResp 表示文档访问会话创建结果。
type DocsSessionResp struct {
	ExpiresIn int `json:"expiresIn"` // cookie 最长有效秒数
}

// DocsSessionHandler 将当前登录态写入 HttpOnly cookie，供浏览器直接加载文档站静态资源。
func DocsSessionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		token := ""
		if meta := requestctx.FromContext(ctx); meta != nil {
			token = strings.TrimSpace(meta.AccessToken)
		}
		if token == "" {
			helper.NewJsonResp(ctx, w).
				SetHttpStatus(http.StatusUnauthorized).
				SetCode(codes.Unauthorized).
				Fail(i18n.MsgKeyUnauthorizedText)
			return
		}

		http.SetCookie(w, docsSessionCookie(r, token))
		helper.NewJsonResp(ctx, w).
			SetCode(codes.Success).
			Success(DocsSessionResp{ExpiresIn: docsSessionMaxAgeSeconds})
	}
}

// DocsSiteHandler 返回后台文档站处理器，并按需代理 API 内网接口文档。
func DocsSiteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	localDocs := sitedocs.Handler()
	return func(w http.ResponseWriter, r *http.Request) {
		if docsPath, ok := apiDocsProxyPath(r.URL.Path); ok {
			serveAPIDocsProxy(w, r, svcCtx, docsPath)
			return
		}
		localDocs(w, r)
	}
}

// docsSessionCookie 构造仅限 /api/docs 使用的文档会话 cookie。
func docsSessionCookie(r *http.Request, token string) *http.Cookie {
	return &http.Cookie{
		Name:     middleware.DocsSessionCookieName,
		Value:    strings.TrimSpace(token),
		Path:     middleware.DocsSessionCookiePath,
		MaxAge:   docsSessionMaxAgeSeconds,
		HttpOnly: true,
		Secure:   docsSessionSecure(r),
		SameSite: http.SameSiteLaxMode,
	}
}

// docsSessionSecure 判断当前请求是否经过 HTTPS，用于生产环境开启 Secure cookie。
func docsSessionSecure(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

// apiDocsProxyPath 将 /api/docs/api 下的路径转换为 API 内网文档资源路径。
func apiDocsProxyPath(requestPath string) (string, bool) {
	if text, err := url.PathUnescape(strings.TrimSpace(requestPath)); err == nil {
		requestPath = text
	}
	cleanPath := pathpkg.Clean("/" + strings.TrimLeft(strings.TrimSpace(requestPath), "/"))
	if cleanPath == apiDocsProxyPathPrefix || cleanPath == apiDocsProxyPathPrefix+"/" {
		return apiDocsProxyDefaultPath, true
	}
	if !strings.HasPrefix(cleanPath, apiDocsProxyPathPrefix+"/") {
		return "", false
	}
	return "/" + strings.TrimPrefix(cleanPath, apiDocsProxyPathPrefix+"/"), true
}

// serveAPIDocsProxy 通过 Admin 后端内网读取 API 文档资源，避免浏览器直连 API 服务。
func serveAPIDocsProxy(w http.ResponseWriter, r *http.Request, svcCtx *svc.ServiceContext, docsPath string) {
	if svcCtx == nil {
		http.Error(w, "API文档代理未初始化", http.StatusBadGateway)
		return
	}
	client, err := apiruntime.NewClient(svcCtx.CurrentConfig().APIService)
	if err != nil {
		http.Error(w, "API文档代理未配置", http.StatusBadGateway)
		return
	}
	asset, err := client.DocsAsset(r.Context(), docsPath)
	if err != nil {
		status := http.StatusBadGateway
		if apiStatus := apiruntime.DocsAssetHTTPStatus(err); apiStatus == http.StatusNotFound || apiStatus == http.StatusForbidden {
			status = apiStatus
		}
		http.Error(w, "API文档暂不可用", status)
		return
	}
	if asset.ContentType != "" {
		w.Header().Set("Content-Type", asset.ContentType)
	}
	if asset.CacheControl != "" {
		w.Header().Set("Cache-Control", asset.CacheControl)
	}
	w.WriteHeader(asset.StatusCode)
	_, _ = w.Write(asset.Body)
}
