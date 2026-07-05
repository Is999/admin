package cdc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"admin/internal/config"
	"admin/internal/infra/cdcx"
	"admin/internal/infra/collectorx"
	"admin/internal/svc"
)

func TestAdminLogRowFromCreateEvent(t *testing.T) {
	event := cdcx.Event{
		Operation: cdcx.OperationCreate,
		After:     []byte(`{"id":11,"user_id":7,"user_name":" admin ","action":"login","route":"/api/login","trace_id":" t1 ","http_status":200,"biz_code":200,"latency_ms":12,"success":1}`),
	}
	row, err := adminLogRowFromEvent(event)
	if err != nil {
		t.Fatalf("adminLogRowFromEvent() error = %v", err)
	}
	if row.ID != 11 || row.UserID != 7 || row.UserName != "admin" || row.TraceID != "t1" {
		t.Fatalf("row 不符合预期: %+v", row)
	}
}

func TestAdminLogRowFromDeleteEventUsesBefore(t *testing.T) {
	event := cdcx.Event{
		Operation: cdcx.OperationDelete,
		Before:    []byte(`{"id":12,"success":false}`),
	}
	row, err := adminLogRowFromEvent(event)
	if err != nil {
		t.Fatalf("adminLogRowFromEvent() error = %v", err)
	}
	if row.ID != 12 || row.Success.Bool() {
		t.Fatalf("delete row 不符合预期: %+v", row)
	}
}

func TestCDCBoolUnmarshal(t *testing.T) {
	cases := map[string]bool{
		`true`:  true,
		`1`:     true,
		`"1"`:   true,
		`false`: false,
		`0`:     false,
		`"0"`:   false,
	}
	for raw, want := range cases {
		var got cdcBool
		if err := got.UnmarshalJSON([]byte(raw)); err != nil {
			t.Fatalf("UnmarshalJSON(%s) error = %v", raw, err)
		}
		if got.Bool() != want {
			t.Fatalf("UnmarshalJSON(%s) = %v, want %v", raw, got.Bool(), want)
		}
	}
}

func TestAdminLogLarkText(t *testing.T) {
	event := cdcx.Event{
		Topic:     "dnmp-admin.admin.admin_log",
		Partition: 1,
		Offset:    9,
		Source:    cdcx.Source{File: "mysql-bin.000001", Position: 128},
	}
	row := adminLogRow{
		ID:         11,
		UserID:     7,
		UserName:   "admin",
		Action:     "login",
		Route:      "/api/login",
		TraceID:    "trace-1",
		HTTPStatus: 200,
		BizCode:    200,
		LatencyMS:  12,
		Success:    cdcBool(true),
	}
	text := adminLogLarkText(event, row)
	for _, want := range []string{"CDC 审核日志", "dnmp-admin.admin.admin_log:1:9", "login", "/api/login", "trace-1", "mysql-bin.000001:128"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Lark 文本缺少 %q:\n%s", want, text)
		}
	}
}

func TestAdminLogCollectorEvent(t *testing.T) {
	event := cdcx.Event{
		Topic:     "dnmp-admin.admin.admin_log",
		Partition: 1,
		Offset:    9,
		Operation: cdcx.OperationCreate,
		Source:    cdcx.Source{File: "mysql-bin.000001", Position: 128},
	}
	row := adminLogRow{ID: 11, UserID: 7, UserName: "admin", Action: "login", Route: "/api/login", TraceID: "trace-1", HTTPStatus: 200, BizCode: 200, LatencyMS: 12, Success: cdcBool(true)}
	got, err := adminLogCollectorEvent(event, row)
	if err != nil {
		t.Fatalf("adminLogCollectorEvent() error = %v", err)
	}
	if got.EventID != adminLogCollectorEventID(event) || got.BizType != adminLogCollectorBizType || got.PartitionKey != "/api/login" {
		t.Fatalf("collector event 不符合预期: %+v", got)
	}
	if len(got.EventID) > 64 || !strings.HasPrefix(got.EventID, adminLogCollectorPrefix) {
		t.Fatalf("collector event_id 长度或前缀不符合预期: %q", got.EventID)
	}
	var payload adminLogCollectorPayload
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("解析 payload 失败: %v", err)
	}
	if payload.ID != 11 || payload.EventKey != event.EventKey() || payload.SourcePos != 128 {
		t.Fatalf("payload 不符合预期: %+v", payload)
	}
}

