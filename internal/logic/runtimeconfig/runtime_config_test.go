package runtimeconfig

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"admin/internal/config"
	"admin/internal/jobs/archive"
	"admin/internal/model"
	"admin/internal/svc"
	tasklimits "admin/internal/task/limits"
	"admin/internal/types"

	"gorm.io/gorm"
)

// runtimeConfigTaskQueueStub 注入运行配置测试需要的周期任务校验结果。
type runtimeConfigTaskQueueStub struct {
	svc.TaskQueue       // 其它任务能力不参与当前测试
	validationErr error // validationErr 是周期任务运行时校验错误
	validateCalls int   // validateCalls 记录周期任务校验调用次数
}

// IsEnabled 返回测试任务系统启用状态。
func (s *runtimeConfigTaskQueueStub) IsEnabled() bool { return true }

// ValidatePeriodicTaskConfigs 返回注入的运行时校验结果。
func (s *runtimeConfigTaskQueueStub) ValidatePeriodicTaskConfigs([]config.TaskPeriodicConfig) error {
	s.validateCalls++
	return s.validationErr
}

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

// TestArchiveProgressToRespPreservesWatermarkAndEstimate 验证执行详情映射保留水位、滞后和区间估算进度。
func TestArchiveProgressToRespPreservesWatermarkAndEstimate(t *testing.T) {
	now := time.Date(2026, time.August, 1, 9, 30, 0, 0, time.Local)
	estimate := 50.0
	resp := archiveProgressToResp(5, archive.Progress{
		JobName:        "admin_log",
		RuntimeMatched: true,
		RuntimeEnabled: true,
		SchemaReady:    true,
		Phase:          archive.ProgressPhaseRunning,
		WatermarkTime:  sql.NullTime{Time: now.Add(-24 * time.Hour), Valid: true},
		LagSeconds:     sql.NullInt64{Int64: 86_400, Valid: true},
		CurrentSegment: &archive.ProgressSegment{
			ID:                       18,
			Status:                   "running",
			RangeStart:               now.Add(-24 * time.Hour),
			RangeEnd:                 now,
			LastArchivedID:           9_007_199_254_740_993,
			EstimatedProgressPercent: &estimate,
		},
		FetchedAt: now,
	})
	if resp.JobID != 5 || resp.JobName != "admin_log" || resp.WatermarkTime == "" {
		t.Fatalf("执行详情基础字段映射异常: %+v", resp)
	}
	if resp.LagSeconds == nil || *resp.LagSeconds != 86_400 {
		t.Fatalf("LagSeconds=%v want 86400", resp.LagSeconds)
	}
	if resp.CurrentSegment == nil || resp.CurrentSegment.EstimatedProgressPercent == nil || *resp.CurrentSegment.EstimatedProgressPercent != 50 {
		t.Fatalf("CurrentSegment=%+v want estimate 50", resp.CurrentSegment)
	}
	if resp.CurrentSegment.LastArchivedID != "9007199254740993" {
		t.Fatalf("LastArchivedID=%q want exact bigint string", resp.CurrentSegment.LastArchivedID)
	}
	if resp.RecentSegments == nil {
		t.Fatal("RecentSegments 应返回空数组而不是 null")
	}
	segmentJSON, err := json.Marshal(archiveSegmentToItem(archive.ProgressSegment{}))
	if err != nil {
		t.Fatalf("序列化空归档区间失败: %v", err)
	}
	if !strings.Contains(string(segmentJSON), `"estimatedProgressPercent":null`) {
		t.Fatalf("非复制阶段应显式返回 null 估算进度: %s", segmentJSON)
	}
}

// TestValidateSnapshotRejectsPeriodicResourceOverridesAboveHardLimits 校验发布预检不会接受无界周期任务参数。
func TestValidateSnapshotRejectsPeriodicResourceOverridesAboveHardLimits(t *testing.T) {
	base := config.TaskPeriodicConfig{
		Name:     "oversized-resource",
		Cron:     "0 * * * *",
		Workflow: "demo.workflow",
	}
	tests := []struct {
		name    string                           // 测试场景
		update  func(*config.TaskPeriodicConfig) // 设置越界参数
		wantErr string                           // 期望错误片段
	}{
		{
			name: "retry",
			update: func(item *config.TaskPeriodicConfig) {
				item.Retry = tasklimits.MaxRetry + 1
			},
			wantErr: "retry",
		},
		{
			name: "timeout",
			update: func(item *config.TaskPeriodicConfig) {
				item.TimeoutSeconds = tasklimits.MaxTimeoutSeconds + 1
			},
			wantErr: "timeout_seconds",
		},
		{
			name: "shard total",
			update: func(item *config.TaskPeriodicConfig) {
				item.ShardTotal = tasklimits.MaxShardTotal + 1
			},
			wantErr: "shard_total",
		},
		{
			name: "unique ttl",
			update: func(item *config.TaskPeriodicConfig) {
				item.UniqueTTLSeconds = tasklimits.MaxUniqueTTLSeconds + 1
			},
			wantErr: "unique_ttl_seconds",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := base
			tt.update(&item)
			_, err := ValidateSnapshot(ReleaseSnapshot{TaskPeriodic: []config.TaskPeriodicConfig{item}})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateSnapshot() error = %v, want %q limit", err, tt.wantErr)
			}
		})
	}
}

