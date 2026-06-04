package cdc

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"admin/internal/config"
	"admin/internal/infra/cdcx"
	"admin/internal/infra/collectorx"
	"admin/internal/infra/larkx"
	"admin/internal/infra/loggerx"
	"admin/internal/svc"

	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	adminLogTable            = "admin.admin_log"      // admin 操作日志表 CDC 路由键
	adminLogCollectorBizType = "cdc.admin_log.audit"  // admin_log CDC 批量测试业务类型
	adminLogCollectorPrefix  = "cdc:admin_log:audit:" // admin_log CDC 批量测试事件前缀
	adminLogLarkTitle        = "【CDC 审核日志】"           // admin_log CDC Lark 消息标题
)

// RegisterProcessors 注册内置 CDC 表处理器。
func RegisterProcessors(svcCtx *svc.ServiceContext, consumer *cdcx.Consumer) error {
	if consumer == nil {
		return nil
	}
	cfg := config.CDCAdminLogTest{}
	if svcCtx != nil {
		cfg = svcCtx.CurrentConfig().CDC.AdminLogTest
	}
	var notifier *larkx.Notifier
	var err error
	if cfg.LarkEnabled {
		if svcCtx == nil {
			return errors.Errorf("admin_log CDC Lark 测试缺少服务上下文")
		}
		notifier, err = larkx.New(svcCtx.CurrentConfig().Alert.Lark)
		if err != nil {
			return errors.Wrap(err, "初始化 admin_log CDC Lark 测试通知器失败")
		}
		if notifier == nil {
			return errors.Errorf("admin_log CDC Lark 测试已启用但 alert.lark 未启用")
		}
	}
	if cfg.CollectorEnabled {
		if svcCtx == nil || svcCtx.Collector == nil {
			return errors.Errorf("admin_log CDC Collector 测试已启用但 Collector 未初始化")
		}
		if !svcCtx.CurrentConfig().Collector.Enabled {
			return errors.Errorf("admin_log CDC Collector 测试已启用但 collector.enabled=false")
		}
		if err = svcCtx.Collector.RegisterProcessor(adminLogCollectorBizType, AdminLogBatchProcessor{}); err != nil {
			return errors.Tag(err)
		}
	}
	return errors.Tag(consumer.RegisterProcessor(adminLogTable, AdminLogProcessor{
		cfg:       cfg,
		notifier:  notifier,
		collector: collectorFromService(svcCtx),
	}))
}

// AdminLogProcessor 处理 admin_log 的 Debezium 增量事件。
type AdminLogProcessor struct {
	cfg       config.CDCAdminLogTest // admin_log CDC 测试开关
	notifier  *larkx.Notifier        // Lark 通知器
	collector collectorEnqueuer      // Collector 批量写入入口
}

// collectorEnqueuer 约束 CDC 写入 Collector 的最小能力。
type collectorEnqueuer interface {
	Enqueue(context.Context, collectorx.Event) (string, error)
}

