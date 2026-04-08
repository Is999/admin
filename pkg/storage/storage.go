package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	stdErrors "errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"admin_cron/helper"
	"admin_cron/internal/config"

	"github.com/Is999/go-utils/errors"
)

const (
	// TypeLocal 表示本地文件系统存储。
	TypeLocal = "local"
	// TypeS3 表示 AWS S3 对象存储。
	TypeS3 = "s3"

	// UploadModeServer 表示前端文件经服务端中转上传。
	UploadModeServer = "server"
	// UploadModeDirect 表示服务端下发预签名后由前端直传。
	UploadModeDirect = "direct"

	// VisibilityPrivate 表示对象仅允许受控下载。
	VisibilityPrivate = "private"
	// VisibilityPublic 表示对象允许公开访问。
	VisibilityPublic = "public"

	// copyBufferSize 表示本地存储流式复制缓冲大小。
	copyBufferSize = 1024 * 1024
	// defaultObjectBizType 表示未传业务类型时的对象目录兜底值。
	defaultObjectBizType = "common"
	// defaultSafeFileName 表示文件名清洗后为空时的兜底文件名。
	defaultSafeFileName = "file"
	// defaultContentType 表示无法从上传声明或扩展名识别 MIME 时的兜底类型。
	defaultContentType = "application/octet-stream"
	// objectDatePathLayout 表示对象 key 的年月/日分区格式。
	objectDatePathLayout = "200601/02"
	// storageTempFilePattern 表示本地对象写入同目录临时文件模板。
	storageTempFilePattern = ".upload-*"
	// storageCopyTempFilePattern 表示跨设备复制到目标目录时的临时文件模板。
	storageCopyTempFilePattern = ".upload-copy-*"
	// storageDirectoryPerm 表示本地对象目录权限，允许服务账号写入和同组读取。
	storageDirectoryPerm os.FileMode = 0o755
	// storageFilePerm 表示最终对象文件权限，避免可执行权限和非必要写权限。
	storageFilePerm os.FileMode = 0o644
)

// SaveLocalFileReq 表示把本地文件持久化到统一存储的请求。
type SaveLocalFileReq struct {
	BizType          string // 业务类型
	ContentType      string // 文件 MIME
	LocalPath        string // 待上传本地文件路径
	OriginalFileName string // 原始文件名，仅用于扩展名推导
	StoredFileName   string // 统一生成后的实际文件名
	Visibility       string // 文件可见性
}

// SaveContentReq 表示把内存内容持久化到统一存储的请求。
type SaveContentReq struct {
	BizType          string    // 业务类型
	ContentType      string    // 文件 MIME
	Body             io.Reader // 文件内容
	Size             int64     // 文件大小
	OriginalFileName string    // 原始文件名，仅用于扩展名推导
	StoredFileName   string    // 统一生成后的实际文件名
	Visibility       string    // 文件可见性
}

// DirectUploadReq 表示生成前端直传预签名的请求。
type DirectUploadReq struct {
	BizType          string // 业务类型
	ContentType      string // 文件 MIME
	FileSize         int64  // 文件大小
	OriginalFileName string // 原始文件名，仅用于扩展名推导
	StoredFileName   string // 统一生成后的实际文件名
	Visibility       string // 文件可见性
}

// DirectUploadPlan 表示前端直传所需的预签名结果。
type DirectUploadPlan struct {
	Method    string            `json:"method"`    // HTTP 方法
	URL       string            `json:"url"`       // 预签名上传地址
	Headers   map[string]string `json:"headers"`   // 需要附带的请求头
	ExpiresAt string            `json:"expiresAt"` // 过期时间
	ObjectKey string            `json:"objectKey"` // 对象 key
}

// OpenObjectReq 表示打开对象流时的读取选项。
type OpenObjectReq struct {
	ObjectKey   string // 对象 key
	RangeHeader string // 原始 Range 请求头；为空表示读取完整对象
}

