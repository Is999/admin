package audit

import (
	"context"
	"strings"
	"testing"
	"time"

	"admin/internal/model"
	"admin/internal/requestctx"
)

// fakeAdminLogEnqueuer 记录测试中投递的审计事件 ID 和日志内容。
type fakeAdminLogEnqueuer struct {
	eventIDs []string         // 已投递的 Collector 事件 ID
	rows     []model.AdminLog // 已投递的审计日志
}

// fakeIPLocator 返回固定归属地，验证 Recorder 已接入本地查询能力。
type fakeIPLocator struct{}

// panicIPLocator 用于确认已有请求级归属地时不会重复查询。
type panicIPLocator struct{}

// countingIPLocator 记录查询次数，验证空结果也不会重复解析。
type countingIPLocator struct {
	calls int // 归属地查询次数
}

// Lookup 返回测试归属地。
func (fakeIPLocator) Lookup(string) string {
	return "中国 广东 深圳"
}

// Lookup 在不应发生的重复查询时直接使测试失败。
func (panicIPLocator) Lookup(string) string {
	panic("不应重复查询 IP 归属地")
}

// Lookup 返回空归属地并累加调用次数。
func (l *countingIPLocator) Lookup(string) string {
	l.calls++
	return ""
}

// EnqueueAdminLog 保存待断言的审计事件，不执行外部投递。
func (f *fakeAdminLogEnqueuer) EnqueueAdminLog(_ context.Context, eventID string, row model.AdminLog) error {
	f.eventIDs = append(f.eventIDs, eventID)
	f.rows = append(f.rows, row)
	return nil
}

// TestSerializeMasksSensitiveFields 验证审计序列化时会自动脱敏密码、token、MFA 秘钥等敏感字段。
func TestSerializeMasksSensitiveFields(t *testing.T) {
	payload := Serialize(map[string]any{
		"password": "secret",
		"token":    "jwt",
		"profile": map[string]any{
			"mfa_secure_key": "abc",
			"nickname":       "tester",
		},
	}, 1024)

	if strings.Contains(payload, "secret") || strings.Contains(payload, "jwt") || strings.Contains(payload, "abc") {
		t.Fatalf("expected sensitive fields to be masked, got %s", payload)
	}
	if !strings.Contains(payload, "***") {
		t.Fatalf("expected masked placeholder in payload: %s", payload)
	}
}

// TestBuildLogEntryUsesRequestMeta 验证审计投递前会自动合并请求链路元数据，并继承 trace、用户和响应结果。
func TestBuildLogEntryUsesRequestMeta(t *testing.T) {
	ctx, _ := requestctx.New(context.Background())
	requestctx.SetTrace(ctx, "trace-1", "span-1")
	requestctx.SetRoute(ctx, "admin.log.query")
	requestctx.SetUser(ctx, 99, "tester", "127.0.0.1")
	requestctx.SetResponse(ctx, 200, 1001, "参数错误", "参数错误")
	requestctx.SetLatency(ctx, 123456*time.Millisecond)

	recorder := NewRecorder(1024)
	recorder.SetIPLocator(fakeIPLocator{})
	entry := recorder.buildLogEntry(ctx, Event{
		Action:   model.ActionAdminLogQuery,
		Method:   "QueryAdminLogHandler",
		Describe: "查询管理员操作日志",
		Data: map[string]any{
			"password": "secret",
			"page":     1,
		},
	})

	if entry.TraceID != "trace-1" || entry.SpanID != "span-1" {
		t.Fatalf("unexpected trace info: %+v", entry)
	}
	if entry.UserID != 99 || entry.UserName != "tester" {
		t.Fatalf("unexpected user info: %+v", entry)
	}
	if entry.Route != "admin.log.query" || entry.HTTPStatus != 200 || entry.BizCode != 1001 {
		t.Fatalf("unexpected route/response info: %+v", entry)
	}
	if entry.LatencyMS != 123456 {
		t.Fatalf("expected latency ms 123456, got %d", entry.LatencyMS)
	}
	if entry.Ipaddr != "中国 广东 深圳" {
		t.Fatalf("expected ip region, got %q", entry.Ipaddr)
	}
	if entry.Success {
		t.Fatalf("expected success=false when error message exists")
	}
	if strings.Contains(entry.Data, "secret") {
		t.Fatalf("expected sanitized data, got %s", entry.Data)
	}
}

