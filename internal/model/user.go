package model

import (
	"fmt"
	"strings"
	"time"

	"admin/common/idgen"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

// 业务用户表名、身份类型和状态枚举。
const (
	// TableNameUser 表示 user 业务用户表名，与后台 admin 管理员表区分。
	TableNameUser = "user"
	// TableNameUserIdentity 表示业务用户登录身份索引表名前缀。
	TableNameUserIdentity = "user_identity"
	// TableNameUserIdentityUsername 表示自定义账号登录身份索引表名。
	TableNameUserIdentityUsername = "user_identity_username"
	// TableNameUserIdentityEmail 表示邮箱登录身份索引表名。
	TableNameUserIdentityEmail = "user_identity_email"
	// TableNameUserIdentityPhone 表示手机号登录身份索引表名。
	TableNameUserIdentityPhone = "user_identity_phone"
	// TableNameUserIdentityOAuth 表示三方登录身份索引表名。
	TableNameUserIdentityOAuth = "user_identity_oauth"

	// UserIdentityTypeUsername 表示自定义账号登录身份。
	UserIdentityTypeUsername = "username"
	// UserIdentityTypeEmail 表示邮箱登录身份。
	UserIdentityTypeEmail = "email"
	// UserIdentityTypePhone 表示手机号登录身份。
	UserIdentityTypePhone = "phone"
	// UserIdentityTypeOAuth 表示三方登录身份。
	UserIdentityTypeOAuth = "oauth"
	// UserIdentityProviderLocal 表示本地账号、邮箱和手机号身份没有三方提供方。
	UserIdentityProviderLocal = ""

	// UserStatusDisabled 表示业务用户禁用状态。
	UserStatusDisabled = 0
	// UserStatusEnabled 表示业务用户启用状态。
	UserStatusEnabled = 1

	// UserRouteShardCountDefault 表示当前默认仍写入单张 user 物理表。
	UserRouteShardCountDefault = 1
	// userRouteShardMod 表示业务用户逻辑分片总数。
	userRouteShardMod = idgen.ShardMod
)

// User 表示 admin 直接管理的业务用户实体。
type User struct {
	ID              int64     `gorm:"column:id;type:bigint;primaryKey;index:idx_user_shard_no_id,priority:2;index:idx_user_status_id,priority:2;comment:雪花 ID" json:"id"`              // 雪花 ID
	ShardNo         int       `gorm:"column:shard_no;type:int;not null;default:0;index:idx_user_shard_no_id,priority:1;comment:ID 哈希分片，CRC32(id字符串)%1024，用于分表和分片游标查询" json:"shard_no"` // ID 哈希分片，来源 idgen.ShardNo(id)
	Username        string    `gorm:"column:username;type:varchar(32);not null;uniqueIndex:uk_user_username;comment:用户名" json:"username"`                                              // 用户名
	Nickname        string    `gorm:"column:nickname;type:varchar(64);not null;default:'';comment:昵称" json:"nickname"`                                                                 // 昵称
	PasswordHash    string    `gorm:"column:password_hash;type:varchar(255);not null;comment:密码哈希" json:"-"`                                                                           // 密码哈希
	Email           string    `gorm:"-" json:"-"`                                                                                                                                      // 邮箱明文，仅用于写入前生成安全字段
	EmailCiphertext string    `gorm:"column:email_ciphertext;type:varchar(512);not null;default:'';comment:邮箱 AES-GCM 密文" json:"-"`                                                    // 邮箱密文
	EmailHash       string    `gorm:"column:email_hash;type:char(64);not null;default:'';index:idx_user_email_hash;comment:邮箱 HMAC 查询哈希" json:"-"`                                     // 邮箱查询哈希
	EmailMasked     string    `gorm:"column:email_masked;type:varchar(128);not null;default:'';comment:邮箱脱敏展示值" json:"emailMasked"`                                                    // 邮箱脱敏展示值
	EmailKeyVersion string    `gorm:"column:email_key_version;type:varchar(32);not null;default:'';comment:邮箱加密密钥版本" json:"-"`                                                         // 邮箱加密密钥版本
	Phone           string    `gorm:"-" json:"-"`                                                                                                                                      // 手机号明文，仅用于写入前生成安全字段
	PhoneCiphertext string    `gorm:"column:phone_ciphertext;type:varchar(512);not null;default:'';comment:手机号 AES-GCM 密文" json:"-"`                                                   // 手机号密文
	PhoneHash       string    `gorm:"column:phone_hash;type:char(64);not null;default:'';index:idx_user_phone_hash;comment:手机号 HMAC 查询哈希" json:"-"`                                    // 手机号查询哈希
	PhoneMasked     string    `gorm:"column:phone_masked;type:varchar(32);not null;default:'';comment:手机号脱敏展示值" json:"phoneMasked"`                                                    // 手机号脱敏展示值
	PhoneKeyVersion string    `gorm:"column:phone_key_version;type:varchar(32);not null;default:'';comment:手机号加密密钥版本" json:"-"`                                                        // 手机号加密密钥版本
	Avatar          string    `gorm:"column:avatar;type:varchar(255);not null;default:'';comment:头像" json:"avatar"`                                                                    // 头像
	Status          int       `gorm:"column:status;type:tinyint;not null;default:1;index:idx_user_status_id,priority:1;comment:状态：1 正常，0 禁用" json:"status"`                            // 状态：1 正常，0 禁用
	LastLoginAt     time.Time `gorm:"column:last_login_at;type:datetime;comment:最后登录时间" json:"last_login_at"`                                                                          // 最后登录时间
	LastLoginIP     string    `gorm:"column:last_login_ip;type:varchar(45);not null;default:'';comment:最后登录 IP" json:"last_login_ip"`                                                  // 最后登录 IP
	CreatedAt       time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`                                               // 创建时间
	UpdatedAt       time.Time `gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:更新时间" json:"updated_at"`                                               // 更新时间
}

// UserIdentity 表示业务用户登录身份索引，负责账号唯一性和物理表定位。
type UserIdentity struct {
	ID                  uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement:true;comment:主键 ID" json:"id"`                                                                                                         // 主键 ID
	IdentityType        string    `gorm:"-" json:"identityType"`                                                                                                                                                                        // 身份类型，由物理表路由决定
	Provider            string    `gorm:"column:provider;type:varchar(32);not null;default:'';comment:三方身份提供方" json:"provider"`                                                                                                         // 三方身份提供方，仅 oauth 表持久化
	IdentityValue       string    `gorm:"column:identity_value;type:varchar(191);not null;comment:归一化身份值" json:"identityValue"`                                                                                                         // 归一化身份值，仅 username/oauth 表持久化
	IdentityHash        string    `gorm:"column:identity_hash;type:char(64);not null;default:'';comment:邮箱或手机号身份 HMAC 哈希" json:"identityHash"`                                                                                          // 邮箱或手机号身份哈希
	UserID              int64     `gorm:"column:user_id;type:bigint;not null;index:idx_user_identity_user_route,priority:3;index:idx_user_identity_shard_user,priority:2;comment:业务用户雪花 ID" json:"userId"`                              // 业务用户雪花 ID
	UserShardNo         int       `gorm:"column:user_shard_no;type:int;not null;index:idx_user_identity_user_route,priority:2;index:idx_user_identity_shard_user,priority:1;comment:业务用户 ID 哈希分片，CRC32(id字符串)%1024" json:"userShardNo"` // 业务用户逻辑分片
	UserRouteShardCount int       `gorm:"column:user_route_shard_count;type:smallint unsigned;not null;default:1;index:idx_user_identity_user_route,priority:1;comment:当前物理用户表数量：1/2/4/.../1024" json:"userRouteShardCount"`            // 当前物理用户表数量
	CreatedAt           time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"createdAt"`                                                                                             // 创建时间
	UpdatedAt           time.Time `gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;comment:更新时间" json:"updatedAt"`                                                                 // 更新时间
}

// TableName 返回业务用户表名。
func (*User) TableName() string {
	return TableNameUser
}

// TableName 返回默认账号登录身份索引表名，真实读写通过身份类型路由。
func (*UserIdentity) TableName() string {
	return TableNameUserIdentityUsername
}

// ValidateUserRouteShardCount 校验物理用户表数量是否支持平滑拆分。
func ValidateUserRouteShardCount(routeShardCount int) error {
	if validUserRouteShardCount(normalizeUserRouteShardCount(routeShardCount)) {
		return nil
	}
	return errors.Errorf("用户物理表数量仅支持 1/2/4/8/16/32/64/128/256/512/1024")
}

// UserPhysicalTableName 返回逻辑分片当前所在的物理用户表名。
func UserPhysicalTableName(shardNo int, routeShardCount int) (string, error) {
	if shardNo < 0 || shardNo >= userRouteShardMod {
		return "", errors.Errorf("用户 shard_no 必须在 0-%d 之间", userRouteShardMod-1)
	}
	routeShardCount = normalizeUserRouteShardCount(routeShardCount)
	if err := ValidateUserRouteShardCount(routeShardCount); err != nil {
		return "", errors.Tag(err)
	}
	if routeShardCount == 1 {
		return TableNameUser, nil
	}
	rangeSize := userRouteShardMod / routeShardCount
	rangeStart := (shardNo / rangeSize) * rangeSize
	return fmt.Sprintf("%s_%04d", TableNameUser, rangeStart), nil
}

// UserIdentityTableName 返回身份类型对应的物理登录身份索引表名。
func UserIdentityTableName(identityType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(identityType)) {
	case UserIdentityTypeUsername:
		return TableNameUserIdentityUsername, nil
	case UserIdentityTypeEmail:
		return TableNameUserIdentityEmail, nil
	case UserIdentityTypePhone:
		return TableNameUserIdentityPhone, nil
	case UserIdentityTypeOAuth:
		return TableNameUserIdentityOAuth, nil
	default:
		return "", errors.Errorf("不支持的用户登录身份类型[%s]", identityType)
	}
}

