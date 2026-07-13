package user

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	i18n "admin/common/i18n"
	"admin/internal/config"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"
	"admin/pkg/excel"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestBuildUserProfileUpdatesKeepsExplicitEmptyValue 验证空字符串是显式清空资料，不应被忽略。
func TestBuildUserProfileUpdatesKeepsExplicitEmptyValue(t *testing.T) {
	nickname := "  新昵称  "
	email := "  "
	req := &types.UpdateUserReq{
		Nickname: &nickname,
		Email:    &email,
	}
	updates := buildUserProfileUpdates(req)
	if updates["nickname"] != "新昵称" {
		t.Fatalf("nickname update = %v, want trimmed nickname", updates["nickname"])
	}
	if value, ok := updates["email"]; !ok || value != "" {
		t.Fatalf("email update = %v ok=%v, want explicit empty value", value, ok)
	}
	if _, ok := updates["phone"]; ok {
		t.Fatalf("phone should not be updated when request field is nil")
	}
}

// TestUserDatabaseUsesMainDB 验证前台用户管理固定使用默认主库。
func TestUserDatabaseUsesMainDB(t *testing.T) {
	if userDatabase != svc.DatabaseMain {
		t.Fatalf("userDatabase = %q, want %q", userDatabase, svc.DatabaseMain)
	}
}

// TestUserModelUsesUserTable 验证后台管理前台用户时读取统一 user 表。
func TestUserModelUsesUserTable(t *testing.T) {
	if model.TableNameUser != "user" {
		t.Fatalf("TableNameUser = %q, want user", model.TableNameUser)
	}
	if tableName := (&model.User{}).TableName(); tableName != "user" {
		t.Fatalf("User.TableName() = %q, want user", tableName)
	}
}

// TestValidateUserIdentityListReq 验证分表阶段后台列表不会退化为扫描用户分表。
func TestValidateUserIdentityListReq(t *testing.T) {
	req := &types.UserListReq{
		Username: "demo",
	}
	if err := validateUserIdentityListReq(req); err != nil {
		t.Fatalf("validateUserIdentityListReq() error = %v", err)
	}

	req.Email = "demo@example.com"
	if err := validateUserIdentityListReq(req); err != nil {
		t.Fatalf("validateUserIdentityListReq(email) error = %v", err)
	}

	req.Phone = "13800000000"
	if err := validateUserIdentityListReq(req); err == nil {
		t.Fatal("expected email and phone combined filter to be rejected in identity-index list")
	}

	req.Email = ""
	req.Phone = ""
	req.OrderBy = "lastLoginAt"
	if err := validateUserIdentityListReq(req); err == nil {
		t.Fatal("expected unsupported order field to be rejected in identity-index list")
	}
	req.OrderBy = "id"
	req.Page = 2
	if err := validateUserIdentityListReq(req); err == nil {
		t.Fatal("expected page after first to require cursor")
	}
	req.CursorID = 100
	if err := validateUserIdentityListReq(req); err != nil {
		t.Fatalf("cursor page should pass validation: %v", err)
	}
}

// TestUserOrderField 验证单表阶段前端排序字段映射。
func TestUserOrderField(t *testing.T) {
	if got := userOrderField("shardNo"); got != "shard_no" {
		t.Fatalf("userOrderField(shardNo) = %q, want shard_no", got)
	}
}

// TestUserIdentityListSQLLetsOptimizerChooseIndex 验证分表列表按用户 ID 游标查询且不强制索引。
func TestUserIdentityListSQLLetsOptimizerChooseIndex(t *testing.T) {
	// sqlLog 记录真实列表函数生成的 SQL。
	var sqlLog bytes.Buffer
	// db 使用 DryRun 模式，仅验证查询边界而不访问数据库。
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		Logger: logger.New(log.New(&sqlLog, "", 0), logger.Config{
			LogLevel: logger.Info,
		}),
	})
	if err != nil {
		t.Fatalf("open user list dry run db: %v", err)
	}
	// logicObj 直接执行分表列表路径，确保测试不复制生产查询。
	logicObj := NewLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	result := logicObj.listByUserIdentity(db, &types.UserListReq{
		Username:    "demo",
		CursorID:    100,
		GetPageReq:  types.GetPageReq{Page: 2, PageSize: 20},
		GetOrderReq: types.GetOrderReq{OrderBy: "id", Order: "asc"},
	})
	sqlText := sqlLog.String()
	if strings.Contains(sqlText, "FORCE INDEX") {
		t.Fatalf("generated sql = %q, should let optimizer choose index", sqlText)
	}
	for _, fragment := range []string{"FROM `user_identity_username`", "user_id > 100", "identity_value LIKE 'demo%'", "ORDER BY user_id asc", "LIMIT 21"} {
		if !strings.Contains(sqlText, fragment) {
			t.Fatalf("generated sql missing %q: %s", fragment, sqlText)
		}
	}
	// data 验证分表列表同时返回筛选能力与游标口径。
	data, ok := result.Data.(types.ListResp[types.UserItem])
	if !ok {
		t.Fatalf("listByUserIdentity() data type = %T", result.Data)
	}
	meta, ok := data.Meta.(types.UserListMeta)
	if !ok || meta.ExactTotal || meta.StatusFilterSupported {
		t.Fatalf("listByUserIdentity() meta = %#v, want sharded capabilities", data.Meta)
	}
}

