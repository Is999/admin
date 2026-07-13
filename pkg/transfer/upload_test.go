package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// failUploadExpireHook 在测试中注入分片 TTL 刷新失败，验证主会话快照不会提前提交。
type failUploadExpireHook struct{}

// DialHook 保持 Redis 建连行为不变。
func (failUploadExpireHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network string, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

// ProcessHook 只阻断 EXPIRE 命令。
func (failUploadExpireHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.FullName() == "expire" {
			return context.DeadlineExceeded
		}
		return next(ctx, cmd)
	}
}

// ProcessPipelineHook 保持 Redis Pipeline 行为不变。
func (failUploadExpireHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// allowUploadedFile 是上传管理器单元测试使用的通过校验器。
func allowUploadedFile(context.Context, *UploadSession, string) error {
	return nil
}

// TestExpectedChunkSize 验证对应场景符合预期。
func TestExpectedChunkSize(t *testing.T) {
	session := &UploadSession{
		FileSize:    1025,
		ChunkSize:   512,
		TotalChunks: 3,
	}
	if got := expectedChunkSize(session, 0); got != 512 {
		t.Fatalf("expectedChunkSize 首片大小错误: got=%d", got)
	}
	if got := expectedChunkSize(session, 2); got != 1 {
		t.Fatalf("expectedChunkSize 尾片大小错误: got=%d", got)
	}
}

// TestBuildUploadFingerprint 验证对应场景符合预期。
func TestBuildUploadFingerprint(t *testing.T) {
	left := buildUploadFingerprint(1, "excel-import", "demo.xlsx", 1024, "abc123")
	right := buildUploadFingerprint(1, "excel-import", "demo.xlsx", 1024, "abc123")
	if left == "" || left != right {
		t.Fatalf("buildUploadFingerprint 结果不稳定: left=%s right=%s", left, right)
	}
}

// TestVerifyUploadedFileHash 验证对应场景符合预期。
func TestVerifyUploadedFileHash(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "demo.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	if err := verifyUploadedFileHash(filePath, "2cf24dba5fb0a030e..."); err == nil {
		t.Fatalf("verifyUploadedFileHash 应在错误摘要下失败")
	}
	if err := verifyUploadedFileHash(filePath, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"); err != nil {
		t.Fatalf("verifyUploadedFileHash 正确摘要校验失败: %v", err)
	}
}

// TestCopyUploadedFileAcrossDevicesVerifiesHashDuringCopy 确保跨设备复制时同步校验摘要，失败时不提交目标文件。
func TestCopyUploadedFileAcrossDevicesVerifiesHashDuringCopy(t *testing.T) {
	body := []byte("hello")
	sum := sha256.Sum256(body)
	srcPath := filepath.Join(t.TempDir(), "src.part")
	dstPath := filepath.Join(t.TempDir(), "dst.data")
	if err := os.WriteFile(srcPath, body, 0o644); err != nil {
		t.Fatalf("写入源文件失败: %v", err)
	}
	if err := copyUploadedFileAcrossDevices(srcPath, dstPath, "bad-hash"); err == nil {
		t.Fatal("期望摘要错误时跨设备复制失败")
	}
	if fileExists(dstPath) {
		t.Fatalf("摘要错误时不应提交目标文件: %s", dstPath)
	}
	if !fileExists(srcPath) {
		t.Fatalf("摘要错误时源文件应保留: %s", srcPath)
	}
	if err := copyUploadedFileAcrossDevices(srcPath, dstPath, hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("摘要正确时跨设备复制失败: %v", err)
	}
	if fileExists(srcPath) {
		t.Fatalf("复制成功后源文件应被清理: %s", srcPath)
	}
	dstBody, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("读取目标文件失败: %v", err)
	}
	if string(dstBody) != string(body) {
		t.Fatalf("目标文件内容错误: %q", string(dstBody))
	}
}

// newTestUploadManager 构造测试依赖。
func newTestUploadManager(t *testing.T) (*LocalUploadManager, string) {
	t.Helper()
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})
	rootDir := t.TempDir()
	return NewLocalUploadManager(
		client,
		rootDir,
		"upload:session:%s",
		"upload:chunks:%s",
		"upload:fingerprint:%s",
		"upload:object:%s",
		time.Hour,
	), rootDir
}