// UserTableName 返回身份索引记录指向的物理用户表名。
func (i *UserIdentity) UserTableName() (string, error) {
	if i == nil {
		return "", errors.New("用户身份索引为空")
	}
	if err := validateUserIdentityRoute(i); err != nil {
		return "", errors.Tag(err)
	}
	return UserPhysicalTableName(i.UserShardNo, i.UserRouteShardCount)
}

// IdentityTableName 返回身份索引记录当前应写入的物理身份表名。
func (i *UserIdentity) IdentityTableName() (string, error) {
	if i == nil {
		return "", errors.New("用户身份索引为空")
	}
	if err := validateUserIdentityRoute(i); err != nil {
		return "", errors.Tag(err)
	}
	return UserIdentityTableName(i.IdentityType)
}

// NormalizeUserIdentity 归一化用户登录身份，保证唯一索引输入稳定。
func NormalizeUserIdentity(identityType string, provider string, identityValue string) (string, string, string, error) {
	normalizedType, normalizedProvider, err := normalizeUserIdentityTypeProvider(identityType, provider)
	if err != nil {
		return "", "", "", errors.Tag(err)
	}
	value := strings.TrimSpace(identityValue)
	switch normalizedType {
	case UserIdentityTypeUsername, UserIdentityTypeEmail:
		value = strings.ToLower(value)
	}
	if value == "" {
		return "", "", "", errors.New("用户登录身份值不能为空")
	}
	return normalizedType, normalizedProvider, value, nil
}