// TestUseUserIdentityListHonorsSplitWriteConfig 验证写入路由切分后列表直接走身份索引。
func TestUseUserIdentityListHonorsSplitWriteConfig(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{
		User: config.UserConfig{RouteShardCount: 2},
	}, svc.Dependencies{})
	logicObj := NewLogic(nil, svcCtx)
	got, err := logicObj.useUserIdentityList(nil)
	if err != nil {
		t.Fatalf("useUserIdentityList() error = %v", err)
	}
	if !got {
		t.Fatal("useUserIdentityList() = false, want true for split write config")
	}
}

// TestBuildUserExportPartFileNameIncludesPartNo 验证导出文件名带固定宽度编号，便于按文件名确认顺序。
func TestBuildUserExportPartFileNameIncludesPartNo(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 34, 56, 0, time.UTC)
	jobID := "11111111-2222-3333-4444-555555555555"
	got := buildUserExportPartFileName(jobID, now, 12)
	wantSuffix := "_part_0012.xlsx"
	if !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("file name = %q, want suffix %q", got, wantSuffix)
	}
	if strings.Contains(got, "-") {
		t.Fatalf("file name = %q, job id hyphen should be removed", got)
	}
}

// TestUserExportSplitHelpers 验证用户导出拆分阈值不会超过配置单文件行数。
func TestUserExportSplitHelpers(t *testing.T) {
	if got := userExportPartQueryLimit(200, 50); got != 50 {
		t.Fatalf("query limit = %d, want remaining rows", got)
	}
	if got := userExportPartQueryLimit(200, 0); got != 1 {
		t.Fatalf("query limit = %d, want lookahead minimum", got)
	}
	if got := userExportSheetRowLimit(500000); got != 500001 {
		t.Fatalf("sheet row limit = %d, want split rows plus header", got)
	}
	if got := userExportSheetRowLimit(userExportMaxSplitRows + 1); got != excel.MaxExcelSheetRows {
		t.Fatalf("sheet row limit = %d, want excel max rows", got)
	}
}

// TestApplyUserExportProgress 验证多文件导出按全局处理量更新百分比、速度和预计剩余时间。
func TestApplyUserExportProgress(t *testing.T) {
	startedAt := time.Date(2026, 7, 16, 12, 0, 0, 0, time.Local)
	status := &types.UserExportStatusResp{Total: 1000000}
	applyUserExportProgress(status, 500000, startedAt, excel.ExportProgress{
		Processed:       100000,
		LastProcessedAt: startedAt.Add(10 * time.Second),
	})
	if status.Processed != 600000 || status.Progress != 60 {
		t.Fatalf("progress = %d processed = %d, want 60/600000", status.Progress, status.Processed)
	}
	if status.AverageRowsPerSec != 60000 || status.EstimatedSeconds != 7 {
		t.Fatalf("speed = %d eta = %d, want 60000/7", status.AverageRowsPerSec, status.EstimatedSeconds)
	}

	applyUserExportProgress(status, 500000, startedAt, excel.ExportProgress{
		Processed:       500000,
		LastProcessedAt: startedAt.Add(20 * time.Second),
	})
	if status.Progress != userExportRunningMaxProgress || status.EstimatedSeconds != 0 {
		t.Fatalf("finished running progress = %d eta = %d, want 99/0", status.Progress, status.EstimatedSeconds)
	}

	unknown := &types.UserExportStatusResp{}
	applyUserExportProgress(unknown, 0, startedAt, excel.ExportProgress{
		Processed:       200,
		LastProcessedAt: startedAt.Add(2 * time.Second),
	})
	if unknown.Progress != 0 || unknown.AverageRowsPerSec != 100 || unknown.EstimatedSeconds != 0 {
		t.Fatalf("unknown total progress = %+v, want progress 0 speed 100 eta 0", unknown)
	}
}

