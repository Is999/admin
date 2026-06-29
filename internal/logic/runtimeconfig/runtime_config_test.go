package runtimeconfig

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"admin/internal/config"
	"admin/internal/types"

	"gorm.io/gorm"
)

// TestCheckRuntimeConfigUpdatedRejectsMissingDraft 验证更新草稿不存在时返回明确错误。
func TestCheckRuntimeConfigUpdatedRejectsMissingDraft(t *testing.T) {
	err := checkRuntimeConfigUpdated(&gorm.DB{RowsAffected: 0}, 42, "周期任务草稿")
	if err == nil || !strings.Contains(err.Error(), "周期任务草稿不存在: 42") {
		t.Fatalf("checkRuntimeConfigUpdated() error = %v", err)
	}
}

// TestCheckRuntimeConfigUpdatedPropagatesDatabaseError 验证数据库错误不会被行数判断吞掉。
func TestCheckRuntimeConfigUpdatedPropagatesDatabaseError(t *testing.T) {
	want := errors.New("db down")
	err := checkRuntimeConfigUpdated(&gorm.DB{Error: want, RowsAffected: 1}, 42, "归档任务草稿")
	if err == nil || !strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("checkRuntimeConfigUpdated() error = %v", err)
	}
}

// TestCheckRuntimeConfigUpdatedAcceptsAffectedRow 验证成功更新一行草稿时不返回错误。
func TestCheckRuntimeConfigUpdatedAcceptsAffectedRow(t *testing.T) {
	if err := checkRuntimeConfigUpdated(&gorm.DB{RowsAffected: 1}, 42, "周期任务草稿"); err != nil {
		t.Fatalf("checkRuntimeConfigUpdated() error = %v", err)
	}
}

// TestRuntimeConfigSnapshotEmpty 验证运行时快照空值判断覆盖周期任务和归档任务。
func TestRuntimeConfigSnapshotEmpty(t *testing.T) {
	if !runtimeConfigSnapshotEmpty(ReleaseSnapshot{}) {
		t.Fatal("空快照应判定为空")
	}
	if runtimeConfigSnapshotEmpty(ReleaseSnapshot{TaskPeriodic: []config.TaskPeriodicConfig{{Name: "demo"}}}) {
		t.Fatal("包含周期任务的快照不应判定为空")
	}
	if runtimeConfigSnapshotEmpty(ReleaseSnapshot{ArchiveJobs: []config.ArchiveJobConfig{{Name: "archive"}}}) {
		t.Fatal("包含归档任务的快照不应判定为空")
	}
}

// TestInitialReleaseSnapshotPrefersDraftTables 验证首次启动优先发布迁移种下的草稿表。
func TestInitialReleaseSnapshotPrefersDraftTables(t *testing.T) {
	draft := ReleaseSnapshot{TaskPeriodic: []config.TaskPeriodicConfig{{Name: "draft-periodic"}}}
	file := ReleaseSnapshot{TaskPeriodic: []config.TaskPeriodicConfig{{Name: "file-periodic"}}}
	got, replaceDraft, remark := initialReleaseSnapshot(draft, file)
	if replaceDraft {
		t.Fatal("草稿表已有数据时不应被文件快照覆盖")
	}
	if remark != "bootstrap publish runtime config draft" {
		t.Fatalf("remark = %q, want draft remark", remark)
	}
	if len(got.TaskPeriodic) != 1 || got.TaskPeriodic[0].Name != "draft-periodic" {
		t.Fatalf("initialReleaseSnapshot() = %+v, want draft snapshot", got)
	}
}

// TestInitialReleaseSnapshotFallsBackToFileSeed 验证草稿为空时仍兼容旧的文件首次导入。
func TestInitialReleaseSnapshotFallsBackToFileSeed(t *testing.T) {
	file := ReleaseSnapshot{ArchiveJobs: []config.ArchiveJobConfig{{Name: "file-archive"}}}
	got, replaceDraft, remark := initialReleaseSnapshot(ReleaseSnapshot{}, file)
	if !replaceDraft {
		t.Fatal("使用文件种子时应先写回草稿表")
	}
	if remark != "bootstrap import current runtime config" {
		t.Fatalf("remark = %q, want file import remark", remark)
	}
	if len(got.ArchiveJobs) != 1 || got.ArchiveJobs[0].Name != "file-archive" {
		t.Fatalf("initialReleaseSnapshot() = %+v, want file snapshot", got)
	}
}

// TestInitialReleaseSnapshotEmptySources 验证文件和草稿都为空时保留明确失败分支。
func TestInitialReleaseSnapshotEmptySources(t *testing.T) {
	got, replaceDraft, remark := initialReleaseSnapshot(ReleaseSnapshot{}, ReleaseSnapshot{})
	if !runtimeConfigSnapshotEmpty(got) {
		t.Fatalf("initialReleaseSnapshot() = %+v, want empty snapshot", got)
	}
	if replaceDraft || remark != "" {
		t.Fatalf("replaceDraft=%v remark=%q, want empty metadata", replaceDraft, remark)
	}
}