// TestBuildLogEntryReusesRequestIPRegion 验证登录链路已解析的归属地会被审计直接复用。
func TestBuildLogEntryReusesRequestIPRegion(t *testing.T) {
	ctx, _ := requestctx.New(context.Background())
	requestctx.SetRequest(ctx, "POST", "/api/auth/login", "114.114.114.114")
	requestctx.SetClientIPRegion(ctx, "114.114.114.114", "中国 江苏 南京")
	recorder := NewRecorder(1024)
	recorder.SetIPLocator(panicIPLocator{})

	entry := recorder.buildLogEntry(ctx, Event{IP: "114.114.114.114"})
	if entry.Ipaddr != "中国 江苏 南京" {
		t.Fatalf("审计未复用请求级 IP 归属地: %q", entry.Ipaddr)
	}
}

// TestBuildLogEntryReusesResolvedEmptyIPRegion 验证登录已查询但未命中时，审计不会重复查询。
func TestBuildLogEntryReusesResolvedEmptyIPRegion(t *testing.T) {
	ctx, _ := requestctx.New(context.Background())
	requestctx.SetRequest(ctx, "POST", "/api/auth/login", "114.114.114.114")
	requestctx.SetClientIPRegion(ctx, "114.114.114.114", "")
	locator := &countingIPLocator{}
	recorder := NewRecorder(1024)
	recorder.SetIPLocator(locator)

	entry := recorder.buildLogEntry(ctx, Event{IP: "114.114.114.114"})
	if entry.Ipaddr != "" || locator.calls != 0 {
		t.Fatalf("已解析空归属地仍重复查询: entry=%+v calls=%d", entry, locator.calls)
	}
}

// TestBuildLogEntryNormalizesExplicitIP 验证显式审计 IP 也使用统一规范文本。
func TestBuildLogEntryNormalizesExplicitIP(t *testing.T) {
	recorder := NewRecorder(1024)
	entry := recorder.buildLogEntry(context.Background(), Event{IP: "fe80::1%very-long-interface-name"})
	if entry.IP != "fe80::1" {
		t.Fatalf("审计 IP=%q, want fe80::1", entry.IP)
	}
}

// TestRecorderRecordEnqueuesAdminLog 验证统一审计入口只投递 Collector，不直接执行单条 DB 写入。
func TestRecorderRecordEnqueuesAdminLog(t *testing.T) {
	recorder := NewRecorder(1024)
	enqueuer := &fakeAdminLogEnqueuer{}
	recorder.SetEnqueuer(enqueuer)

	if err := recorder.Record(context.Background(), Event{
		Action:   model.ActionAdminLogQuery,
		Route:    "admin.log.query",
		Method:   "QueryAdminLogHandler",
		Describe: "查询管理员操作日志",
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if len(enqueuer.rows) != 1 {
		t.Fatalf("期望投递 1 条审计日志，实际 %d", len(enqueuer.rows))
	}
	if len(enqueuer.eventIDs) != 1 || !strings.HasPrefix(enqueuer.eventIDs[0], adminLogEventIDPrefix) || len(enqueuer.eventIDs[0]) > 64 {
		t.Fatalf("投递 event_id 不符合预期: %+v", enqueuer.eventIDs)
	}
	if enqueuer.rows[0].Route != "admin.log.query" {
		t.Fatalf("投递审计日志不符合预期: %+v", enqueuer.rows[0])
	}
}

// TestRecorderRecordRequiresCollector 验证未注入 Collector 时不会回退到同步单条写库。
func TestRecorderRecordRequiresCollector(t *testing.T) {
	recorder := NewRecorder(1024)
	err := recorder.Record(context.Background(), Event{Action: model.ActionAdminLogQuery})
	if err == nil || !strings.Contains(err.Error(), "Collector 未初始化") {
		t.Fatalf("Record() error = %v, want Collector 未初始化", err)
	}
}
