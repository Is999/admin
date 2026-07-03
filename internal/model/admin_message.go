package model

import (
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

const (
	// TableNameAdminMessage 管理员消息主表表名常量。
	TableNameAdminMessage = "admin_message"
	// TableNameAdminMessageReceiver 管理员消息收件箱表表名常量。
	TableNameAdminMessageReceiver = "admin_message_receiver"
)

const (
	// AdminMessageReadStatusUnread 表示消息收件状态为未读。
	AdminMessageReadStatusUnread = 0
	// AdminMessageReadStatusRead 表示消息收件状态为已读。
	AdminMessageReadStatusRead = 1
)

// AdminMessageLevel 定义消息等级枚举。
type AdminMessageLevel int

const (
	// AdminMessageLevelInfo 表示信息级别消息。
	AdminMessageLevelInfo AdminMessageLevel = 1
	// AdminMessageLevelWarning 表示警告级别消息。
	AdminMessageLevelWarning AdminMessageLevel = 2
	// AdminMessageLevelError 表示错误级别消息。
	AdminMessageLevelError AdminMessageLevel = 3
)

// AdminMessage 管理员消息主表，承载消息正文、类型、等级与跳转信息。
type AdminMessage struct {
	ID                 int64      `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement:true;comment:主键" json:"id"`                                                              // 主键
	Type               string     `gorm:"column:type;type:varchar(64);not null;index:idx_type,priority:1;default:'';comment:消息类型" json:"type"`                                            // 消息类型
	Level              int        `gorm:"column:level;type:tinyint unsigned;not null;default:1;index:idx_level,priority:1;comment:消息等级" json:"level"`                                     // 消息等级
	Title              string     `gorm:"column:title;type:varchar(200);not null;default:'';comment:消息标题" json:"title"`                                                                   // 消息标题
	Content            string     `gorm:"column:content;type:text;comment:消息内容" json:"content"`                                                                                           // 消息内容
	Data               string     `gorm:"column:data;type:text;comment:扩展数据JSON" json:"data"`                                                                                             // 扩展数据 JSON
	Link               string     `gorm:"column:link;type:varchar(500);not null;default:'';comment:跳转链接(路由或外链)" json:"link"`                                                              // 跳转链接
	SenderAdminID      int        `gorm:"column:sender_admin_id;type:int unsigned;not null;default:0;index:idx_sender_admin_id,priority:1;comment:发送人管理员ID" json:"senderAdminId"`         // 发送人管理员 ID
	SenderAdminName    string     `gorm:"column:sender_admin_name;type:varchar(20);not null;default:'';comment:发送人账号快照" json:"senderAdminName"`                                           // 发送人账号快照
	HandledStatus      int        `gorm:"column:handled_status;type:tinyint unsigned;not null;default:0;index:idx_handled_status,priority:1;comment:处理状态(0未处理1已处理)" json:"handledStatus"` // 处理状态
	HandledByAdminID   int        `gorm:"column:handled_by_admin_id;type:int unsigned;not null;default:0;comment:处理人管理员ID" json:"handledByAdminId"`                                       // 处理人管理员ID
	HandledByAdminName string     `gorm:"column:handled_by_admin_name;type:varchar(20);not null;default:'';comment:处理人账号快照" json:"handledByAdminName"`                                    // 处理人账号快照
	HandledAt          *time.Time `gorm:"column:handled_at;type:datetime;default:null;comment:处理时间" json:"handledAt"`                                                                     // 处理时间
	CreatedAt          time.Time  `gorm:"column:created_at;type:datetime;not null;index:idx_created_at,priority:1;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`              // 创建时间
}

// TableName 返回管理员消息主表表名。
func (*AdminMessage) TableName() string {
	return TableNameAdminMessage
}

// AdminMessageReceiver 管理员消息收件箱表，保存每个收件人的已读/删除状态。
type AdminMessageReceiver struct {
	ID              int64      `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement:true;comment:主键" json:"id"`                                                                                     // 主键
	MessageID       int64      `gorm:"column:message_id;type:bigint unsigned;not null;index:idx_message_id,priority:1;comment:消息ID" json:"messageId"`                                                         // 消息 ID
	ReceiverAdminID int        `gorm:"column:receiver_admin_id;type:int unsigned;not null;index:idx_receiver_state,priority:1;index:idx_receiver_deleted,priority:1;comment:接收人管理员ID" json:"receiverAdminId"` // 接收人管理员 ID
	ReadStatus      int        `gorm:"column:read_status;type:tinyint unsigned;not null;default:0;index:idx_receiver_state,priority:2;comment:是否已读(0未读1已读)" json:"readStatus"`                                // 是否已读
	ReadAt          *time.Time `gorm:"column:read_at;type:datetime;default:null;comment:已读时间" json:"readAt"`                                                                                                  // 已读时间
	DeleteStatus    int        `gorm:"column:delete_status;type:tinyint unsigned;not null;default:0;index:idx_receiver_deleted,priority:2;comment:是否删除(0未删1已删)" json:"deleteStatus"`                          // 是否删除
	DeletedAt       *time.Time `gorm:"column:deleted_at;type:datetime;default:null;comment:删除时间" json:"deletedAt"`                                                                                            // 删除时间
	CreatedAt       time.Time  `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"`                                                                     // 创建时间
}

// TableName 返回管理员消息收件箱表名。
func (*AdminMessageReceiver) TableName() string {
	return TableNameAdminMessageReceiver
}

// AdminMessageInboxItem 表示“消息+收件状态”联表后的收件箱列表项。
type AdminMessageInboxItem struct {
	ID                 int64  `json:"id"`                 // 消息ID
	Type               string `json:"type"`               // 消息类型
	Level              int    `json:"level"`              // 消息等级
	Title              string `json:"title"`              // 消息标题
	Content            string `json:"content"`            // 消息内容
	Data               string `json:"data"`               // 扩展数据JSON
	Link               string `json:"link"`               // 跳转链接
	SenderAdminID      int    `json:"senderAdminId"`      // 发送人管理员ID
	SenderAdminName    string `json:"senderAdminName"`    // 发送人账号
	HandledStatus      int    `json:"handledStatus"`      // 处理状态：0未处理 1已处理
	HandledByAdminName string `json:"handledByAdminName"` // 处理人账号
	HandledAt          string `json:"handledAt"`          // 处理时间（格式化字符串）
	IsRead             bool   `json:"isRead"`             // 是否已读
	ReadAt             string `json:"readAt"`             // 已读时间（格式化字符串）
	CreatedAt          string `json:"createdAt"`          // 创建时间（格式化字符串）
}

// ListAdminMessageInbox 分页查询指定管理员的消息收件箱列表。
func ListAdminMessageInbox(
	db *gorm.DB,
	receiverAdminID int,
	page, pageSize int,
	msgType string,
	level *int,
	readStatus *int,
	keyword string,
	startTime *time.Time,
	endTime *time.Time,
) ([]AdminMessageInboxItem, int64, error) {
	page, pageSize, err := validatePage(page, pageSize)
	if err != nil {
		return nil, 0, errors.Tag(err)
	}
	keyword = strings.TrimSpace(keyword)
	msgType = strings.TrimSpace(msgType)

	dbq := db.Table(TableNameAdminMessageReceiver+" AS r").
		Joins("JOIN "+TableNameAdminMessage+" AS m ON m.id = r.message_id").
		Where("r.receiver_admin_id = ?", receiverAdminID).
		Where("r.delete_status = 0")
	if msgType != "" {
		dbq = dbq.Where("m.type = ?", msgType)
	}
	if level != nil {
		dbq = dbq.Where("m.level = ?", *level)
	}
	if readStatus != nil {
		dbq = dbq.Where("r.read_status = ?", *readStatus)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		dbq = dbq.Where("(m.title LIKE ? OR m.content LIKE ?)", like, like)
	}
	if startTime != nil {
		dbq = dbq.Where("m.created_at >= ?", *startTime)
	}
	if endTime != nil {
		dbq = dbq.Where("m.created_at <= ?", *endTime)
	}

	var total int64
	if err := dbq.Count(&total).Error; err != nil {
		return nil, 0, errors.Tag(err)
	}
	var items []AdminMessageInboxItem
	if total == 0 {
		return items, 0, nil
	}

	type row struct {
		ID                 int64      `gorm:"column:id"`                    // 消息 ID
		Type               string     `gorm:"column:type"`                  // 消息类型
		Level              int        `gorm:"column:level"`                 // 消息等级
		Title              string     `gorm:"column:title"`                 // 消息标题
		Content            string     `gorm:"column:content"`               // 消息内容
		Data               string     `gorm:"column:data"`                  // 扩展数据 JSON
		Link               string     `gorm:"column:link"`                  // 跳转链接
		SenderAdminID      int        `gorm:"column:sender_admin_id"`       // 发送人管理员 ID
		SenderAdminName    string     `gorm:"column:sender_admin_name"`     // 发送人账号快照
		HandledStatus      int        `gorm:"column:handled_status"`        // 处理状态
		HandledByAdminName string     `gorm:"column:handled_by_admin_name"` // 处理人账号快照
		HandledAt          *time.Time `gorm:"column:handled_at"`            // 处理时间
		ReadStatus         int        `gorm:"column:read_status"`           // 收件人已读状态
		ReadAt             *time.Time `gorm:"column:read_at"`               // 收件人已读时间
		CreatedAt          time.Time  `gorm:"column:created_at"`            // 消息创建时间
	}
	var rows []row

	err = dbq.Select([]string{
		"m.id AS id",
		"m.type AS type",
		"m.level AS level",
		"m.title AS title",
		"m.content AS content",
		"m.data AS data",
		"m.link AS link",
		"m.sender_admin_id AS sender_admin_id",
		"m.sender_admin_name AS sender_admin_name",
		"m.handled_status AS handled_status",
		"m.handled_by_admin_name AS handled_by_admin_name",
		"m.handled_at AS handled_at",
		"r.read_status AS read_status",
		"r.read_at AS read_at",
		"m.created_at AS created_at",
	}).
		Order("m.created_at DESC").Order("m.id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, errors.Tag(err)
	}

	items = make([]AdminMessageInboxItem, 0, len(rows))
	for _, it := range rows {
		readAt := ""
		if it.ReadAt != nil {
			readAt = it.ReadAt.Format(time.DateTime)
		}
		handledAt := ""
		if it.HandledAt != nil {
			handledAt = it.HandledAt.Format(time.DateTime)
		}
		items = append(items, AdminMessageInboxItem{
			ID:                 it.ID,
			Type:               it.Type,
			Level:              it.Level,
			Title:              it.Title,
			Content:            it.Content,
			Data:               it.Data,
			Link:               it.Link,
			SenderAdminID:      it.SenderAdminID,
			SenderAdminName:    it.SenderAdminName,
			HandledStatus:      it.HandledStatus,
			HandledByAdminName: it.HandledByAdminName,
			HandledAt:          handledAt,
			IsRead:             it.ReadStatus == AdminMessageReadStatusRead,
			ReadAt:             readAt,
			CreatedAt:          it.CreatedAt.Format(time.DateTime),
		})
	}
	return items, total, nil
}

// AdminMessageSentItem 表示“已发送”消息列表项，包含收件人已读统计与处理状态。
type AdminMessageSentItem struct {
	ID                  int64  `json:"id"`                  // 消息ID
	Type                string `json:"type"`                // 消息类型
	Level               int    `json:"level"`               // 消息等级
	Title               string `json:"title"`               // 消息标题
	Content             string `json:"content"`             // 消息内容
	Data                string `json:"data"`                // 扩展数据JSON
	Link                string `json:"link"`                // 跳转链接
	SenderAdminID       int    `json:"senderAdminId"`       // 发送人管理员ID
	SenderAdminName     string `json:"senderAdminName"`     // 发送人账号
	ReceiverTotal       int64  `json:"receiverTotal"`       // 收件人总数
	ReceiverReadTotal   int64  `json:"receiverReadTotal"`   // 已读收件人数
	ReceiverUnreadTotal int64  `json:"receiverUnreadTotal"` // 未读收件人数
	HandledStatus       int    `json:"handledStatus"`       // 处理状态：0未处理 1已处理
	HandledByAdminName  string `json:"handledByAdminName"`  // 处理人账号
	HandledAt           string `json:"handledAt"`           // 处理时间（格式化字符串）
	CreatedAt           string `json:"createdAt"`           // 创建时间（格式化字符串）
}

// ListAdminMessageSent 分页查询指定管理员发送的消息列表，并附带收件人已读统计。
func ListAdminMessageSent(
	db *gorm.DB,
	senderAdminID int,
	page, pageSize int,
	msgType string,
	level *int,
	keyword string,
	startTime *time.Time,
	endTime *time.Time,
) ([]AdminMessageSentItem, int64, error) {
	page, pageSize, err := validatePage(page, pageSize)
	if err != nil {
		return nil, 0, errors.Tag(err)
	}
	keyword = strings.TrimSpace(keyword)
	msgType = strings.TrimSpace(msgType)

	dbq := db.Table(TableNameAdminMessage+" AS m").
		Where("m.sender_admin_id = ?", senderAdminID)
	if msgType != "" {
		dbq = dbq.Where("m.type = ?", msgType)
	}
	if level != nil {
		dbq = dbq.Where("m.level = ?", *level)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		dbq = dbq.Where("(m.title LIKE ? OR m.content LIKE ?)", like, like)
	}
	if startTime != nil {
		dbq = dbq.Where("m.created_at >= ?", *startTime)
	}
	if endTime != nil {
		dbq = dbq.Where("m.created_at <= ?", *endTime)
	}

	var total int64
	if err := dbq.Count(&total).Error; err != nil {
		return nil, 0, errors.Tag(err)
	}
	if total == 0 {
		return []AdminMessageSentItem{}, 0, nil
	}

	type row struct {
		ID                 int64      `gorm:"column:id"`                    // 消息 ID
		Type               string     `gorm:"column:type"`                  // 消息类型
		Level              int        `gorm:"column:level"`                 // 消息等级
		Title              string     `gorm:"column:title"`                 // 消息标题
		Content            string     `gorm:"column:content"`               // 消息内容
		Data               string     `gorm:"column:data"`                  // 扩展数据 JSON
		Link               string     `gorm:"column:link"`                  // 跳转链接
		SenderAdminID      int        `gorm:"column:sender_admin_id"`       // 发送人管理员 ID
		SenderAdminName    string     `gorm:"column:sender_admin_name"`     // 发送人账号快照
		ReceiverTotal      int64      `gorm:"column:receiver_total"`        // 收件人总数
		ReceiverReadTotal  int64      `gorm:"column:receiver_read_total"`   // 已读收件人数
		HandledStatus      int        `gorm:"column:handled_status"`        // 处理状态
		HandledByAdminName string     `gorm:"column:handled_by_admin_name"` // 处理人账号快照
		HandledAt          *time.Time `gorm:"column:handled_at"`            // 处理时间
		CreatedAt          time.Time  `gorm:"column:created_at"`            // 消息创建时间
	}
	var rows []row

	err = dbq.Select([]string{
		"m.id AS id",
		"m.type AS type",
		"m.level AS level",
		"m.title AS title",
		"m.content AS content",
		"m.data AS data",
		"m.link AS link",
		"m.sender_admin_id AS sender_admin_id",
		"m.sender_admin_name AS sender_admin_name",
		"(SELECT COUNT(1) FROM " + TableNameAdminMessageReceiver + " AS r WHERE r.message_id = m.id) AS receiver_total",
		"(SELECT COUNT(1) FROM " + TableNameAdminMessageReceiver + " AS r WHERE r.message_id = m.id AND r.read_status = 1) AS receiver_read_total",
		"m.handled_status AS handled_status",
		"m.handled_by_admin_name AS handled_by_admin_name",
		"m.handled_at AS handled_at",
		"m.created_at AS created_at",
	}).
		Order("m.created_at DESC").Order("m.id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, errors.Tag(err)
	}

	items := make([]AdminMessageSentItem, 0, len(rows))
	for _, it := range rows {
		handledAt := ""
		if it.HandledAt != nil {
			handledAt = it.HandledAt.Format(time.DateTime)
		}
		items = append(items, AdminMessageSentItem{
			ID:                  it.ID,
			Type:                it.Type,
			Level:               it.Level,
			Title:               it.Title,
			Content:             it.Content,
			Data:                it.Data,
			Link:                it.Link,
			SenderAdminID:       it.SenderAdminID,
			SenderAdminName:     it.SenderAdminName,
			ReceiverTotal:       it.ReceiverTotal,
			ReceiverReadTotal:   it.ReceiverReadTotal,
			ReceiverUnreadTotal: it.ReceiverTotal - it.ReceiverReadTotal,
			HandledStatus:       it.HandledStatus,
			HandledByAdminName:  it.HandledByAdminName,
			HandledAt:           handledAt,
			CreatedAt:           it.CreatedAt.Format(time.DateTime),
		})
	}
	return items, total, nil
}

// AdminMessageReceiverItem 表示发送人查看的收件人已读明细项。
type AdminMessageReceiverItem struct {
	ReceiverAdminID   int    `json:"receiverAdminId"`   // 收件人管理员ID
	ReceiverAdminName string `json:"receiverAdminName"` // 收件人账号
	ReceiverRealName  string `json:"receiverRealName"`  // 收件人姓名
	ReadStatus        int    `json:"readStatus"`        // 是否已读：0未读 1已读
	ReadAt            string `json:"readAt"`            // 已读时间
	DeleteStatus      int    `json:"deleteStatus"`      // 是否删除：0未删 1已删
	DeletedAt         string `json:"deletedAt"`         // 删除时间
}

// ListAdminMessageReceivers 查询指定消息的收件人已读明细（仅发送人可查看）。
func ListAdminMessageReceivers(db *gorm.DB, messageID int64) ([]AdminMessageReceiverItem, error) {
	type row struct {
		ReceiverAdminID   int        `gorm:"column:receiver_admin_id"` // 收件人管理员 ID
		ReceiverAdminName string     `gorm:"column:name"`              // 收件人账号
		ReceiverRealName  string     `gorm:"column:real_name"`         // 收件人姓名
		ReadStatus        int        `gorm:"column:read_status"`       // 已读状态
		ReadAt            *time.Time `gorm:"column:read_at"`           // 已读时间
		DeleteStatus      int        `gorm:"column:delete_status"`     // 删除状态
		DeletedAt         *time.Time `gorm:"column:deleted_at"`        // 删除时间
	}
	var rows []row
	if err := db.Table(TableNameAdminMessageReceiver+" AS r").
		Joins("JOIN "+TableNameAdmin+" AS a ON a.id = r.receiver_admin_id").
		Where("r.message_id = ?", messageID).
		Order("r.read_status ASC").Order("r.id ASC").
		Select([]string{
			"r.receiver_admin_id AS receiver_admin_id",
			"a.name AS name",
			"a.real_name AS real_name",
			"r.read_status AS read_status",
			"r.read_at AS read_at",
			"r.delete_status AS delete_status",
			"r.deleted_at AS deleted_at",
		}).
		Scan(&rows).Error; err != nil {
		return nil, errors.Tag(err)
	}
	items := make([]AdminMessageReceiverItem, 0, len(rows))
	for _, it := range rows {
		readAt := ""
		if it.ReadAt != nil {
			readAt = it.ReadAt.Format(time.DateTime)
		}
		deletedAt := ""
		if it.DeletedAt != nil {
			deletedAt = it.DeletedAt.Format(time.DateTime)
		}
		items = append(items, AdminMessageReceiverItem{
			ReceiverAdminID:   it.ReceiverAdminID,
			ReceiverAdminName: it.ReceiverAdminName,
			ReceiverRealName:  it.ReceiverRealName,
			ReadStatus:        it.ReadStatus,
			ReadAt:            readAt,
			DeleteStatus:      it.DeleteStatus,
			DeletedAt:         deletedAt,
		})
	}
	return items, nil
}

// MarkAdminMessageHandled 将指定消息标记为已处理（并发下仅允许首个处理者成功）。
func MarkAdminMessageHandled(db *gorm.DB, messageID int64, handledByAdminID int, handledByAdminName string) (bool, error) {
	res := db.Model(&AdminMessage{}).
		Where("id = ?", messageID).
		Where("handled_status = 0").
		Updates(map[string]any{
			"handled_status":        1,
			"handled_by_admin_id":   handledByAdminID,
			"handled_by_admin_name": handledByAdminName,
			"handled_at":            new(time.Now()),
		})
	if res.Error != nil {
		return false, errors.Tag(res.Error)
	}
	return res.RowsAffected > 0, nil
}

// CountAdminMessageUnread 统计指定管理员未读消息数量。
func CountAdminMessageUnread(db *gorm.DB, receiverAdminID int) (int64, error) {
	var total int64
	if err := db.Model(&AdminMessageReceiver{}).
		Where("receiver_admin_id = ?", receiverAdminID).
		Where("delete_status = 0").
		Where("read_status = ?", AdminMessageReadStatusUnread).
		Count(&total).Error; err != nil {
		return 0, errors.Tag(err)
	}
	return total, nil
}

// ListAdminMessageNotifications 查询指定管理员的通知列表（用于顶部铃铛）。
func ListAdminMessageNotifications(db *gorm.DB, receiverAdminID int, limit int) ([]AdminMessageInboxItem, error) {
	if limit <= 0 {
		limit = 10
	}
	readStatus := AdminMessageReadStatusUnread
	items, _, err := ListAdminMessageInbox(
		db,
		receiverAdminID,
		1,
		limit,
		"",
		nil,
		&readStatus,
		"",
		nil,
		nil,
	)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return items, nil
}

// MarkAdminMessagesRead 把指定管理员的指定消息标记为已读。
func MarkAdminMessagesRead(db *gorm.DB, receiverAdminID int, messageIDs []int64) (int64, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}
	res := db.Model(&AdminMessageReceiver{}).
		Where("receiver_admin_id = ?", receiverAdminID).
		Where("delete_status = 0").
		Where("read_status = ?", AdminMessageReadStatusUnread).
		Where("message_id IN ?", messageIDs).
		Updates(map[string]any{
			"read_status": AdminMessageReadStatusRead,
			"read_at":     new(time.Now()),
		})
	if res.Error != nil {
		return 0, errors.Tag(res.Error)
	}
	return res.RowsAffected, nil
}

