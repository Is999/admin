package model

import "time"

// TableNameSecretKey 秘钥配置表名常量。
const TableNameSecretKey = "secret_key"

// SecretKey 表示单个接入应用/供应商通道的秘钥主配置。
// 主表只承载版本路由、灰度配置和全局状态；具体材料落在版本表。
type SecretKey struct {
	ID            int       `gorm:"column:id;type:int unsigned;primaryKey;autoIncrement:true;comment:主键" json:"id"`                        // 主键
	UUID          string    `gorm:"column:uuid;type:varchar(64);not null;uniqueIndex:uuid,priority:1;comment:API KEY 唯一标识" json:"uuid"`    // API KEY 唯一标识
	Title         string    `gorm:"column:title;type:varchar(100);not null;comment:标题" json:"title"`                                       // 标题
	StableVersion string    `gorm:"column:stable_version;type:varchar(64);not null;default:'';comment:当前稳定生效版本" json:"stable_version"`     // 当前稳定生效版本
	GrayVersion   string    `gorm:"column:gray_version;type:varchar(64);not null;default:'';comment:当前灰度版本" json:"gray_version"`           // 当前灰度版本
	GrayPercent   int       `gorm:"column:gray_percent;type:tinyint unsigned;not null;default:0;comment:灰度流量百分比0-100" json:"gray_percent"` // 灰度流量百分比 0-100
	GraySalt      string    `gorm:"column:gray_salt;type:varchar(64);not null;default:'';comment:灰度哈希盐值" json:"gray_salt"`                 // 灰度哈希盐值
	Status        int       `gorm:"column:status;type:tinyint;not null;default:1;comment:状态：1启用，0禁用" json:"status"`                        // 状态：1启用，0禁用
	SignStatus    int       `gorm:"column:sign_status;type:tinyint;not null;default:1;comment:签名验签状态：1启用，0停用" json:"sign_status"`          // 签名验签状态：1启用，0停用
	CryptoStatus  int       `gorm:"column:crypto_status;type:tinyint;not null;default:1;comment:加密解密状态：1启用，0停用" json:"crypto_status"`      // 加密解密状态：1启用，0停用
	Remark        string    `gorm:"column:remark;type:varchar(255);not null;comment:备注" json:"remark"`                                     // 备注
	CreatedAt     time.Time `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`    // 创建时间
	UpdatedAt     time.Time `gorm:"column:updated_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:修改时间" json:"updated_at"`    // 修改时间
}

// TableName 返回秘钥配置表名。
func (*SecretKey) TableName() string {
	return TableNameSecretKey
}
