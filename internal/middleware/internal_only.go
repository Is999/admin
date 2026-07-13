package middleware

import (
	"net"
	"net/http"
	"strings"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	"admin/helper"
	"admin/internal/requestctx"
	"admin/internal/svc"
)

// InternalOnlyMiddleware 仅允许内网或回环地址访问，适用于服务间内网调用入口。
type InternalOnlyMiddleware struct {
	svc *svc.ServiceContext // 内网来源解析使用的可信代理配置
}

// NewInternalOnlyMiddleware 创建内网访问限制中间件。
func NewInternalOnlyMiddleware(svcCtx *svc.ServiceContext) *InternalOnlyMiddleware {
	return &InternalOnlyMiddleware{svc: svcCtx}
}

// Handle 校验请求来源 IP 是否来自内网；内网请求免 JWT，公网请求直接拒绝。
func (m *InternalOnlyMiddleware) Handle(next http.HandlerFunc, alias RouteAlias) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, _ := requestctx.New(r.Context())
		clientIP := requestClientIP(m.svc, r)
		requestctx.SetRequest(ctx, r.Method, r.URL.Path, clientIP)
		if alias != "" && alias != Ignore {
			requestctx.SetRoute(ctx, string(alias))
		}
		if !isPrivateClientIP(clientIP) {
			helper.NewJSONResp(ctx, w).
				SetHTTPStatus(http.StatusUnauthorized).
				SetCode(codes.Unauthorized).
				Fail(i18n.MsgKeyUnauthorizedText)
			return
		}
		next(w, r.WithContext(ctx))
	}
}

// isPrivateClientIP 判断客户端地址是否属于可信内网或本机回环地址。
func isPrivateClientIP(clientIP string) bool {
	host := strings.TrimSpace(clientIP)
	if host == "" {
		return false
	}
	if parsed := net.ParseIP(host); parsed != nil {
		return isPrivateIP(parsed)
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		if parsed := net.ParseIP(parsedHost); parsed != nil {
			return isPrivateIP(parsed)
		}
	}
	return false
}

// isPrivateIP 只放行回环地址和私有地址；链路本地地址不属于业务内网白名单。
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}