// MarkAdminMessagesReadAll 把指定管理员的全部未读消息标记为已读。
func MarkAdminMessagesReadAll(db *gorm.DB, receiverAdminID int) (int64, error) {
	res := db.Model(&AdminMessageReceiver{}).
		Where("receiver_admin_id = ?", receiverAdminID).
		Where("delete_status = 0").
		Where("read_status = ?", AdminMessageReadStatusUnread).
		Updates(map[string]any{
			"read_status": AdminMessageReadStatusRead,
			"read_at":     new(time.Now()),
		})
	if res.Error != nil {
		return 0, errors.Tag(res.Error)
	}
	return res.RowsAffected, nil
}

// DeleteAdminMessages 把指定管理员的指定消息标记为删除（软删除）。
func DeleteAdminMessages(db *gorm.DB, receiverAdminID int, messageIDs []int64) (int64, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}
	res := db.Model(&AdminMessageReceiver{}).
		Where("receiver_admin_id = ?", receiverAdminID).
		Where("delete_status = 0").
		Where("message_id IN ?", messageIDs).
		Updates(map[string]any{
			"delete_status": 1,
			"deleted_at":    new(time.Now()),
		})
	if res.Error != nil {
		return 0, errors.Tag(res.Error)
	}
	return res.RowsAffected, nil
}