// UserIdentitySubject 返回风控限流使用的稳定身份主体。
func UserIdentitySubject(identityType string, provider string, identityValue string) string {
	normalizedType, normalizedProvider, normalizedValue, err := NormalizeUserIdentity(identityType, provider, identityValue)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(identityType)) + ":" + strings.TrimSpace(identityValue)
	}
	if normalizedProvider != "" {
		return normalizedType + ":" + normalizedProvider + ":" + normalizedValue
	}
	return normalizedType + ":" + normalizedValue
}

// FindUserByIdentity 根据登录身份查询业务用户；未命中时返回 nil。
func FindUserByIdentity(db *gorm.DB, identityType string, provider string, identityValue string, privacySecret string) (*User, error) {
	identity, err := FindUserIdentity(db, identityType, provider, identityValue, privacySecret)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return FindUserByIdentityRow(db, identity)
}

// FindUserByID 根据 ID 查询业务用户；未命中时返回 nil。
func FindUserByID(db *gorm.DB, id int64) (*User, error) {
	if id <= 0 {
		return nil, nil
	}
	identity, err := FindUserIdentityByUserIDAndType(db, id, UserIdentityTypeUsername, UserIdentityProviderLocal)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if identity == nil {
		return nil, errors.Errorf("用户身份索引缺失 user_id=%d type=%s", id, UserIdentityTypeUsername)
	}
	return FindUserByIdentityRow(db, identity)
}

