package middleware

import (
	"net"
	"net/http"
	"strings"

	codes "admin_cron/common/codes"
	i18n "admin_cron/common/i18n"
	"admin_cron/helper"
	"admin_cron/internal/requestctx"

	"github.com/Is999/go-utils"
)

// InternalOnlyMiddleware 仅允许内网或回环地址访问，适用于服务间内网调用入口。
type InternalOnlyMiddleware struct{}

// NewInternalOnlyMiddleware 创建内网访问限制中间件。
func NewInternalOnlyMiddleware() *InternalOnlyMiddleware {
	return &InternalOnlyMiddleware{}
}

// Handle 校验请求来源 IP 是否来自内网；内网请求免 JWT，公网请求直接拒绝。
func (m *InternalOnlyMiddleware) Handle(next http.HandlerFunc, alias RouteAlias) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, _ := requestctx.New(r.Context())
		clientIP := utils.ClientIP(r)
		requestctx.SetRequest(ctx, r.Method, r.URL.Path, clientIP)
		if alias != "" && alias != Ignore {
			requestctx.SetRoute(ctx, string(alias))
		}
		if !isPrivateClientIP(clientIP) {
			helper.NewJsonResp(ctx, w).
				SetHttpStatus(http.StatusUnauthorized).
				SetCode(codes.Unauthorized).
				Fail(i18n.MsgKeyUnauthorizedText)
			return
		}
		next(w, r.WithContext(ctx))
	}
}

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

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return true
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		// 兼容链路本地与运营网络偶发探测请求。
		return ipv4[0] == 169 && ipv4[1] == 254
	}
	// fc00::/7 为 IPv6 内网地址，fe80::/10 为链路本地地址。
	return len(ip) == net.IPv6len &&
		((ip[0]&0xfe) == 0xfc || (ip[0] == 0xfe && (ip[1]&0xc0) == 0x80))
}