// TestResetUserExportFilesClearsRetryProgress 验证首个文件生成前失败也会清空旧进度和预计时间。
func TestResetUserExportFilesClearsRetryProgress(t *testing.T) {
	status := &types.UserExportStatusResp{
		Processed:         100,
		Total:             1000,
		Progress:          10,
		EstimatedSeconds:  9,
		AverageRowsPerSec: 100,
		StartedAt:         "2026-07-16 12:00:00",
		LastProcessedAt:   "2026-07-16 12:00:01",
	}
	resetUserExportFiles(status)
	if status.Processed != 0 || status.Total != 0 || status.Progress != 0 || status.EstimatedSeconds != 0 || status.AverageRowsPerSec != 0 {
		t.Fatalf("重试进度未清空: %+v", status)
	}
	if status.StartedAt != "" || status.LastProcessedAt != "" {
		t.Fatalf("重试时间未清空: %+v", status)
	}
}

// TestCountUserExportRowsUsesCurrentStorageQuery 验证总量统计与单表、身份索引导出路径一致。
func TestCountUserExportRowsUsesCurrentStorageQuery(t *testing.T) {
	buffer := &bytes.Buffer{}
	db := newUserLogicDryRunDB(t).Session(&gorm.Session{Logger: logger.New(log.New(buffer, "", 0), logger.Config{LogLevel: logger.Info})})
	logicObj := NewLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{}, svc.Dependencies{
		SiteDBs: svc.SiteDatabases{MainDB: db},
	}))
	status := 1
	if _, err := logicObj.countUserExportRows(context.Background(), &types.UserExportReq{Status: &status}, false); err != nil {
		t.Fatalf("count user table rows: %v", err)
	}
	if sqlText := buffer.String(); !strings.Contains(sqlText, "SELECT count(*) FROM `user` WHERE status = 1") {
		t.Fatalf("user count sql = %q, want filtered user table count", sqlText)
	}

	buffer.Reset()
	if _, err := logicObj.countUserExportRows(context.Background(), &types.UserExportReq{Username: "demo"}, true); err != nil {
		t.Fatalf("count identity rows: %v", err)
	}
	if sqlText := buffer.String(); !strings.Contains(sqlText, "FROM `user_identity_username` WHERE identity_value LIKE 'demo%'") || strings.Contains(sqlText, "FORCE INDEX") {
		t.Fatalf("identity count sql = %q, want optimizer-selected username identity query", sqlText)
	}
}

// TestNextUserExportCursorAfterEmptyPage 验证身份索引空页只能在游标推进时跳过。
func TestNextUserExportCursorAfterEmptyPage(t *testing.T) {
	nextCursor, skipped, err := nextUserExportCursorAfterEmptyPage(&excel.CursorPage[userExportRow, int64]{
		HasMore:    true,
		NextCursor: 20,
	}, 10)
	if err != nil {
		t.Fatalf("nextUserExportCursorAfterEmptyPage() error = %v", err)
	}
	if !skipped || nextCursor != 20 {
		t.Fatalf("next cursor = %d skipped=%v, want 20/true", nextCursor, skipped)
	}

	_, skipped, err = nextUserExportCursorAfterEmptyPage(&excel.CursorPage[userExportRow, int64]{
		Items:   []userExportRow{{CursorID: 11}},
		HasMore: true,
	}, 10)
	if err != nil || skipped {
		t.Fatalf("non-empty page skipped=%v err=%v, want no skip", skipped, err)
	}

	_, skipped, err = nextUserExportCursorAfterEmptyPage(&excel.CursorPage[userExportRow, int64]{
		HasMore:    true,
		NextCursor: 10,
	}, 10)
	if err == nil || skipped {
		t.Fatalf("stalled empty page skipped=%v err=%v, want error", skipped, err)
	}
}

// TestBuildUserExportRequestFingerprintIncludesSplitRows 验证拆分配置变化不会复用旧导出文件。
func TestBuildUserExportRequestFingerprintIncludesSplitRows(t *testing.T) {
	req := &types.UserExportReq{Username: "demo"}
	left := buildUserExportRequestFingerprint(req, 500000)
	right := buildUserExportRequestFingerprint(req, 250000)
	if left == right {
		t.Fatal("export fingerprint should include split rows")
	}
}

