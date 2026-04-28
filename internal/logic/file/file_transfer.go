package file

import (
	corelogic "admin/internal/logic"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	i18n "admin/common/i18n"
	"admin/internal/svc"
	"admin/internal/types"
	"admin/pkg/storage"
	"admin/pkg/transfer"

	"github.com/Is999/go-utils/errors"
	"github.com/google/uuid"
)

const (
	// fileTransferMinChunkSize 表示断点续传允许的最小分片大小，避免极小分片放大请求与 Redis 状态开销。
	fileTransferMinChunkSize int64 = 256 * 1024
	// fileTransferMaxChunkCount 表示单个上传会话允许的最大分片数，防止异常分片参数拖垮上传链路。
	fileTransferMaxChunkCount = 2000

	// FileTransferBizAdminAvatar 表示管理员头像上传业务。
	FileTransferBizAdminAvatar = "admin-avatar"
	// FileTransferBizSecretKeyMaterial 表示秘钥材料上传业务。
	FileTransferBizSecretKeyMaterial = "secret-key-material"
	// FileTransferBizSysConfigExcelImport 表示字典 Excel 导入文件上传业务。
	FileTransferBizSysConfigExcelImport = "sys-config-excel-import"
)

// FileTransferLogic 承载导入导出文件上传下载通用逻辑。
type FileTransferLogic struct {
	*corelogic.BaseLogic
}

// FileUploadPolicy 描述不同业务文件的安全校验与访问控制规则。
type FileUploadPolicy struct {
	MaxSize            int64    // 最大文件大小
	AllowedExts        []string // 允许扩展名
	Visibility         string   // 可见性：public/private
	AllowPublicAccess  bool     // 是否允许匿名访问
	AllowDirectUpload  bool     // 是否允许走前端直传
	ExpectedMIMEPrefix string   // 期望 MIME 前缀，用于上传完成后做基础探测
}

// NewFileTransferLogic 创建文件传输逻辑对象。
func NewFileTransferLogic(r *http.Request, svcCtx *svc.ServiceContext) *FileTransferLogic {
	return &FileTransferLogic{
		BaseLogic: corelogic.NewBaseLogic(r, svcCtx),
	}
}

// NewFileTransferLogicWithContext 创建绑定任意上下文的文件传输逻辑对象。
func NewFileTransferLogicWithContext(ctx context.Context, svcCtx *svc.ServiceContext) *FileTransferLogic {
	return &FileTransferLogic{
		BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx),
	}
}

// InitUpload 初始化断点续传上传会话。
func (l *FileTransferLogic) InitUpload(req *types.FileUploadInitReq) *types.BizResult {
	if err := l.validateInitRequest(req); err != nil {
		return types.ParamErrorResult(err).
			WithError(corelogic.WrapLogicError(err, "FileTransferLogic.InitUpload 参数校验失败"))
	}
	policy, err := l.uploadPolicy(strings.TrimSpace(req.BizType))
	if err != nil {
		return types.ParamErrorResult(err).
			WithError(corelogic.WrapLogicError(err, "FileTransferLogic.InitUpload 业务类型校验失败"))
	}
	ctxAdmin := l.GetCtxAdmin()
	operatorID := 0
	if ctxAdmin != nil {
		operatorID = ctxAdmin.ID
	}
	uploadMode := l.uploadMode(policy)
	var session *transfer.UploadSession
	if uploadMode == storage.UploadModeDirect {
		// 前端直传模式由服务端先签发上传计划，再在完成态做二次校验与入库确认。
		session, err = l.initDirectUpload(req, operatorID, policy)
	} else {
		// 本地存储或服务端中转模式直接初始化断点续传会话，后续由分片上传写入临时文件。
		uploadManager, managerErr := l.uploadManager()
		if managerErr != nil {
			err = managerErr
		} else {
			session, err = uploadManager.Init(l.Ctx, transfer.InitUploadReq{
				OperatorID:  operatorID,
				BizType:     strings.TrimSpace(req.BizType),
				FileName:    req.FileName,
				FileSize:    req.FileSize,
				ChunkSize:   req.ChunkSize,
				FileHash:    req.FileHash,
				ContentType: req.ContentType,
			})
		}
	}
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"FileTransferLogic.InitUpload 初始化上传会话失败").ToBizResult()
	}
	l.decorateSessionURLs(session)
	return types.NewBizResult(1).SetI18nMessage(i18n.MsgKeySuccess).WithData(session)
}

