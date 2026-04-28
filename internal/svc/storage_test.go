package svc

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"admin/internal/config"
	"admin/pkg/storage"
	"admin/pkg/transfer"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

const (
	// testUploadSessionTTLSeconds 表示测试上传会话 TTL。
	testUploadSessionTTLSeconds = 60
)

// testServiceVirusScanner 是 ServiceContext 文件存储测试使用的病毒扫描器。
type testServiceVirusScanner struct {
	called *bool // 是否已执行扫描
}

// ScanFile 记录测试扫描调用。
func (s testServiceVirusScanner) ScanFile(context.Context, string, string) error {
	if s.called != nil {
		*s.called = true
	}
	return nil
}

// TestServiceContextObjectStorageCachesByConfig 确保对象存储实例按配置指纹缓存并在配置变化后重建。
func TestServiceContextObjectStorageCachesByConfig(t *testing.T) {
	rootDir := t.TempDir()
	svcCtx := NewServiceContext(config.Config{
		AppID: "site-a",
		FileStorage: config.FileStorageConfig{
			Type: storage.TypeLocal,
			Local: config.FileStorageLocalConfig{
				RootDir: rootDir,
			},
		},
	}, Dependencies{})

	first, err := svcCtx.ObjectStorage()
	if err != nil {
		t.Fatalf("首次创建对象存储失败: %v", err)
	}
	second, err := svcCtx.ObjectStorage()
	if err != nil {
		t.Fatalf("再次获取对象存储失败: %v", err)
	}
	if first != second {
		t.Fatal("相同配置应复用对象存储实例")
	}

	svcCtx.UpdateConfig(config.Config{
		FileStorage: config.FileStorageConfig{
			Type:       storage.TypeLocal,
			UploadMode: storage.UploadModeDirect,
			Local: config.FileStorageLocalConfig{
				RootDir: rootDir,
			},
			VirusScanner: config.FileStorageVirusScannerConfig{
				Name: "noop",
			},
		},
	})
	modeChanged, err := svcCtx.ObjectStorage()
	if err != nil {
		t.Fatalf("上传模式变化后获取对象存储失败: %v", err)
	}
	if modeChanged != first {
		t.Fatal("上传模式或扫描器配置变化不应重建对象存储实例")
	}

	svcCtx.UpdateConfig(config.Config{
		FileStorage: config.FileStorageConfig{
			Type: storage.TypeLocal,
			Local: config.FileStorageLocalConfig{
				RootDir: t.TempDir(),
			},
		},
	})
	third, err := svcCtx.ObjectStorage()
	if err != nil {
		t.Fatalf("配置变化后创建对象存储失败: %v", err)
	}
	if third == first {
		t.Fatal("配置变化后应重建对象存储实例")
	}
}

// TestScopedServiceContextSharesStorageRuntime 确保请求作用域 ServiceContext 复用全局存储运行时缓存。
func TestScopedServiceContextSharesStorageRuntime(t *testing.T) {
	svcCtx := NewServiceContext(config.Config{
		FileStorage: config.FileStorageConfig{
			Type: storage.TypeLocal,
			Local: config.FileStorageLocalConfig{
				RootDir: t.TempDir(),
			},
		},
	}, Dependencies{})
	scoped := svcCtx.ScopedWithContext(context.Background())

	first, err := svcCtx.ObjectStorage()
	if err != nil {
		t.Fatalf("全局上下文创建对象存储失败: %v", err)
	}
	second, err := scoped.ObjectStorage()
	if err != nil {
		t.Fatalf("作用域上下文获取对象存储失败: %v", err)
	}
	if first != second {
		t.Fatal("作用域上下文应共享同一存储运行时缓存")
	}
}