// CreateUserWithIdentities 创建业务用户并同步写入基础登录身份索引。
func CreateUserWithIdentities(db *gorm.DB, user *User, routeShardCount int, privacySecret string, omitColumns ...string) error {
	if user == nil {
		return errors.New("User.Create 用户为空")
	}
	normalizeUserProfile(user)
	if user.Username == "" {
		return errors.New("User.Create 用户名为空")
	}
	if err := ProtectUserContacts(user, privacySecret); err != nil {
		return errors.Tag(err)
	}
	if err := validateUserShardNo(user); err != nil {
		return errors.Tag(err)
	}
	routeShardCount = normalizeUserRouteShardCount(routeShardCount)
	if err := ValidateUserRouteShardCount(routeShardCount); err != nil {
		return errors.Tag(err)
	}
	tableName, err := UserPhysicalTableName(user.ShardNo, routeShardCount)
	if err != nil {
		return errors.Tag(err)
	}
	identities, err := userProfileIdentities(user, routeShardCount)
	if err != nil {
		return errors.Tag(err)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for index := range identities {
			if err := createUserIdentity(tx, &identities[index]); err != nil {
				return errors.Tag(err)
			}
		}
		query := tx.Table(tableName)
		if len(omitColumns) > 0 {
			query = query.Omit(omitColumns...)
		}
		return errors.Tag(query.Create(user).Error)
	})
}

// UpdateUser 按主键更新业务用户可变字段。
func UpdateUser(db *gorm.DB, id int64, updates map[string]any) error {
	if id <= 0 || len(updates) == 0 {
		return nil
	}
	updates = safeUserUpdates(updates, false)
	if len(updates) == 0 {
		return nil
	}
	tableName, err := userTableNameByID(db, id)
	if err != nil {
		return errors.Tag(err)
	}
	return errors.Tag(cleanUserDB(db).Model(&User{}).Table(tableName).Where("id = ?", id).Updates(updates).Error)
}

// UpdateUserProfileWithIdentities 更新用户资料并同步邮箱、手机号登录身份。
func UpdateUserProfileWithIdentities(db *gorm.DB, id int64, updates map[string]any, privacySecret string) error {
	if id <= 0 || len(updates) == 0 {
		return nil
	}
	var err error
	updates, err = ProtectUserProfileUpdates(updates, privacySecret)
	if err != nil {
		return errors.Tag(err)
	}
	updates = safeUserUpdates(updates, false)
	if len(updates) == 0 {
		return nil
	}
	identity, err := FindUserIdentityByUserIDAndType(db, id, UserIdentityTypeUsername, UserIdentityProviderLocal)
	if err != nil {
		return errors.Tag(err)
	}
	if identity == nil {
		return errors.Errorf("用户身份索引缺失 user_id=%d type=%s", id, UserIdentityTypeUsername)
	}
	tableName, err := identity.UserTableName()
	if err != nil {
		return errors.Tag(err)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := cleanUserDB(tx).Model(&User{}).Table(tableName).Where("id = ?", id).Updates(updates).Error; err != nil {
			return errors.Tag(err)
		}
		if !profileIdentityChanged(updates) {
			return nil
		}
		row, err := findUserByIDInTable(tx, tableName, id)
		if err != nil {
			return errors.Tag(err)
		}
		if row == nil {
			return errors.Errorf("用户资料已更新但主表记录缺失 user_id=%d table=%s", id, tableName)
		}
		return syncUserContactIdentities(tx, row, identity.UserRouteShardCount)
	})
}

// UpdateUserPasswordHash 更新业务用户密码哈希。
func UpdateUserPasswordHash(db *gorm.DB, id int64, passwordHash string, updatedAt time.Time) error {
	if id <= 0 || strings.TrimSpace(passwordHash) == "" {
		return nil
	}
	tableName, err := userTableNameByID(db, id)
	if err != nil {
		return errors.Tag(err)
	}
	return errors.Tag(cleanUserDB(db).Model(&User{}).Table(tableName).Where("id = ?", id).Updates(map[string]any{
		"password_hash": passwordHash,
		"updated_at":    updatedAt,
	}).Error)
}

