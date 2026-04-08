package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

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

func TestBuildUploadFingerprint(t *testing.T) {
	left := buildUploadFingerprint(1, "excel-import", "demo.xlsx", 1024, "abc123")
	right := buildUploadFingerprint(1, "excel-import", "demo.xlsx", 1024, "abc123")
	if left == "" || left != right {
		t.Fatalf("buildUploadFingerprint 结果不稳定: left=%s right=%s", left, right)
	}
}

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
	completed, err := manager.Complete(context.Background(), session.UploadID)
	if err != nil {
		t.Fatalf("首次完成上传失败: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("期望会话已完成，实际为 %s", completed.Status)
	}
	if _, err = manager.Complete(context.Background(), session.UploadID); err != nil {
		t.Fatalf("重复完成上传应保持幂等，实际错误: %v", err)
	}
	if _, err = manager.UploadChunk(context.Background(), session.UploadID, 0, strings.NewReader(string(body))); err != nil {
		t.Fatalf("完成后的重复分片上传应直接返回完成会话，实际错误: %v", err)
	}
	if fileExists(session.TempPath) {
		t.Fatalf("完成后的重复分片不应重建临时文件: %s", session.TempPath)
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
	completed, err := manager.Complete(context.Background(), session.UploadID)
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
	if _, err = manager.Complete(context.Background(), session.UploadID); err == nil {
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
	recovered, err := manager.Complete(context.Background(), session.UploadID)
	if err != nil {
		t.Fatalf("恢复完成状态失败: %v", err)
	}
	if recovered.Status != "completed" {
		t.Fatalf("期望恢复后的会话已完成，实际为 %s", recovered.Status)
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
