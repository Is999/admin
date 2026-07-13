package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"admin/common/runtimecfg"
	"admin/internal/config"
	"admin/internal/model"
	"admin/internal/svc"
	taskqueue "admin/internal/task/queue"
	"admin/internal/types"
	"admin/pkg/storage"
	"admin/pkg/transfer"

	"github.com/Is999/go-utils/errors"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// cleanupTaskCall 记录测试任务队列收到的一次投递。
type cleanupTaskCall struct {
	TaskType string          // 任务类型
	Payload  []byte          // 原始任务载荷
	Options  svc.TaskOptions // 任务投递选项
}

// cleanupTaskQueueStub 是上传对象清理测试使用的最小任务队列。
type cleanupTaskQueueStub struct {
	svc.TaskQueue                   // 其余任务能力不参与当前测试
	Enabled       bool              // 是否模拟已启用任务队列
	EnqueueErr    error             // 投递时返回的测试错误
	Calls         []cleanupTaskCall // 已收到的任务投递记录
}

// IsEnabled 返回测试任务队列开关。
func (q *cleanupTaskQueueStub) IsEnabled() bool {
	return q != nil && q.Enabled
}

// EnqueueTask 记录上传对象清理任务及其投递选项。
func (q *cleanupTaskQueueStub) EnqueueTask(_ context.Context, taskType string, payload []byte, opts ...svc.TaskOption) error {
	if q == nil {
		return errors.Errorf("测试任务队列为空")
	}
	options := svc.TaskOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	q.Calls = append(q.Calls, cleanupTaskCall{
		TaskType: taskType,
		Payload:  append([]byte(nil), payload...),
		Options:  options,
	})
	return q.EnqueueErr
}

// TestCleanupUploadedObjectWaitsForSessionThenDeletes 验证有效会话只顺延任务，会话失效后才删除精确对象。
func TestCleanupUploadedObjectWaitsForSessionThenDeletes(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	previousConfig := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "site-a"})
	t.Cleanup(func() { runtimecfg.Restore(previousConfig) })

	objectRoot := t.TempDir()
	uploadRoot := t.TempDir()
	objectKey := "sys-config-excel-import/202607/16/import.xlsx"
	objectPath := filepath.Join(objectRoot, filepath.FromSlash(objectKey))
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o755); err != nil {
		t.Fatalf("创建测试对象目录失败: %v", err)
	}
	if err := os.WriteFile(objectPath, []byte("xlsx"), 0o644); err != nil {
		t.Fatalf("写入测试对象失败: %v", err)
	}
	stagingPath := filepath.Join(uploadRoot, "completed", FileTransferBizSysConfigExcelImport, filepath.Base(objectKey))
	if err := os.MkdirAll(filepath.Dir(stagingPath), 0o755); err != nil {
		t.Fatalf("创建测试 completed 目录失败: %v", err)
	}
	if err := os.WriteFile(stagingPath, []byte("xlsx"), 0o644); err != nil {
		t.Fatalf("写入测试 completed 文件失败: %v", err)
	}
	siblingKey := "sys-config-excel-import/202607/16/keep.xlsx"
	siblingPath := filepath.Join(objectRoot, filepath.FromSlash(siblingKey))
	if err := os.WriteFile(siblingPath, []byte("keep"), 0o644); err != nil {
		t.Fatalf("写入测试相邻对象失败: %v", err)
	}

	taskQueue := &cleanupTaskQueueStub{Enabled: true}
	svcCtx := svc.NewServiceContext(config.Config{
		AppID: "site-a",
		FileStorage: config.FileStorageConfig{
			Type:  storage.TypeLocal,
			Local: config.FileStorageLocalConfig{RootDir: objectRoot},
			UploadSession: config.FileStorageUploadSessionConfig{
				RootDir:    uploadRoot,
				TTLSeconds: 60,
			},
		},
	}, svc.Dependencies{Rds: client})
	svcCtx.Task = taskQueue
	uploadManager, err := svcCtx.FileTransferUploadManager()
	if err != nil {
		t.Fatalf("创建上传管理器失败: %v", err)
	}
	session := &transfer.UploadSession{
		UploadID:  "active-upload",
		BizType:   FileTransferBizSysConfigExcelImport,
		ObjectKey: objectKey,
	}
	if err := uploadManager.PersistSession(context.Background(), session); err != nil {
		t.Fatalf("保存测试上传会话失败: %v", err)
	}

	logicObj := NewFileTransferLogicWithContext(context.Background(), svcCtx)
	payload := &types.FileUploadCleanupTaskPayload{
		UploadID:  session.UploadID,
		BizType:   session.BizType,
		ObjectKey: session.ObjectKey,
	}
	if err := logicObj.CleanupUploadedObject(payload); err != nil {
		t.Fatalf("有效会话顺延清理任务失败: %v", err)
	}
	if _, err := os.Stat(objectPath); err != nil {
		t.Fatalf("有效会话对象不应被删除: %v", err)
	}
	if _, err := os.Stat(stagingPath); err != nil {
		t.Fatalf("有效会话 completed 文件不应被删除: %v", err)
	}
	if len(taskQueue.Calls) != 1 || taskQueue.Calls[0].TaskType != types.FileUploadCleanupTaskType {
		t.Fatalf("顺延任务投递记录错误: %+v", taskQueue.Calls)
	}
	if taskQueue.Calls[0].Options.Queue != taskqueue.QueueMaintenance || taskQueue.Calls[0].Options.Delay != time.Minute {
		t.Fatalf("顺延任务选项错误: %+v", taskQueue.Calls[0].Options)
	}

	redisServer.FastForward(2 * time.Minute)
	if err := logicObj.CleanupUploadedObject(payload); err != nil {
		t.Fatalf("失效会话对象清理失败: %v", err)
	}
	if _, err := os.Stat(objectPath); !os.IsNotExist(err) {
		t.Fatalf("失效会话对象仍存在 err=%v", err)
	}
	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Fatalf("失效会话 completed 文件仍存在 err=%v", err)
	}
	if _, err := os.Stat(siblingPath); err != nil {
		t.Fatalf("精确清理不应删除相邻对象: %v", err)
	}
}

