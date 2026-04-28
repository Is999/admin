package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
)

// LocalStorage 表示基于本地文件系统的统一存储实现。
type LocalStorage struct {
	rootDir string // 本地文件根目录
	domain  string // 本地域名，用于识别自有文件 URL
}

// NewLocalStorage 创建本地文件存储实现。
func NewLocalStorage(cfg config.FileStorageConfig) *LocalStorage {
	rootDir := strings.TrimSpace(cfg.Local.RootDir)
	if rootDir == "" {
		rootDir = filepath.Join(os.TempDir(), "admin", "storage")
	}
	if absRootDir, err := filepath.Abs(rootDir); err == nil {
		rootDir = absRootDir
	}
	return &LocalStorage{
		rootDir: rootDir,
		domain:  trimTrailingSlash(cfg.Local.Domain),
	}
}

// Type 返回当前存储类型。
func (s *LocalStorage) Type() string {
	return TypeLocal
}

// SaveLocalFile 把本地文件复制到统一对象目录。
func (s *LocalStorage) SaveLocalFile(ctx context.Context, req SaveLocalFileReq) (*StoredObject, error) {
	ctx = nonNilContext(ctx)
	select {
	case <-ctx.Done():
		return nil, errors.Wrap(ctx.Err(), "保存本地文件前上下文已取消")
	default:
	}
	info, err := fileInfoFromPath(req.LocalPath)
	if err != nil {
		return nil, errors.Tag(err)
	}
	src, err := os.Open(strings.TrimSpace(req.LocalPath))
	if err != nil {
		return nil, errors.Wrap(err, "打开待保存文件失败")
	}
	defer src.Close()
	return s.saveFromReader(ctx, req.BizType, req.StoredFileName, req.OriginalFileName, req.ContentType, info.Size(), req.Visibility, src)
}

// SaveContent 把内容流写入本地对象目录。
func (s *LocalStorage) SaveContent(ctx context.Context, req SaveContentReq) (*StoredObject, error) {
	ctx = nonNilContext(ctx)
	select {
	case <-ctx.Done():
		return nil, errors.Wrap(ctx.Err(), "保存内容前上下文已取消")
	default:
	}
	return s.saveFromReader(ctx, req.BizType, req.StoredFileName, req.OriginalFileName, req.ContentType, req.Size, req.Visibility, req.Body)
}

// CreateDirectUpload 本地文件系统不支持浏览器直传，调用方应回退到服务端中转。
func (s *LocalStorage) CreateDirectUpload(context.Context, DirectUploadReq) (*DirectUploadPlan, error) {
	return nil, errors.Errorf("本地存储不支持前端直传")
}

// CompleteDirectUpload 本地文件系统不支持浏览器直传完成。
func (s *LocalStorage) CompleteDirectUpload(context.Context, string) (*StoredObject, error) {
	return nil, errors.Errorf("本地存储不支持前端直传")
}

// Open 打开本地对象流。
func (s *LocalStorage) Open(ctx context.Context, req OpenObjectReq) (*OpenObjectResult, error) {
	ctx = nonNilContext(ctx)
	select {
	case <-ctx.Done():
		return nil, errors.Wrap(ctx.Err(), "打开对象前上下文已取消")
	default:
	}
	objectKey, err := s.ResolveObjectKey(req.ObjectKey)
	if err != nil {
		return nil, errors.Tag(err)
	}
	filePath := s.absPath(objectKey)
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, errors.Wrap(err, "读取本地对象信息失败")
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, errors.Wrap(err, "打开本地对象失败")
	}
	return &OpenObjectResult{
		Reader:        file,
		ContentType:   DetectContentType(info.Name(), ""),
		ContentLength: info.Size(),
		FileName:      info.Name(),
		LastModified:  info.ModTime(),
		AcceptRanges:  true,
	}, nil
}

// Delete 删除本地对象，供上传校验失败时回滚使用。
func (s *LocalStorage) Delete(ctx context.Context, objectKey string) error {
	ctx = nonNilContext(ctx)
	select {
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "删除对象前上下文已取消")
	default:
	}
	objectKey, err := s.ResolveObjectKey(objectKey)
	if err != nil {
		return errors.Tag(err)
	}
	if err := os.Remove(s.absPath(objectKey)); err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "删除本地对象失败")
	}
	return nil
}

// ResolveObjectKey 把本地 URL 或对象 key 解析成统一 key。
func (s *LocalStorage) ResolveObjectKey(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.Errorf("对象标识不能为空")
	}
	if objectKey, ok := parseURLObjectKey(raw, s.domain); ok {
		raw = objectKey
	}
	// 本地对象统一限制在存储根目录下，禁止通过 `..` 等路径片段越界读取。
	raw, err := sanitizeResolvedObjectKey(raw)
	if err != nil {
		return "", errors.Tag(err)
	}
	return filepath.ToSlash(raw), nil
}

// saveFromReader 从流中保存文件。
func (s *LocalStorage) saveFromReader(ctx context.Context, bizType string, storedFileName string, originalFileName string, contentType string, size int64, visibility string, reader io.Reader) (*StoredObject, error) {
	if s == nil {
		return nil, errors.Errorf("本地文件存储未初始化")
	}
	if reader == nil {
		return nil, errors.Errorf("本地文件存储写入流不能为空")
	}
	storedFileName = strings.TrimSpace(storedFileName)
	if storedFileName == "" {
		storedFileName = sanitizeFileName(filepath.Base(strings.TrimSpace(originalFileName)))
	}
	objectKey := buildObjectKey("", bizType, storedFileName)
	filePath := s.absPath(objectKey)
	copyResult, err := copyToFile(filePath, newContextReader(ctx, reader))
	if err != nil {
		return nil, errors.Tag(err)
	}
	publicURL := ""
	// 仅 public 语义才向上层透出公开访问地址，private 对象统一走受控下载链路。
	if normalizeVisibility(visibility) == VisibilityPublic {
		publicURL = combineURL(s.domain, objectKey)
	}
	return &StoredObject{
		StorageType: TypeLocal,
		ObjectKey:   objectKey,
		FileName:    filepath.Base(filePath),
		ContentType: DetectContentType(originalFileName, contentType),
		Size:        firstNonNegativeInt64(copyResult.Size, size),
		SHA256:      copyResult.SHA256,
		PublicURL:   publicURL,
	}, nil
}

// absPath 返回对象的绝对路径。
func (s *LocalStorage) absPath(objectKey string) string {
	return filepath.Join(strings.TrimSpace(s.rootDir), filepath.FromSlash(strings.TrimLeft(objectKey, "/")))
}

// assert 实现了 ObjectStorage 接口。
var _ ObjectStorage = (*LocalStorage)(nil)