// StoredObject 表示统一存储后的对象信息。
type StoredObject struct {
	StorageType string // 存储类型
	ObjectKey   string // 对象 key
	FileName    string // 实际存储文件名
	ContentType string // 文件 MIME
	Size        int64  // 文件大小
	SHA256      string // 对象内容 SHA-256 摘要；本地顺序落盘时随写入流式计算
	PublicURL   string // 公开访问 URL；私有对象始终为空
}

// OpenObjectResult 表示打开对象流后的读取结果。
type OpenObjectResult struct {
	Reader        io.ReadCloser // 对象读取流
	ContentType   string        // 文件 MIME
	ContentLength int64         // 文件长度
	FileName      string        // 默认文件名
	LastModified  time.Time     // 最近修改时间
	AcceptRanges  bool          // 是否声明支持字节范围读取
	ContentRange  string        // 部分读取时的 Content-Range 头
}

// ObjectStorage 定义统一文件存储组件能力。
type ObjectStorage interface {
	// Type 返回当前存储实现类型，如 local 或 s3。
	Type() string

	// SaveLocalFile 把本地已有文件保存到统一存储，并返回最终对象信息。
	SaveLocalFile(ctx context.Context, req SaveLocalFileReq) (*StoredObject, error)

	// SaveContent 把内存或流式内容直接写入统一存储，并返回最终对象信息。
	SaveContent(ctx context.Context, req SaveContentReq) (*StoredObject, error)

	// CreateDirectUpload 生成前端直传所需的上传计划，如预签名地址和必要请求头。
	CreateDirectUpload(ctx context.Context, req DirectUploadReq) (*DirectUploadPlan, error)

	// CompleteDirectUpload 在前端直传完成后校验并返回统一存储对象信息。
	CompleteDirectUpload(ctx context.Context, objectKey string) (*StoredObject, error)

	// Open 打开指定对象的读取流，并返回下载/预览所需的元数据。
	Open(ctx context.Context, req OpenObjectReq) (*OpenObjectResult, error)

	// Delete 删除指定对象，供上传回滚或清理过期文件使用。
	Delete(ctx context.Context, objectKey string) error

	// ResolveObjectKey 把对象 key、公开 URL 或兼容地址统一解析为内部对象 key。
	ResolveObjectKey(raw string) (string, error)
}

// ObjectStorageFactory 定义对象存储工厂函数。
type ObjectStorageFactory func(config.FileStorageConfig) (ObjectStorage, error)

// VirusScanner 定义病毒扫描扩展点；默认实现为空操作。
type VirusScanner interface {
	// ScanFile 对指定文件执行病毒扫描；命中风险时返回错误阻断后续流程。
	ScanFile(ctx context.Context, filePath string, bizType string) error
}

// VirusScannerFactory 定义病毒扫描器工厂函数。
type VirusScannerFactory func() VirusScanner

const (
	// defaultVirusScannerName 表示默认病毒扫描器名称。
	defaultVirusScannerName = "noop"
)

var objectStorageFactories = struct {
	mu        sync.RWMutex
	factories map[string]ObjectStorageFactory
}{
	factories: map[string]ObjectStorageFactory{
		TypeLocal: func(cfg config.FileStorageConfig) (ObjectStorage, error) {
			return NewLocalStorage(cfg), nil
		},
		TypeS3: func(cfg config.FileStorageConfig) (ObjectStorage, error) {
			return NewS3Storage(cfg)
		},
	},
}

var virusScannerFactories = struct {
	mu        sync.RWMutex
	factories map[string]VirusScannerFactory
}{
	factories: map[string]VirusScannerFactory{
		defaultVirusScannerName: func() VirusScanner {
			return noopVirusScanner{}
		},
	},
}

// storageCopyBufferPool 复用本地文件复制缓冲，减少高频上传时的堆分配。
var storageCopyBufferPool = sync.Pool{
	New: func() any {
		// 每个缓冲固定 1MB，兼顾大文件顺序复制吞吐和并发上传时的内存上限。
		buffer := make([]byte, copyBufferSize)
		return &buffer
	},
}

