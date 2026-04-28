package svc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	keys "admin/common/rediskeys"
	"admin/internal/config"
	"admin/pkg/transfer"

	utils "github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
)

const (
	// defaultFileTransferSessionTTL 表示断点续传上传会话默认保留时间。
	defaultFileTransferSessionTTL = transfer.DefaultUploadSessionTTL
)

// FileTransferRuntime 负责统一托管文件上传会话管理器。
// 该结构只负责运行时缓存，具体会话读写由 pkg/transfer.LocalUploadManager 完成。
type FileTransferRuntime struct {
	mu sync.RWMutex // 保护下方缓存字段

	uploadManager            *transfer.LocalUploadManager // 当前断点续传上传管理器
	uploadManagerFingerprint string                       // 当前上传会话配置指纹
}

// NewFileTransferRuntime 创建文件上传运行时容器。
func NewFileTransferRuntime() *FileTransferRuntime {
	return &FileTransferRuntime{}
}

// UploadManager 按配置返回断点续传上传管理器实例。
// 当上传会话配置未变化时直接复用缓存实例，避免 logic 层重复装配 Redis key 与临时目录。
func (r *FileTransferRuntime) UploadManager(cfg config.FileStorageConfig, appID string, client redis.UniversalClient) (*transfer.LocalUploadManager, error) {
	if r == nil {
		return nil, errors.Errorf("文件上传运行时未初始化")
	}
	if client == nil {
		return nil, errors.Errorf("文件上传 Redis 客户端未初始化")
	}
	fingerprint := uploadManagerFingerprint(cfg.UploadSession, appID)

	// 高并发请求下优先走读锁快速路径，配置不变时无额外分配。
	r.mu.RLock()
	if r.uploadManager != nil && r.uploadManagerFingerprint == fingerprint {
		manager := r.uploadManager
		r.mu.RUnlock()
		return manager, nil
	}
	r.mu.RUnlock()

	// 首次访问或上传会话配置变化时重建管理器。
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.uploadManager != nil && r.uploadManagerFingerprint == fingerprint {
		return r.uploadManager, nil
	}
	manager := transfer.NewLocalUploadManager(
		client,
		fileTransferRootDir(cfg.UploadSession),
		keys.AppScopedKey(appID, keys.FileTransferUploadSession),
		keys.AppScopedKey(appID, keys.FileTransferUploadChunks),
		keys.AppScopedKey(appID, keys.FileTransferUploadFingerprint),
		keys.AppScopedKey(appID, keys.FileTransferUploadObjectIndex),
		fileTransferUploadSessionTTL(cfg),
	)
	r.uploadManager = manager
	r.uploadManagerFingerprint = fingerprint
	return manager, nil
}

// FileTransferUploadManager 返回当前 ServiceContext 绑定的断点续传上传管理器。
func (s *ServiceContext) FileTransferUploadManager() (*transfer.LocalUploadManager, error) {
	if s == nil {
		return nil, errors.Errorf("ServiceContext 为空")
	}
	cfg := s.CurrentConfig()
	return s.fileTransferRuntime().UploadManager(cfg.FileStorage, cfg.AppID, s.Rds)
}

// FileTransferUploadSessionTTL 返回当前上传会话保留时间。
func (s *ServiceContext) FileTransferUploadSessionTTL() time.Duration {
	if s == nil {
		return defaultFileTransferSessionTTL
	}
	return fileTransferUploadSessionTTL(s.CurrentConfig().FileStorage)
}

// fileTransferUploadSessionTTL 根据文件存储配置计算上传会话保留时间。
func fileTransferUploadSessionTTL(cfg config.FileStorageConfig) time.Duration {
	if cfg.UploadSession.TTLSeconds <= 0 {
		return defaultFileTransferSessionTTL
	}
	return time.Duration(cfg.UploadSession.TTLSeconds) * time.Second
}

// fileTransferRuntime 返回文件上传运行时，兼容测试中直接构造 ServiceContext 的场景。
func (s *ServiceContext) fileTransferRuntime() *FileTransferRuntime {
	if s == nil {
		return NewFileTransferRuntime()
	}
	// 正常启动路径会在 NewServiceContext 中初始化；这里兜底兼容单元测试直接构造 ServiceContext。
	if runtime, ok := s.uploadValue.Load().(*FileTransferRuntime); ok && runtime != nil {
		return runtime
	}
	runtime := NewFileTransferRuntime()
	s.uploadValue.Store(runtime)
	return runtime
}

// uploadManagerFingerprint 计算上传会话管理器配置指纹；指纹只用于比较，不输出配置明文。
func uploadManagerFingerprint(cfg config.FileStorageUploadSessionConfig, appID string) string {
	body, err := json.Marshal(struct {
		RootDir    string `json:"root_dir"`
		TTLSeconds int    `json:"ttl_seconds"`
		AppID      string `json:"app_id"`
	}{
		RootDir:    fileTransferRootDir(cfg),
		TTLSeconds: int(fileTransferUploadSessionTTL(config.FileStorageConfig{UploadSession: cfg}) / time.Second),
		AppID:      keys.NormalizeAppID(appID),
	})
	if err != nil {
		return ""
	}
	return utils.Sha256(string(body))
}

// fileTransferRootDir 返回文件传输组件的本地临时目录根路径。
func fileTransferRootDir(cfg config.FileStorageUploadSessionConfig) string {
	rootDir := strings.TrimSpace(cfg.RootDir)
	if rootDir == "" {
		rootDir = filepath.Join(os.TempDir(), "admin", "file-transfer")
	}
	if absRootDir, err := filepath.Abs(rootDir); err == nil {
		rootDir = absRootDir
	}
	return rootDir
}
