package model

import "time"

// TableNameAdminDocPermission 管理员文档权限表名常量。
const TableNameAdminDocPermission = "admin_doc_permission"

// AdminDocPermission 表示后台可授权的单篇文档。
type AdminDocPermission struct {
	ID          int       `gorm:"column:id;type:int unsigned;primaryKey;autoIncrement:true;comment:主键" json:"id"`                                              // 主键
	Site        string    `gorm:"column:site;type:varchar(16);not null;uniqueIndex:uk_site_path,priority:1;comment:文档站：admin或api" json:"site"`                 // 文档站：admin 或 api
	Path        string    `gorm:"column:path;type:varchar(500) COLLATE utf8mb4_bin;not null;uniqueIndex:uk_site_path,priority:2;comment:文档站内相对路径" json:"path"` // 文档站内相对路径
	Title       string    `gorm:"column:title;type:varchar(100);not null;default:'';comment:文档标题" json:"title"`                                                // 文档标题
	Description string    `gorm:"column:description;type:varchar(255);not null;default:'';comment:描述" json:"description"`                                      // 描述
	Status      int       `gorm:"column:status;type:tinyint unsigned;not null;default:1;comment:状态：1启用；0禁用" json:"status"`                                     // 状态：1 启用；0 禁用
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`                           // 创建时间
	UpdatedAt   time.Time `gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:修改时间" json:"updated_at"`                           // 修改时间
}

// TableName 返回管理员文档权限表名。
func (*AdminDocPermission) TableName() string {
	return TableNameAdminDocPermission
}