// GetUploadStatus 查询断点续传上传会话状态。
func (l *FileTransferLogic) GetUploadStatus(req *types.FileUploadStatusReq) *types.BizResult {
	uploadManager, err := l.uploadManager()
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"FileTransferLogic.GetUploadStatus 获取上传管理器失败").ToBizResult()
	}
	session, err := uploadManager.Status(l.Ctx, req.UploadID)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"FileTransferLogic.GetUploadStatus 查询上传会话[%s]失败", req.UploadID).ToBizResult()
	}
	if err := l.ensureSessionOwner(session); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).ToBizResult().
			WithError(corelogic.WrapLogicError(err, "FileTransferLogic.GetUploadStatus 无权访问上传会话"))
	}
	l.decorateSessionURLs(session)
	return types.NewBizResult(1).SetI18nMessage(i18n.MsgKeyQuerySuccess).WithData(session)
}

// UploadChunk 写入单个上传分片。
func (l *FileTransferLogic) UploadChunk(req *types.FileUploadChunkReq, body io.Reader) *types.BizResult {
	uploadManager, err := l.uploadManager()
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"FileTransferLogic.UploadChunk 获取上传管理器失败").ToBizResult()
	}
	session, err := uploadManager.GetSession(l.Ctx, req.UploadID)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"FileTransferLogic.UploadChunk 读取上传会话[%s]失败", req.UploadID).ToBizResult()
	}
	if err := l.ensureSessionOwner(session); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).ToBizResult().
			WithError(corelogic.WrapLogicError(err, "FileTransferLogic.UploadChunk 无权访问上传会话"))
	}
	body = limitUploadChunkReader(session, req.ChunkIndex, body)
	session, err = uploadManager.UploadChunk(l.Ctx, req.UploadID, req.ChunkIndex, body)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"FileTransferLogic.UploadChunk 上传会话[%s]分片[%d]失败", req.UploadID, req.ChunkIndex).ToBizResult()
	}
	l.decorateSessionURLs(session)
	return types.NewBizResult(1).SetI18nMessage(i18n.MsgKeySuccess).WithData(session)
}

// CompleteUpload 完成上传会话并返回最终文件信息。
func (l *FileTransferLogic) CompleteUpload(req *types.FileUploadCompleteReq) *types.BizResult {
	uploadManager, err := l.uploadManager()
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"FileTransferLogic.CompleteUpload 获取上传管理器失败").ToBizResult()
	}
	session, err := uploadManager.GetSession(l.Ctx, req.UploadID)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"FileTransferLogic.CompleteUpload 读取上传会话[%s]失败", req.UploadID).ToBizResult()
	}
	if err := l.ensureSessionOwner(session); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).ToBizResult().
			WithError(corelogic.WrapLogicError(err, "FileTransferLogic.CompleteUpload 无权访问上传会话"))
	}
	switch strings.ToLower(strings.TrimSpace(session.UploadMode)) {
	case storage.UploadModeDirect:
		// S3 直传模式在完成态补做对象存在、摘要、MIME 与病毒扫描校验。
		session, err = l.completeDirectUpload(session)
	default:
		// 服务端中转模式由本地临时文件合并后再持久化到统一存储。
		session, err = l.completeServerUpload(req.UploadID)
	}
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"FileTransferLogic.CompleteUpload 完成上传会话[%s]失败", req.UploadID).ToBizResult()
	}
	l.decorateSessionURLs(session)
	return types.NewBizResult(1).SetI18nMessage(i18n.MsgKeySuccess).WithData(session)
}

// ServeDownload 使用支持 Range 的方式输出文件。
func (l *FileTransferLogic) ServeDownload(w http.ResponseWriter, r *http.Request, filePath string, fileName string, contentType string) *types.BizResult {
	if err := transfer.ServeDownload(w, r, filePath, fileName, contentType); err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"FileTransferLogic.ServeDownload 输出文件[%s]失败", filePath).ToBizResult()
	}
	return nil
}

