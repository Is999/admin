package model

import "time"

// TableNameAdminRoleDocPermissionRel 角色与文档权限关系表名常量。
const TableNameAdminRoleDocPermissionRel = "admin_role_doc_permission_rel"

// AdminRoleDocPermissionRel 表示角色与单篇文档的授权关系。
type AdminRoleDocPermissionRel struct {
	RoleID          int       `gorm:"column:role_id;type:int unsigned;primaryKey;comment:角色ID" json:"role_id"`                                                              // 角色 ID
	DocPermissionID int       `gorm:"column:doc_permission_id;type:int unsigned;primaryKey;index:idx_doc_permission_id,priority:1;comment:文档权限ID" json:"doc_permission_id"` // 文档权限 ID
	CreatedAt       time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:关联创建时间" json:"created_at"`                                  // 关联创建时间
}

// TableName 返回角色文档权限关系表名。
func (*AdminRoleDocPermissionRel) TableName() string {
	return TableNameAdminRoleDocPermissionRel
}