// TestPersistSessionCommitsPrimarySnapshotLast 验证辅助索引写入后失败不会暴露未提交的完成会话。
func TestPersistSessionCommitsPrimarySnapshotLast(t *testing.T) {
	manager, _ := newTestUploadManager(t)
	ctx := context.Background()
	session := &UploadSession{
		UploadID: "commit-last",
		Status:   "uploading",
	}
	if err := manager.PersistSession(ctx, session); err != nil {
		t.Fatalf("保存初始上传会话失败: %v", err)
	}
	manager.client.AddHook(failUploadExpireHook{})
	session.Status = "completed"
	session.ObjectKey = "private/demo.xlsx"
	if err := manager.PersistSession(ctx, session); err == nil {
		t.Fatal("期望分片 TTL 刷新失败")
	}
	stored, err := manager.GetSession(ctx, session.UploadID)
	if err != nil {
		t.Fatalf("读取原上传会话失败: %v", err)
	}
	if stored.Status != "uploading" || stored.ObjectKey != "" {
		t.Fatalf("主会话被提前提交: status=%s objectKey=%s", stored.Status, stored.ObjectKey)
	}
	if _, err := manager.FindSessionByObjectKey(ctx, session.ObjectKey); err == nil {
		t.Fatal("未提交完成的对象反查索引不应返回会话")
	}
}

// TestHasSession 验证上传会话存在性检查只依赖精确 Redis key。
func TestHasSession(t *testing.T) {
	manager, _ := newTestUploadManager(t)
	ctx := context.Background()
	if exists, err := manager.HasSession(ctx, "missing"); err != nil || exists {
		t.Fatalf("不存在的上传会话检查结果错误 exists=%t err=%v", exists, err)
	}
	if err := manager.PersistSession(ctx, &UploadSession{UploadID: "active-session"}); err != nil {
		t.Fatalf("保存测试上传会话失败: %v", err)
	}
	if exists, err := manager.HasSession(ctx, "active-session"); err != nil || !exists {
		t.Fatalf("有效上传会话检查结果错误 exists=%t err=%v", exists, err)
	}
}

// TestInitCleansExpiredTempFiles 验证新建上传会话会小批量回收已超过 TTL 的中断上传文件。
func TestInitCleansExpiredTempFiles(t *testing.T) {
	manager, rootDir := newTestUploadManager(t)
	now := time.Now()
	oldTime := now.Add(-2 * time.Hour)
	oldTempDir := filepath.Join(rootDir, "uploading", "excel-import", oldTime.Format(uploadTempDirLayout))
	recentTempDir := filepath.Join(rootDir, "uploading", "excel-import", now.Format(uploadTempDirLayout))
	if err := os.MkdirAll(oldTempDir, 0o755); err != nil {
		t.Fatalf("创建过期上传临时目录失败: %v", err)
	}
	if err := os.MkdirAll(recentTempDir, 0o755); err != nil {
		t.Fatalf("创建上传临时目录失败: %v", err)
	}
	oldPath := filepath.Join(oldTempDir, "old.part")
	activePath := filepath.Join(oldTempDir, "active.part")
	recentPath := filepath.Join(recentTempDir, "recent.part")
	for _, path := range []string{oldPath, activePath, recentPath} {
		if err := os.WriteFile(path, []byte("part"), 0o644); err != nil {
			t.Fatalf("创建上传临时文件失败: %v", err)
		}
	}
	for _, path := range []string{oldPath, activePath} {
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			t.Fatalf("设置过期上传临时文件时间失败: %v", err)
		}
	}
	if err := manager.client.Set(context.Background(), manager.sessionKey("active"), `{}`, time.Hour).Err(); err != nil {
		t.Fatalf("保存有效上传会话失败: %v", err)
	}

	session, err := manager.Init(context.Background(), InitUploadReq{
		OperatorID: 1,
		BizType:    "excel-import",
		FileName:   "demo.txt",
		FileSize:   4,
		ChunkSize:  4,
	})
	if err != nil {
		t.Fatalf("初始化上传会话失败: %v", err)
	}
	if fileExists(oldPath) {
		t.Fatalf("过期上传临时文件未清理: %s", oldPath)
	}
	if !fileExists(activePath) {
		t.Fatalf("仍有有效会话的上传临时文件不应被清理: %s", activePath)
	}
	if !fileExists(recentPath) {
		t.Fatalf("有效期内上传临时文件不应被清理: %s", recentPath)
	}
	createdAt, err := time.Parse(time.DateTime, session.CreatedAt)
	if err != nil {
		t.Fatalf("解析上传会话创建时间失败: %v", err)
	}
	if got, want := filepath.Base(filepath.Dir(session.TempPath)), createdAt.Format(uploadTempDirLayout); got != want {
		t.Fatalf("上传临时目录日期 = %s, want %s", got, want)
	}
}

