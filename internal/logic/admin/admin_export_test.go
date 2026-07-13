package admin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"admin/internal/config"
	"admin/internal/svc"
	"admin/internal/types"
	"admin/pkg/excel"
	"admin/pkg/storage"
)

// TestBuildAdminExportRequestFingerprintStable 验证对应场景符合预期。
func TestBuildAdminExportRequestFingerprintStable(t *testing.T) {
	roleID := 3
	left := buildAdminExportRequestFingerprint(&types.AdminExportReq{
		Username: " admin ",
		RealName: " tester ",
		Status:   new(1),
		RoleID:   &roleID,
	})
	right := buildAdminExportRequestFingerprint(&types.AdminExportReq{
		Username: "admin",
		RealName: "tester",
		Status:   new(1),
		RoleID:   &roleID,
	})
	if left != right {
		t.Fatalf("buildAdminExportRequestFingerprint 结果不稳定: left=%s right=%s", left, right)
	}
}

// TestAddAuthorizedAdmin 验证对应场景符合预期。
func TestAddAuthorizedAdmin(t *testing.T) {
	status := &types.AdminExportStatusResp{
		AuthorizedAdminIDs: []int{1},
	}
	if !addAuthorizedAdmin(status, 2) {
		t.Fatalf("addAuthorizedAdmin 新增管理员失败")
	}
	if addAuthorizedAdmin(status, 2) {
		t.Fatalf("addAuthorizedAdmin 不应重复添加同一管理员")
	}
	if len(status.AuthorizedAdminIDs) != 2 {
		t.Fatalf("addAuthorizedAdmin 数量不正确: got=%d", len(status.AuthorizedAdminIDs))
	}
}

// TestAdminExportStatusSnapshotRoundTrip 验证对应场景符合预期。
func TestAdminExportStatusSnapshotRoundTrip(t *testing.T) {
	status := &types.AdminExportStatusResp{
		JobID:              "job-1",
		TaskID:             "task-1",
		Queue:              "maintenance",
		Status:             types.AdminExportStatusSucceeded,
		FileName:           "admin.xlsx",
		DownloadReady:      true,
		FilePath:           "/tmp/admin.xlsx",
		ObjectKey:          "admin-export/202605/03/admin.xlsx",
		StorageType:        "local",
		ContentType:        "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		ProcessAt:          "2026-05-03 03:00:00",
		LastCursorID:       123,
		OperatorID:         1,
		RequestFingerprint: "fingerprint",
		AuthorizedAdminIDs: []int{1, 2},
	}

	snapshot := newAdminExportStatusSnapshot(status)
	restored := snapshot.toStatus()

	if restored.FilePath != status.FilePath {
		t.Fatalf("FilePath 未正确还原: got=%s want=%s", restored.FilePath, status.FilePath)
	}
	if restored.ObjectKey != status.ObjectKey {
		t.Fatalf("ObjectKey 未正确还原: got=%s want=%s", restored.ObjectKey, status.ObjectKey)
	}
	if restored.StorageType != status.StorageType {
		t.Fatalf("StorageType 未正确还原: got=%s want=%s", restored.StorageType, status.StorageType)
	}
	if restored.ContentType != status.ContentType {
		t.Fatalf("ContentType 未正确还原: got=%s want=%s", restored.ContentType, status.ContentType)
	}
	if restored.LastCursorID != status.LastCursorID {
		t.Fatalf("LastCursorID 未正确还原: got=%d want=%d", restored.LastCursorID, status.LastCursorID)
	}
	if restored.OperatorID != status.OperatorID {
		t.Fatalf("OperatorID 未正确还原: got=%d want=%d", restored.OperatorID, status.OperatorID)
	}
	if restored.RequestFingerprint != status.RequestFingerprint {
		t.Fatalf("RequestFingerprint 未正确还原: got=%s want=%s", restored.RequestFingerprint, status.RequestFingerprint)
	}
	if len(restored.AuthorizedAdminIDs) != len(status.AuthorizedAdminIDs) {
		t.Fatalf("AuthorizedAdminIDs 数量不正确: got=%d want=%d", len(restored.AuthorizedAdminIDs), len(status.AuthorizedAdminIDs))
	}
}

