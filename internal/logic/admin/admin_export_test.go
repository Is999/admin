package admin

import (
	"testing"

	"admin/internal/types"
)

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
