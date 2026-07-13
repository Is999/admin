package model

import (
	"math"

	"admin/common/idgen"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

// BeforeCreate 校验最终标签记录使用与 user 一致的固定逻辑桶。
func (row *UserTagRecord) BeforeCreate(_ *gorm.DB) error {
	if row.UID > math.MaxInt64 {
		return errors.Errorf("用户标签 UID[%d] 超出 int64 范围", row.UID)
	}
	return validateUserTagShardNo(int64(row.UID), row.ShardNo)
}

// BeforeCreate 校验运行期 UID 使用与 user 一致的固定逻辑桶。
func (row *UserTagRuntimeUID) BeforeCreate(_ *gorm.DB) error {
	return validateUserTagShardNo(row.UID, row.ShardNo)
}

// BeforeCreate 校验事件 outbox 使用与 user 一致的固定逻辑桶。
func (row *UserTagEventOutbox) BeforeCreate(_ *gorm.DB) error {
	return validateUserTagShardNo(row.UID, row.ShardNo)
}

// validateUserTagShardNo 拒绝非正 UID 和裸 UID 取模形成的错误桶。
func validateUserTagShardNo(uid int64, shardNo int) error {
	if uid <= 0 {
		return errors.Errorf("用户标签 UID 必须为正数 uid=%d", uid)
	}
	expected := idgen.ShardNo(uid)
	if shardNo != expected {
		return errors.Errorf("用户标签 shard_no=%d 与 UID[%d]固定桶 %d 不一致", shardNo, uid, expected)
	}
	return nil
}