// copyToFileResult 表示本地流式落盘后的结果元数据。
type copyToFileResult struct {
	Size   int64  // 实际写入字节数，来源于本次 io.CopyBuffer 的返回值
	SHA256 string // 写入过程中同步计算的 SHA-256 摘要，避免落盘后再次扫盘
}

// RegisterObjectStorage 注册对象存储实现，便于扩展 MinIO、OSS、COS 等后端。
func RegisterObjectStorage(storageType string, factory ObjectStorageFactory) error {
	if strings.TrimSpace(storageType) == "" {
		return errors.Errorf("对象存储类型不能为空")
	}
	storageType = normalizeStorageType(storageType)
	if factory == nil {
		return errors.Errorf("对象存储工厂不能为空: %s", storageType)
	}
	objectStorageFactories.mu.Lock()
	defer objectStorageFactories.mu.Unlock()
	if _, exists := objectStorageFactories.factories[storageType]; exists {
		return errors.Errorf("对象存储类型已注册: %s", storageType)
	}
	objectStorageFactories.factories[storageType] = factory
	return nil
}

// NewObjectStorage 根据运行配置创建统一文件存储实例。
func NewObjectStorage(cfg config.FileStorageConfig) (ObjectStorage, error) {
	storageType := normalizeStorageType(cfg.Type)
	objectStorageFactories.mu.RLock()
	factory := objectStorageFactories.factories[storageType]
	objectStorageFactories.mu.RUnlock()
	if factory == nil {
		return nil, errors.Errorf("不支持的文件存储类型: %s", storageType)
	}
	return factory(cfg)
}

// RegisterVirusScanner 注册病毒扫描器实现，便于接入 ClamAV 或云查毒。
func RegisterVirusScanner(name string, factory VirusScannerFactory) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return errors.Errorf("病毒扫描器名称不能为空")
	}
	if factory == nil {
		return errors.Errorf("病毒扫描器工厂不能为空: %s", name)
	}
	virusScannerFactories.mu.Lock()
	defer virusScannerFactories.mu.Unlock()
	if _, exists := virusScannerFactories.factories[name]; exists {
		return errors.Errorf("病毒扫描器已注册: %s", name)
	}
	virusScannerFactories.factories[name] = factory
	return nil
}

// NewVirusScanner 返回当前启用的病毒扫描器；默认使用空实现。
func NewVirusScanner() VirusScanner {
	scanner, err := NewNamedVirusScanner(defaultVirusScannerName)
	if err != nil {
		return noopVirusScanner{}
	}
	return scanner
}

// NewNamedVirusScanner 按名称创建病毒扫描器。
func NewNamedVirusScanner(name string) (VirusScanner, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = defaultVirusScannerName
	}
	virusScannerFactories.mu.RLock()
	factory := virusScannerFactories.factories[name]
	virusScannerFactories.mu.RUnlock()
	if factory == nil {
		return nil, errors.Errorf("不支持的病毒扫描器: %s", name)
	}
	scanner := factory()
	if scanner == nil {
		return nil, errors.Errorf("病毒扫描器初始化为空: %s", name)
	}
	return scanner, nil
}

// NormalizeUploadMode 返回归一化后的上传模式；当本地存储误配直传时回退为服务端中转。
func NormalizeUploadMode(cfg config.FileStorageConfig) string {
	mode := strings.ToLower(strings.TrimSpace(cfg.UploadMode))
	if mode != UploadModeDirect {
		return UploadModeServer
	}
	if normalizeStorageType(cfg.Type) != TypeS3 || !cfg.S3.Enabled {
		return UploadModeServer
	}
	return UploadModeDirect
}

// BuildStoredFileName 生成统一的 UUID 文件名，避免原始文件名注入和覆盖风险。
func BuildStoredFileName(fileID string, originalFileName string) string {
	fileID = sanitizeFileName(strings.TrimSpace(fileID))
	ext := sanitizeExt(filepath.Ext(strings.TrimSpace(originalFileName)))
	if ext == "" {
		return fileID
	}
	return fileID + ext
}

