package cdcx

import "testing"

func TestDecodeDebeziumEvent(t *testing.T) {
	raw := []byte(`{"before":null,"after":{"id":1},"source":{"db":"admin","table":"admin_log","file":"mysql-bin.000001","pos":128,"row":1,"server_id":223344,"gtid":"g1","snapshot":false,"ts_ms":1782311237336},"op":"c"}`)
	key := []byte(`{"schema":{"type":"struct"},"payload":{"id":1}}`)
	event, err := DecodeDebeziumEvent("dnmp-admin.admin.admin_log", 2, 9, key, raw)
	if err != nil {
		t.Fatalf("DecodeDebeziumEvent() error = %v", err)
	}
	if event.TableKey() != "admin.admin_log" {
		t.Fatalf("TableKey() = %q", event.TableKey())
	}
	if event.Operation != OperationCreate {
		t.Fatalf("Operation = %q", event.Operation)
	}
	if len(event.After) == 0 || len(event.Before) != 0 {
		t.Fatalf("After/Before 不符合预期 after=%s before=%s", event.After, event.Before)
	}
	if event.SourceTime.IsZero() {
		t.Fatal("SourceTime 不应为空")
	}
	if string(event.PrimaryKey) != `{"id":1}` {
		t.Fatalf("PrimaryKey = %s", event.PrimaryKey)
	}
	if event.EventKey() != "dnmp-admin.admin.admin_log:2:9" {
		t.Fatalf("EventKey() = %q", event.EventKey())
	}
	if event.Source.File != "mysql-bin.000001" || event.Source.Position != 128 || event.Source.Snapshot != "false" {
		t.Fatalf("Source 不符合预期: %+v", event.Source)
	}
}

func TestDecodeDebeziumEventRejectsMissingOp(t *testing.T) {
	_, err := DecodeDebeziumEvent("topic", 0, 1, nil, []byte(`{"after":{"id":1},"source":{"db":"admin","table":"admin_log"}}`))
	if err == nil {
		t.Fatal("缺少 op 应返回错误")
	}
}

func TestEventRowData(t *testing.T) {
	createEvent := Event{Operation: OperationCreate, After: []byte(`{"id":1}`), Before: []byte(`{"id":0}`)}
	if string(createEvent.RowData()) != `{"id":1}` {
		t.Fatalf("create RowData() = %s", createEvent.RowData())
	}
	deleteEvent := Event{Operation: OperationDelete, After: []byte(`{"id":1}`), Before: []byte(`{"id":0}`)}
	if string(deleteEvent.RowData()) != `{"id":0}` {
		t.Fatalf("delete RowData() = %s", deleteEvent.RowData())
	}
}
