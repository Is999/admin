package model

import (
	"fmt"
	"strings"
	"time"

	"admin/common/idgen"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

// 业务用户表名和状态枚举。
const (
	// TableNameUser 表示 user 业务用户表名，与后台 admin 管理员表区分。
	TableNameUser = "user"
	// TableNameUserAccount 表示业务用户全局账号索引表名。
	TableNameUserAccount = "user_account"

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
	ID           int64     `gorm:"column:id;type:bigint;primaryKey;index:idx_user_shard_no_id,priority:2;index:idx_user_status_id,priority:2;comment:雪花 ID" json:"id"`              // 雪花 ID
	ShardNo      int       `gorm:"column:shard_no;type:int;not null;default:0;index:idx_user_shard_no_id,priority:1;comment:ID 哈希分片，CRC32(id字符串)%1024，用于分表和分片游标查询" json:"shard_no"` // ID 哈希分片，来源 idgen.ShardNo(id)
	Username     string    `gorm:"column:username;type:varchar(32);not null;uniqueIndex:uk_user_username;comment:用户名" json:"username"`                                              // 用户名
	Nickname     string    `gorm:"column:nickname;type:varchar(64);not null;default:'';comment:昵称" json:"nickname"`                                                                 // 昵称
	PasswordHash string    `gorm:"column:password_hash;type:varchar(255);not null;comment:密码哈希" json:"-"`                                                                           // 密码哈希
	Email        string    `gorm:"column:email;type:varchar(128);not null;default:'';index:idx_user_email;comment:邮箱" json:"email"`                                                 // 邮箱
	Phone        string    `gorm:"column:phone;type:varchar(32);not null;default:'';index:idx_user_phone;comment:手机号" json:"phone"`                                                 // 手机号
	Avatar       string    `gorm:"column:avatar;type:varchar(255);not null;default:'';comment:头像" json:"avatar"`                                                                    // 头像
	Status       int       `gorm:"column:status;type:tinyint;not null;default:1;index:idx_user_status_id,priority:1;comment:状态：1 正常，0 禁用" json:"status"`                            // 状态：1 正常，0 禁用
	LastLoginAt  time.Time `gorm:"column:last_login_at;type:timestamp;comment:最后登录时间" json:"last_login_at"`                                                                         // 最后登录时间
	LastLoginIP  string    `gorm:"column:last_login_ip;type:varchar(45);not null;default:'';comment:最后登录 IP" json:"last_login_ip"`                                                  // 最后登录 IP
	CreatedAt    time.Time `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`                                              // 创建时间
	UpdatedAt    time.Time `gorm:"column:updated_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:更新时间" json:"updated_at"`                                              // 更新时间
}

// UserAccount 表示业务用户全局账号索引，负责 username 唯一性和物理表定位。
type UserAccount struct {
	ID              uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement:true;comment:主键 ID" json:"id"`                                                                                                                    // 主键 ID
	Username        string    `gorm:"column:username;type:varchar(32);not null;uniqueIndex:uk_user_account_username;comment:用户名" json:"username"`                                                                                              // 全局唯一用户名
	UserID          int64     `gorm:"column:user_id;type:bigint;not null;uniqueIndex:uk_user_account_user_id;index:idx_user_account_shard_user,priority:2;index:idx_user_account_route_shard_user,priority:3;comment:业务用户雪花 ID" json:"userId"` // 业务用户雪花 ID
	ShardNo         int       `gorm:"column:shard_no;type:int;not null;index:idx_user_account_shard_user,priority:1;index:idx_user_account_route_shard_user,priority:2;comment:ID 哈希分片，CRC32(id字符串)%1024" json:"shardNo"`                      // 逻辑分片号
	RouteShardCount int       `gorm:"column:route_shard_count;type:smallint unsigned;not null;default:1;index:idx_user_account_route_shard_user,priority:1;comment:当前物理用户表数量：1/2/4/.../1024" json:"routeShardCount"`                           // 当前物理表数量
	CreatedAt       time.Time `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"createdAt"`                                                                                                       // 创建时间
	UpdatedAt       time.Time `gorm:"column:updated_at;type:timestamp;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;comment:更新时间" json:"updatedAt"`                                                                           // 更新时间
}

// TableName 返回业务用户表名。
func (*User) TableName() string {
	return TableNameUser
}

// TableName 返回业务用户全局账号索引表名。
func (*UserAccount) TableName() string {
	return TableNameUserAccount
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

// UserTableName 返回账号索引记录指向的物理用户表名。
func (a *UserAccount) UserTableName() (string, error) {
	if a == nil {
		return "", errors.New("用户账号索引为空")
	}
	if err := validateUserAccountRoute(a); err != nil {
		return "", errors.Tag(err)
	}
	return UserPhysicalTableName(a.ShardNo, a.RouteShardCount)
}

// FindUserByID 根据 ID 查询业务用户；未命中时返回 nil。
func FindUserByID(db *gorm.DB, id int64) (*User, error) {
	if id <= 0 {
		return nil, nil
	}
	account, err := FindUserAccountByUserID(db, id)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if account != nil {
		return FindUserByAccount(db, account)
	}
	return findUserByIDInTable(db, TableNameUser, id)
}

// FindUserByUsername 根据用户名查询业务用户；未命中时返回 nil。
func FindUserByUsername(db *gorm.DB, username string) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}
	account, err := FindUserAccountByUsername(db, username)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if account != nil {
		return FindUserByAccount(db, account)
	}
	return findUserByUsernameInTable(db, TableNameUser, username)
}

