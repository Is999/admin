package svc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"admin/common/runtimecfg"
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

// TestServiceContextVirusScannerUsesConfig 确保病毒扫描器按配置创建并复用。
func TestServiceContextVirusScannerUsesConfig(t *testing.T) {
	command := filepath.Join(t.TempDir(), "clamdscan")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("创建 clamdscan 测试替身失败: %v", err)
	}

	svcCtx := NewServiceContext(config.Config{
		FileStorage: config.FileStorageConfig{
			VirusScanner: config.FileStorageVirusScannerConfig{
				Name:    config.VirusScannerClamAV,
				Command: command,
			},
		},
	}, Dependencies{})

	scanner, err := svcCtx.VirusScanner()
	if err != nil {
		t.Fatalf("获取病毒扫描器失败: %v", err)
	}
	if err := scanner.Ready(context.Background()); err != nil {
		t.Fatalf("检查病毒扫描器失败: %v", err)
	}
	if err := scanner.ScanFile(context.Background(), "/tmp/demo.txt"); err != nil {
		t.Fatalf("执行病毒扫描失败: %v", err)
	}
	again, err := svcCtx.VirusScanner()
	if err != nil || again != scanner {
		t.Fatalf("相同配置应复用病毒扫描器 instance_same=%t err=%v", again == scanner, err)
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
	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "site-a"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})
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

// TestFileTransferUploadManagerFailsClosedOnAppIDMismatch 确保上传会话缓存不会写入错误站点命名空间。
func TestFileTransferUploadManagerFailsClosedOnAppIDMismatch(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})

	prev := runtimecfg.Get()
	runtimecfg.Set(config.Config{AppID: "site-b"})
	t.Cleanup(func() {
		runtimecfg.Restore(prev)
	})
	svcCtx := NewServiceContext(config.Config{
		AppID: "site-a",
		FileStorage: config.FileStorageConfig{
			UploadSession: config.FileStorageUploadSessionConfig{
				RootDir: t.TempDir(),
			},
		},
	}, Dependencies{Rds: client})

	if _, err := svcCtx.FileTransferUploadManager(); err == nil || !strings.Contains(err.Error(), "app_id") {
		t.Fatalf("FileTransferUploadManager() error = %v, want app_id mismatch", err)
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
