package model

import "time"

// TableNameSecretKeyVersion 表示秘钥版本表名常量。
const TableNameSecretKeyVersion = "secret_key_version"

// SecretKeyVersion 表示单个接入应用的具体秘钥版本配置。
type SecretKeyVersion struct {
	ID                     int       `gorm:"column:id;type:int unsigned;primaryKey;autoIncrement:true;comment:主键" json:"id"`                                                                // 主键
	SecretKeyID            int       `gorm:"column:secret_key_id;type:int unsigned;not null;index:idx_secret_key_id;comment:秘钥主表ID" json:"secret_key_id"`                                   // 秘钥主表 ID
	UUID                   string    `gorm:"column:uuid;type:varchar(64);not null;uniqueIndex:uk_secret_key_version,priority:1;index:idx_secret_key_uuid;comment:API KEY 唯一标识" json:"uuid"` // API KEY 唯一标识
	KeyVersion             string    `gorm:"column:key_version;type:varchar(64);not null;uniqueIndex:uk_secret_key_version,priority:2;comment:秘钥版本号" json:"key_version"`                    // 秘钥版本号
	AESKeyRef              string    `gorm:"column:aes_key_ref;type:varchar(500);not null;comment:AES KEY 文件绝对路径" json:"aes_key_ref"`                                                       // AES KEY 文件绝对路径
	AESIVRef               string    `gorm:"column:aes_iv_ref;type:varchar(500);not null;comment:AES IV 文件绝对路径" json:"aes_iv_ref"`                                                          // AES IV 文件绝对路径
	RSAPublicKeyUserRef    string    `gorm:"column:rsa_public_key_user_ref;type:varchar(500);not null;comment:用户 RSA 公钥文件绝对路径" json:"rsa_public_key_user_ref"`                              // 用户 RSA 公钥文件绝对路径
	RSAPublicKeyServerRef  string    `gorm:"column:rsa_public_key_server_ref;type:varchar(500);not null;comment:服务端 RSA 公钥路径，可为空并由私钥派生" json:"rsa_public_key_server_ref"`                   // 服务端 RSA 公钥路径，可为空并由私钥派生
	RSAPrivateKeyServerRef string    `gorm:"column:rsa_private_key_server_ref;type:varchar(500);not null;comment:服务端 RSA 私钥文件绝对路径" json:"rsa_private_key_server_ref"`                       // 服务端 RSA 私钥文件绝对路径
	Status                 int       `gorm:"column:status;type:tinyint;not null;default:1;comment:版本状态：1启用，0停用" json:"status"`                                                              // 版本状态：1启用，0停用
	Remark                 string    `gorm:"column:remark;type:varchar(255);not null;comment:版本备注" json:"remark"`                                                                           // 版本备注
	CreatedAt              time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`                                             // 创建时间
	UpdatedAt              time.Time `gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:修改时间" json:"updated_at"`                                             // 修改时间
}

// TableName 返回秘钥版本表名。
func (*SecretKeyVersion) TableName() string {
	return TableNameSecretKeyVersion
}
