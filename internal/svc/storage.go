package svc

import (
	"encoding/json"
	"strings"
	"sync"

	"admin/internal/config"
	"admin/pkg/storage"

	utils "github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
)

const (
	// defaultStorageVirusScannerName 表示默认病毒扫描器注册名称。
	defaultStorageVirusScannerName = "noop"
)

// StorageRuntime 负责统一托管文件存储与上传安全扩展运行时。
// 该结构只缓存可复用组件实例，具体实现仍由 pkg/storage 注册表按配置创建。
type StorageRuntime struct {
	mu sync.RWMutex // 保护下方缓存字段

	objectStorage            storage.ObjectStorage // 当前文件存储实例
	objectStorageFingerprint string                // 当前文件存储配置指纹

	virusScanner     storage.VirusScanner // 当前病毒扫描器实例
	virusScannerName string               // 当前病毒扫描器注册名称
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

	// 配置变化或首次访问时进入写锁，二次确认后再按注册表创建实现。
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
// 扫描器通过注册名称选择，未配置时使用 pkg/storage 内置 noop 实现。
func (r *StorageRuntime) VirusScanner(cfg config.FileStorageConfig) (storage.VirusScanner, error) {
	if r == nil {
		return nil, errors.Errorf("文件存储运行时未初始化")
	}
	name := normalizeVirusScannerName(cfg.VirusScanner.Name)

	// 扫描器实例按注册名缓存，保证上传完成校验链路不重复初始化外部客户端。
	r.mu.RLock()
	if r.virusScanner != nil && r.virusScannerName == name {
		scanner := r.virusScanner
		r.mu.RUnlock()
		return scanner, nil
	}
	r.mu.RUnlock()

	// 注册名变化时重新从 pkg/storage 注册表获取扫描器实现。
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.virusScanner != nil && r.virusScannerName == name {
		return r.virusScanner, nil
	}
	scanner, err := storage.NewNamedVirusScanner(name)
	if err != nil {
		return nil, errors.Tag(err)
	}
	r.virusScanner = scanner
	r.virusScannerName = name
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

// storageRuntime 返回 ServiceContext 的文件存储运行时，兼容测试中直接构造 ServiceContext 的场景。
func (s *ServiceContext) storageRuntime() *StorageRuntime {
	if s == nil {
		return NewStorageRuntime()
	}
	// 正常启动路径会在 NewServiceContext 中初始化；这里兜底兼容单元测试直接构造 ServiceContext。
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

// normalizeVirusScannerName 归一化病毒扫描器名称，空值交给 pkg/storage 回退到 noop。
func normalizeVirusScannerName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return defaultStorageVirusScannerName
	}
	return name
}
