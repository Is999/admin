package file

import (
	"encoding/json"
	"strings"
	"time"

	"admin/internal/svc"
	taskqueue "admin/internal/task/queue"
	"admin/internal/types"
	"admin/pkg/storage"
	"admin/pkg/transfer"

	"github.com/Is999/go-utils/errors"
)

const (
	// fileUploadCleanupRetry 表示上传对象清理任务的最大重试次数。
	fileUploadCleanupRetry = 5
	// fileUploadCleanupTimeout 表示单次上传对象清理的执行超时。
	fileUploadCleanupTimeout = 2 * time.Minute
	// fileAdminAvatarCleanupDelay 为头像数据库变更预留提交时间，执行时仍会复查真实引用。
	fileAdminAvatarCleanupDelay = time.Minute
)

// CleanupUploadedObject 在上传会话失效后清理未被业务保留的精确对象。
func (l *FileTransferLogic) CleanupUploadedObject(payload *types.FileUploadCleanupTaskPayload) error {
	if payload == nil {
		return errors.Errorf("上传对象清理任务参数不能为空")
	}
	uploadID := strings.TrimSpace(payload.UploadID)
	bizType := strings.TrimSpace(payload.BizType)
	objectKey, managed, err := l.resolveManagedUploadObjectKey(bizType, payload.ObjectKey)
	if err != nil {
		return errors.Tag(err)
	}
	if !managed {
		return errors.Errorf("上传对象不属于业务目录 biz_type=%s object_key=%s", bizType, strings.TrimSpace(payload.ObjectKey))
	}
	var uploadManager *transfer.LocalUploadManager
	if uploadID != "" {
		uploadManager, err = l.uploadManager()
		if err != nil {
			return errors.Tag(err)
		}
		exists, err := uploadManager.HasSession(l.Ctx, uploadID)
		if err != nil {
			return errors.Wrapf(err, "检查上传会话失败 upload_id=%s", uploadID)
		}
		if exists {
			return l.enqueueUploadedObjectCleanup(uploadID, bizType, objectKey, l.uploadSessionTTL())
		}
	}
	if bizType == FileTransferBizAdminAvatar {
		if l == nil || l.Svc == nil {
			return errors.Errorf("管理员头像引用数据库未初始化")
		}
		db := l.Svc.WriteDB(svc.DatabaseMain)
		if db == nil {
			return errors.Errorf("管理员头像引用数据库未初始化")
		}
		referenced, err := isAdminAvatarReferenced(db, objectKey)
		if err != nil {
			return errors.Wrapf(err, "检查管理员头像引用失败 object_key=%s", objectKey)
		}
		if referenced {
			return nil
		}
	}
	if err := l.deleteUploadedObject(objectKey); err != nil {
		return errors.Tag(err)
	}
	if uploadManager != nil {
		return errors.Tag(uploadManager.DeleteCompletedFile(bizType, objectKey))
	}
	return nil
}

// ScheduleReplacedAdminAvatarCleanup 在头像替换或管理员删除前投递旧头像清理任务。
func (l *FileTransferLogic) ScheduleReplacedAdminAvatarCleanup(oldAvatar string, newAvatar string) error {
	oldAvatar = strings.TrimSpace(oldAvatar)
	if oldAvatar == "" || oldAvatar == strings.TrimSpace(newAvatar) {
		return nil
	}
	objectKey, managed, err := l.resolveManagedUploadObjectKey(FileTransferBizAdminAvatar, oldAvatar)
	if err != nil {
		return errors.Tag(err)
	}
	if !managed {
		return nil
	}
	return l.enqueueUploadedObjectCleanup("", FileTransferBizAdminAvatar, objectKey, fileAdminAvatarCleanupDelay)
}

