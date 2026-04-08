package model

import (
	"time"
)

// TableNameAdminRolePermissionRel 角色与权限绑定关系表名常量。
const TableNameAdminRolePermissionRel = "admin_role_permission_rel"

// AdminRolePermissionRel 表示角色与权限点的关联关系。
type AdminRolePermissionRel struct {
	RoleID       int64     `gorm:"column:role_id;type:bigint unsigned;primaryKey;comment:角色ID" json:"role_id"`                                                // 角色 ID
	PermissionID int64     `gorm:"column:permission_id;type:bigint unsigned;primaryKey;index:idx_permission_id,priority:1;comment:权限ID" json:"permission_id"` // 权限 ID
	CreatedAt    time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP" json:"created_at"`                                      // 关联创建时间
}

// TableName 返回角色权限关系表名。
func (*AdminRolePermissionRel) TableName() string {
	return TableNameAdminRolePermissionRel
}