// TestStateCacheDecodesTableCacheHashStrings 验证 table-cache 字符串哈希值可解码为状态缓存。
func TestStateCacheDecodesTableCacheHashStrings(t *testing.T) {
	payload, err := json.Marshal(map[string]string{
		"active_release_id": "1",
		"active_version":    "2",
		"active_checksum":   "abc",
		"published_at_unix": "1782215750",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var got StateCache
	if err = json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("json.Unmarshal(StateCache) error = %v", err)
	}
	if got.ActiveReleaseID != 1 || got.ActiveVersion != 2 || got.ActiveChecksum != "abc" || got.PublishedAtUnix != 1782215750 {
		t.Fatalf("StateCache decoded mismatch: %+v", got)
	}
}

// TestArchiveConfigToModelDefaultsDelayDays 验证归档配置入库时删除延迟天数默认跟随热数据保留天数。
func TestArchiveConfigToModelDefaultsDelayDays(t *testing.T) {
	row := archiveConfigToModel(config.ArchiveJobConfig{
		Name:              "admin_log",
		TableName:         "admin_log",
		HotKeepDays:       32,
		ArchiveDelayDays:  2,
		DeleteDelayDays:   0,
		DeleteBatchSize:   1000,
		MaxHistoryTables:  12,
		ArchiveWindowMode: "auto",
	}, 0, 0)
	if row.ArchiveDelayDays != 2 {
		t.Fatalf("ArchiveDelayDays=%d want 2", row.ArchiveDelayDays)
	}
	if row.DeleteDelayDays != 32 {
		t.Fatalf("DeleteDelayDays=%d want 32", row.DeleteDelayDays)
	}
}

// TestCurrentSnapshotFromConfigDefaultsArchiveDelayDays 验证静态配置生成快照时补齐归档延迟默认值。
func TestCurrentSnapshotFromConfigDefaultsArchiveDelayDays(t *testing.T) {
	snapshot := CurrentSnapshotFromConfig(config.Config{
		Archive: config.ArchiveConfig{Jobs: []config.ArchiveJobConfig{
			{Name: "admin_log", TableName: "admin_log", HotKeepDays: 32},
		}},
	})
	if len(snapshot.ArchiveJobs) != 1 {
		t.Fatalf("ArchiveJobs len=%d want 1", len(snapshot.ArchiveJobs))
	}
	job := snapshot.ArchiveJobs[0]
	if job.ArchiveDelayDays != 32 {
		t.Fatalf("ArchiveDelayDays=%d want 32", job.ArchiveDelayDays)
	}
	if job.DeleteDelayDays != 32 {
		t.Fatalf("DeleteDelayDays=%d want 32", job.DeleteDelayDays)
	}
}

// TestCurrentSnapshotFromConfigDefaultsPeriodicEnabled 验证周期任务快照缺省 enabled 时按启用处理。
func TestCurrentSnapshotFromConfigDefaultsPeriodicEnabled(t *testing.T) {
	snapshot := CurrentSnapshotFromConfig(config.Config{
		Task: config.TaskQueueConfig{Periodic: []config.TaskPeriodicConfig{
			{Name: "archive-admin-log-hourly", Cron: "5 * * * *", Workflow: "archive.run"},
		}},
	})
	if len(snapshot.TaskPeriodic) != 1 {
		t.Fatalf("TaskPeriodic len=%d want 1", len(snapshot.TaskPeriodic))
	}
	if snapshot.TaskPeriodic[0].Enabled == nil || !*snapshot.TaskPeriodic[0].Enabled {
		t.Fatalf("期望周期任务默认启用，实际=%v", snapshot.TaskPeriodic[0].Enabled)
	}
}

// TestEncodeSnapshotUsesNormalizedArchiveDelayDays 验证编码快照前会写入归一化后的归档延迟字段。
func TestEncodeSnapshotUsesNormalizedArchiveDelayDays(t *testing.T) {
	snapshot := normalizeReleaseSnapshot(ReleaseSnapshot{ArchiveJobs: []config.ArchiveJobConfig{
		{Name: "admin_log", TableName: "admin_log", HotKeepDays: 32},
	}})
	jsonText, _, _, err := EncodeSnapshot(snapshot)
	if err != nil {
		t.Fatalf("EncodeSnapshot() error = %v", err)
	}
	if !strings.Contains(jsonText, `"archive_delay_days":32`) || !strings.Contains(jsonText, `"delete_delay_days":32`) {
		t.Fatalf("encoded snapshot missing normalized delay days: %s", jsonText)
	}
}

// TestPeriodicConfigToModelDefaultsEnabled 验证运行配置首次导入草稿时周期任务默认启用。
func TestPeriodicConfigToModelDefaultsEnabled(t *testing.T) {
	row := periodicConfigToModel(config.TaskPeriodicConfig{
		Name:     "archive-admin-log-hourly",
		Cron:     "5 * * * *",
		Workflow: "archive.run",
	}, 7, 0)
	if !row.Enabled {
		t.Fatal("期望缺省 enabled 的周期任务导入草稿时默认启用")
	}
}

// TestArchiveReqToModelDefaultsDelayDays 验证后台保存归档任务时补齐归档和删除延迟默认值。
func TestArchiveReqToModelDefaultsDelayDays(t *testing.T) {
	row := archiveReqToModel(&types.SaveRuntimeArchiveJobReq{
		Name:        "admin_log",
		TableName:   "admin_log",
		HotKeepDays: 45,
	}, 7)
	if row.ArchiveDelayDays != 45 {
		t.Fatalf("ArchiveDelayDays=%d want 45", row.ArchiveDelayDays)
	}
	if row.DeleteDelayDays != 45 {
		t.Fatalf("DeleteDelayDays=%d want 45", row.DeleteDelayDays)
	}
}