// TestCleanupUploadedObjectKeepsReferencedAvatar 验证数据库仍引用头像时不会删除对象。
func TestCleanupUploadedObjectKeepsReferencedAvatar(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("创建头像引用测试数据库失败: %v", err)
	}
	if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		if admin, ok := tx.Statement.Dest.(*model.Admin); ok {
			admin.ID = 7
			tx.RowsAffected = 1
		}
	}); err != nil {
		t.Fatalf("替换头像引用测试回调失败: %v", err)
	}

	objectRoot := t.TempDir()
	objectKey := "admin-avatar/202607/16/avatar.png"
	objectPath := filepath.Join(objectRoot, filepath.FromSlash(objectKey))
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o755); err != nil {
		t.Fatalf("创建测试头像目录失败: %v", err)
	}
	if err := os.WriteFile(objectPath, []byte("png"), 0o644); err != nil {
		t.Fatalf("写入测试头像失败: %v", err)
	}
	svcCtx := svc.NewServiceContext(config.Config{
		FileStorage: config.FileStorageConfig{
			Type:  storage.TypeLocal,
			Local: config.FileStorageLocalConfig{RootDir: objectRoot},
		},
	}, svc.Dependencies{SiteDBs: svc.SiteDatabases{MainDB: db}})
	logicObj := NewFileTransferLogicWithContext(context.Background(), svcCtx)
	if err := logicObj.CleanupUploadedObject(&types.FileUploadCleanupTaskPayload{
		BizType:   FileTransferBizAdminAvatar,
		ObjectKey: objectKey,
	}); err != nil {
		t.Fatalf("保留已引用头像失败: %v", err)
	}
	if _, err := os.Stat(objectPath); err != nil {
		t.Fatalf("已引用头像不应被删除: %v", err)
	}
}

