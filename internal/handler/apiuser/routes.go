package apiuser

import (
	"net/http"

	"admin/internal/handler/shared"
)

// RouteSpecs 返回前台用户和 API 运行态管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodGet,
			Path:        "/api/api-users", // 查询前台用户列表。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.APIUserList,
			Description: shared.APIUserList.Describe,
			Handler:     ListHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/api-users", // 新增前台用户。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.APIUserAdd,
			Description: shared.APIUserAdd.Describe,
			Handler:     CreateHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/api-users/:id", // 查询前台用户详情。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.APIUserInfo,
			Description: shared.APIUserInfo.Describe,
			Handler:     GetHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/api-users/:id", // 编辑前台用户资料。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.APIUserUpdate,
			Description: shared.APIUserUpdate.Describe,
			Handler:     UpdateHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/api-users/status/:id", // 修改前台用户状态。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.APIUserStatusUpdate,
			Description: shared.APIUserStatusUpdate.Describe,
			Handler:     UpdateStatusHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/api-users/password/reset/:id", // 重置前台用户密码。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.APIUserPasswordReset,
			Description: shared.APIUserPasswordReset.Describe,
			Handler:     ResetPasswordHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/api-users/runtime-sync/:id", // 手动同步前台用户 API 运行态。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.APIUserRuntimeSync,
			Description: shared.APIUserRuntimeSync.Describe,
			Handler:     SyncRuntimeHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/api-runtime/config-reload", // 查询 API 配置热加载状态。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.APIRuntimeConfigReloadStatus,
			Description: shared.APIRuntimeConfigReloadStatus.Describe,
			Handler:     APIRuntimeReloadStatusHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/api-runtime/config-reload", // 手动触发 API 配置热加载。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.APIRuntimeConfigReloadRun,
			Description: shared.APIRuntimeConfigReloadRun.Describe,
			Handler:     APIRuntimeReloadRunHandler,
		},
	}
}
