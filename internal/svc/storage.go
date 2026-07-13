package svc

import (
	"encoding/json"
	"sync"

	"admin/internal/config"
	"admin/pkg/storage"

	utils "github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
)

// StorageRuntime 统一缓存文件存储与病毒扫描器实例。
type StorageRuntime struct {
	mu sync.RWMutex // 保护下方缓存字段

	objectStorage            storage.ObjectStorage // 当前文件存储实例
	objectStorageFingerprint string                // 当前文件存储配置指纹

	virusScanner       storage.VirusScanner                 // 当前病毒扫描器实例
	virusScannerConfig config.FileStorageVirusScannerConfig // 当前病毒扫描器配置
}

// NewStorageRuntime 创建文件存储运行时容器。
func NewStorageRuntime() *StorageRuntime {
	return &StorageRuntime{}
}

// ObjectStorage 按配置返回统一文件存储实例。
// 当配置未变化时直接复用缓存实例，避免每次请求重复创建 S3 client。
func (r *StorageRuntime) ObjectStorage(cfg config.FileStorageConfig) (storage.ObjectStorage, error) {
	if r == nil {
		return nil, errors.Errorf("文件存储运行时未初始化")
	}
	fingerprint := objectStorageFingerprint(cfg)

	// 先走读锁快速路径，高并发请求下避免重复创建底层客户端。
	r.mu.RLock()
	if r.objectStorage != nil && r.objectStorageFingerprint == fingerprint {
		objectStorage := r.objectStorage
		r.mu.RUnlock()
		return objectStorage, nil
	}
	r.mu.RUnlock()

	// 配置变化或首次访问时进入写锁，二次确认后再创建实现。
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.objectStorage != nil && r.objectStorageFingerprint == fingerprint {
		return r.objectStorage, nil
	}
	objectStorage, err := storage.NewObjectStorage(cfg)
	if err != nil {
		return nil, errors.Tag(err)
	}
	r.objectStorage = objectStorage
	r.objectStorageFingerprint = fingerprint
	return objectStorage, nil
}

// VirusScanner 按配置返回病毒扫描器实例。
func (r *StorageRuntime) VirusScanner(cfg config.FileStorageConfig) (storage.VirusScanner, error) {
	if r == nil {
		return nil, errors.Errorf("文件存储运行时未初始化")
	}
	// 扫描器实例按完整配置缓存，保证上传完成校验链路不重复查找外部命令。
	r.mu.RLock()
	if r.virusScanner != nil && r.virusScannerConfig == cfg.VirusScanner {
		scanner := r.virusScanner
		r.mu.RUnlock()
		return scanner, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.virusScanner != nil && r.virusScannerConfig == cfg.VirusScanner {
		return r.virusScanner, nil
	}
	scanner, err := storage.NewVirusScanner(cfg.VirusScanner)
	if err != nil {
		return nil, errors.Tag(err)
	}
	r.virusScanner = scanner
	r.virusScannerConfig = cfg.VirusScanner
	return scanner, nil
}

// ObjectStorage 返回当前 ServiceContext 绑定的统一文件存储实例。
func (s *ServiceContext) ObjectStorage() (storage.ObjectStorage, error) {
	if s == nil {
		return nil, errors.Errorf("ServiceContext 为空")
	}
	return s.storageRuntime().ObjectStorage(s.CurrentConfig().FileStorage)
}

// VirusScanner 返回当前 ServiceContext 绑定的病毒扫描器实例。
func (s *ServiceContext) VirusScanner() (storage.VirusScanner, error) {
	if s == nil {
		return nil, errors.Errorf("ServiceContext 为空")
	}
	return s.storageRuntime().VirusScanner(s.CurrentConfig().FileStorage)
}

// UploadMode 返回当前配置归一化后的上传模式。
func (s *ServiceContext) UploadMode() string {
	if s == nil {
		return storage.UploadModeServer
	}
	return storage.NormalizeUploadMode(s.CurrentConfig().FileStorage)
}

// storageRuntime 返回 ServiceContext 的文件存储运行时，支持测试直接构造 ServiceContext。
func (s *ServiceContext) storageRuntime() *StorageRuntime {
	if s == nil {
		return NewStorageRuntime()
	}
	// 正常启动路径会在 NewServiceContext 中初始化；这里为单元测试直接构造 ServiceContext 提供默认运行时。
	if runtime, ok := s.storageValue.Load().(*StorageRuntime); ok && runtime != nil {
		return runtime
	}
	runtime := NewStorageRuntime()
	s.storageValue.Store(runtime)
	return runtime
}

// objectStorageFingerprint 计算对象存储相关配置指纹；指纹只用于比较，不输出敏感配置明文。
func objectStorageFingerprint(cfg config.FileStorageConfig) string {
	// 仅纳入会影响对象存储实例的字段，避免上传模式或扫描器变更导致 S3 client 无意义重建。
	body, err := json.Marshal(struct {
		Type  string                        `json:"type"`  // Type 表示文件存储类型。
		Local config.FileStorageLocalConfig `json:"local"` // Local 表示本地存储配置。
		S3    config.FileStorageS3Config    `json:"s3"`    // S3 表示对象存储配置。
	}{
		Type:  cfg.Type,
		Local: cfg.Local,
		S3:    cfg.S3,
	})
	if err != nil {
		return ""
	}
	return utils.SHA256(string(body))
}
