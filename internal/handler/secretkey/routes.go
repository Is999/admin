package secretkey

import (
	"net/http"

	"admin/internal/handler/shared"
	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册秘钥管理接口。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/secret-keys", // 查询秘钥配置列表
			Handler: authMw.Handle(ListSecretKeyHandler(serverCtx), shared.SecretKeyList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/secret-keys/:id", // 查询秘钥配置详情
			Handler: authMw.Handle(GetSecretKeyHandler(serverCtx), shared.SecretKeyGet.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/secret-keys", // 新增秘钥配置
			Handler: authMw.Handle(AddSecretKeyHandler(serverCtx), shared.SecretKeyAdd.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/secret-keys/:id", // 编辑秘钥配置
			Handler: authMw.Handle(UpdateSecretKeyHandler(serverCtx), shared.SecretKeyUpdate.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/secret-keys/status/:id", // 修改秘钥状态
			Handler: authMw.Handle(UpdateSecretKeyStatusHandler(serverCtx), shared.SecretKeyStatus.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/secret-keys/cache/refresh/:uuid", // 刷新秘钥缓存
			Handler: authMw.Handle(RenewSecretKeyHandler(serverCtx), shared.SecretKeyRenew.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/secret-keys/validate", // 预检秘钥路径和材料可用性
			Handler: authMw.Handle(ValidateSecretKeyHandler(serverCtx), shared.SecretKeyValidate.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/secret-keys/self-check/:uuid", // 执行秘钥运行态自检
			Handler: authMw.Handle(SelfCheckSecretKeyHandler(serverCtx), shared.SecretKeySelfCheck.Alias),
		},
	})
}
