package secretkey

import (
	"net/http"

	"admin/internal/handler/shared"
)

// RouteSpecs 返回秘钥管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodGet,
			Path:        "/api/secret-keys", // 查询秘钥列表。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SecretKeyList,
			Description: shared.SecretKeyList.Describe,
			Handler:     ListSecretKeyHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/secret-keys/:id", // 查询秘钥详情。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SecretKeyGet,
			Description: shared.SecretKeyGet.Describe,
			Handler:     GetSecretKeyHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/secret-keys", // 新增秘钥。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SecretKeyAdd,
			Description: shared.SecretKeyAdd.Describe,
			Handler:     AddSecretKeyHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/secret-keys/:id", // 编辑秘钥。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SecretKeyUpdate,
			Description: shared.SecretKeyUpdate.Describe,
			Handler:     UpdateSecretKeyHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/secret-keys/status/:id", // 修改秘钥状态。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SecretKeyStatus,
			Description: shared.SecretKeyStatus.Describe,
			Handler:     UpdateSecretKeyStatusHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/secret-keys/cache/refresh/:uuid", // 刷新秘钥缓存。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SecretKeyRenew,
			Description: shared.SecretKeyRenew.Describe,
			Handler:     RenewSecretKeyHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/secret-keys/validate", // 预检秘钥配置。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SecretKeyValidate,
			Description: shared.SecretKeyValidate.Describe,
			Handler:     ValidateSecretKeyHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/secret-keys/self-check/:uuid", // 执行秘钥自检。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.SecretKeySelfCheck,
			Description: shared.SecretKeySelfCheck.Describe,
			Handler:     SelfCheckSecretKeyHandler,
		},
	}
}