// CreateUserWithAccount 创建业务用户并同步写入全局账号索引。
func CreateUserWithAccount(db *gorm.DB, user *User, routeShardCount int, omitColumns ...string) error {
	if user == nil {
		return errors.New("User.Create 用户为空")
	}
	user.Username = strings.TrimSpace(user.Username)
	if user.Username == "" {
		return errors.New("User.Create 用户名为空")
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
	account := &UserAccount{
		Username:        user.Username,
		UserID:          user.ID,
		ShardNo:         user.ShardNo,
		RouteShardCount: routeShardCount,
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(account).Error; err != nil {
			return errors.Tag(err)
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

// FindUserAccountByUsername 根据用户名查询全局账号索引；未命中时返回 nil。
func FindUserAccountByUsername(db *gorm.DB, username string) (*UserAccount, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}
	var row UserAccount
	if err := cleanUserDB(db).Table(TableNameUserAccount).Where("username = ?", username).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "UserAccount.FindByUsername 查询用户索引[%s]失败", username)
	}
	return &row, nil
}

// FindUserAccountByUserID 根据用户 ID 查询全局账号索引；未命中时返回 nil。
func FindUserAccountByUserID(db *gorm.DB, userID int64) (*UserAccount, error) {
	if userID <= 0 {
		return nil, nil
	}
	var row UserAccount
	if err := cleanUserDB(db).Table(TableNameUserAccount).Where("user_id = ?", userID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "UserAccount.FindByUserID 查询用户索引ID[%d]失败", userID)
	}
	return &row, nil
}

// FindUserByAccount 根据账号索引定位物理表并读取业务用户。
func FindUserByAccount(db *gorm.DB, account *UserAccount) (*User, error) {
	if account == nil {
		return nil, nil
	}
	tableName, err := account.UserTableName()
	if err != nil {
		return nil, errors.Tag(err)
	}
	row, err := findUserByIDInTable(db, tableName, account.UserID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if row == nil {
		return nil, errors.Errorf("用户账号索引存在但主表记录缺失 user_id=%d table=%s", account.UserID, tableName)
	}
	return row, nil
}

// HasSplitUserAccounts 判断账号索引中是否已有物理拆分路由记录。
func HasSplitUserAccounts(db *gorm.DB) (bool, error) {
	var marker int
	err := splitUserAccountQuery(db).Scan(&marker).Error
	if err != nil {
		return false, errors.Wrap(err, "UserAccount.HasSplit 查询已拆分账号索引失败")
	}
	return marker == 1, nil
}

// splitUserAccountQuery 构造已拆分账号探测查询，必须保持走 route_shard_count 索引前缀并只取一行。
func splitUserAccountQuery(db *gorm.DB) *gorm.DB {
	return cleanUserDB(db).Table(TableNameUserAccount).
		Select("1").
		Where("route_shard_count > ?", UserRouteShardCountDefault).
		Limit(1)
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

// findUserByUsernameInTable 在指定物理用户表中按用户名查询用户，未命中返回 nil。
func findUserByUsernameInTable(db *gorm.DB, tableName string, username string) (*User, error) {
	var row User
	if err := cleanUserDB(db).Table(tableName).Where("username = ?", username).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "User.FindByUsername 查询用户[%s]失败", username)
	}
	return &row, nil
}

// userTableNameByID 返回用户当前所在的物理表；缺少账号索引时回退单表。
func userTableNameByID(db *gorm.DB, id int64) (string, error) {
	account, err := FindUserAccountByUserID(db, id)
	if err != nil {
		return "", errors.Tag(err)
	}
	if account == nil {
		return TableNameUser, nil
	}
	return account.UserTableName()
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

// validateUserAccountRoute 校验账号索引中的用户 ID 与逻辑分片一致。
func validateUserAccountRoute(account *UserAccount) error {
	if account.UserID <= 0 {
		return errors.New("用户账号索引 user_id 必须大于 0")
	}
	wantShardNo := idgen.ShardNo(account.UserID)
	if account.ShardNo != wantShardNo {
		return errors.Errorf("用户账号索引 shard_no=%d 与 user_id=%d 计算值 %d 不一致", account.ShardNo, account.UserID, wantShardNo)
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
		case "", "id", "shard_no", "username", "created_at":
			continue
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
