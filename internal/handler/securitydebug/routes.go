package securitydebug

import (
	"net/http"

	"admin/internal/handler/shared"
)

// RouteSpecs 返回安全调试路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodPost,
			Path:        "/api/security/debug/sign", // 安全调试签名。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SecurityDebugSign,
			Description: shared.SecurityDebugSign.Describe,
			Handler:     SecurityDebugSignHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/security/debug/verify", // 安全调试验签。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SecurityDebugVerify,
			Description: shared.SecurityDebugVerify.Describe,
			Handler:     SecurityDebugVerifyHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/security/debug/encrypt", // 安全调试加密。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SecurityDebugEncrypt,
			Description: shared.SecurityDebugEncrypt.Describe,
			Handler:     SecurityDebugEncryptHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/security/debug/decrypt", // 安全调试解密。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SecurityDebugDecrypt,
			Description: shared.SecurityDebugDecrypt.Describe,
			Handler:     SecurityDebugDecryptHandler,
		},
	}
}
