package profile

import (
	"net/http"

	"admin/internal/handler/shared"
)

// RouteSpecs 返回个人中心和账号安全管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodPatch,
			Path:        "/api/admins/mfa-status/:id", // 修改管理员MFA状态。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminMFAStatus,
			Description: shared.AdminMFAStatus.Describe,
			Handler:     AdminMFAStatusHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/admins/mfa-secret-key-url/:id", // 生成管理员MFA绑定地址。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminBuildMFAURL,
			Description: shared.AdminBuildMFAURL.Describe,
			Handler:     AdminBuildMFASecretKeyURLHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/profile/password", // 个人中心修改密码。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.ProfileUpdatePassword,
			Description: shared.ProfileUpdatePassword.Describe,
			Handler:     ProfileUpdatePasswordHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/profile", // 个人中心修改资料。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.ProfileUpdateMine,
			Description: shared.ProfileUpdateMine.Describe,
			Handler:     ProfileUpdateMineHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/profile/mfa-status", // 个人中心修改MFA状态。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.ProfileUpdateMFA,
			Description: shared.ProfileUpdateMFA.Describe,
			Handler:     ProfileUpdateMFAStatusHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/profile/mfa-secret", // 个人中心修改MFA秘钥。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.ProfileUpdateMFAKey,
			Description: shared.ProfileUpdateMFAKey.Describe,
			Handler:     ProfileUpdateMFASecureKeyHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/profile/mfa-secret/refresh", // 个人中心重新生成MFA秘钥。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.ProfileRefreshMFAKey,
			Description: shared.ProfileRefreshMFAKey.Describe,
			Handler:     ProfileRefreshMFASecretKeyHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/profile/avatar", // 个人中心修改头像。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.ProfileUpdateAvatar,
			Description: shared.ProfileUpdateAvatar.Describe,
			Handler:     ProfileUpdateAvatarHandler,
		},
	}
}