// FindUserIdentity 根据身份类型、提供方和身份值查询索引；未命中时返回 nil。
func FindUserIdentity(db *gorm.DB, identityType string, provider string, identityValue string, privacySecret string) (*UserIdentity, error) {
	if strings.TrimSpace(identityType) == "" || strings.TrimSpace(identityValue) == "" {
		return nil, nil
	}
	identityType, provider, identityValue, err := NormalizeUserIdentity(identityType, provider, identityValue)
	if err != nil {
		return nil, errors.Tag(err)
	}
	tableName, err := UserIdentityTableName(identityType)
	if err != nil {
		return nil, errors.Tag(err)
	}
	var row UserIdentity
	query, err := userIdentityLookupQuery(cleanUserDB(db).Table(tableName), identityType, provider, identityValue, privacySecret)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if err := query.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "UserIdentity.Find 查询用户身份 type=%s provider=%s 失败", identityType, provider)
	}
	row.IdentityType = identityType
	return &row, nil
}

// FindUserIdentityByUserIDAndType 根据用户 ID 和身份类型在对应身份表查询索引。
func FindUserIdentityByUserIDAndType(db *gorm.DB, userID int64, identityType string, provider string) (*UserIdentity, error) {
	if userID <= 0 {
		return nil, nil
	}
	identityType, provider, err := normalizeUserIdentityTypeProvider(identityType, provider)
	if err != nil {
		return nil, errors.Tag(err)
	}
	tableName, err := UserIdentityTableName(identityType)
	if err != nil {
		return nil, errors.Tag(err)
	}
	var row UserIdentity
	query := cleanUserDB(db).Table(tableName).Where("user_id = ?", userID)
	if identityType == UserIdentityTypeOAuth {
		query = query.Where("provider = ?", provider)
	}
	if err := query.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "UserIdentity.FindByUserID 查询用户身份 user_id=%d type=%s provider=%s 失败", userID, identityType, provider)
	}
	row.IdentityType = identityType
	return &row, nil
}