// TestServiceContextVirusScannerUsesConfig 确保病毒扫描器按配置注册名加载。
func TestServiceContextVirusScannerUsesConfig(t *testing.T) {
	called := false
	scannerName := fmt.Sprintf("svc_scanner_%d", time.Now().UnixNano())
	if err := storage.RegisterVirusScanner(scannerName, func() storage.VirusScanner {
		return testServiceVirusScanner{called: &called}
	}); err != nil {
		t.Fatalf("注册测试病毒扫描器失败: %v", err)
	}

	svcCtx := NewServiceContext(config.Config{
		FileStorage: config.FileStorageConfig{
			VirusScanner: config.FileStorageVirusScannerConfig{
				Name: scannerName,
			},
		},
	}, Dependencies{})

	scanner, err := svcCtx.VirusScanner()
	if err != nil {
		t.Fatalf("获取病毒扫描器失败: %v", err)
	}
	if err := scanner.ScanFile(context.Background(), "/tmp/demo.txt", "demo"); err != nil {
		t.Fatalf("执行病毒扫描失败: %v", err)
	}
	if !called {
		t.Fatal("期望配置指定的病毒扫描器被调用")
	}
}

// TestServiceContextFileTransferUploadManagerCachesByConfig 确保上传管理器按会话配置缓存并在配置变化后重建。
func TestServiceContextFileTransferUploadManagerCachesByConfig(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})

	rootDir := t.TempDir()
	svcCtx := NewServiceContext(config.Config{
		AppID: "site-a",
		FileStorage: config.FileStorageConfig{
			UploadSession: config.FileStorageUploadSessionConfig{
				RootDir:    rootDir,
				TTLSeconds: testUploadSessionTTLSeconds,
			},
		},
	}, Dependencies{Rds: client})

	first, err := svcCtx.FileTransferUploadManager()
	if err != nil {
		t.Fatalf("首次创建上传管理器失败: %v", err)
	}
	second, err := svcCtx.FileTransferUploadManager()
	if err != nil {
		t.Fatalf("再次获取上传管理器失败: %v", err)
	}
	if first != second {
		t.Fatal("相同上传会话配置应复用上传管理器实例")
	}

	session, err := first.Init(context.Background(), transferInitRequest())
	if err != nil {
		t.Fatalf("初始化上传会话失败: %v", err)
	}
	relPath, err := filepath.Rel(rootDir, session.TempPath)
	if err != nil {
		t.Fatalf("计算上传临时路径失败: %v", err)
	}
	if strings.HasPrefix(relPath, "..") {
		t.Fatalf("上传临时路径应位于配置根目录内: %s", session.TempPath)
	}
	if svcCtx.FileTransferUploadSessionTTL() != time.Duration(testUploadSessionTTLSeconds)*time.Second {
		t.Fatal("上传会话 TTL 未按配置生效")
	}

	scoped := svcCtx.ScopedWithContext(context.Background())
	scopedManager, err := scoped.FileTransferUploadManager()
	if err != nil {
		t.Fatalf("作用域上下文获取上传管理器失败: %v", err)
	}
	if scopedManager != first {
		t.Fatal("作用域上下文应共享上传运行时缓存")
	}

	svcCtx.UpdateConfig(config.Config{
		AppID: "site-a",
		FileStorage: config.FileStorageConfig{
			UploadSession: config.FileStorageUploadSessionConfig{
				RootDir:    t.TempDir(),
				TTLSeconds: testUploadSessionTTLSeconds,
			},
		},
	})
	third, err := svcCtx.FileTransferUploadManager()
	if err != nil {
		t.Fatalf("配置变化后创建上传管理器失败: %v", err)
	}
	if third == first {
		t.Fatal("上传会话配置变化后应重建上传管理器实例")
	}
}

// transferInitRequest 返回测试上传会话初始化请求。
func transferInitRequest() transfer.InitUploadReq {
	return transfer.InitUploadReq{
		OperatorID:  1,
		BizType:     "admin-avatar",
		FileName:    "demo.txt",
		FileSize:    5,
		ChunkSize:   5,
		ContentType: "text/plain",
	}
}
