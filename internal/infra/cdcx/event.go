package cdcx

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"
)

// Operation 表示 Debezium 事件操作类型。
type Operation string

const (
	// OperationCreate 表示 insert 事件。
	OperationCreate Operation = "c"
	// OperationUpdate 表示 update 事件。
	OperationUpdate Operation = "u"
	// OperationDelete 表示 delete 事件。
	OperationDelete Operation = "d"
	// OperationRead 表示 snapshot read 事件。
	OperationRead Operation = "r"
)

// Event 表示解析后的 Debezium CDC 事件。
type Event struct {
	Topic       string          // Kafka Topic 名称
	Partition   int             // Kafka 分区号
	Offset      int64           // Kafka 消费位点
	Key         json.RawMessage // Kafka 消息 key
	PrimaryKey  json.RawMessage // Debezium 主键 payload
	Database    string          // 来源数据库
	Table       string          // 来源表
	Operation   Operation       // 事件操作类型
	Before      json.RawMessage // 变更前数据，delete/update 时可能存在
	After       json.RawMessage // 变更后数据，insert/update/read 时可能存在
	Source      Source          // 来源 binlog 位点信息
	SourceTime  time.Time       // 来源库事件时间
	ProcessedAt time.Time       // 本地处理时间
	Raw         json.RawMessage // 原始消息体
}

// Source 描述 Debezium source 中可用于排障和幂等的位点信息。
type Source struct {
	Database    string // 来源数据库
	Table       string // 来源表
	File        string // binlog 文件名
	GTID        string // GTID 集合或当前 GTID
	Snapshot    string // 快照状态：true/false/last
	ServerID    int64  // MySQL 服务实例 ID
	Position    int64  // binlog 文件偏移量
	Row         int64  // binlog row 序号
	TimestampMS int64  // 来源事件毫秒时间戳
}

// TableKey 返回 db.table 格式的稳定路由键。
func (e Event) TableKey() string {
	db := strings.TrimSpace(e.Database)
	table := strings.TrimSpace(e.Table)
	if db == "" {
		return table
	}
	if table == "" {
		return db
	}
	return db + "." + table
}

// EventKey 返回 Kafka 位点幂等键。
func (e Event) EventKey() string {
	return strings.TrimSpace(e.Topic) + ":" + intString(e.Partition) + ":" + int64String(e.Offset)
}

// RowData 返回当前操作对应的行数据，delete 使用 before，其它操作使用 after。
func (e Event) RowData() json.RawMessage {
	if e.Operation == OperationDelete {
		return cloneRaw(e.Before)
	}
	return cloneRaw(e.After)
}

// DecodeDebeziumEvent 把 Debezium envelope 解析为本地 CDC 事件。
func DecodeDebeziumEvent(topic string, partition int, offset int64, key []byte, value []byte) (Event, error) {
	if len(value) == 0 {
		return Event{}, Skip("CDC tombstone 消息")
	}
	var envelope debeziumEnvelope
	if err := json.Unmarshal(value, &envelope); err != nil {
		return Event{}, errors.Wrap(err, "解析 Debezium 消息失败")
	}
	op := Operation(strings.TrimSpace(envelope.Operation))
	if op == "" {
		return Event{}, errors.Errorf("Debezium 消息缺少 op topic=%s partition=%d offset=%d", topic, partition, offset)
	}
	sourceTime := time.Time{}
	if envelope.Source.TimestampMS > 0 {
		sourceTime = time.UnixMilli(envelope.Source.TimestampMS)
	}
	source := envelope.Source.Source()
	pk, err := decodePrimaryKey(key)
	if err != nil {
		return Event{}, errors.Tag(err)
	}
	return Event{
		Topic:       strings.TrimSpace(topic),
		Partition:   partition,
		Offset:      offset,
		Key:         append(json.RawMessage(nil), key...),
		PrimaryKey:  pk,
		Database:    strings.TrimSpace(envelope.Source.Database),
		Table:       strings.TrimSpace(envelope.Source.Table),
		Operation:   op,
		Before:      cloneRaw(envelope.Before),
		After:       cloneRaw(envelope.After),
		Source:      source,
		SourceTime:  sourceTime,
		ProcessedAt: time.Now(),
		Raw:         append(json.RawMessage(nil), value...),
	}, nil
}

// debeziumEnvelope 只声明当前消费器需要的 Debezium 字段。
type debeziumEnvelope struct {
	Before    json.RawMessage `json:"before"` // 变更前数据
	After     json.RawMessage `json:"after"`  // 变更后数据
	Source    debeziumSource  `json:"source"` // 来源库表信息
	Operation string          `json:"op"`     // 操作类型
}

// debeziumSource 描述 Debezium source 中的库表和事件时间。
type debeziumSource struct {
	Database    string `json:"db"`        // 来源数据库
	Table       string `json:"table"`     // 来源表
	File        string `json:"file"`      // binlog 文件名
	GTID        string `json:"gtid"`      // 全局事务标识
	Snapshot    any    `json:"snapshot"`  // 快照状态
	ServerID    int64  `json:"server_id"` // MySQL 服务实例 ID
	Position    int64  `json:"pos"`       // binlog 文件偏移量
	Row         int64  `json:"row"`       // binlog row 序号
	TimestampMS int64  `json:"ts_ms"`     // 来源事件毫秒时间戳
}

// Source 转换为稳定的内部 source 结构。
func (s debeziumSource) Source() Source {
	return Source{
		Database:    strings.TrimSpace(s.Database),
		Table:       strings.TrimSpace(s.Table),
		File:        strings.TrimSpace(s.File),
		GTID:        strings.TrimSpace(s.GTID),
		Snapshot:    snapshotString(s.Snapshot),
		ServerID:    s.ServerID,
		Position:    s.Position,
		Row:         s.Row,
		TimestampMS: s.TimestampMS,
	}
}

// decodePrimaryKey 兼容 Debezium JSON converter 的 key 包装格式。
func decodePrimaryKey(key []byte) (json.RawMessage, error) {
	if len(key) == 0 {
		return nil, nil
	}
	var wrapper struct {
		Payload json.RawMessage `json:"payload"` // Debezium key 包装后的主键 JSON
	}
	if err := json.Unmarshal(key, &wrapper); err == nil && len(wrapper.Payload) > 0 && string(wrapper.Payload) != "null" {
		return cloneRaw(wrapper.Payload), nil
	}
	var raw json.RawMessage
	if err := json.Unmarshal(key, &raw); err != nil {
		return nil, errors.Wrap(err, "解析 Debezium key 失败")
	}
	return cloneRaw(raw), nil
}

// cloneRaw 复制 JSON 原文，避免上层复用底层 buffer。
func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

// snapshotString 兼容 Debezium snapshot 的 bool/string 形态。
func snapshotString(value any) string {
	switch v := value.(type) {
	case bool:
		if v {
			return "true"
		}
		return "false"
	case string:
		return strings.TrimSpace(v)
	case nil:
		return ""
	default:
		return ""
	}
}

// intString 转换 int，避免额外格式模板散落在调用处。
func intString(v int) string {
	return int64String(int64(v))
}

// int64String 转换 int64，供幂等键拼接使用。
func int64String(v int64) string {
	return strconv.FormatInt(v, 10)
}