// FindUserByIdentityRow 根据身份索引定位物理表并读取业务用户。
func FindUserByIdentityRow(db *gorm.DB, identity *UserIdentity) (*User, error) {
	if identity == nil {
		return nil, nil
	}
	tableName, err := identity.UserTableName()
	if err != nil {
		return nil, errors.Tag(err)
	}
	row, err := findUserByIDInTable(db, tableName, identity.UserID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if row == nil {
		return nil, errors.Errorf("用户身份索引存在但主表记录缺失 user_id=%d table=%s", identity.UserID, tableName)
	}
	return row, nil
}

// HasSplitUserIdentities 判断账号身份表中是否已有用户表拆分路由记录。
func HasSplitUserIdentities(db *gorm.DB) (bool, error) {
	var marker int
	err := splitUserIdentityQuery(db).Scan(&marker).Error
	if err != nil {
		return false, errors.Wrap(err, "UserIdentity.HasSplit 查询已拆分身份索引失败")
	}
	return marker == 1, nil
}

// splitUserIdentityQuery 构造已拆分用户路由探测查询，必须保持走 user_route_shard_count 索引前缀并只取一行。
func splitUserIdentityQuery(db *gorm.DB) *gorm.DB {
	return cleanUserDB(db).Table(TableNameUserIdentityUsername).
		Select("1").
		Where("user_route_shard_count > ?", UserRouteShardCountDefault).
		Limit(1)
}

// userIdentityLookupQuery 根据身份表结构构造唯一索引查询。
func userIdentityLookupQuery(db *gorm.DB, identityType string, provider string, identityValue string, privacySecret string) (*gorm.DB, error) {
	switch identityType {
	case UserIdentityTypeUsername:
		return db.Where("identity_value = ?", identityValue), nil
	case UserIdentityTypeEmail, UserIdentityTypePhone:
		identityHash, err := UserContactIdentityHash(identityType, identityValue, privacySecret)
		if err != nil {
			return nil, errors.Tag(err)
		}
		return db.Where("identity_hash = ?", identityHash), nil
	case UserIdentityTypeOAuth:
		return db.Where("provider = ? AND identity_value = ?", provider, identityValue), nil
	default:
		return nil, errors.Errorf("不支持的用户登录身份类型[%s]", identityType)
	}
}

// createUserIdentity 按身份表字段差异写入索引，避免本地身份表出现空 provider 列。
func createUserIdentity(db *gorm.DB, identity *UserIdentity) error {
	if identity == nil {
		return errors.New("用户身份索引为空")
	}
	tableName, err := identity.IdentityTableName()
	if err != nil {
		return errors.Tag(err)
	}
	query := cleanUserDB(db).Table(tableName)
	switch identity.IdentityType {
	case UserIdentityTypeUsername:
		query = query.Select("identity_value", "user_id", "user_shard_no", "user_route_shard_count")
	case UserIdentityTypeEmail, UserIdentityTypePhone:
		query = query.Select("identity_hash", "user_id", "user_shard_no", "user_route_shard_count")
	case UserIdentityTypeOAuth:
		query = query.Select("provider", "identity_value", "user_id", "user_shard_no", "user_route_shard_count")
	default:
		return errors.Errorf("不支持的用户登录身份类型[%s]", identity.IdentityType)
	}
	return errors.Tag(query.Create(identity).Error)
}

// updateUserIdentity 按身份表结构更新可变身份值和用户表路由信息。
func updateUserIdentity(db *gorm.DB, tableName string, id uint64, next *UserIdentity) error {
	if strings.TrimSpace(tableName) == "" || id == 0 || next == nil {
		return nil
	}
	updates := map[string]any{
		"user_shard_no":          next.UserShardNo,
		"user_route_shard_count": next.UserRouteShardCount,
		"updated_at":             time.Now(),
	}
	switch next.IdentityType {
	case UserIdentityTypeEmail, UserIdentityTypePhone:
		updates["identity_hash"] = next.IdentityHash
	case UserIdentityTypeUsername, UserIdentityTypeOAuth:
		updates["identity_value"] = next.IdentityValue
	}
	return errors.Tag(cleanUserDB(db).Table(tableName).Where("id = ?", id).Updates(updates).Error)
}

// findUserByIDInTable 在指定物理用户表中按 ID 查询用户，未命中返回 nil。
func findUserByIDInTable(db *gorm.DB, tableName string, id int64) (*User, error) {
	var row User
	if err := cleanUserDB(db).Table(tableName).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "User.FindByID 查询用户 ID[%d]失败", id)
	}
	return &row, nil
}

// userTableNameByID 返回用户当前所在的物理表。
func userTableNameByID(db *gorm.DB, id int64) (string, error) {
	identity, err := FindUserIdentityByUserIDAndType(db, id, UserIdentityTypeUsername, UserIdentityProviderLocal)
	if err != nil {
		return "", errors.Tag(err)
	}
	if identity == nil {
		return "", errors.Errorf("用户身份索引缺失 user_id=%d type=%s", id, UserIdentityTypeUsername)
	}
	return identity.UserTableName()
}

// normalizeUserProfile 归一化用户资料中的登录身份字段。
func normalizeUserProfile(user *User) {
	if user == nil {
		return
	}
	user.Username = strings.TrimSpace(user.Username)
	user.Email = strings.ToLower(strings.TrimSpace(user.Email))
	user.Phone = strings.TrimSpace(user.Phone)
}

// userProfileIdentities 生成用户资料对应的基础登录身份索引。
func userProfileIdentities(user *User, routeShardCount int) ([]UserIdentity, error) {
	items := make([]UserIdentity, 0, 3)
	usernameIdentity, err := newUserIdentity(user, routeShardCount, UserIdentityTypeUsername, UserIdentityProviderLocal, user.Username, "")
	if err != nil {
		return nil, errors.Tag(err)
	}
	items = append(items, *usernameIdentity)
	if strings.TrimSpace(user.EmailHash) != "" {
		emailIdentity, err := newUserIdentity(user, routeShardCount, UserIdentityTypeEmail, UserIdentityProviderLocal, "", user.EmailHash)
		if err != nil {
			return nil, errors.Tag(err)
		}
		items = append(items, *emailIdentity)
	}
	if strings.TrimSpace(user.PhoneHash) != "" {
		phoneIdentity, err := newUserIdentity(user, routeShardCount, UserIdentityTypePhone, UserIdentityProviderLocal, "", user.PhoneHash)
		if err != nil {
			return nil, errors.Tag(err)
		}
		items = append(items, *phoneIdentity)
	}
	return items, nil
}