func TestAdminLogCollectorEventIDStableAndBounded(t *testing.T) {
	event := cdcx.Event{Topic: "dnmp-admin.admin.admin_log.with.extra.long.topic", Partition: 12, Offset: 9223372036854775807}
	got := adminLogCollectorEventID(event)
	if got != adminLogCollectorEventID(event) {
		t.Fatal("collector event_id 应保持稳定")
	}
	if len(got) > 64 {
		t.Fatalf("collector event_id 不能超过 DB 字段长度，实际=%d id=%s", len(got), got)
	}
	next := adminLogCollectorEventID(cdcx.Event{Topic: event.Topic, Partition: event.Partition, Offset: event.Offset - 1})
	if got == next {
		t.Fatalf("不同 Kafka 位点不应生成相同 event_id: %s", got)
	}
}

func TestAdminLogBatchProcessor(t *testing.T) {
	payload, _ := json.Marshal(adminLogCollectorPayload{ID: 11, Action: "login", Route: "/api/login", TraceID: "trace-1"})
	results, err := (&AdminLogBatchProcessor{}).ProcessBatch(context.Background(), []collectorx.Event{
		{EventID: "ok", BizType: adminLogCollectorBizType, Payload: payload},
		{EventID: "bad", BizType: adminLogCollectorBizType, Payload: []byte(`{`)},
	})
	if err != nil {
		t.Fatalf("ProcessBatch() error = %v", err)
	}
	if len(results) != 2 || !results[0].Success || results[1].Success || results[1].Error == "" {
		t.Fatalf("ProcessBatch() results = %+v", results)
	}
}

func TestAdminLogBatchProcessorWritesOutputFile(t *testing.T) {
	outputFile := filepath.Join(t.TempDir(), "cdc_admin_log_audit.jsonl")
	payload, _ := json.Marshal(adminLogCollectorPayload{
		EventKey: "topic:0:9",
		ID:       11,
		Action:   "管理员登录",
		Route:    "auth.login",
		TraceID:  "trace-1",
	})
	processor := &AdminLogBatchProcessor{outputFile: outputFile}
	results, err := processor.ProcessBatch(context.Background(), []collectorx.Event{
		{EventID: "event-1", BizType: adminLogCollectorBizType, Payload: payload},
	})
	if err != nil {
		t.Fatalf("ProcessBatch() error = %v", err)
	}
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("ProcessBatch() results = %+v", results)
	}
	body, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("读取批处理观察文件失败: %v", err)
	}
	text := string(body)
	for _, want := range []string{`"type":"batch"`, `"collected_count":1`, `"type":"event"`, "event-1", "管理员登录", "auth.login", "trace-1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("观察文件缺少 %q: %s", want, text)
		}
	}
}

func TestAdminLogBatchProcessorOutputKeepsInterval(t *testing.T) {
	outputFile := filepath.Join(t.TempDir(), "cdc_admin_log_audit.jsonl")
	payload, _ := json.Marshal(adminLogCollectorPayload{ID: 11, Action: "login"})
	processor := &AdminLogBatchProcessor{outputFile: outputFile}
	if _, err := processor.ProcessBatch(context.Background(), []collectorx.Event{{EventID: "event-1", Payload: payload}}); err != nil {
		t.Fatalf("first ProcessBatch() error = %v", err)
	}
	if _, err := processor.ProcessBatch(context.Background(), []collectorx.Event{{EventID: "event-2", Payload: payload}}); err != nil {
		t.Fatalf("second ProcessBatch() error = %v", err)
	}
	body, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("读取批处理观察文件失败: %v", err)
	}
	text := string(body)
	if strings.Count(text, `"type":"batch"`) != 2 || !strings.Contains(text, `"previous_time":"`) {
		t.Fatalf("批次头缺少上批时间: %s", text)
	}
}

func TestAdminLogBatchProcessorRecordsSummaryWithoutOutputFile(t *testing.T) {
	processor := &AdminLogBatchProcessor{}
	summary, err := processor.recordOutput([][]byte{[]byte(`{"type":"event"}` + "\n")})
	if err != nil {
		t.Fatalf("recordOutput() error = %v", err)
	}
	if summary.CollectedCount != 1 || summary.BatchTime == "" || summary.PreviousTime != "" {
		t.Fatalf("首次批次摘要不符合预期: %+v", summary)
	}
	next, err := processor.recordOutput([][]byte{[]byte(`{"type":"event"}` + "\n")})
	if err != nil {
		t.Fatalf("second recordOutput() error = %v", err)
	}
	if next.PreviousTime == "" {
		t.Fatalf("第二批次应带上批时间: %+v", next)
	}
}

func TestAdminLogProcessorNoopWhenTestDisabled(t *testing.T) {
	err := (AdminLogProcessor{}).ProcessCDC(context.Background(), cdcx.Event{
		Operation: cdcx.OperationCreate,
		After:     []byte(`{`),
	})
	if err != nil {
		t.Fatalf("验证配置未启用时不应处理事件: %v", err)
	}
}

