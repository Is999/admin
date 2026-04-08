package logic

import (
	"admin_cron/internal/svc"
	"net/http"
)

// AdminRolePermissionRelLogic 预留角色权限关系逻辑入口。
type AdminRolePermissionRelLogic struct {
	*BaseLogic // 复用上下文、数据库和日志能力
}

// NewAdminRolePermissionRelLogic 创建角色权限关系逻辑对象。
func NewAdminRolePermissionRelLogic(r *http.Request, svcCtx *svc.ServiceContext) *AdminRolePermissionRelLogic {
	return &AdminRolePermissionRelLogic{
		BaseLogic: NewBaseLogic(r, svcCtx),
	}
}
