//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"
)

// AdminMessageType 表示消息类型编码，便于前后端对齐筛选与展示。
type AdminMessageType string

const (
	// AdminMessageTypeAdminLogin 表示“管理员登录”消息。
	AdminMessageTypeAdminLogin AdminMessageType = "admin_login"
	// AdminMessageTypeSystemNotice 表示系统通知消息。
	AdminMessageTypeSystemNotice AdminMessageType = "system_notice"
	// AdminMessageTypeSecurityAlert 表示安全告警消息。
	AdminMessageTypeSecurityAlert AdminMessageType = "security_alert"
	// AdminMessageTypeTaskResult 表示任务结果消息。
	AdminMessageTypeTaskResult AdminMessageType = "task_result"
	// AdminMessageTypeTaskException 表示任务异常消息。
	AdminMessageTypeTaskException AdminMessageType = "task_exception"
	// AdminMessageTypeApprovalNotice 表示审批提醒消息。
	AdminMessageTypeApprovalNotice AdminMessageType = "approval_notice"
	// AdminMessageTypeLeaveMessage 表示“他人留言”消息。
	AdminMessageTypeLeaveMessage AdminMessageType = "leave_message"
	// AdminMessageTypeWorkHandover 表示“工作交接”消息。
	AdminMessageTypeWorkHandover AdminMessageType = "work_handover"
)

// AdminMessageLevel 表示消息等级，1=info，2=warning，3=error。
type AdminMessageLevel int

const (
	// AdminMessageLevelInfo 表示信息级别消息。
	AdminMessageLevelInfo AdminMessageLevel = 1
	// AdminMessageLevelWarning 表示警告级别消息。
	AdminMessageLevelWarning AdminMessageLevel = 2
	// AdminMessageLevelError 表示错误级别消息。
	AdminMessageLevelError AdminMessageLevel = 3
)

// AdminMessageQueryReq 表示管理员消息收件箱分页查询请求参数。
type AdminMessageQueryReq struct {
	Type       string `json:"type,optional" form:"type,optional"`             // 消息类型筛选
	Level      *int   `json:"level,optional" form:"level,optional"`           // 消息等级筛选（可选）
	ReadStatus *int   `json:"readStatus,optional" form:"readStatus,optional"` // 已读状态筛选：0未读 1已读
	Keyword    string `json:"keyword,optional" form:"keyword,optional"`       // 关键字（标题/内容模糊匹配）
	StartTime  string `json:"startTime,optional" form:"startTime,optional"`   // 起始时间（格式：YYYY-MM-DD HH:MM:SS）
	EndTime    string `json:"endTime,optional" form:"endTime,optional"`       // 结束时间（格式：YYYY-MM-DD HH:MM:SS）
	GetPageReq        // 分页参数
}

// Validate 校验并归一化查询参数。
func (r *AdminMessageQueryReq) Validate() error {
	r.Type = strings.TrimSpace(r.Type)
	r.Keyword = strings.TrimSpace(r.Keyword)
	r.StartTime = strings.TrimSpace(r.StartTime)
	r.EndTime = strings.TrimSpace(r.EndTime)
	if _, _, err := r.TimeRange(); err != nil {
		return errors.Tag(err)
	}
	return r.GetPageReq.Validate()
}

// TimeRange 解析消息查询时间范围。
func (r *AdminMessageQueryReq) TimeRange() (*time.Time, *time.Time, error) {
	startTime, err := parseOptionalDateTimeForMessage(r.StartTime)
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	endTime, err := parseOptionalDateTimeForMessage(r.EndTime)
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	if startTime != nil && endTime != nil && startTime.After(*endTime) {
		return nil, nil, errors.Errorf("结束时间不能早于开始时间")
	}
	return startTime, endTime, nil
}

// parseOptionalDateTimeForMessage 解析可选的日期时间字符串（为空返回 nil）。
func parseOptionalDateTimeForMessage(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsedAt, err := time.ParseInLocation(time.DateTime, value, time.Local)
	if err != nil {
		return nil, errors.Errorf("时间格式错误，必须为 YYYY-MM-DD HH:MM:SS")
	}
	return &parsedAt, nil
}

// AdminMessageItem 表示前端展示的消息列表项。
type AdminMessageItem struct {
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
	HandledAt          string `json:"handledAt"`          // 处理时间
	IsRead             bool   `json:"isRead"`             // 是否已读
	ReadAt             string `json:"readAt"`             // 已读时间
	CreatedAt          string `json:"createdAt"`          // 创建时间
}

// AdminMessageSentQueryReq 表示管理员已发送消息分页查询请求参数。
type AdminMessageSentQueryReq struct {
	Type      string `json:"type,optional" form:"type,optional"`           // 消息类型筛选
	Level     *int   `json:"level,optional" form:"level,optional"`         // 消息等级筛选（可选）
	Keyword   string `json:"keyword,optional" form:"keyword,optional"`     // 关键字（标题/内容模糊匹配）
	StartTime string `json:"startTime,optional" form:"startTime,optional"` // 起始时间（格式：YYYY-MM-DD HH:MM:SS）
	EndTime   string `json:"endTime,optional" form:"endTime,optional"`     // 结束时间（格式：YYYY-MM-DD HH:MM:SS）
	GetPageReq
}