// DetectContentType 根据文件扩展名和回退值推导内容类型。
func DetectContentType(fileName string, fallback string) string {
	contentType := strings.TrimSpace(fallback)
	if contentType != "" {
		if mediaType, _, err := mime.ParseMediaType(contentType); err == nil && strings.TrimSpace(mediaType) != "" {
			return contentType
		}
	}
	contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))))
	if strings.TrimSpace(contentType) == "" {
		contentType = defaultContentType
	}
	return contentType
}

// normalizeStorageType 归一化存储类型，空值默认使用本地存储。
func normalizeStorageType(storageType string) string {
	switch strings.ToLower(strings.TrimSpace(storageType)) {
	case "", TypeLocal:
		return TypeLocal
	case TypeS3:
		return TypeS3
	default:
		return strings.ToLower(strings.TrimSpace(storageType))
	}
}

// normalizeVisibility 归一化文件可见性，非 public 一律按 private 处理。
func normalizeVisibility(visibility string) string {
	if strings.EqualFold(strings.TrimSpace(visibility), VisibilityPublic) {
		return VisibilityPublic
	}
	return VisibilityPrivate
}

// buildObjectKey 生成统一对象 key，包含可选路径前缀、业务段、日期分区和清洗后的文件名。
func buildObjectKey(pathPrefix string, bizType string, storedFileName string) string {
	normalizedPathPrefix := sanitizeObjectSegment(strings.TrimSpace(pathPrefix))
	normalizedBizType := sanitizeObjectSegment(strings.TrimSpace(bizType))
	if normalizedBizType == "" {
		normalizedBizType = defaultObjectBizType
	}
	datePath := time.Now().Format(objectDatePathLayout)
	if normalizedPathPrefix == "" {
		return filepath.ToSlash(filepath.Join(normalizedBizType, datePath, sanitizeFileName(storedFileName)))
	}
	return filepath.ToSlash(filepath.Join(normalizedPathPrefix, normalizedBizType, datePath, sanitizeFileName(storedFileName)))
}

// sanitizeObjectSegment 清洗对象 key 的业务路径段，只保留安全字符。
func sanitizeObjectSegment(value string) string {
	if value == "" {
		return ""
	}
	builder := strings.Builder{}
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z':
			builder.WriteRune(ch)
		case ch >= 'A' && ch <= 'Z':
			builder.WriteRune(ch + ('a' - 'A'))
		case ch >= '0' && ch <= '9':
			builder.WriteRune(ch)
		case ch == '-', ch == '_', ch == '/':
			builder.WriteRune(ch)
		}
	}
	result := strings.Trim(builder.String(), "/")
	return strings.ReplaceAll(result, "//", "/")
}

// sanitizeFileName 清洗文件名，只保留可控 ASCII 字符，避免路径注入。
func sanitizeFileName(fileName string) string {
	fileName = filepath.Base(strings.TrimSpace(fileName))
	if fileName == "." || fileName == "/" || fileName == "" {
		return defaultSafeFileName
	}
	builder := strings.Builder{}
	for _, ch := range fileName {
		switch {
		case ch >= 'a' && ch <= 'z':
			builder.WriteRune(ch)
		case ch >= 'A' && ch <= 'Z':
			builder.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			builder.WriteRune(ch)
		case ch == '-', ch == '_', ch == '.':
			builder.WriteRune(ch)
		}
	}
	if builder.Len() == 0 {
		return defaultSafeFileName
	}
	return builder.String()
}