// DeleteAdminMessagesAllRead 把指定管理员的全部“已读消息”标记为删除（用于清理收件箱）。
func DeleteAdminMessagesAllRead(db *gorm.DB, receiverAdminID int) (int64, error) {
	res := db.Model(&AdminMessageReceiver{}).
		Where("receiver_admin_id = ?", receiverAdminID).
		Where("delete_status = 0").
		Where("read_status = ?", AdminMessageReadStatusRead).
		Updates(map[string]any{
			"delete_status": 1,
			"deleted_at":    new(time.Now()),
		})
	if res.Error != nil {
		return 0, errors.Tag(res.Error)
	}
	return res.RowsAffected, nil
}

// CreateAdminMessageWithReceivers 创建一条消息，并批量写入收件箱关系。
func CreateAdminMessageWithReceivers(tx *gorm.DB, msg *AdminMessage, receiverAdminIDs []int) error {
	if tx == nil {
		return errors.Tag(gorm.ErrInvalidDB)
	}
	if msg == nil {
		return errors.New("消息对象为空")
	}
	if len(receiverAdminIDs) == 0 {
		return errors.New("收件人为空")
	}
	if err := tx.Create(msg).Error; err != nil {
		return errors.Tag(err)
	}

	now := time.Now()
	relations := make([]AdminMessageReceiver, 0, len(receiverAdminIDs))
	for _, receiverID := range receiverAdminIDs {
		relations = append(relations, AdminMessageReceiver{
			MessageID:       msg.ID,
			ReceiverAdminID: receiverID,
			ReadStatus:      0,
			DeleteStatus:    0,
			CreatedAt:       now,
		})
	}
	return errors.Tag(tx.CreateInBatches(relations, 200).Error)
}