func TestAdminLogProcessorSkipsNonCreateOperation(t *testing.T) {
	err := (AdminLogProcessor{cfg: config.AdminLogAuditTestScenario{LarkEnabled: true}}).ProcessCDC(context.Background(), cdcx.Event{
		Operation: cdcx.OperationRead,
		After:     []byte(`{`),
	})
	if err != nil {
		t.Fatalf("非新增审计日志不应进入验证处理: %v", err)
	}
}

func TestAdminLogProcessorEnqueuesCollectorBeforeLark(t *testing.T) {
	notifier := &fakeAdminLogNotifier{}
	collector := &fakeCollectorEnqueuer{err: errors.New("collector failed")}
	processor := AdminLogProcessor{
		cfg:       config.AdminLogAuditTestScenario{LarkEnabled: true, CollectorEnabled: true},
		notifier:  notifier,
		collector: collector,
	}
	err := processor.ProcessCDC(context.Background(), cdcx.Event{
		Topic:     "dnmp-admin.admin.admin_log",
		Partition: 0,
		Offset:    11,
		Operation: cdcx.OperationCreate,
		After:     []byte(`{"id":11,"user_id":7,"user_name":"admin","action":"管理员登录","route":"auth.login","http_status":200,"biz_code":200,"success":1}`),
	})
	if err == nil {
		t.Fatal("Collector 失败应返回错误")
	}
	if len(collector.events) != 1 {
		t.Fatalf("Collector 应先收到事件，实际=%d", len(collector.events))
	}
	if len(notifier.texts) != 0 {
		t.Fatalf("Collector 失败时不应发送 Lark，实际=%d", len(notifier.texts))
	}
}

func TestAdminLogProcessorSendsLarkAfterCollectorSuccess(t *testing.T) {
	notifier := &fakeAdminLogNotifier{}
	collector := &fakeCollectorEnqueuer{}
	processor := AdminLogProcessor{
		cfg:       config.AdminLogAuditTestScenario{LarkEnabled: true, CollectorEnabled: true},
		notifier:  notifier,
		collector: collector,
	}
	err := processor.ProcessCDC(context.Background(), cdcx.Event{
		Topic:     "dnmp-admin.admin.admin_log",
		Partition: 0,
		Offset:    12,
		Operation: cdcx.OperationCreate,
		After:     []byte(`{"id":12,"user_id":7,"user_name":"admin","action":"管理员登录","route":"auth.login","http_status":200,"biz_code":200,"success":1}`),
	})
	if err != nil {
		t.Fatalf("ProcessCDC() error = %v", err)
	}
	if len(collector.events) != 1 || len(notifier.texts) != 1 {
		t.Fatalf("输出链路不符合预期 collector=%d lark=%d", len(collector.events), len(notifier.texts))
	}
}

func TestAdminLogProcessorCollectorIgnoresLarkFilters(t *testing.T) {
	notifier := &fakeAdminLogNotifier{}
	collector := &fakeCollectorEnqueuer{}
	processor := AdminLogProcessor{
		cfg: config.AdminLogAuditTestScenario{
			LarkEnabled:      true,
			CollectorEnabled: true,
			Actions:          []string{"管理员登录"},
			Routes:           []string{"auth.login"},
		},
		notifier:  notifier,
		collector: collector,
	}
	err := processor.ProcessCDC(context.Background(), cdcx.Event{
		Topic:     "dnmp-admin.admin.admin_log",
		Partition: 0,
		Offset:    13,
		Operation: cdcx.OperationCreate,
		After:     []byte(`{"id":13,"user_id":7,"user_name":"admin","action":"查询Collector观测概览","route":"collector.overview","trace_id":"trace-1","http_status":200,"biz_code":200,"success":1}`),
	})
	if err != nil {
		t.Fatalf("ProcessCDC() error = %v", err)
	}
	if len(collector.events) != 1 {
		t.Fatalf("Collector 调试应收到非登录审计日志，实际=%d", len(collector.events))
	}
	if len(notifier.texts) != 0 {
		t.Fatalf("Lark 仍应受动作和路由过滤，实际=%d", len(notifier.texts))
	}
}

func TestAdminLogProcessorCollectorRespectsTracePrefix(t *testing.T) {
	collector := &fakeCollectorEnqueuer{}
	processor := AdminLogProcessor{
		cfg:       config.AdminLogAuditTestScenario{CollectorEnabled: true, TraceIDPrefix: "login-"},
		collector: collector,
	}
	err := processor.ProcessCDC(context.Background(), cdcx.Event{
		Topic:     "dnmp-admin.admin.admin_log",
		Partition: 0,
		Offset:    14,
		Operation: cdcx.OperationCreate,
		After:     []byte(`{"id":14,"action":"查询Collector观测概览","route":"collector.overview","trace_id":"normal-request","success":1}`),
	})
	if err != nil {
		t.Fatalf("ProcessCDC() error = %v", err)
	}
	if len(collector.events) != 0 {
		t.Fatalf("trace_id 前缀不匹配时不应写入 Collector，实际=%d", len(collector.events))
	}
}

