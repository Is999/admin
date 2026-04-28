package main

import (
	"bytes"
	"strings"
	"testing"

	"admin/internal/database"
)

// TestResolveMigrationActionDefault 验证空 action 默认只查看迁移状态。
func TestResolveMigrationActionDefault(t *testing.T) {
	got, err := resolveMigrationAction(" ")
	if err != nil {
		t.Fatalf("resolve migration action failed: %v", err)
	}
	if got != actionStatus {
		t.Fatalf("expected default action %q, got %q", actionStatus, got)
	}
}

// TestResolveMigrationActionNormalizes 验证 action 会去空格并统一小写。
func TestResolveMigrationActionNormalizes(t *testing.T) {
	got, err := resolveMigrationAction(" UP ")
	if err != nil {
		t.Fatalf("resolve migration action failed: %v", err)
	}
	if got != actionUp {
		t.Fatalf("expected normalized action %q, got %q", actionUp, got)
	}
}

// TestResolveMigrationActionRejectsUnknown 验证未知 action 会被拒绝，避免误执行。
func TestResolveMigrationActionRejectsUnknown(t *testing.T) {
	if _, err := resolveMigrationAction("rollback"); err == nil {
		t.Fatal("expected unknown action to be rejected")
	}
}

// TestPrintResults 验证迁移结果输出包含状态、资产和拦截原因。
func TestPrintResults(t *testing.T) {
	var output bytes.Buffer
	err := printResults(&output, []database.MigrationRunItem{
		{
			Version: "202606050001",
			Name:    "bootstrap_admin_table",
			Asset:   "admin.sql",
			Status:  database.MigrationStatusBlocked,
			Reason:  "bootstrap-only 迁移需要显式允许",
		},
	})
	if err != nil {
		t.Fatalf("print migration results failed: %v", err)
	}
	text := output.String()
	for _, want := range []string{"STATUS", "202606050001", "admin.sql", "bootstrap-only 迁移需要显式允许"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, text)
		}
	}
}

// TestPrintResultsRejectsNilWriter 验证输出目标为空时返回错误。
func TestPrintResultsRejectsNilWriter(t *testing.T) {
	if err := printResults(nil, nil); err == nil {
		t.Fatal("expected nil output writer to be rejected")
	}
}
