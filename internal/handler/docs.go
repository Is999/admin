package handler

import (
	"net/http"
	"strings"

	codes "admin_cron/common/codes"
	i18n "admin_cron/common/i18n"
	"admin_cron/helper"
	"admin_cron/internal/middleware"
	"admin_cron/internal/requestctx"
)

const (
	// docsSessionMaxAgeSeconds 控制文档 cookie 最长有效期，实际访问仍受 JWT 与 Redis 会话约束。
	docsSessionMaxAgeSeconds = 3600
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
