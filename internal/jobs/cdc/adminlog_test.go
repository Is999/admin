package cdc

import (
	"context"
	"encoding/json"
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
	if got.EventID != adminLogCollectorPrefix+event.EventKey() || got.BizType != adminLogCollectorBizType || got.PartitionKey != "/api/login" {
		t.Fatalf("collector event 不符合预期: %+v", got)
	}
	var payload adminLogCollectorPayload
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("解析 payload 失败: %v", err)
	}
	if payload.ID != 11 || payload.EventKey != event.EventKey() || payload.SourcePos != 128 {
		t.Fatalf("payload 不符合预期: %+v", payload)
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

func TestAdminLogProcessorNoopWhenTestDisabled(t *testing.T) {
	err := (AdminLogProcessor{}).ProcessCDC(context.Background(), cdcx.Event{
		Operation: cdcx.OperationCreate,
		After:     []byte(`{`),
	})
	if err != nil {
		t.Fatalf("测试配置未启用时不应处理事件: %v", err)
	}
}

func TestAdminLogTestMatchedByTraceIDPrefix(t *testing.T) {
	cfg := config.AdminLogAuditTestScenario{TraceIDPrefix: "codex-cdc-"}
	if !adminLogTestMatched(cfg, adminLogRow{TraceID: "codex-cdc-demo"}) {
		t.Fatal("期望匹配测试 trace_id 前缀")
	}
	if adminLogTestMatched(cfg, adminLogRow{TraceID: "normal-request"}) {
		t.Fatal("普通 trace_id 不应触发测试链路")
	}
	if !adminLogTestMatched(config.AdminLogAuditTestScenario{}, adminLogRow{TraceID: "normal-request"}) {
		t.Fatal("未配置前缀时应保持不过滤")
	}
}

func TestAdminLogTestMatchedByActionAndRoute(t *testing.T) {
	cfg := config.AdminLogAuditTestScenario{
		Actions: []string{"管理员登录"},
		Routes:  []string{"auth.login"},
	}
	if !adminLogTestMatched(cfg, adminLogRow{Action: "管理员登录", Route: "auth.login"}) {
		t.Fatal("期望登录日志命中测试链路")
	}
	if adminLogTestMatched(cfg, adminLogRow{Action: "查询管理员列表", Route: "admin.list"}) {
		t.Fatal("普通查询日志不应命中登录测试链路")
	}
}

func TestAdminLogTestEnabled(t *testing.T) {
	if adminLogTestEnabled(config.AdminLogAuditTestScenario{}) {
		t.Fatal("未配置测试输出时不应启用 admin_log 测试")
	}
	if !adminLogTestEnabled(config.AdminLogAuditTestScenario{LarkEnabled: true}) {
		t.Fatal("启用 Lark 时应启用 admin_log 测试")
	}
	if !adminLogTestEnabled(config.AdminLogAuditTestScenario{CollectorEnabled: true}) {
		t.Fatal("启用 Collector 时应启用 admin_log 测试")
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
		t.Fatal("期望 Lark 测试开关缺少 alert.lark 时注册失败")
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
		t.Fatal("期望 Collector 测试开关未启用 collector 时注册失败")
	}
}