// TestCompleteIsIdempotent 确保已完成上传重复完成时直接返回会话快照。
func TestCompleteIsIdempotent(t *testing.T) {
	manager, _ := newTestUploadManager(t)
	body := []byte("hello")
	sum := sha256.Sum256(body)
	session, err := manager.Init(context.Background(), InitUploadReq{
		OperatorID:  1,
		BizType:     "excel-import",
		FileName:    "demo.txt",
		FileSize:    int64(len(body)),
		ChunkSize:   int64(len(body)),
		FileHash:    hex.EncodeToString(sum[:]),
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("初始化上传会话失败: %v", err)
	}
	if _, err = manager.UploadChunk(context.Background(), session.UploadID, 0, strings.NewReader(string(body))); err != nil {
		t.Fatalf("上传分片失败: %v", err)
	}
	completed, err := manager.Complete(context.Background(), session.UploadID, allowUploadedFile)
	if err != nil {
		t.Fatalf("首次完成上传失败: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("期望会话已完成，实际为 %s", completed.Status)
	}
	if _, err = manager.Complete(context.Background(), session.UploadID, allowUploadedFile); err != nil {
		t.Fatalf("重复完成上传应保持幂等，实际错误: %v", err)
	}
	if _, err = manager.UploadChunk(context.Background(), session.UploadID, 0, strings.NewReader(string(body))); err != nil {
		t.Fatalf("完成后的重复分片上传应直接返回完成会话，实际错误: %v", err)
	}
	if fileExists(session.TempPath) {
		t.Fatalf("完成后的重复分片不应重建临时文件: %s", session.TempPath)
	}
}

// TestCompleteValidationFailureKeepsSessionIncomplete 确保安全校验失败时文件不会进入可下载完成态。
func TestCompleteValidationFailureKeepsSessionIncomplete(t *testing.T) {
	manager, _ := newTestUploadManager(t)
	body := []byte("unsafe")
	sum := sha256.Sum256(body)
	session, err := manager.Init(context.Background(), InitUploadReq{
		OperatorID:  1,
		BizType:     "excel-import",
		FileName:    "demo.txt",
		FileSize:    int64(len(body)),
		ChunkSize:   int64(len(body)),
		FileHash:    hex.EncodeToString(sum[:]),
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("初始化上传会话失败: %v", err)
	}
	if _, err = manager.UploadChunk(context.Background(), session.UploadID, 0, strings.NewReader(string(body))); err != nil {
		t.Fatalf("上传分片失败: %v", err)
	}
	if _, err = manager.Complete(context.Background(), session.UploadID, func(context.Context, *UploadSession, string) error {
		return context.Canceled
	}); err == nil {
		t.Fatal("期望完成前校验失败")
	}
	current, err := manager.GetSession(context.Background(), session.UploadID)
	if err != nil {
		t.Fatalf("读取校验失败后的会话失败: %v", err)
	}
	if current.Status == "completed" {
		t.Fatal("安全校验失败的文件不应进入 completed 状态")
	}
	if !fileExists(session.TempPath) || fileExists(session.StoragePath) {
		t.Fatalf("安全校验失败应保留隔离文件且不提交 temp=%t storage=%t", fileExists(session.TempPath), fileExists(session.StoragePath))
	}
	if _, err = manager.Complete(context.Background(), session.UploadID, allowUploadedFile); err != nil {
		t.Fatalf("校验服务恢复后应可重试完成: %v", err)
	}
}

// TestUploadChunkRejectsOversizedBodyWithoutMarking 确保超长分片会被拒绝且不会写入已上传分片状态。
func TestUploadChunkRejectsOversizedBodyWithoutMarking(t *testing.T) {
	manager, _ := newTestUploadManager(t)
	session, err := manager.Init(context.Background(), InitUploadReq{
		OperatorID:  1,
		BizType:     "excel-import",
		FileName:    "demo.txt",
		FileSize:    10,
		ChunkSize:   5,
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("初始化上传会话失败: %v", err)
	}
	if _, err = manager.UploadChunk(context.Background(), session.UploadID, 0, strings.NewReader("123456")); err == nil {
		t.Fatal("期望超长分片上传失败")
	}
	chunks, err := manager.uploadedChunks(context.Background(), session.UploadID)
	if err != nil {
		t.Fatalf("读取分片状态失败: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("超长分片不应被标记为已上传: %+v", chunks)
	}
	if _, err = manager.UploadChunk(context.Background(), session.UploadID, 0, strings.NewReader("12345")); err != nil {
		t.Fatalf("正确分片重试应成功: %v", err)
	}
}

// TestUploadChunksCanWriteDifferentOffsetsConcurrently 确保不同分片可并行写入并正确完成文件。
func TestUploadChunksCanWriteDifferentOffsetsConcurrently(t *testing.T) {
	manager, _ := newTestUploadManager(t)
	body := []byte("helloworld")
	sum := sha256.Sum256(body)
	session, err := manager.Init(context.Background(), InitUploadReq{
		OperatorID:  1,
		BizType:     "excel-import",
		FileName:    "demo.txt",
		FileSize:    int64(len(body)),
		ChunkSize:   5,
		FileHash:    hex.EncodeToString(sum[:]),
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("初始化上传会话失败: %v", err)
	}

	var waitGroup sync.WaitGroup
	errCh := make(chan error, 2)
	for index, value := range []string{"hello", "world"} {
		waitGroup.Add(1)
		go func(index int, value string) {
			defer waitGroup.Done()
			if _, err := manager.UploadChunk(context.Background(), session.UploadID, index, strings.NewReader(value)); err != nil {
				errCh <- err
			}
		}(index, value)
	}
	waitGroup.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("并发上传分片失败: %v", err)
		}
	}
	completed, err := manager.Complete(context.Background(), session.UploadID, allowUploadedFile)
	if err != nil {
		t.Fatalf("完成并发上传失败: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("期望并发上传后完成，实际为 %s", completed.Status)
	}
	data, err := os.ReadFile(session.StoragePath)
	if err != nil {
		t.Fatalf("读取完成文件失败: %v", err)
	}
	if string(data) != string(body) {
		t.Fatalf("完成文件内容错误: %q", string(data))
	}
}

// TestCompleteRejectsMissingChunksByCount 确保未传完分片时 Complete 先按数量快速拒绝，避免无意义读取完整分片集合。
func TestCompleteRejectsMissingChunksByCount(t *testing.T) {
	manager, _ := newTestUploadManager(t)
	session, err := manager.Init(context.Background(), InitUploadReq{
		OperatorID:  1,
		BizType:     "excel-import",
		FileName:    "demo.txt",
		FileSize:    10,
		ChunkSize:   5,
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("初始化上传会话失败: %v", err)
	}
	if _, err = manager.UploadChunk(context.Background(), session.UploadID, 0, strings.NewReader("12345")); err != nil {
		t.Fatalf("上传第一个分片失败: %v", err)
	}
	if _, err = manager.Complete(context.Background(), session.UploadID, allowUploadedFile); err == nil {
		t.Fatal("期望分片未传完时完成上传失败")
	}
	count, err := manager.uploadedChunkCount(context.Background(), session.UploadID)
	if err != nil {
		t.Fatalf("读取分片数量失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("uploadedChunkCount = %d, want 1", count)
	}
}

// TestCompleteRecoversAfterRename 确保 Rename 成功但会话未持久化时可再次 Complete 修复状态。
func TestCompleteRecoversAfterRename(t *testing.T) {
	manager, _ := newTestUploadManager(t)
	body := []byte("hello")
	sum := sha256.Sum256(body)
	session, err := manager.Init(context.Background(), InitUploadReq{
		OperatorID:  1,
		BizType:     "excel-import",
		FileName:    "demo.txt",
		FileSize:    int64(len(body)),
		ChunkSize:   int64(len(body)),
		FileHash:    hex.EncodeToString(sum[:]),
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("初始化上传会话失败: %v", err)
	}
	if _, err = manager.UploadChunk(context.Background(), session.UploadID, 0, strings.NewReader(string(body))); err != nil {
		t.Fatalf("上传分片失败: %v", err)
	}
	if err = os.MkdirAll(filepath.Dir(session.StoragePath), 0o755); err != nil {
		t.Fatalf("创建完成目录失败: %v", err)
	}
	if err = os.Rename(session.TempPath, session.StoragePath); err != nil {
		t.Fatalf("模拟文件提交失败: %v", err)
	}
	validatedPath := ""
	recovered, err := manager.Complete(context.Background(), session.UploadID, func(_ context.Context, _ *UploadSession, filePath string) error {
		validatedPath = filePath
		return nil
	})
	if err != nil {
		t.Fatalf("恢复完成状态失败: %v", err)
	}
	if recovered.Status != "completed" {
		t.Fatalf("期望恢复后的会话已完成，实际为 %s", recovered.Status)
	}
	if validatedPath != session.StoragePath {
		t.Fatalf("恢复完成前应重新校验最终文件 path=%s", validatedPath)
	}
}

// TestInitSanitizesBizTypePath 确保业务类型不会被用作未清洗的本地路径段。
func TestInitSanitizesBizTypePath(t *testing.T) {
	manager, rootDir := newTestUploadManager(t)
	session, err := manager.Init(context.Background(), InitUploadReq{
		OperatorID:  1,
		BizType:     "../unsafe/path",
		FileName:    "demo.txt",
		FileSize:    10,
		ChunkSize:   10,
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("初始化上传会话失败: %v", err)
	}
	for _, item := range []string{session.TempPath, session.StoragePath} {
		rel, relErr := filepath.Rel(rootDir, item)
		if relErr != nil {
			t.Fatalf("计算相对路径失败: %v", relErr)
		}
		if strings.HasPrefix(rel, "..") || strings.Contains(rel, "..") {
			t.Fatalf("路径未正确清洗: %s", item)
		}
	}
}