// TestValidateSnapshotRejectsTooManyPeriodicTasks 校验发布快照不会绕过周期任务总量上限。
func TestValidateSnapshotRejectsTooManyPeriodicTasks(t *testing.T) {
	items := make([]config.TaskPeriodicConfig, tasklimits.MaxPeriodicCount+1)
	if _, err := ValidateSnapshot(ReleaseSnapshot{TaskPeriodic: items}); err == nil || !strings.Contains(err.Error(), "周期任务不能超过") {
		t.Fatalf("ValidateSnapshot() error = %v, want periodic count limit", err)
	}
}

// TestPublishSnapshotRejectsRuntimeInvalidPeriodicConfig 验证发布写库前会执行任务运行时校验。
func TestPublishSnapshotRejectsRuntimeInvalidPeriodicConfig(t *testing.T) {
	validator := &runtimeConfigTaskQueueStub{validationErr: errors.New("workflow shard_total max=1")}
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{})
	svcCtx.Task = validator
	logicObj := NewRuntimeConfigLogicWithContext(context.Background(), svcCtx)
	snapshot := ReleaseSnapshot{TaskPeriodic: []config.TaskPeriodicConfig{{
		Name:       "single-shard-periodic",
		Cron:       "*/5 * * * *",
		Workflow:   "single-shard.workflow",
		ShardTotal: 2,
	}}}

	if _, err := logicObj.publishSnapshot(snapshot, "test", 0); err == nil || !strings.Contains(err.Error(), "max=1") {
		t.Fatalf("publishSnapshot() error = %v, want runtime validation error", err)
	}
	if validator.validateCalls != 1 {
		t.Fatalf("运行时周期任务校验调用次数 = %d, want 1", validator.validateCalls)
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

// TestEncodeReleaseSnapshotNormalizesDefaults 验证概览、预检和发布共用的快照编码会先补齐默认值。
func TestEncodeReleaseSnapshotNormalizesDefaults(t *testing.T) {
	snapshot, jsonText, _, checksum, err := encodeReleaseSnapshot(ReleaseSnapshot{
		ArchiveJobs: []config.ArchiveJobConfig{
			{Name: "admin_log", TableName: "admin_log", HotKeepDays: 32},
		},
		TaskPeriodic: []config.TaskPeriodicConfig{
			{Name: "archive-admin-log-hourly", Cron: "5 * * * *", Workflow: "archive.run"},
		},
	})
	if err != nil {
		t.Fatalf("encodeReleaseSnapshot() error = %v", err)
	}
	if checksum == "" {
		t.Fatal("checksum 不能为空")
	}
	if snapshot.ArchiveJobs[0].ArchiveDelayDays != 32 || snapshot.ArchiveJobs[0].DeleteDelayDays != 32 {
		t.Fatalf("归档默认值未补齐: %+v", snapshot.ArchiveJobs[0])
	}
	if snapshot.TaskPeriodic[0].Enabled == nil || !*snapshot.TaskPeriodic[0].Enabled {
		t.Fatalf("周期任务默认启用未补齐: %+v", snapshot.TaskPeriodic[0].Enabled)
	}
	if !strings.Contains(jsonText, `"archive_delay_days":32`) || !strings.Contains(jsonText, `"enabled":true`) {
		t.Fatalf("encoded snapshot missing normalized defaults: %s", jsonText)
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

// TestRuntimeConfigReloadMatchesRelease 验证只有完整匹配本次发布的重载回执才能标记已应用。
func TestRuntimeConfigReloadMatchesRelease(t *testing.T) {
	// release 表示本次已持久化的发布记录。
	release := model.RuntimeConfigRelease{ID: 13, VersionNo: 7, Checksum: "checksum-7"}
	// tests 覆盖无回执和各个字段不匹配的场景。
	tests := []struct {
		name   string                        // name 表示测试场景。
		reload svc.RuntimeConfigReloadResult // reload 表示运行态重载回执。
		want   bool                          // want 表示是否应标记已应用。
	}{
		{name: "missing receipt"},
		{name: "release mismatch", reload: svc.RuntimeConfigReloadResult{ReleaseID: 12, VersionNo: 7, Checksum: "checksum-7"}},
		{name: "version mismatch", reload: svc.RuntimeConfigReloadResult{ReleaseID: 13, VersionNo: 6, Checksum: "checksum-7"}},
		{name: "checksum mismatch", reload: svc.RuntimeConfigReloadResult{ReleaseID: 13, VersionNo: 7, Checksum: "checksum-6"}},
		{name: "exact match", reload: svc.RuntimeConfigReloadResult{ReleaseID: 13, VersionNo: 7, Checksum: "checksum-7"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtimeConfigReloadMatchesRelease(release, tt.reload); got != tt.want {
				t.Fatalf("runtimeConfigReloadMatchesRelease() = %v, want %v", got, tt.want)
			}
		})
	}
}