// ProcessCDC 将 admin_log CDC 事件清洗成轻量统计字段。
func (p AdminLogProcessor) ProcessCDC(ctx context.Context, event cdcx.Event) error {
	row, err := adminLogRowFromEvent(event)
	if err != nil {
		return errors.Tag(err)
	}
	if row.ID == 0 {
		return errors.Errorf("admin_log CDC 事件缺少 id op=%s topic=%s offset=%d", event.Operation, event.Topic, event.Offset)
	}
	if !adminLogTestMatched(p.cfg, row) {
		loggerx.Infow(ctx, "CDC admin_log 测试事件已跳过",
			logx.Field("event_key", event.EventKey()),
			logx.Field("id", row.ID),
			logx.Field("trace_id", row.TraceID),
			logx.Field("trace_id_prefix", strings.TrimSpace(p.cfg.TraceIDPrefix)),
		)
		return nil
	}
	loggerx.Infow(ctx, "CDC admin_log 已处理",
		logx.Field("table", event.TableKey()),
		logx.Field("event_key", event.EventKey()),
		logx.Field("op", string(event.Operation)),
		logx.Field("id", row.ID),
		logx.Field("user_id", row.UserID),
		logx.Field("user_name", row.UserName),
		logx.Field("action", row.Action),
		logx.Field("route", row.Route),
		logx.Field("success", row.Success.Bool()),
		logx.Field("http_status", row.HTTPStatus),
		logx.Field("biz_code", row.BizCode),
		logx.Field("latency_ms", row.LatencyMS),
		logx.Field("trace_id", row.TraceID),
		logx.Field("source_file", event.Source.File),
		logx.Field("source_pos", event.Source.Position),
	)
	if p.cfg.LarkEnabled {
		if p.notifier == nil {
			return errors.Errorf("admin_log CDC Lark 通知器未初始化")
		}
		if err = p.notifier.SendText(ctx, adminLogLarkText(event, row)); err != nil {
			return errors.Wrap(err, "发送 admin_log CDC Lark 消息失败")
		}
		loggerx.Infow(ctx, "CDC admin_log Lark 已推送",
			logx.Field("event_key", event.EventKey()),
			logx.Field("id", row.ID),
			logx.Field("trace_id", row.TraceID),
		)
	}
	if p.cfg.CollectorEnabled {
		if p.collector == nil {
			return errors.Errorf("admin_log CDC Collector 未初始化")
		}
		collectorEvent, err := adminLogCollectorEvent(event, row)
		if err != nil {
			return errors.Tag(err)
		}
		eventID, err := p.collector.Enqueue(ctx, collectorEvent)
		if err != nil {
			return errors.Wrap(err, "写入 admin_log CDC Collector 事件失败")
		}
		loggerx.Infow(ctx, "CDC admin_log Collector 已写入",
			logx.Field("event_id", eventID),
			logx.Field("id", row.ID),
			logx.Field("trace_id", row.TraceID),
		)
	}
	return nil
}

// adminLogTestMatched 判断当前 admin_log 是否命中测试过滤条件。
func adminLogTestMatched(cfg config.CDCAdminLogTest, row adminLogRow) bool {
	prefix := strings.TrimSpace(cfg.TraceIDPrefix)
	if prefix == "" {
		return true
	}
	return strings.HasPrefix(row.TraceID, prefix)
}

// AdminLogBatchProcessor 批量消费 admin_log CDC 测试事件。
type AdminLogBatchProcessor struct{}

// ProcessBatch 批量处理 admin_log CDC 测试事件。
func (AdminLogBatchProcessor) ProcessBatch(ctx context.Context, events []collectorx.Event) ([]collectorx.ProcessResult, error) {
	if len(events) == 0 {
		return nil, nil
	}
	results := make([]collectorx.ProcessResult, 0, len(events))
	for _, event := range events {
		var row adminLogCollectorPayload
		if err := json.Unmarshal(event.Payload, &row); err != nil {
			results = append(results, collectorx.ProcessResult{EventID: event.EventID, Success: false, Error: "解析 admin_log 批量事件失败"})
			continue
		}
		loggerx.Infow(ctx, "CDC admin_log 批量事件已消费",
			logx.Field("event_id", event.EventID),
			logx.Field("admin_log_id", row.ID),
			logx.Field("action", row.Action),
			logx.Field("route", row.Route),
			logx.Field("trace_id", row.TraceID),
		)
		results = append(results, collectorx.ProcessResult{EventID: event.EventID, Success: true})
	}
	return results, nil
}

// adminLogRow 表示 admin_log 统计需要的最小字段集。
type adminLogRow struct {
	ID         int64   `json:"id"`          // 日志 ID
	UserID     int64   `json:"user_id"`     // 操作人 ID
	UserName   string  `json:"user_name"`   // 操作人账号
	Action     string  `json:"action"`      // 操作动作
	Route      string  `json:"route"`       // 路由名称
	TraceID    string  `json:"trace_id"`    // Trace ID
	HTTPStatus int     `json:"http_status"` // HTTP 状态码
	BizCode    int     `json:"biz_code"`    // 业务码
	LatencyMS  int64   `json:"latency_ms"`  // 请求耗时毫秒
	Success    cdcBool `json:"success"`     // 是否成功，兼容 Debezium tinyint 数字
}

// adminLogCollectorPayload 表示写入 Collector 的 admin_log 测试负载。
type adminLogCollectorPayload struct {
	EventKey   string `json:"event_key"`   // CDC 幂等键
	ID         int64  `json:"id"`          // 日志 ID
	UserID     int64  `json:"user_id"`     // 操作人 ID
	UserName   string `json:"user_name"`   // 操作人账号
	Action     string `json:"action"`      // 操作动作
	Route      string `json:"route"`       // 路由名称
	TraceID    string `json:"trace_id"`    // Trace ID
	HTTPStatus int    `json:"http_status"` // HTTP 状态码
	BizCode    int    `json:"biz_code"`    // 业务码
	LatencyMS  int64  `json:"latency_ms"`  // 请求耗时毫秒
	Success    bool   `json:"success"`     // 是否成功
	Op         string `json:"op"`          // CDC 操作类型
	SourceFile string `json:"source_file"` // binlog 文件
	SourcePos  int64  `json:"source_pos"`  // binlog position
}