// sanitizeExt 清洗扩展名，拒绝路径片段和上级目录语义。
func sanitizeExt(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	if strings.Contains(ext, "/") || strings.Contains(ext, `\`) || strings.Contains(ext, "..") {
		return ""
	}
	return sanitizeFileName(ext)
}

// trimTrailingSlash 清理 URL 或域名末尾斜杠。
func trimTrailingSlash(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

// combineURL 拼接基础域名与对象 key。
func combineURL(base string, objectKey string) string {
	base = trimTrailingSlash(base)
	objectKey = strings.TrimLeft(strings.TrimSpace(objectKey), "/")
	if base == "" || objectKey == "" {
		return ""
	}
	return base + "/" + objectKey
}

// nonNilContext 返回可安全使用的上下文，避免工具包被直接调用时因 nil context 崩溃。
func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// firstNonNegativeInt64 返回第一个非负整数。
func firstNonNegativeInt64(values ...int64) int64 {
	for _, value := range values {
		if value >= 0 {
			return value
		}
	}
	return 0
}

// newContextReader 在每次读取前检查上下文，确保本地大文件复制可被及时取消。
func newContextReader(ctx context.Context, reader io.Reader) io.Reader {
	return &contextReader{ctx: nonNilContext(ctx), reader: reader}
}

// contextReader 是带上下文取消感知的 io.Reader 包装器。
type contextReader struct {
	ctx    context.Context // 读取上下文
	reader io.Reader       // 原始读取器
}

// Read 读取数据，并在上下文取消时尽快返回错误。
func (r *contextReader) Read(p []byte) (int, error) {
	if r == nil || r.reader == nil {
		return 0, io.ErrUnexpectedEOF
	}
	if err := r.ctx.Err(); err != nil {
		return 0, errors.Tag(err)
	}
	n, err := r.reader.Read(p)
	if err == nil {
		if ctxErr := r.ctx.Err(); ctxErr != nil {
			return n, errors.Tag(ctxErr)
		}
	}
	if err != nil {
		if stdErrors.Is(err, io.EOF) {
			return n, err
		}
		return n, errors.Tag(err)
	}
	return n, nil
}

// parseURLObjectKey 从已配置域名 URL 中解析对象 key。
func parseURLObjectKey(raw string, domain string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	rawDomain := trimTrailingSlash(domain)
	if rawDomain != "" && strings.HasPrefix(raw, rawDomain+"/") {
		return strings.TrimLeft(strings.TrimPrefix(raw, rawDomain), "/"), true
	}
	return "", false
}

// sanitizeResolvedObjectKey 校验外部传入或 URL 解析出的对象 key，避免路径穿越和异常路径片段。
func sanitizeResolvedObjectKey(raw string) (string, error) {
	raw = strings.TrimLeft(strings.TrimSpace(raw), "/")
	if raw == "" || strings.Contains(raw, `\`) || strings.ContainsAny(raw, "\r\n\x00") {
		return "", errors.Errorf("对象标识不合法")
	}
	for _, segment := range strings.Split(raw, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", errors.Errorf("对象标识不合法")
		}
	}
	return raw, nil
}

// fileInfoFromPath 读取本地文件信息，并拒绝目录输入。
func fileInfoFromPath(filePath string) (os.FileInfo, error) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return nil, errors.Errorf("文件路径不能为空")
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, errors.Wrap(err, "读取文件信息失败")
	}
	if info.IsDir() {
		return nil, errors.Errorf("目标路径不能是目录")
	}
	return info, nil
}

// copyToFile 使用临时文件 + 原子 rename 写入目标路径，并在流式写入时同步计算内容摘要。
func copyToFile(dstPath string, reader io.Reader) (*copyToFileResult, error) {
	if reader == nil {
		return nil, errors.Errorf("写入存储文件的读取流不能为空")
	}
	dstPath = strings.TrimSpace(dstPath)
	if dstPath == "" {
		return nil, errors.Errorf("写入存储文件路径不能为空")
	}
	dir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dir, storageDirectoryPerm); err != nil {
		return nil, errors.Wrap(err, "创建文件目录失败")
	}
	file, err := os.CreateTemp(dir, storageTempFilePattern)
	if err != nil {
		return nil, errors.Wrap(err, "创建存储临时文件失败")
	}
	tempPath := file.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()
	bufferPtr := storageCopyBufferPool.Get().(*[]byte)
	defer storageCopyBufferPool.Put(bufferPtr)
	// digest 在读取源流时同步接收字节，确保摘要来自本次落盘输入而不是事后重新打开目标文件。
	digest := sha256.New()
	// hashingReader 用 TeeReader 把同一份输入同时送入文件和摘要计算器，避免大文件二次磁盘 I/O。
	hashingReader := io.TeeReader(reader, digest)
	// written 记录本次真实写入量，供调用方返回对象大小并发现上游 size 元数据偏差。
	written, err := io.CopyBuffer(file, hashingReader, *bufferPtr)
	if err != nil {
		_ = file.Close()
		return nil, errors.Wrap(err, "写入存储文件失败")
	}
	if err := file.Chmod(storageFilePerm); err != nil {
		_ = file.Close()
		return nil, errors.Wrap(err, "设置存储文件权限失败")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, errors.Wrap(err, "同步存储临时文件失败")
	}
	if err := file.Close(); err != nil {
		return nil, errors.Wrap(err, "关闭存储临时文件失败")
	}
	if err := commitTempFile(tempPath, dstPath); err != nil {
		return nil, errors.Tag(err)
	}
	return &copyToFileResult{
		Size:   written,
		SHA256: hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

// commitTempFile 提交临时文件；同设备优先使用 rename，跨设备时降级为复制到目标目录后再原子替换。
func commitTempFile(tempPath string, dstPath string) error {
	if err := os.Rename(tempPath, dstPath); err != nil {
		if !helper.IsCrossDeviceRenameError(err) {
			return errors.Wrap(err, "提交存储文件失败")
		}
		if copyErr := copyAcrossDevices(tempPath, dstPath); copyErr != nil {
			return errors.Wrap(copyErr, "跨设备提交存储文件失败")
		}
	}
	return nil
}

// copyAcrossDevices 处理临时目录和目标目录不在同一挂载卷时的落盘兜底。
// 复制阶段仍写入目标目录下的临时文件，最后一次 rename 保证目标路径不会暴露半截内容。
func copyAcrossDevices(srcPath string, dstPath string) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return errors.Wrap(err, "打开跨设备源文件失败")
	}
	defer srcFile.Close()

	dstDir := filepath.Dir(dstPath)
	dstFile, err := os.CreateTemp(dstDir, storageCopyTempFilePattern)
	if err != nil {
		return errors.Wrap(err, "创建跨设备目标临时文件失败")
	}
	dstTempPath := dstFile.Name()
	defer func() {
		_ = os.Remove(dstTempPath)
	}()

	bufferPtr := storageCopyBufferPool.Get().(*[]byte)
	defer storageCopyBufferPool.Put(bufferPtr)
	if _, err := io.CopyBuffer(dstFile, srcFile, *bufferPtr); err != nil {
		_ = dstFile.Close()
		return errors.Wrap(err, "复制跨设备存储文件失败")
	}
	if err := dstFile.Chmod(storageFilePerm); err != nil {
		_ = dstFile.Close()
		return errors.Wrap(err, "设置跨设备存储文件权限失败")
	}
	if err := dstFile.Sync(); err != nil {
		_ = dstFile.Close()
		return errors.Wrap(err, "同步跨设备目标临时文件失败")
	}
	if err := dstFile.Close(); err != nil {
		return errors.Wrap(err, "关闭跨设备目标临时文件失败")
	}
	if err := os.Rename(dstTempPath, dstPath); err != nil {
		return errors.Wrap(err, "提交跨设备目标文件失败")
	}
	_ = os.Remove(srcPath)
	return nil
}

// buildInlineHeaders 构造直传兼容响应头。
func buildInlineHeaders(contentType string, fileName string, disposition string) http.Header {
	headers := http.Header{}
	headers.Set("Content-Type", DetectContentType(fileName, contentType))
	headers.Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, sanitizeFileName(fileName)))
	return headers
}

type noopVirusScanner struct{}

// ScanFile 默认不执行任何病毒扫描；后续可替换为 ClamAV 或云查毒实现。
func (noopVirusScanner) ScanFile(context.Context, string, string) error {
	return nil
}