func TestAdminLogAuditLarkMatchedByTraceIDPrefix(t *testing.T) {
	cfg := config.AdminLogAuditTestScenario{TraceIDPrefix: "codex-cdc-"}
	if !adminLogAuditLarkMatched(cfg, adminLogRow{TraceID: "codex-cdc-demo"}) {
		t.Fatal("期望匹配验证 trace_id 前缀")
	}
	if adminLogAuditLarkMatched(cfg, adminLogRow{TraceID: "normal-request"}) {
		t.Fatal("普通 trace_id 不应触发验证链路")
	}
	if !adminLogAuditLarkMatched(config.AdminLogAuditTestScenario{}, adminLogRow{TraceID: "normal-request"}) {
		t.Fatal("未配置前缀时应保持不过滤")
	}
}

func TestAdminLogAuditLarkMatchedByActionAndRoute(t *testing.T) {
	cfg := config.AdminLogAuditTestScenario{
		Actions: []string{"管理员登录"},
		Routes:  []string{"auth.login"},
	}
	if !adminLogAuditLarkMatched(cfg, adminLogRow{Action: "管理员登录", Route: "auth.login"}) {
		t.Fatal("期望登录日志命中验证链路")
	}
	if adminLogAuditLarkMatched(cfg, adminLogRow{Action: "查询管理员列表", Route: "admin.list"}) {
		t.Fatal("普通查询日志不应命中登录验证链路")
	}
}

func TestAdminLogAuditLarkMatchedIgnoresBlankFilters(t *testing.T) {
	cfg := config.AdminLogAuditTestScenario{
		Actions: []string{" "},
		Routes:  []string{""},
	}
	if !adminLogAuditLarkMatched(cfg, adminLogRow{Action: "查询管理员列表", Route: "admin.list"}) {
		t.Fatal("空白过滤项应按未配置处理")
	}
}

func TestAdminLogAuditEnabled(t *testing.T) {
	if adminLogAuditEnabled(config.AdminLogAuditTestScenario{}) {
		t.Fatal("未配置验证输出时不应启用 admin_log 验证")
	}
	if !adminLogAuditEnabled(config.AdminLogAuditTestScenario{LarkEnabled: true}) {
		t.Fatal("启用 Lark 时应启用 admin_log 验证")
	}
	if !adminLogAuditEnabled(config.AdminLogAuditTestScenario{CollectorEnabled: true}) {
		t.Fatal("启用 Collector 时应启用 admin_log 验证")
	}
}

func TestRegisterProcessorsRejectsMissingLarkConfig(t *testing.T) {
	consumer, err := cdcx.New(config.CDCConfig{})
	if err != nil {
		t.Fatalf("cdcx.New() error = %v", err)
	}
	svcCtx := svc.NewServiceContext(config.Config{
		TestScenarios: config.TestScenariosConfig{AdminLogAudit: config.AdminLogAuditTestScenario{LarkEnabled: true}},
	}, svc.Dependencies{})
	if err := RegisterProcessors(svcCtx, consumer); err == nil {
		t.Fatal("期望 Lark 验证开关缺少 alert.lark 时注册失败")
	}
}

func TestRegisterProcessorsRejectsDisabledCollector(t *testing.T) {
	consumer, err := cdcx.New(config.CDCConfig{})
	if err != nil {
		t.Fatalf("cdcx.New() error = %v", err)
	}
	svcCtx := svc.NewServiceContext(config.Config{
		TestScenarios: config.TestScenariosConfig{AdminLogAudit: config.AdminLogAuditTestScenario{CollectorEnabled: true}},
		Collector:     config.CollectorConfig{Enabled: false},
	}, svc.Dependencies{})
	if err := RegisterProcessors(svcCtx, consumer); err == nil {
		t.Fatal("期望 Collector 验证开关未启用 collector 时注册失败")
	}
}

type fakeAdminLogNotifier struct {
	texts []string // 已发送文本
	err   error    // 固定返回错误
}

func (n *fakeAdminLogNotifier) SendText(_ context.Context, text string) error {
	n.texts = append(n.texts, text)
	return n.err
}

type fakeCollectorEnqueuer struct {
	events []collectorx.Event // 已入队事件
	err    error              // 固定返回错误
}

func (c *fakeCollectorEnqueuer) Enqueue(_ context.Context, event collectorx.Event) (string, error) {
	c.events = append(c.events, event)
	if c.err != nil {
		return "", c.err
	}
	return event.EventID, nil
}
