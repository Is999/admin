package model

import (
	"time"
)

// TableNameSysConfig 系统配置表名常量。
const TableNameSysConfig = "sys_config"

// SysConfig 表示系统配置项实体。
type SysConfig struct {
	ID        int       `gorm:"column:id;type:int unsigned;primaryKey;autoIncrement:true;comment:主键" json:"id"`                                                                                                   // 主键
	UUID      string    `gorm:"column:uuid;type:varchar(100);not null;uniqueIndex:uk_uuid,priority:1;comment:配置唯一标识,命名规则(驼峰)：项目名+key" json:"uuid"`                                                                // 配置唯一标识,命名规则(驼峰)：项目名+key
	Title     string    `gorm:"column:title;type:varchar(100);not null;comment:配置标题" json:"title"`                                                                                                                // 配置标题
	Type      int       `gorm:"column:type;type:tinyint unsigned;not null;default:1;comment:展示和校验类型：0 仅做分组标题（配置归类）; 1 Object; 2 Array; 3 String; 4 Integer; 5 Float; 6 Boolean（0 = false，1 = true）;" json:"type"` // 展示和校验类型：0 仅做分组标题（配置归类）; 1 Object; 2 Array; 3 String; 4 Integer; 5 Float; 6 Boolean（0 = false，1 = true）;
	Value     string    `gorm:"column:value;type:json;not null;comment:配置值(JSON 格式，可为string/number/bool/array/object)" json:"value"`                                                                              // 配置值(JSON 格式，可为string/number/bool/array/object)
	Example   string    `gorm:"column:example;type:json;not null;comment:配置示例，帮助说明结构" json:"example"`                                                                                                             // 配置示例，帮助说明结构
	Remark    string    `gorm:"column:remark;type:varchar(255);not null;comment:备注" json:"remark"`                                                                                                                // 备注
	Page      string    `gorm:"column:page;type:varchar(200);not null;comment:配置项所属页面路径，例如 /system/config/base" json:"page"`                                                                                      // 配置项所属页面路径，例如 /system/config/base
	Pid       int       `gorm:"column:pid;type:int unsigned;not null;comment:上级ID" json:"pid"`                                                                                                                    // 上级ID
	Pids      string    `gorm:"column:pids;type:varchar(255);not null;comment:上级ID(族谱)" json:"pids"`                                                                                                              // 上级ID(族谱)
	Version   int       `gorm:"column:version;type:int unsigned;not null;comment:版本号" json:"version"`                                                                                                             // 版本号
	CreatedAt time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`                                                                                // 创建时间
	UpdatedAt time.Time `gorm:"column:updated_at;type:datetime;default:CURRENT_TIMESTAMP;comment:更新时间" json:"updated_at"`                                                                                         // 更新时间
}

// TableName 返回系统配置表名。
func (*SysConfig) TableName() string {
	return TableNameSysConfig
}
