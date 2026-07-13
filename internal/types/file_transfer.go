package types

import (
	"strings"

	"github.com/Is999/go-utils/errors"
)

const (
	// FileUploadCleanupTaskType 表示上传对象的延迟清理任务类型。
	FileUploadCleanupTaskType = "file:upload:cleanup"
)

// FileUploadCleanupTaskPayload 表示上传对象延迟清理任务负载。
type FileUploadCleanupTaskPayload struct {
	UploadID  string `json:"uploadId,omitempty"` // 上传会话 ID；头像替换清理时为空
	BizType   string `json:"bizType"`            // 上传业务类型，用于校验对象目录和引用规则
	ObjectKey string `json:"objectKey"`          // 需要清理的统一存储对象 key
}

// FileUploadInitReq 表示初始化断点续传上传会话请求。
type FileUploadInitReq struct {
	BizType     string `json:"bizType,optional"`     // 业务类型
	FileName    string `json:"fileName"`             // 文件名
	FileSize    int64  `json:"fileSize"`             // 文件总大小
	ChunkSize   int64  `json:"chunkSize"`            // 分片大小
	FileHash    string `json:"fileHash,optional"`    // 文件摘要
	ContentType string `json:"contentType,optional"` // 文件类型
}

// Validate 校验上传初始化请求。
func (r *FileUploadInitReq) Validate() error {
	if strings.TrimSpace(r.FileName) == "" {
		return errors.Errorf("文件名不能为空")
	}
	if r.FileSize <= 0 {
		return errors.Errorf("文件大小必须大于 0")
	}
	if r.ChunkSize <= 0 {
		return errors.Errorf("分片大小必须大于 0")
	}
	return nil
}

// FileUploadStatusReq 表示查询上传状态请求。
type FileUploadStatusReq struct {
	UploadID string `form:"uploadId"` // 上传会话 ID
}

// Validate 校验上传状态请求。
func (r *FileUploadStatusReq) Validate() error {
	if strings.TrimSpace(r.UploadID) == "" {
		return errors.Errorf("uploadId 不能为空")
	}
	return nil
}

// FilePublicAccessReq 表示匿名访问公开上传对象的请求。
type FilePublicAccessReq struct {
	ObjectKey string `form:"objectKey"` // 公开对象 key
}

// Validate 校验公开对象访问请求。
func (r *FilePublicAccessReq) Validate() error {
	if strings.TrimSpace(r.ObjectKey) == "" {
		return errors.Errorf("objectKey 不能为空")
	}
	return nil
}

// FileUploadChunkReq 表示上传单个分片请求。
type FileUploadChunkReq struct {
	UploadID   string `json:"uploadId" form:"uploadId"`     // 上传会话 ID
	ChunkIndex int    `json:"chunkIndex" form:"chunkIndex"` // 分片下标
}

// Validate 校验上传分片请求。
func (r *FileUploadChunkReq) Validate() error {
	if strings.TrimSpace(r.UploadID) == "" {
		return errors.Errorf("uploadId 不能为空")
	}
	if r.ChunkIndex < 0 {
		return errors.Errorf("chunkIndex 不能小于 0")
	}
	return nil
}

// FileUploadCompleteReq 表示完成上传请求。
type FileUploadCompleteReq struct {
	UploadID string `json:"uploadId" form:"uploadId"` // 上传会话 ID
}

// Validate 校验完成上传请求。
func (r *FileUploadCompleteReq) Validate() error {
	if strings.TrimSpace(r.UploadID) == "" {
		return errors.Errorf("uploadId 不能为空")
	}
	return nil
}