// TestScheduleReplacedAdminAvatarCleanup 验证头像替换只投递旧的托管对象。
func TestScheduleReplacedAdminAvatarCleanup(t *testing.T) {
	taskQueue := &cleanupTaskQueueStub{Enabled: true}
	svcCtx := svc.NewServiceContext(config.Config{
		FileStorage: config.FileStorageConfig{
			Type:  storage.TypeLocal,
			Local: config.FileStorageLocalConfig{RootDir: t.TempDir()},
		},
	}, svc.Dependencies{})
	svcCtx.Task = taskQueue
	logicObj := NewFileTransferLogicWithContext(context.Background(), svcCtx)
	oldAvatar := "/api/file-transfer/access?objectKey=admin-avatar%2F202607%2F16%2Fold.png"
	newAvatar := "/api/file-transfer/access?objectKey=admin-avatar%2F202607%2F16%2Fnew.png"
	if err := logicObj.ScheduleReplacedAdminAvatarCleanup(oldAvatar, newAvatar); err != nil {
		t.Fatalf("投递旧头像清理任务失败: %v", err)
	}
	if len(taskQueue.Calls) != 1 || taskQueue.Calls[0].Options.Delay != fileAdminAvatarCleanupDelay {
		t.Fatalf("旧头像清理任务投递记录错误: %+v", taskQueue.Calls)
	}
	var payload types.FileUploadCleanupTaskPayload
	if err := json.Unmarshal(taskQueue.Calls[0].Payload, &payload); err != nil {
		t.Fatalf("解析旧头像清理任务失败: %v", err)
	}
	if payload.UploadID != "" || payload.BizType != FileTransferBizAdminAvatar || payload.ObjectKey != "admin-avatar/202607/16/old.png" {
		t.Fatalf("旧头像清理任务载荷错误: %+v", payload)
	}
	if err := logicObj.ScheduleReplacedAdminAvatarCleanup(newAvatar, newAvatar); err != nil {
		t.Fatalf("相同头像不应投递清理任务: %v", err)
	}
	if len(taskQueue.Calls) != 1 {
		t.Fatalf("相同头像被重复投递清理任务: %+v", taskQueue.Calls)
	}
}

// TestPersistServerUploadedFileRollsBackWhenCleanupEnqueueFails 验证清理任务投递失败时回滚刚写入的对象并保留源文件重试。
func TestPersistServerUploadedFileRollsBackWhenCleanupEnqueueFails(t *testing.T) {
	objectRoot := t.TempDir()
	body := []byte("test-xlsx")
	sum := sha256.Sum256(body)
	sourcePath := filepath.Join(t.TempDir(), "import.xlsx")
	if err := os.WriteFile(sourcePath, body, 0o644); err != nil {
		t.Fatalf("写入测试源文件失败: %v", err)
	}
	taskQueue := &cleanupTaskQueueStub{Enabled: true, EnqueueErr: errors.Errorf("enqueue failed")}
	svcCtx := svc.NewServiceContext(config.Config{
		FileStorage: config.FileStorageConfig{
			Type:  storage.TypeLocal,
			Local: config.FileStorageLocalConfig{RootDir: objectRoot},
		},
	}, svc.Dependencies{})
	svcCtx.Task = taskQueue
	logicObj := NewFileTransferLogicWithContext(context.Background(), svcCtx)
	_, err := logicObj.persistServerUploadedFile(&transfer.UploadSession{
		UploadID:       "rollback-upload",
		BizType:        FileTransferBizSysConfigExcelImport,
		FileName:       "import.xlsx",
		StoredFileName: "rollback-upload.xlsx",
		ContentType:    "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		StoragePath:    sourcePath,
		FileHash:       hex.EncodeToString(sum[:]),
	})
	if err == nil {
		t.Fatal("清理任务投递失败时持久化应失败")
	}
	if _, err := os.Stat(sourcePath); err != nil {
		t.Fatalf("持久化失败后源文件应保留以便重试: %v", err)
	}
	regularFiles := 0
	if err := filepath.WalkDir(objectRoot, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			regularFiles++
		}
		return nil
	}); err != nil {
		t.Fatalf("检查对象存储目录失败: %v", err)
	}
	if regularFiles != 0 {
		t.Fatalf("清理任务投递失败后仍残留 %d 个对象", regularFiles)
	}
}
