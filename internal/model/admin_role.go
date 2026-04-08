package model

import (
	"time"
)

// TableNameAdminRole 管理员角色表名常量。
const TableNameAdminRole = "admin_role"

// AdminRole 表示后台角色实体。
type AdminRole struct {
	ID        int       `gorm:"column:id;type:int unsigned;primaryKey;autoIncrement:true;comment:主键" json:"id"`                     // 主键
	Title     string    `gorm:"column:title;type:varchar(100);not null;uniqueIndex:uk_title,priority:1;comment:角色名称" json:"title"`  // 角色名称
	Pid       int       `gorm:"column:pid;type:int unsigned;not null;index:idx_pid,priority:1;comment:父级ID" json:"pid"`             // 父级ID
	Pids      string    `gorm:"column:pids;type:varchar(500);not null;comment:父级ID(族谱)" json:"pids"`                                // 父级ID(族谱)
	Status    int       `gorm:"column:status;type:tinyint;not null;default:1;comment:状态：1正常，0禁用" json:"status"`                     // 状态：1正常，0禁用
	Describe  string    `gorm:"column:describe;type:varchar(255);not null;comment:描述" json:"describe"`                              // 描述
	IsDelete  int       `gorm:"column:is_delete;type:tinyint;not null;comment:是否删除: 1删除(关联有用户或下级角色不能删除)" json:"is_delete"`          // 是否删除: 1删除(关联有用户或下级角色不能删除)
	CreatedAt time.Time `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"` // 创建时间
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:修改时间" json:"updated_at"` // 修改时间
}

// TableName 返回管理员角色表名。
func (*AdminRole) TableName() string {
	return TableNameAdminRole
}