// syncUserContactIdentities 同步用户邮箱和手机号身份索引。
func syncUserContactIdentities(db *gorm.DB, user *User, routeShardCount int) error {
	normalizeUserProfile(user)
	if err := syncUserContactIdentity(db, user, routeShardCount, UserIdentityTypeEmail, user.EmailHash); err != nil {
		return errors.Tag(err)
	}
	return errors.Tag(syncUserContactIdentity(db, user, routeShardCount, UserIdentityTypePhone, user.PhoneHash))
}

// syncUserContactIdentity 按资料字段新增、更新或删除单个联系身份。
func syncUserContactIdentity(db *gorm.DB, user *User, routeShardCount int, identityType string, identityHash string) error {
	identityType, provider, err := normalizeUserIdentityTypeProvider(identityType, UserIdentityProviderLocal)
	if err != nil {
		return errors.Tag(err)
	}
	exists, err := FindUserIdentityByUserIDAndType(db, user.ID, identityType, provider)
	if err != nil {
		return errors.Tag(err)
	}
	if strings.TrimSpace(identityHash) == "" {
		if exists == nil {
			return nil
		}
		tableName, err := exists.IdentityTableName()
		if err != nil {
			return errors.Tag(err)
		}
		return errors.Tag(cleanUserDB(db).Table(tableName).Where("id = ?", exists.ID).Delete(&UserIdentity{}).Error)
	}
	next, err := newUserIdentity(user, routeShardCount, identityType, provider, "", identityHash)
	if err != nil {
		return errors.Tag(err)
	}
	nextTableName, err := next.IdentityTableName()
	if err != nil {
		return errors.Tag(err)
	}
	if exists == nil {
		return errors.Tag(createUserIdentity(db, next))
	}
	existsTableName, err := exists.IdentityTableName()
	if err != nil {
		return errors.Tag(err)
	}
	if existsTableName != nextTableName {
		if err := cleanUserDB(db).Table(existsTableName).Where("id = ?", exists.ID).Delete(&UserIdentity{}).Error; err != nil {
			return errors.Tag(err)
		}
		return errors.Tag(createUserIdentity(db, next))
	}
	return errors.Tag(updateUserIdentity(db, existsTableName, exists.ID, next))
}

// newUserIdentity 构造带路由信息的用户身份索引。
func newUserIdentity(user *User, routeShardCount int, identityType string, provider string, identityValue string, identityHash string) (*UserIdentity, error) {
	if user == nil {
		return nil, errors.New("用户为空")
	}
	if err := validateUserShardNo(user); err != nil {
		return nil, errors.Tag(err)
	}
	identityType, provider, err := normalizeUserIdentityTypeProvider(identityType, provider)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if identityType == UserIdentityTypeUsername || identityType == UserIdentityTypeOAuth {
		_, _, identityValue, err = NormalizeUserIdentity(identityType, provider, identityValue)
		if err != nil {
			return nil, errors.Tag(err)
		}
	}
	routeShardCount = normalizeUserRouteShardCount(routeShardCount)
	if err := ValidateUserRouteShardCount(routeShardCount); err != nil {
		return nil, errors.Tag(err)
	}
	identityHash = strings.TrimSpace(identityHash)
	if identityType == UserIdentityTypeEmail || identityType == UserIdentityTypePhone {
		if len(identityHash) != userContactHashHexSize {
			return nil, errors.Errorf("用户%s身份哈希长度必须为%d", identityType, userContactHashHexSize)
		}
		identityValue = ""
	}
	return &UserIdentity{
		IdentityType:        identityType,
		Provider:            provider,
		IdentityValue:       identityValue,
		IdentityHash:        identityHash,
		UserID:              user.ID,
		UserShardNo:         user.ShardNo,
		UserRouteShardCount: routeShardCount,
	}, nil
}