// TestUserExportStatusSnapshotKeepsFileObjects 验证 Redis 快照保留内部对象信息，对外响应隐藏内部路径。
func TestUserExportStatusSnapshotKeepsFileObjects(t *testing.T) {
	status := &types.UserExportStatusResp{
		JobID: "job-1",
		Files: []types.UserExportFileItem{{
			PartNo:        2,
			FileName:      "user_export_part_0002.xlsx",
			RowCount:      100,
			ProcessedFrom: 101,
			ProcessedTo:   200,
			DownloadReady: true,
			DownloadURL:   "internal",
			FilePath:      "/tmp/user_export_part_0002.xlsx",
			ObjectKey:     "exports/user/part-2.xlsx",
			StorageType:   "local",
			ContentType:   userExportContentType,
		}},
		AuthorizedAdminIDs: []int{1, 2},
	}
	snapshot := newUserExportStatusSnapshot(status)
	if len(snapshot.UserExportStatusResp.Files) != 0 {
		t.Fatal("embedded response files should be cleared to avoid duplicate stale snapshots")
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	decoded := &userExportStatusSnapshot{}
	if err := json.Unmarshal(payload, decoded); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	restored := decoded.toStatus()
	if len(restored.Files) != 1 || restored.Files[0].ObjectKey != "exports/user/part-2.xlsx" {
		t.Fatalf("restored files = %+v, want object key preserved", restored.Files)
	}
	selected, err := selectUserExportDownloadFile(restored, 2)
	if err != nil {
		t.Fatalf("select export file: %v", err)
	}
	if selected.FileName != "user_export_part_0002.xlsx" {
		t.Fatalf("selected file = %q, want part 2", selected.FileName)
	}
	publicFiles := publicUserExportFiles(restored.JobID, restored.Files)
	if publicFiles[0].ObjectKey != "" || publicFiles[0].FilePath != "" {
		t.Fatalf("public files leaked internal storage fields: %+v", publicFiles[0])
	}
	if !strings.HasSuffix(publicFiles[0].DownloadURL, "?partNo=2") {
		t.Fatalf("download url = %q, want partNo query", publicFiles[0].DownloadURL)
	}
}

// TestUserExportReusableObjectHonorsFilesList 验证多分片任务只按分片列表判断复用可用性。
func TestUserExportReusableObjectHonorsFilesList(t *testing.T) {
	filePath := t.TempDir() + "/user_export_part_0001.xlsx"
	if err := os.WriteFile(filePath, []byte("xlsx"), 0o644); err != nil {
		t.Fatalf("write temp export file: %v", err)
	}
	logicObj := NewLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	status := &types.UserExportStatusResp{
		FilePath:      filePath,
		DownloadReady: true,
		Files: []types.UserExportFileItem{{
			PartNo:        1,
			DownloadReady: false,
		}},
	}
	if logicObj.hasReusableUserExportDownloadObject(status) {
		t.Fatal("multi-part status should not fallback to top-level file when no part is ready")
	}
	status.Files = nil
	if !logicObj.hasReusableUserExportDownloadObject(status) {
		t.Fatal("legacy single-file status should fallback to top-level file")
	}
}

// TestAPIRuntimeSyncWarningPreservesDBSuccessSemantics 验证写库后的同步失败只作为可重试告警返回。
func TestAPIRuntimeSyncWarningPreservesDBSuccessSemantics(t *testing.T) {
	logicObj := NewLogic(nil, svc.NewServiceContext(config.Config{}, svc.Dependencies{}))
	resp := logicObj.apiRuntimeSyncWarning(7, types.UserRuntimeSyncResp{Enabled: true}, i18n.MsgKeyAPIRuntimeProfileSyncWarning, assertError("timeout"))
	if resp.Success {
		t.Fatal("sync warning should mark success false")
	}
	if !resp.Enabled || resp.UserID != 7 {
		t.Fatalf("sync warning response = %+v, want enabled user 7", resp)
	}
	if !strings.Contains(resp.Message, "资料已更新") || strings.Contains(resp.Message, "timeout") {
		t.Fatalf("sync warning message = %q, want safe fallback without internal cause", resp.Message)
	}
}

// assertError 是测试用固定错误文本。
type assertError string

// Error 返回固定错误文本。
func (e assertError) Error() string {
	return string(e)
}

// newUserLogicDryRunDB 创建用户管理逻辑测试使用的 MySQL DryRun 连接。
func newUserLogicDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open dry run db: %v", err)
	}
	return db
}