// GetSession 查询上传会话原始信息，供其他业务复用上传结果。
func (l *FileTransferLogic) GetSession(uploadID string) (*transfer.UploadSession, error) {
	uploadManager, err := l.uploadManager()
	if err != nil {
		return nil, errors.Tag(err)
	}
	session, err := uploadManager.GetSession(l.Ctx, uploadID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	l.decorateSessionURLs(session)
	return session, nil
}

// PrepareDownload 校验上传会话归属和完成状态，并返回下载所需信息。
func (l *FileTransferLogic) PrepareDownload(uploadID string) (*transfer.UploadSession, error) {
	session, err := l.GetSession(uploadID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if err := l.ensureSessionOwner(session); err != nil {
		return nil, errors.Tag(err)
	}
	if !l.isCompletedSession(session) {
		return nil, errors.Errorf("上传会话尚未完成")
	}
	return session, nil
}

// EnsureSessionOwner 校验上传会话属于当前登录管理员。
func (l *FileTransferLogic) EnsureSessionOwner(session *transfer.UploadSession) error {
	return l.ensureSessionOwner(session)
}

// IsCompletedSession 判断上传会话是否已完成。
func (l *FileTransferLogic) IsCompletedSession(session *transfer.UploadSession) bool {
	return l.isCompletedSession(session)
}

// PreparePublicAccess 校验上传会话是否允许匿名访问。
func (l *FileTransferLogic) PreparePublicAccess(uploadID string) (*transfer.UploadSession, error) {
	session, err := l.GetSession(uploadID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if !l.isCompletedSession(session) {
		return nil, errors.Errorf("上传会话尚未完成")
	}
	if !isPublicAccessibleBizType(session.BizType) {
		return nil, errors.Errorf("当前文件不允许公开访问")
	}
	return session, nil
}

// OpenSessionObject 打开上传会话对应的统一存储对象。
func (l *FileTransferLogic) OpenSessionObject(session *transfer.UploadSession, rangeHeader string) (*storage.OpenObjectResult, error) {
	if session == nil {
		return nil, errors.Errorf("上传会话不存在")
	}
	if strings.TrimSpace(session.ObjectKey) != "" {
		objectStorage, err := l.objectStorage()
		if err != nil {
			return nil, errors.Tag(err)
		}
		return objectStorage.Open(l.Ctx, storage.OpenObjectReq{
			ObjectKey:   session.ObjectKey,
			RangeHeader: rangeHeader,
		})
	}
	if strings.TrimSpace(session.StoragePath) == "" {
		return nil, errors.Errorf("上传会话缺少对象信息")
	}
	file, err := os.Open(strings.TrimSpace(session.StoragePath))
	if err != nil {
		return nil, errors.Wrap(err, "打开本地上传文件失败")
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, errors.Wrap(err, "读取本地上传文件信息失败")
	}
	return &storage.OpenObjectResult{
		Reader:        file,
		ContentType:   storage.DetectContentType(session.FileName, session.ContentType),
		ContentLength: info.Size(),
		FileName:      filepath.Base(strings.TrimSpace(session.StoragePath)),
		LastModified:  info.ModTime(),
		AcceptRanges:  true,
	}, nil
}

// MaterializeSessionObject 把上传会话对象落到临时文件，便于 Excel 等仅接受文件路径的能力复用。
func (l *FileTransferLogic) MaterializeSessionObject(session *transfer.UploadSession) (string, func(), error) {
	if session == nil {
		return "", nil, errors.Errorf("上传会话不存在")
	}
	if session.ObjectKey == "" && strings.TrimSpace(session.StoragePath) != "" && localFileExists(session.StoragePath) {
		return session.StoragePath, func() {}, nil
	}
	objectStream, err := l.OpenSessionObject(session, "")
	if err != nil {
		return "", nil, errors.Tag(err)
	}
	defer objectStream.Reader.Close()
	return l.writeTempObjectFile(session.StoredFileName, objectStream.Reader)
}

// uploadManager 获取统一上传管理器实例。
func (l *FileTransferLogic) uploadManager() (*transfer.LocalUploadManager, error) {
	if l == nil || l.Svc == nil {
		return nil, errors.Errorf("文件传输服务上下文为空")
	}
	return l.Svc.FileTransferUploadManager()
}

// validateInitRequest 校验上传初始化请求的基础字段和业务安全策略。
func (l *FileTransferLogic) validateInitRequest(req *types.FileUploadInitReq) error {
	if req == nil {
		return errors.Errorf("上传请求不能为空")
	}
	if err := req.Validate(); err != nil {
		return errors.Tag(err)
	}
	if hasUnsafeFileName(req.FileName) {
		return errors.Errorf("文件名不合法")
	}
	bizType := strings.TrimSpace(req.BizType)
	policy, err := l.uploadPolicy(bizType)
	if err != nil {
		return errors.Tag(err)
	}
	if req.FileSize > policy.MaxSize {
		return errors.Errorf("文件大小超过限制")
	}
	if requiresUploadSHA256(policy) {
		if err := validateRequiredUploadSHA256(req.FileHash); err != nil {
			return errors.Tag(err)
		}
	}
	// 大文件必须使用合理的分片尺寸，避免 1B 等极小分片把一次上传拆成海量请求。
	if req.FileSize > fileTransferMinChunkSize && req.ChunkSize < fileTransferMinChunkSize {
		return errors.Errorf("分片大小不能小于 %d KB", fileTransferMinChunkSize/1024)
	}
	totalChunks := int((req.FileSize + req.ChunkSize - 1) / req.ChunkSize)
	if totalChunks > fileTransferMaxChunkCount {
		return errors.Errorf("分片数量超过限制")
	}
	if !matchesFileExt(req.FileName, policy.AllowedExts...) {
		return errors.Errorf("文件类型不支持")
	}
	return nil
}

// ResolveManagedSessionByFileURL 根据统一存储 URL 反查上传会话，确保 fileUrl 仍受上传业务与归属边界约束。
func (l *FileTransferLogic) ResolveManagedSessionByFileURL(fileURL string) (*transfer.UploadSession, error) {
	objectStorage, err := l.objectStorage()
	if err != nil {
		return nil, errors.Tag(err)
	}
	objectKey, err := objectStorage.ResolveObjectKey(fileURL)
	if err != nil {
		return nil, errors.Tag(err)
	}
	// URL 只负责定位对象，真正的权限归属仍以 upload session 反查结果为准。
	uploadManager, err := l.uploadManager()
	if err != nil {
		return nil, errors.Tag(err)
	}
	session, err := uploadManager.FindSessionByObjectKey(l.Ctx, objectKey)
	if err != nil {
		return nil, errors.Tag(err)
	}
	l.decorateSessionURLs(session)
	return session, nil
}

// ensureSessionOwner 确保上传会话归属于当前请求的管理员。
func (l *FileTransferLogic) ensureSessionOwner(session *transfer.UploadSession) error {
	if session == nil {
		return errors.Errorf("上传会话不存在")
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin == nil || ctxAdmin.ID <= 0 {
		return errors.Errorf("当前请求未登录")
	}
	if session.OperatorID > 0 && session.OperatorID != ctxAdmin.ID {
		return errors.Errorf("无权访问其他管理员的上传会话")
	}
	return nil
}

// decorateSessionURLs 为上传会话添加访问和下载 URL。
func (l *FileTransferLogic) decorateSessionURLs(session *transfer.UploadSession) {
	if session == nil {
		return
	}
	session.AccessURL = buildUploadAccessURL(session)
	session.DownloadURL = buildUploadDownloadURL(session.UploadID)
}

// isCompletedSession 判断上传会话是否已完成。
func (l *FileTransferLogic) isCompletedSession(session *transfer.UploadSession) bool {
	return session != nil && strings.EqualFold(strings.TrimSpace(session.Status), "completed")
}

// matchesFileExt 判断文件名是否匹配给定的扩展名列表。
func matchesFileExt(fileName string, exts ...string) bool {
	fileName = strings.ToLower(strings.TrimSpace(fileName))
	for _, ext := range exts {
		if strings.HasSuffix(fileName, strings.ToLower(strings.TrimSpace(ext))) {
			return true
		}
	}
	return false
}

// isPublicAccessibleBizType 判断上传业务类型是否允许公开访问。
func isPublicAccessibleBizType(bizType string) bool {
	return strings.TrimSpace(bizType) == FileTransferBizAdminAvatar
}

// buildUploadAccessURL 构建上传会话的访问 URL。
func buildUploadAccessURL(session *transfer.UploadSession) string {
	if session == nil || !isPublicAccessibleBizType(session.BizType) {
		return ""
	}
	return fmt.Sprintf("/api/file-transfer/access?uploadId=%s", session.UploadID)
}

// buildUploadDownloadURL 构建上传会话的下载 URL。
func buildUploadDownloadURL(uploadID string) string {
	uploadID = strings.TrimSpace(uploadID)
	if uploadID == "" {
		return ""
	}
	return fmt.Sprintf("/api/file-transfer/download?uploadId=%s", uploadID)
}

// objectStorage 获取对象存储实例。
func (l *FileTransferLogic) objectStorage() (storage.ObjectStorage, error) {
	if l == nil || l.Svc == nil {
		return nil, errors.Errorf("文件传输服务上下文为空")
	}
	return l.Svc.ObjectStorage()
}

// uploadMode 获取上传模式。
func (l *FileTransferLogic) uploadMode(policy FileUploadPolicy) string {
	if !policy.AllowDirectUpload {
		return storage.UploadModeServer
	}
	if l == nil || l.Svc == nil {
		return storage.UploadModeServer
	}
	return l.Svc.UploadMode()
}

// uploadSessionTTL 获取上传会话保留时间。
func (l *FileTransferLogic) uploadSessionTTL() time.Duration {
	if l == nil || l.Svc == nil {
		return transfer.DefaultUploadSessionTTL
	}
	return l.Svc.FileTransferUploadSessionTTL()
}

// uploadPolicy 获取上传策略。
func (l *FileTransferLogic) uploadPolicy(bizType string) (FileUploadPolicy, error) {
	return fileUploadPolicyOf(bizType)
}

// initDirectUpload 初始化直传会话。
func (l *FileTransferLogic) initDirectUpload(req *types.FileUploadInitReq, operatorID int, policy FileUploadPolicy) (*transfer.UploadSession, error) {
	objectStorage, err := l.objectStorage()
	if err != nil {
		return nil, errors.Tag(err)
	}
	uploadID := uuid.NewString()
	storedFileName := storage.BuildStoredFileName(uploadID, req.FileName)
	plan, err := objectStorage.CreateDirectUpload(l.Ctx, storage.DirectUploadReq{
		BizType:          strings.TrimSpace(req.BizType),
		ContentType:      req.ContentType,
		FileSize:         req.FileSize,
		OriginalFileName: req.FileName,
		StoredFileName:   storedFileName,
		Visibility:       policy.Visibility,
	})
	if err != nil {
		return nil, errors.Tag(err)
	}
	now := time.Now()
	session := &transfer.UploadSession{
		UploadID:       uploadID,
		OperatorID:     operatorID,
		BizType:        strings.TrimSpace(req.BizType),
		FileName:       strings.TrimSpace(req.FileName),
		StoredFileName: storedFileName,
		FileSize:       req.FileSize,
		ChunkSize:      req.FileSize,
		TotalChunks:    1,
		FileHash:       strings.TrimSpace(req.FileHash),
		ContentType:    storage.DetectContentType(req.FileName, req.ContentType),
		StoragePath:    strings.TrimSpace(plan.ObjectKey),
		StorageType:    objectStorage.Type(),
		ObjectKey:      strings.TrimSpace(plan.ObjectKey),
		UploadMode:     storage.UploadModeDirect,
		Status:         "pending",
		CreatedAt:      corelogic.FormatDateTime(now),
		UpdatedAt:      corelogic.FormatDateTime(now),
		ExpiresAt:      corelogic.FormatDateTime(now.Add(l.uploadSessionTTL())),
		DirectUpload:   plan,
	}
	uploadManager, err := l.uploadManager()
	if err != nil {
		return nil, errors.Tag(err)
	}
	if err := uploadManager.PersistSession(l.Ctx, session); err != nil {
		return nil, errors.Tag(err)
	}
	return session, nil
}

// completeServerUpload 完成服务器端上传会话。
func (l *FileTransferLogic) completeServerUpload(uploadID string) (*transfer.UploadSession, error) {
	uploadManager, err := l.uploadManager()
	if err != nil {
		return nil, errors.Tag(err)
	}
	session, err := uploadManager.Complete(l.Ctx, uploadID)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if isPersistedServerUploadSession(session) {
		return session, nil
	}
	persisted, err := l.persistServerUploadedFile(session)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return persisted, nil
}

// completeDirectUpload 完成 S3 直传会话，并在会话落库前执行统一安全校验。
func (l *FileTransferLogic) completeDirectUpload(session *transfer.UploadSession) (*transfer.UploadSession, error) {
	if session == nil {
		return nil, errors.Errorf("上传会话不存在")
	}
	if strings.EqualFold(strings.TrimSpace(session.Status), "completed") {
		return session, nil
	}
	objectStorage, err := l.objectStorage()
	if err != nil {
		return nil, errors.Tag(err)
	}
	storedObject, err := objectStorage.CompleteDirectUpload(l.Ctx, session.ObjectKey)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if err := l.validateDirectUploadedObject(session, storedObject); err != nil {
		if deleteErr := objectStorage.Delete(l.Ctx, storedObject.ObjectKey); deleteErr != nil {
			corelogic.LogWrappedError(l, deleteErr, "FileTransferLogic.completeDirectUpload 回滚非法直传对象失败")
		}
		return nil, errors.Tag(err)
	}
	session.Status = "completed"
	session.StorageType = storedObject.StorageType
	session.ObjectKey = storedObject.ObjectKey
	session.StoragePath = storedObject.ObjectKey
	session.StoredFileName = storedObject.FileName
	session.ContentType = storedObject.ContentType
	session.UpdatedAt = corelogic.FormatDateTime(time.Now())
	session.DirectUpload = nil
	uploadManager, err := l.uploadManager()
	if err != nil {
		return nil, errors.Tag(err)
	}
	if err := uploadManager.PersistSession(l.Ctx, session); err != nil {
		return nil, errors.Tag(err)
	}
	return session, nil
}

// persistServerUploadedFile 把服务端中转生成的本地文件持久化到统一存储，并把会话切换到对象 key。
func (l *FileTransferLogic) persistServerUploadedFile(session *transfer.UploadSession) (*transfer.UploadSession, error) {
	if session == nil {
		return nil, errors.Errorf("上传会话不存在")
	}
	policy, err := l.uploadPolicy(session.BizType)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if err := l.validateCompletedFile(session.StoragePath, policy); err != nil {
		return nil, errors.Tag(err)
	}
	scanner, err := l.virusScanner()
	if err != nil {
		return nil, errors.Tag(err)
	}
	if err := scanner.ScanFile(l.Ctx, session.StoragePath, session.BizType); err != nil {
		return nil, errors.Tag(err)
	}
	objectStorage, err := l.objectStorage()
	if err != nil {
		return nil, errors.Tag(err)
	}
	storedObject, err := objectStorage.SaveLocalFile(l.Ctx, storage.SaveLocalFileReq{
		BizType:          session.BizType,
		ContentType:      session.ContentType,
		LocalPath:        session.StoragePath,
		OriginalFileName: session.FileName,
		StoredFileName:   session.StoredFileName,
		Visibility:       policy.Visibility,
	})
	if err != nil {
		return nil, errors.Tag(err)
	}
	if err := validateStoredObjectSHA256(storedObject, session.FileHash); err != nil {
		// 对象存储返回流式摘要时，直接比对前端整文件哈希。
		if deleteErr := objectStorage.Delete(l.Ctx, storedObject.ObjectKey); deleteErr != nil {
			corelogic.LogWrappedError(l, deleteErr, "FileTransferLogic.persistServerUploadedFile 回滚摘要异常对象失败")
		}
		return nil, errors.Tag(err)
	}
	if localFileExists(session.StoragePath) {
		_ = os.Remove(session.StoragePath)
	}
	session.StorageType = storedObject.StorageType
	session.ObjectKey = storedObject.ObjectKey
	session.StoragePath = storedObject.ObjectKey
	session.StoredFileName = storedObject.FileName
	session.ContentType = storedObject.ContentType
	session.UpdatedAt = corelogic.FormatDateTime(time.Now())
	session.DirectUpload = nil
	uploadManager, err := l.uploadManager()
	if err != nil {
		return nil, errors.Tag(err)
	}
	if err := uploadManager.PersistSession(l.Ctx, session); err != nil {
		return nil, errors.Tag(err)
	}
	return session, nil
}

// validateStoredObjectSHA256 使用对象存储返回的流式摘要校验最终对象完整性。
// S3 直传等无法在保存阶段拿到本地流式摘要的存储实现会返回空值，此时继续依赖原有对象读取校验链路。
func validateStoredObjectSHA256(storedObject *storage.StoredObject, expectedHash string) error {
	if storedObject == nil {
		return errors.Errorf("存储对象不能为空")
	}
	expectedHash = strings.ToLower(strings.TrimSpace(expectedHash))
	if expectedHash == "" || strings.TrimSpace(storedObject.SHA256) == "" {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(storedObject.SHA256), expectedHash) {
		return errors.Errorf("上传文件完整性校验失败")
	}
	return nil
}

// isPersistedServerUploadSession 判断服务端中转上传是否已经切换为统一对象存储，保证重复 Complete 幂等返回。
func isPersistedServerUploadSession(session *transfer.UploadSession) bool {
	if session == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(session.Status), "completed") {
		return false
	}
	objectKey := strings.TrimSpace(session.ObjectKey)
	if objectKey == "" {
		return false
	}
	return strings.TrimSpace(session.StoragePath) == objectKey
}

// requiresUploadSHA256 对私有上传强制要求前端提交整文件 SHA-256，避免关键文件跳过完整性校验。
func requiresUploadSHA256(policy FileUploadPolicy) bool {
	return strings.EqualFold(strings.TrimSpace(policy.Visibility), storage.VisibilityPrivate)
}

// validateRequiredUploadSHA256 校验 SHA-256 摘要格式，避免错误摘要拖到完成阶段才失败。
func validateRequiredUploadSHA256(value string) error {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != sha256.Size*2 {
		return errors.Errorf("文件 SHA-256 摘要不能为空且必须为 64 位十六进制")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return errors.Errorf("文件 SHA-256 摘要格式不合法")
	}
	return nil
}

// limitUploadChunkReader 最多读取“期望分片大小+1”字节。
func limitUploadChunkReader(session *transfer.UploadSession, chunkIndex int, body io.Reader) io.Reader {
	if session == nil || body == nil || chunkIndex < 0 || chunkIndex >= session.TotalChunks {
		return body
	}
	if session.ChunkSize <= 0 {
		return body
	}
	expectedSize := session.ChunkSize
	if chunkIndex == session.TotalChunks-1 {
		if remainder := session.FileSize % session.ChunkSize; remainder != 0 {
			expectedSize = remainder
		}
	}
	if expectedSize < 0 {
		return body
	}
	return io.LimitReader(body, expectedSize+1)
}

// validateCompletedFile 校验上传完成后的文件大小和基础 MIME 特征。
func (l *FileTransferLogic) validateCompletedFile(filePath string, policy FileUploadPolicy) error {
	info, err := os.Stat(strings.TrimSpace(filePath))
	if err != nil {
		return errors.Wrap(err, "读取上传结果文件失败")
	}
	if info.Size() > policy.MaxSize {
		return errors.Errorf("文件大小超过限制")
	}
	if policy.ExpectedMIMEPrefix == "" {
		return nil
	}
	file, err := os.Open(strings.TrimSpace(filePath))
	if err != nil {
		return errors.Wrap(err, "打开上传结果文件失败")
	}
	defer file.Close()
	buffer := make([]byte, 512)
	readSize, _ := file.Read(buffer)
	detected := http.DetectContentType(buffer[:readSize])
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(detected)), strings.ToLower(policy.ExpectedMIMEPrefix)) {
		return errors.Errorf("文件内容类型校验失败")
	}
	return nil
}

// validateDirectUploadedObject 校验 S3 直传完成对象。
func (l *FileTransferLogic) validateDirectUploadedObject(session *transfer.UploadSession, storedObject *storage.StoredObject) error {
	if session == nil || storedObject == nil {
		return errors.Errorf("直传对象校验参数不能为空")
	}
	policy, err := l.uploadPolicy(session.BizType)
	if err != nil {
		return errors.Tag(err)
	}
	if storedObject.Size > 0 && session.FileSize > 0 && storedObject.Size != session.FileSize {
		return errors.Errorf("直传文件大小校验失败")
	}
	objectStorage, err := l.objectStorage()
	if err != nil {
		return errors.Tag(err)
	}
	objectStream, err := objectStorage.Open(l.Ctx, storage.OpenObjectReq{
		ObjectKey: storedObject.ObjectKey,
	})
	if err != nil {
		return errors.Tag(err)
	}
	defer objectStream.Reader.Close()

	// 直传对象先落到临时文件，再复用现有文件校验和病毒扫描能力。
	tempPath, cleanup, err := l.writeTempObjectFile(storedObject.FileName, objectStream.Reader)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return errors.Tag(err)
	}
	if err := verifyUploadedFileSHA256(tempPath, session.FileHash); err != nil {
		return errors.Tag(err)
	}
	if err := l.validateCompletedFile(tempPath, policy); err != nil {
		return errors.Tag(err)
	}
	scanner, err := l.virusScanner()
	if err != nil {
		return errors.Tag(err)
	}
	if err := scanner.ScanFile(l.Ctx, tempPath, session.BizType); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// virusScanner 获取统一病毒扫描器实例。
func (l *FileTransferLogic) virusScanner() (storage.VirusScanner, error) {
	if l == nil || l.Svc == nil {
		return nil, errors.Errorf("文件传输服务上下文为空")
	}
	return l.Svc.VirusScanner()
}

// writeTempObjectFile 将对象流写入临时文件并返回文件路径。
func (l *FileTransferLogic) writeTempObjectFile(fileName string, reader io.Reader) (string, func(), error) {
	tempDir := filepath.Join(os.TempDir(), "admin", "storage-import")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return "", nil, errors.Wrap(err, "创建临时文件目录失败")
	}
	safeFileName := storage.BuildStoredFileName(uuid.NewString(), filepath.Base(strings.TrimSpace(fileName)))
	tempPath := filepath.Join(tempDir, safeFileName)
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", nil, errors.Wrap(err, "创建临时文件失败")
	}
	defer file.Close()
	if _, err := io.Copy(file, reader); err != nil {
		_ = os.Remove(tempPath)
		return "", nil, errors.Wrap(err, "写入临时文件失败")
	}
	return tempPath, func() {
		_ = os.Remove(tempPath)
	}, nil
}

// hasUnsafeFileName 判断原始文件名是否包含目录穿越或路径注入风险。
func hasUnsafeFileName(fileName string) bool {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return true
	}
	return fileName != filepath.Base(fileName) || strings.Contains(fileName, "..")
}

// localFileExists 判断本地文件是否存在，便于支持历史本地文件路径分支。
func localFileExists(filePath string) bool {
	if strings.TrimSpace(filePath) == "" {
		return false
	}
	_, err := os.Stat(filePath)
	return err == nil
}

// verifyUploadedFileSHA256 校验上传文件的 SHA256 摘要，保证前后端完整性一致。
func verifyUploadedFileSHA256(filePath string, expectedHash string) error {
	expectedHash = strings.ToLower(strings.TrimSpace(expectedHash))
	if expectedHash == "" {
		return nil
	}
	file, err := os.Open(strings.TrimSpace(filePath))
	if err != nil {
		return errors.Wrap(err, "打开待校验文件失败")
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return errors.Wrap(err, "计算上传文件摘要失败")
	}
	if hex.EncodeToString(digest.Sum(nil)) != expectedHash {
		return errors.Errorf("上传文件完整性校验失败")
	}
	return nil
}
