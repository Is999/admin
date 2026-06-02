package model

import (
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

// 前台用户表名和状态枚举。
const (
	// TableNameUser 表示前台用户业务表名，与后台 admin 管理员表区分。
	TableNameUser = "user"

	// UserStatusDisabled 表示前台用户禁用状态。
	UserStatusDisabled = 0
	// UserStatusEnabled 表示前台用户启用状态。
	UserStatusEnabled = 1
)

// User 表示 admin 直接管理的前台用户实体。
type User struct {
	ID           int64     `gorm:"column:id;type:bigint;primaryKey;comment:雪花ID" json:"id"`                                                        // 雪花 ID
	ShardNo      int       `gorm:"column:shard_no;type:int;not null;default:0;index:idx_user_shard_no_id,priority:1;comment:取模分片" json:"shard_no"` // 取模分片
	Username     string    `gorm:"column:username;type:varchar(32);not null;uniqueIndex:uk_user_username;comment:用户名" json:"username"`             // 用户名
	Nickname     string    `gorm:"column:nickname;type:varchar(64);not null;default:'';comment:昵称" json:"nickname"`                                // 昵称
	PasswordHash string    `gorm:"column:password_hash;type:varchar(255);not null;comment:密码哈希" json:"-"`                                          // 密码哈希
	Email        string    `gorm:"column:email;type:varchar(128);not null;default:'';index:idx_user_email;comment:邮箱" json:"email"`                // 邮箱
	Phone        string    `gorm:"column:phone;type:varchar(32);not null;default:'';index:idx_user_phone;comment:手机号" json:"phone"`                // 手机号
	Avatar       string    `gorm:"column:avatar;type:varchar(255);not null;default:'';comment:头像" json:"avatar"`                                   // 头像
	Status       int       `gorm:"column:status;type:tinyint;not null;default:1;index:idx_user_status;comment:状态：1 正常，0 禁用" json:"status"`         // 状态：1 正常，0 禁用
	LastLoginAt  time.Time `gorm:"column:last_login_at;type:timestamp;comment:最后登录时间" json:"last_login_at"`                                        // 最后登录时间
	LastLoginIP  string    `gorm:"column:last_login_ip;type:varchar(45);not null;default:'';comment:最后登录 IP" json:"last_login_ip"`                 // 最后登录 IP
	CreatedAt    time.Time `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`             // 创建时间
	UpdatedAt    time.Time `gorm:"column:updated_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:更新时间" json:"updated_at"`             // 更新时间
}

// TableName 返回前台用户表名。
func (*User) TableName() string {
	return TableNameUser
}

// FindUserByID 根据 ID 查询前台用户；未命中时返回 nil。
func FindUserByID(db *gorm.DB, id int64) (*User, error) {
	if id <= 0 {
		return nil, nil
	}
	var row User
	if err := db.Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "User.FindByID 查询用户ID[%d]失败", id)
	}
	return &row, nil
}

// FindUserByUsername 根据用户名查询前台用户；未命中时返回 nil。
func FindUserByUsername(db *gorm.DB, username string) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}
	var row User
	if err := db.Where("username = ?", username).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "User.FindByUsername 查询用户[%s]失败", username)
	}
	return &row, nil
}
