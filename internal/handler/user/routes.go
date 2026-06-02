package user

import (
	"net/http"

	"admin/internal/handler/shared"
)

// RouteSpecs 返回前台用户和 API 运行态管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodGet,
			Path:        "/api/users", // 查询前台用户列表。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.UserList,
			Description: shared.UserList.Describe,
			Handler:     ListHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/users", // 新增前台用户。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.UserAdd,
			Description: shared.UserAdd.Describe,
			Handler:     CreateHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/users/:id", // 查询前台用户详情。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.UserInfo,
			Description: shared.UserInfo.Describe,
			Handler:     GetHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/users/:id", // 编辑前台用户资料。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.UserUpdate,
			Description: shared.UserUpdate.Describe,
			Handler:     UpdateHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/users/status/:id", // 修改前台用户状态。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.UserStatusUpdate,
			Description: shared.UserStatusUpdate.Describe,
			Handler:     UpdateStatusHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/users/password/reset/:id", // 重置前台用户密码。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.UserPasswordReset,
			Description: shared.UserPasswordReset.Describe,
			Handler:     ResetPasswordHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/users/runtime-sync/:id", // 手动同步前台用户 API 运行态。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.UserRuntimeSync,
			Description: shared.UserRuntimeSync.Describe,
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
			Method:      http.MethodGet,
			Path:        "/api/api-runtime/config-reload/items", // 查询 API 当前运行态配置项。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.APIRuntimeConfigReloadItems,
			Description: shared.APIRuntimeConfigReloadItems.Describe,
			Handler:     APIRuntimeReloadItemsHandler,
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
