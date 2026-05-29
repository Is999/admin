package runtimeconfig

import (
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestCheckRuntimeConfigUpdatedRejectsMissingDraft(t *testing.T) {
	err := checkRuntimeConfigUpdated(&gorm.DB{RowsAffected: 0}, 42, "周期任务草稿")
	if err == nil || !strings.Contains(err.Error(), "周期任务草稿不存在: 42") {
		t.Fatalf("checkRuntimeConfigUpdated() error = %v", err)
	}
}

func TestCheckRuntimeConfigUpdatedPropagatesDatabaseError(t *testing.T) {
	want := errors.New("db down")
	err := checkRuntimeConfigUpdated(&gorm.DB{Error: want, RowsAffected: 1}, 42, "归档任务草稿")
	if err == nil || !strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("checkRuntimeConfigUpdated() error = %v", err)
	}
}

func TestCheckRuntimeConfigUpdatedAcceptsAffectedRow(t *testing.T) {
	if err := checkRuntimeConfigUpdated(&gorm.DB{RowsAffected: 1}, 42, "周期任务草稿"); err != nil {
		t.Fatalf("checkRuntimeConfigUpdated() error = %v", err)
	}
}