// TestApplyAdminExportProgress 验证管理员导出更新真实百分比、速度和预计剩余时间。
func TestApplyAdminExportProgress(t *testing.T) {
	startedAt := time.Date(2026, 7, 16, 12, 0, 0, 0, time.Local)
	status := &types.AdminExportStatusResp{Total: 1000000}
	applyAdminExportProgress(status, startedAt, excel.ExportProgress{
		Processed:       600000,
		LastProcessedAt: startedAt.Add(10 * time.Second),
	})
	if status.Processed != 600000 || status.Progress != 60 {
		t.Fatalf("progress = %d processed = %d, want 60/600000", status.Progress, status.Processed)
	}
	if status.AverageRowsPerSec != 60000 || status.EstimatedSeconds != 7 {
		t.Fatalf("speed = %d eta = %d, want 60000/7", status.AverageRowsPerSec, status.EstimatedSeconds)
	}

	applyAdminExportProgress(status, startedAt, excel.ExportProgress{
		Processed:       1000000,
		LastProcessedAt: startedAt.Add(20 * time.Second),
	})
	if status.Progress != adminExportRunningMaxProgress || status.EstimatedSeconds != 0 {
		t.Fatalf("finished running progress = %d eta = %d, want 99/0", status.Progress, status.EstimatedSeconds)
	}

	unknown := &types.AdminExportStatusResp{}
	applyAdminExportProgress(unknown, startedAt, excel.ExportProgress{
		Processed:       200,
		LastProcessedAt: startedAt.Add(2 * time.Second),
	})
	if unknown.Progress != 0 || unknown.AverageRowsPerSec != 100 || unknown.EstimatedSeconds != 0 {
		t.Fatalf("unknown total progress = %+v, want progress 0 speed 100 eta 0", unknown)
	}
}

// TestAdminExportRetryKeepsNewObject 验证最终状态保存失败后的重试不会被旧清理任务误删。
func TestAdminExportRetryKeepsNewObject(t *testing.T) {
	rootDir := t.TempDir()
	filePath := filepath.Join(rootDir, "admin.xlsx")
	if err := os.WriteFile(filePath, []byte("admin export"), 0o600); err != nil {
		t.Fatalf("创建管理员导出测试文件失败: %v", err)
	}
	svcCtx := svc.NewServiceContext(config.Config{
		FileStorage: config.FileStorageConfig{
			Type: storage.TypeLocal,
			Local: config.FileStorageLocalConfig{
				RootDir: filepath.Join(rootDir, "objects"),
			},
		},
	}, svc.Dependencies{})
	logicObj := NewAdminExportLogicWithContext(context.Background(), svcCtx)
	status := &types.AdminExportStatusResp{
		JobID:    "same-job-id",
		FileName: "admin.xlsx",
		FilePath: filePath,
	}

	// 第一次对象已经投递延迟清理，但最终状态保存失败，因此相同任务会重新执行持久化。
	if err := logicObj.persistExportObject(status); err != nil {
		t.Fatalf("首次持久化管理员导出对象失败: %v", err)
	}
	firstObjectKey := status.ObjectKey
	if err := logicObj.persistExportObject(status); err != nil {
		t.Fatalf("重试持久化管理员导出对象失败: %v", err)
	}
	secondObjectKey := status.ObjectKey
	if firstObjectKey == secondObjectKey {
		t.Fatalf("重试覆盖了旧对象: object_key=%s", firstObjectKey)
	}

	if err := logicObj.CleanupExportFile(&types.AdminExportCleanupTaskPayload{
		JobID:     status.JobID,
		ObjectKey: firstObjectKey,
	}); err != nil {
		t.Fatalf("执行旧管理员导出清理任务失败: %v", err)
	}
	objectStorage, err := svcCtx.ObjectStorage()
	if err != nil {
		t.Fatalf("获取管理员导出对象存储失败: %v", err)
	}
	objectStream, err := objectStorage.Open(context.Background(), storage.OpenObjectReq{ObjectKey: secondObjectKey})
	if err != nil {
		t.Fatalf("旧清理任务误删了重试成功对象: %v", err)
	}
	if err := objectStream.Reader.Close(); err != nil {
		t.Fatalf("关闭管理员导出对象读取流失败: %v", err)
	}
}