// Validate 校验并归一化已发送查询参数。
func (r *AdminMessageSentQueryReq) Validate() error {
	r.Type = strings.TrimSpace(r.Type)
	r.Keyword = strings.TrimSpace(r.Keyword)
	r.StartTime = strings.TrimSpace(r.StartTime)
	r.EndTime = strings.TrimSpace(r.EndTime)
	if _, _, err := r.TimeRange(); err != nil {
		return errors.Tag(err)
	}
	return r.GetPageReq.Validate()
}

// TimeRange 解析已发送查询时间范围。
func (r *AdminMessageSentQueryReq) TimeRange() (*time.Time, *time.Time, error) {
	startTime, err := parseOptionalDateTimeForMessage(r.StartTime)
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	endTime, err := parseOptionalDateTimeForMessage(r.EndTime)
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	if startTime != nil && endTime != nil && startTime.After(*endTime) {
		return nil, nil, errors.Errorf("结束时间不能早于开始时间")
	}
	return startTime, endTime, nil
}

// AdminMessageSentItem 表示前端展示的“已发送”消息列表项。
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
	HandledAt           string `json:"handledAt"`           // 处理时间
	CreatedAt           string `json:"createdAt"`           // 创建时间
}

// AdminMessageIDPathReq 表示消息ID路径参数请求。
type AdminMessageIDPathReq struct {
	ID int64 `path:"id" json:"id,optional" form:"id,optional"` // 消息ID
}

// Validate 校验消息ID路径参数。
func (r *AdminMessageIDPathReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("id 不能为空")
	}
	return nil
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

// AdminMessageHandleReq 表示标记消息已处理请求参数。
type AdminMessageHandleReq struct {
	ID int64 `json:"id"` // 消息ID
}

// Validate 校验处理请求参数。
func (r *AdminMessageHandleReq) Validate() error {
	if r.ID <= 0 {
		return errors.Errorf("id 不能为空")
	}
	return nil
}

// AdminMessageUnreadCountResp 表示未读消息数量响应。
type AdminMessageUnreadCountResp struct {
	Unread int64 `json:"unread"` // 未读数量
}

// AdminMessageNotificationReq 表示通知列表请求参数。
type AdminMessageNotificationReq struct {
	Limit int `json:"limit,optional" form:"limit,optional"` // 最大条数
}

// Validate 校验通知列表请求参数。
func (r *AdminMessageNotificationReq) Validate() error {
	if r.Limit <= 0 {
		r.Limit = 10
	}
	if r.Limit > 50 {
		r.Limit = 50
	}
	return nil
}

// AdminMessageMarkReadReq 表示标记已读请求参数。
type AdminMessageMarkReadReq struct {
	IDs []int64 `json:"ids,optional"` // 消息ID列表
	All bool    `json:"all,optional"` // 是否标记全部
}

// Validate 校验标记已读请求参数。
func (r *AdminMessageMarkReadReq) Validate() error {
	if r.All {
		return nil
	}
	if len(r.IDs) == 0 {
		return errors.Errorf("ids 不能为空")
	}
	return nil
}

// AdminMessageDeleteReq 表示删除消息请求参数。
type AdminMessageDeleteReq struct {
	IDs     []int64 `json:"ids,optional"`     // 消息ID列表
	AllRead bool    `json:"allRead,optional"` // 是否删除全部已读
}

// Validate 校验删除请求参数。
func (r *AdminMessageDeleteReq) Validate() error {
	if r.AllRead {
		return nil
	}
	if len(r.IDs) == 0 {
		return errors.Errorf("ids 不能为空")
	}
	return nil
}

// AdminMessageSendReq 表示发送消息请求参数。
type AdminMessageSendReq struct {
	Type        string `json:"type"`                 // 消息类型
	Level       int    `json:"level"`                // 消息等级
	Title       string `json:"title"`                // 消息标题
	Content     string `json:"content"`              // 消息内容
	Data        string `json:"data,optional"`        // 扩展数据JSON
	Link        string `json:"link,optional"`        // 跳转链接
	ReceiverIDs []int  `json:"receiverIDs,optional"` // 收件人管理员ID列表；为空表示广播给全部启用管理员
}

// Validate 校验发送消息参数。
func (r *AdminMessageSendReq) Validate() error {
	r.Type = strings.TrimSpace(r.Type)
	r.Title = strings.TrimSpace(r.Title)
	r.Content = strings.TrimSpace(r.Content)
	r.Data = strings.TrimSpace(r.Data)
	r.Link = strings.TrimSpace(r.Link)
	if r.Type == "" {
		return errors.Errorf("type 不能为空")
	}
	if r.Level != int(AdminMessageLevelInfo) && r.Level != int(AdminMessageLevelWarning) && r.Level != int(AdminMessageLevelError) {
		return errors.Errorf("level 不合法")
	}
	if r.Title == "" {
		return errors.Errorf("title 不能为空")
	}
	if r.Content == "" {
		return errors.Errorf("content 不能为空")
	}
	return nil
}
