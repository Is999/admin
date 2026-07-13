package model

import "time"

// TableNameSecurityCacheSyncTask 表示安全缓存失效补偿任务表名。
const TableNameSecurityCacheSyncTask = "security_cache_sync_task"

// SecurityCacheSyncTask 保存一次可幂等重试的安全缓存失效计划。
type SecurityCacheSyncTask struct {
	ID          uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement:true;index:idx_app_next_id,priority:3;comment:主键ID" json:"id"`                    // 主键 ID
	AppID       string    `gorm:"column:app_id;type:varchar(64);not null;uniqueIndex:uk_app_digest,priority:1;index:idx_app_next_id,priority:1;comment:应用ID" json:"appId"` // 应用命名空间
	Digest      string    `gorm:"column:digest;type:char(64);not null;uniqueIndex:uk_app_digest,priority:2;comment:任务摘要" json:"digest"`                                    // 失效计划 SHA256
	PayloadJSON string    `gorm:"column:payload_json;type:json;not null;comment:失效计划JSON" json:"payloadJson"`                                                              // 精确缓存键与管理员范围
	Revision    uint64    `gorm:"column:revision;type:bigint unsigned;not null;default:1;comment:任务修订号" json:"revision"`                                                   // 每次同摘要任务重新写入时递增
	Attempts    int       `gorm:"column:attempts;type:int unsigned;not null;default:0;comment:已重试次数" json:"attempts"`                                                      // 已重试次数
	NextRetryAt time.Time `gorm:"column:next_retry_at;type:datetime(3);not null;index:idx_app_next_id,priority:2;comment:下次重试时间" json:"nextRetryAt"`                       // 下次重试时间
	LastError   string    `gorm:"column:last_error;type:varchar(1000);not null;default:'';comment:最近错误" json:"lastError"`                                                  // 最近一次失败摘要
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3);comment:创建时间" json:"createdAt"`                                  // 创建时间
	UpdatedAt   time.Time `gorm:"column:updated_at;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3);comment:更新时间" json:"updatedAt"`   // 更新时间
}

// TableName 返回安全缓存失效补偿任务表名。
func (*SecurityCacheSyncTask) TableName() string {
	return TableNameSecurityCacheSyncTask
}
