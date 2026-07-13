package user

import (
	"testing"
	"time"

	"admin/internal/types"
	"admin/pkg/excel"
)

// TestUserExportCompletedReuseRemaining 验证完成结果只在完成后的 20 分钟内复用。
func TestUserExportCompletedReuseRemaining(t *testing.T) {
	finishedAt := time.Date(2026, 7, 14, 18, 0, 0, 0, time.Local)
	status := &types.UserExportStatusResp{
		Status:     types.UserExportStatusSucceeded,
		FinishedAt: excel.FormatDateTime(finishedAt),
	}
	if ttl := userExportCompletedReuseRemaining(status, finishedAt.Add(19*time.Minute)); ttl != time.Minute {
		t.Fatalf("reuse ttl = %s, want 1m", ttl)
	}
	if ttl := userExportCompletedReuseRemaining(status, finishedAt.Add(20*time.Minute)); ttl > 0 {
		t.Fatalf("expired reuse ttl = %s, want <= 0", ttl)
	}
}

// TestUserExportStatusRemaining 验证完成状态和下载入口从完成时间起保留 24 小时。
func TestUserExportStatusRemaining(t *testing.T) {
	finishedAt := time.Date(2026, 7, 14, 18, 0, 0, 0, time.Local)
	status := &types.UserExportStatusResp{
		Status:     types.UserExportStatusSucceeded,
		FinishedAt: excel.FormatDateTime(finishedAt),
	}
	if ttl := userExportStatusRemaining(status, finishedAt.Add(23*time.Hour)); ttl != time.Hour {
		t.Fatalf("status ttl = %s, want 1h", ttl)
	}
	status.Status = types.UserExportStatusRunning
	if ttl := userExportStatusRemaining(status, finishedAt.Add(23*time.Hour)); ttl != userExportStatusTTL {
		t.Fatalf("running status ttl = %s, want %s", ttl, userExportStatusTTL)
	}
}

// TestResetUserExportFiles 验证失败重试不会继续复用已删除的旧文件状态。
func TestResetUserExportFiles(t *testing.T) {
	status := &types.UserExportStatusResp{
		Files:         []types.UserExportFileItem{{PartNo: 1, ObjectKey: "user-export/demo.xlsx"}},
		PartCount:     1,
		DownloadReady: true,
		Processed:     100,
		Total:         100,
		Progress:      100,
		LastCursorID:  100,
	}
	resetUserExportFiles(status)
	if len(status.Files) != 0 || status.PartCount != 0 || status.DownloadReady || status.Processed != 0 || status.Total != 0 || status.Progress != 0 || status.LastCursorID != 0 {
		t.Fatalf("reset status = %+v", status)
	}
}

// TestBuildUserExportPartStorageIDIsUnique 验证失败重试不会覆盖前一次仍待清理的对象。
func TestBuildUserExportPartStorageIDIsUnique(t *testing.T) {
	first := buildUserExportPartStorageID("job-1", 1)
	second := buildUserExportPartStorageID("job-1", 1)
	if first == second {
		t.Fatalf("storage id duplicated: %s", first)
	}
}
