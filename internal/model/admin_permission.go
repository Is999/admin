package model

import (
	"time"
)

// TableNameAdminPermission 管理员权限表名常量。
const TableNameAdminPermission = "admin_permission"

// AdminPermission 表示后台权限点实体。
type AdminPermission struct {
	ID          int       `gorm:"column:id;type:int unsigned;primaryKey;autoIncrement:true;comment:主键" json:"id"`                        // 主键
	UUID        string    `gorm:"column:uuid;type:char(8);not null;uniqueIndex:uk_uuid,priority:1;comment:唯一标识" json:"uuid"`             // 唯一标识
	Title       string    `gorm:"column:title;type:varchar(100);not null;index:idx_title,priority:1;comment:权限名称" json:"title"`          // 权限名称
	Module      string    `gorm:"column:module;type:varchar(250);not null;comment:权限匹配模型(路由名称 | 控制器/方法)" json:"module"`                  // 权限匹配模型(路由名称 | 控制器/方法)
	Pid         int       `gorm:"column:pid;type:int unsigned;not null;comment:父级ID" json:"pid"`                                         // 父级ID
	Pids        string    `gorm:"column:pids;type:varchar(500);not null;comment:父级ID(族谱)" json:"pids"`                                   // 父级ID(族谱)
	Type        int       `gorm:"column:type;type:tinyint;not null;comment:类型: 0查看, 1新增, 2修改, 3删除, 4目录, 5菜单, 6页面, 7按钮, 8其它" json:"type"` // 类型: 0查看, 1新增, 2修改, 3删除, 4目录, 5菜单, 6页面, 7按钮, 8其它
	Description string    `gorm:"column:description;type:varchar(255);not null;comment:描述" json:"description"`                           // 描述
	Status      int       `gorm:"column:status;type:tinyint unsigned;not null;default:1;comment:状态：1 启用；0 禁用" json:"status"`             // 状态：1 启用；0 禁用
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`     // 创建时间
	UpdatedAt   time.Time `gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:修改时间" json:"updated_at"`     // 修改时间
}

// TableName 返回管理员权限表名。
func (*AdminPermission) TableName() string {
	return TableNameAdminPermission
}
