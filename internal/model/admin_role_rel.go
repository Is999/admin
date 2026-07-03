package model

import (
	"time"
)

// TableNameAdminRoleRel 管理员与角色绑定关系表名常量。
const TableNameAdminRoleRel = "admin_role_rel"

// AdminRoleRel 表示管理员与角色的关联关系。
type AdminRoleRel struct {
	UserID    int       `gorm:"column:user_id;type:int unsigned;primaryKey;comment:用户id" json:"user_id"`                              // 用户id
	RoleID    int       `gorm:"column:role_id;type:int unsigned;primaryKey;index:idx_role_id,priority:1;comment:角色id" json:"role_id"` // 角色id
	CreatedAt time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`    // 创建时间
}

// TableName 返回管理员角色关系表名。
func (*AdminRoleRel) TableName() string {
	return TableNameAdminRoleRel
}