// DeleteImportedObject 在配置导入成功后立即删除已经消费的上传对象。
func (l *FileTransferLogic) DeleteImportedObject(session *transfer.UploadSession) error {
	if session == nil || strings.TrimSpace(session.BizType) != FileTransferBizSysConfigExcelImport {
		return errors.Errorf("配置导入上传会话不合法")
	}
	objectKey, managed, err := l.resolveManagedUploadObjectKey(session.BizType, session.ObjectKey)
	if err != nil {
		return errors.Tag(err)
	}
	if !managed {
		return errors.Errorf("配置导入对象不属于业务目录 object_key=%s", strings.TrimSpace(session.ObjectKey))
	}
	return l.deleteUploadedObject(objectKey)
}

// enqueueUploadedObjectCleanup 投递精确对象清理任务。
func (l *FileTransferLogic) enqueueUploadedObjectCleanup(uploadID string, bizType string, objectKey string, delay time.Duration) error {
	if err := l.ensureUploadCleanupTaskQueue(); err != nil {
		return errors.Tag(err)
	}
	if delay <= 0 {
		return errors.Errorf("上传对象清理延迟必须大于 0")
	}
	payload, err := json.Marshal(types.FileUploadCleanupTaskPayload{
		UploadID:  strings.TrimSpace(uploadID),
		BizType:   strings.TrimSpace(bizType),
		ObjectKey: strings.TrimSpace(objectKey),
	})
	if err != nil {
		return errors.Wrap(err, "序列化上传对象清理任务失败")
	}
	if err := l.Svc.Task.EnqueueTask(l.Ctx, types.FileUploadCleanupTaskType, payload,
		svc.WithTaskQueue(taskqueue.QueueMaintenance),
		svc.WithTaskRetry(fileUploadCleanupRetry),
		svc.WithTaskTimeout(fileUploadCleanupTimeout),
		svc.WithTaskDelay(delay),
	); err != nil {
		return errors.Wrapf(err, "投递上传对象清理任务失败 upload_id=%s biz_type=%s object_key=%s",
			strings.TrimSpace(uploadID), strings.TrimSpace(bizType), strings.TrimSpace(objectKey))
	}
	return nil
}

// ensureUploadCleanupTaskQueue 校验上传对象清理所需的任务队列已经启用。
func (l *FileTransferLogic) ensureUploadCleanupTaskQueue() error {
	if l == nil || l.Svc == nil || l.Svc.Task == nil || !l.Svc.Task.IsEnabled() {
		return errors.Errorf("上传对象清理任务系统未启用")
	}
	return nil
}

// resolveManagedUploadObjectKey 解析并校验对象属于指定上传业务目录。
func (l *FileTransferLogic) resolveManagedUploadObjectKey(bizType string, rawObjectKey string) (string, bool, error) {
	bizType = strings.TrimSpace(bizType)
	if _, err := l.uploadPolicy(bizType); err != nil {
		return "", false, errors.Tag(err)
	}
	objectStorage, err := l.objectStorage()
	if err != nil {
		return "", false, errors.Tag(err)
	}
	objectKey, err := resolveFileObjectKey(objectStorage, rawObjectKey)
	if err != nil {
		return "", false, nil
	}
	pathPrefix := ""
	if objectStorage.Type() == storage.TypeS3 {
		pathPrefix = l.Svc.CurrentConfig().FileStorage.S3.PathPrefix
	}
	expectedPrefix := storage.ObjectKeyPrefix(pathPrefix, bizType)
	return objectKey, isManagedUploadObjectKey(objectKey, expectedPrefix), nil
}

// deleteUploadedObject 删除统一存储中的精确对象，存储层保证对象不存在时幂等成功。
func (l *FileTransferLogic) deleteUploadedObject(objectKey string) error {
	objectStorage, err := l.objectStorage()
	if err != nil {
		return errors.Tag(err)
	}
	if err := objectStorage.Delete(l.Ctx, objectKey); err != nil {
		return errors.Wrapf(err, "删除上传对象失败 object_key=%s", strings.TrimSpace(objectKey))
	}
	return nil
}