// adminLogRowFromEvent 根据 op 选择 after/before 数据并解析成统计字段。
func adminLogRowFromEvent(event cdcx.Event) (adminLogRow, error) {
	body := event.RowData()
	if len(body) == 0 {
		return adminLogRow{}, errors.Errorf("admin_log CDC 事件缺少行数据 op=%s", event.Operation)
	}
	var row adminLogRow
	if err := json.Unmarshal(body, &row); err != nil {
		return adminLogRow{}, errors.Wrap(err, "解析 admin_log CDC 行数据失败")
	}
	row.UserName = strings.TrimSpace(row.UserName)
	row.Action = strings.TrimSpace(row.Action)
	row.Route = strings.TrimSpace(row.Route)
	row.TraceID = strings.TrimSpace(row.TraceID)
	return row, nil
}

// adminLogLarkText 构造审核日志 Lark 测试消息。
func adminLogLarkText(event cdcx.Event, row adminLogRow) string {
	lines := []string{
		adminLogLarkTitle,
		"- EventKey：" + event.EventKey(),
		"- 操作：" + row.Action,
		"- 路由：" + row.Route,
		"- 操作人：" + row.UserName + "(" + strconv.FormatInt(row.UserID, 10) + ")",
		"- 状态：" + strconv.Itoa(row.HTTPStatus) + "/" + strconv.Itoa(row.BizCode),
		"- 成功：" + strconv.FormatBool(row.Success.Bool()),
		"- 耗时：" + strconv.FormatInt(row.LatencyMS, 10) + "ms",
	}
	if row.TraceID != "" {
		lines = append(lines, "- TraceID："+row.TraceID)
	}
	if event.Source.File != "" || event.Source.Position > 0 {
		lines = append(lines, "- Binlog："+event.Source.File+":"+strconv.FormatInt(event.Source.Position, 10))
	}
	return strings.Join(lines, "\n")
}

// adminLogCollectorEvent 构造写入 Collector 的批量测试事件。
func adminLogCollectorEvent(event cdcx.Event, row adminLogRow) (collectorx.Event, error) {
	payload := adminLogCollectorPayload{
		EventKey:   event.EventKey(),
		ID:         row.ID,
		UserID:     row.UserID,
		UserName:   row.UserName,
		Action:     row.Action,
		Route:      row.Route,
		TraceID:    row.TraceID,
		HTTPStatus: row.HTTPStatus,
		BizCode:    row.BizCode,
		LatencyMS:  row.LatencyMS,
		Success:    row.Success.Bool(),
		Op:         string(event.Operation),
		SourceFile: event.Source.File,
		SourcePos:  event.Source.Position,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return collectorx.Event{}, errors.Tag(err)
	}
	return collectorx.Event{
		EventID:      adminLogCollectorPrefix + event.EventKey(),
		BizType:      adminLogCollectorBizType,
		PartitionKey: row.Route,
		Payload:      body,
	}, nil
}

// collectorFromService 返回 ServiceContext 中的 Collector。
func collectorFromService(svcCtx *svc.ServiceContext) collectorEnqueuer {
	if svcCtx == nil {
		return nil
	}
	return svcCtx.Collector
}

// cdcBool 兼容 Debezium 将 MySQL tinyint(1) 输出为 0/1 的情况。
type cdcBool bool

// UnmarshalJSON 支持 bool、0/1 和 "0"/"1" 三种常见形态。
func (b *cdcBool) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return errors.Wrap(err, "解析 CDC bool 字段失败")
	}
	switch v := raw.(type) {
	case bool:
		*b = cdcBool(v)
	case float64:
		*b = cdcBool(v != 0)
	case string:
		*b = cdcBool(strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "0")
	case nil:
		*b = false
	default:
		return errors.Errorf("不支持的 CDC bool 字段类型 %T", raw)
	}
	return nil
}

// Bool 返回普通 bool 值，供日志和统计使用。
func (b cdcBool) Bool() bool {
	return bool(b)
}
