package model

const (
	// UserTagEventKey 表示下游识别用户标签事件的 Kafka key。
	UserTagEventKey = "user_tag_event"
)

// UserTagMessage 表示推送给下游的标签事件。
type UserTagMessage struct {
	ActionType string `json:"action_type"`      // getTag 或 lostTag
	UID        int64  `json:"uid"`              // 用户 UID
	Time       string `json:"time,omitempty"`   // 标签变更时间，格式：2006-01-02 15:04:05
	TagID      int    `json:"tag_id"`           // 标签类型
	EventID    string `json:"id,omitempty"`     // 事件幂等 ID，供排查和扩展使用
	Source     string `json:"source,omitempty"` // 消息来源，默认由 cron 用户标签链路生成
}

// UserTagEventEnvelope 表示用户标签事件的统一消息信封。
type UserTagEventEnvelope struct {
	Key  string         `json:"key"`  // 消息 key，固定为 user_tag_event
	Data UserTagMessage `json:"data"` // 标签得失事件内容
}

// NewUserTagEventEnvelope 构造可识别的用户标签事件消息。
func NewUserTagEventEnvelope(message UserTagMessage) UserTagEventEnvelope {
	return UserTagEventEnvelope{
		Key:  UserTagEventKey,
		Data: message,
	}
}