// normalizeUserIdentityTypeProvider 归一化身份类型和三方提供方。
func normalizeUserIdentityTypeProvider(identityType string, provider string) (string, string, error) {
	identityType = strings.ToLower(strings.TrimSpace(identityType))
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch identityType {
	case UserIdentityTypeUsername, UserIdentityTypeEmail, UserIdentityTypePhone:
		return identityType, UserIdentityProviderLocal, nil
	case UserIdentityTypeOAuth:
		if provider == "" {
			return "", "", errors.New("三方登录身份 provider 不能为空")
		}
		return identityType, provider, nil
	default:
		return "", "", errors.Errorf("不支持的用户登录身份类型[%s]", identityType)
	}
}

// profileIdentityChanged 判断本次资料更新是否影响登录身份索引。
func profileIdentityChanged(updates map[string]any) bool {
	for key := range updates {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "email", "phone", "email_hash", "phone_hash":
			return true
		}
	}
	return false
}

// normalizeUserRouteShardCount 规范化物理表数量，空值使用单表默认值。
func normalizeUserRouteShardCount(routeShardCount int) int {
	if routeShardCount <= 0 {
		return UserRouteShardCountDefault
	}
	return routeShardCount
}

// validUserRouteShardCount 判断物理表数量是否为 1024 逻辑分片可平分的 2 的幂。
func validUserRouteShardCount(routeShardCount int) bool {
	return routeShardCount > 0 && routeShardCount <= userRouteShardMod && routeShardCount&(routeShardCount-1) == 0
}

// validateUserIdentityRoute 校验身份索引中的身份类型、用户 ID 与逻辑分片一致。
func validateUserIdentityRoute(identity *UserIdentity) error {
	if identity.UserID <= 0 {
		return errors.New("用户身份索引 user_id 必须大于 0")
	}
	identityType, _, err := normalizeUserIdentityTypeProvider(identity.IdentityType, identity.Provider)
	if err != nil {
		return errors.Tag(err)
	}
	switch identityType {
	case UserIdentityTypeEmail, UserIdentityTypePhone:
		if len(strings.TrimSpace(identity.IdentityHash)) != userContactHashHexSize {
			return errors.Errorf("用户身份索引 identity_hash 长度必须为 %d", userContactHashHexSize)
		}
	case UserIdentityTypeUsername, UserIdentityTypeOAuth:
		if strings.TrimSpace(identity.IdentityValue) == "" {
			return errors.New("用户身份索引 identity_value 不能为空")
		}
	}
	wantShardNo := idgen.ShardNo(identity.UserID)
	if identity.UserShardNo != wantShardNo {
		return errors.Errorf("用户身份索引 user_shard_no=%d 与 user_id=%d 计算值 %d 不一致", identity.UserShardNo, identity.UserID, wantShardNo)
	}
	return nil
}

// validateUserShardNo 校验用户主表记录的 ID 与逻辑分片一致。
func validateUserShardNo(user *User) error {
	if user.ID <= 0 {
		return errors.New("User.Create 用户 ID 必须大于 0")
	}
	wantShardNo := idgen.ShardNo(user.ID)
	if user.ShardNo != wantShardNo {
		return errors.Errorf("User.Create shard_no=%d 与用户 ID[%d]计算值 %d 不一致", user.ShardNo, user.ID, wantShardNo)
	}
	return nil
}

// safeUserUpdates 过滤用户通用更新字段，避免改动主键、分片、账号和密码哈希。
func safeUserUpdates(updates map[string]any, allowPassword bool) map[string]any {
	filtered := make(map[string]any, len(updates))
	for key, value := range updates {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "", "id", "shard_no", "username", "email", "phone", "created_at":
			continue
		case "email_ciphertext", "email_hash", "email_masked", "email_key_version",
			"phone_ciphertext", "phone_hash", "phone_masked", "phone_key_version":
			value = strings.TrimSpace(fmt.Sprint(value))
		case "password_hash":
			if !allowPassword {
				continue
			}
		}
		filtered[key] = value
	}
	return filtered
}

// cleanUserDB 返回不继承上层查询条件的 GORM 会话，避免动态表查询被外层 Model 污染。
func cleanUserDB(db *gorm.DB) *gorm.DB {
	return db.Session(&gorm.Session{NewDB: true})
}
