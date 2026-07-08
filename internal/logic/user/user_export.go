package user

import (
	corelogic "admin/internal/logic"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	i18n "admin/common/i18n"
	keys "admin/common/rediskeys"
	"admin/helper"
	redislock "admin/internal/infra/redsync"
	"admin/internal/model"
	"admin/internal/svc"
	taskqueue "admin/internal/task/queue"
	"admin/internal/types"
	"admin/pkg/excel"
	"admin/pkg/storage"

	utils "github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

const (
	// userExportStatusTTL 表示前台用户导出任务状态在 Redis 中的保留时长。
	userExportStatusTTL = 24 * time.Hour
	// userExportBatchSize 表示前台用户导出游标分页每批读取行数。
	userExportBatchSize = 200
	// userExportMinWorkerCount 表示前台用户导出最小并发行格式化 worker 数。
	userExportMinWorkerCount = 2
	// userExportMaxWorkerCount 表示前台用户导出最大并发行格式化 worker 数。
	userExportMaxWorkerCount = 4
	// userExportDefaultFileNamePrefix 表示前台用户导出文件名前缀。
	userExportDefaultFileNamePrefix = "user_export"
	// userExportSheetName 表示导出 Excel 工作表名称。
	userExportSheetName = "前台用户列表"
	// userExportMaxConcurrentShards 表示前台用户导出最大并发分片数。
	userExportMaxConcurrentShards = 4
	// userExportProgressMinInterval 表示前台用户导出进度最小上报时间间隔。
	userExportProgressMinInterval = 500 * time.Millisecond
	// userExportProgressMinRows 表示前台用户导出进度最小上报行数增量。
	userExportProgressMinRows = 400
	// userExportTriggerLockTTL 表示前台用户导出同条件触发锁 TTL。
	userExportTriggerLockTTL = 15 * time.Second
	// userExportReuseIndexTTL 表示前台用户导出同条件复用索引保留时间。
	userExportReuseIndexTTL = 24 * time.Hour
	// userExportTaskTimeoutSeconds 表示前台用户大批量导出任务的默认超时秒数。
	userExportTaskTimeoutSeconds = 3600
	// userExportDefaultSplitRows 表示缺省单文件拆分行数。
	userExportDefaultSplitRows = 500000
	// userExportMaxSplitRows 表示 Excel 单 Sheet 扣除表头后的最大数据行数。
	userExportMaxSplitRows = excel.MaxExcelSheetRows - 1
	// userExportContentType 表示前台用户导出 Excel 文件 MIME。
	userExportContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
)

// userExportStatusSnapshot 表示写入 Redis 的前台用户导出任务完整快照。
type userExportStatusSnapshot struct {
	types.UserExportStatusResp                          // 对外导出状态响应字段
	Files                      []userExportFileSnapshot `json:"files"`              // 导出文件分片完整快照
	FilePath                   string                   `json:"filePath"`           // 服务器本地文件路径
	ObjectKey                  string                   `json:"objectKey"`          // 统一存储对象 key
	StorageType                string                   `json:"storageType"`        // 统一存储类型
	ContentType                string                   `json:"contentType"`        // 文件 MIME
	LastCursorID               int64                    `json:"lastCursorId"`       // 最近处理到的用户 ID
	OperatorID                 int                      `json:"operatorId"`         // 发起导出的管理员 ID
	RequestFingerprint         string                   `json:"requestFingerprint"` // 归一化导出条件指纹
	AuthorizedAdminIDs         []int                    `json:"authorizedAdminIds"` // 允许复用该导出结果的管理员 ID 列表
}

// userExportFileSnapshot 表示单个导出文件分片的 Redis 持久化快照。
type userExportFileSnapshot struct {
	PartNo        int    `json:"partNo"`        // 文件编号
	FileName      string `json:"fileName"`      // 下载文件名
	RowCount      int64  `json:"rowCount"`      // 当前文件数据行数
	ProcessedFrom int64  `json:"processedFrom"` // 全局起始行序号
	ProcessedTo   int64  `json:"processedTo"`   // 全局结束行序号
	FileSize      int64  `json:"fileSize"`      // 文件字节数
	DownloadReady bool   `json:"downloadReady"` // 是否可下载
	DownloadURL   string `json:"downloadUrl"`   // 下载地址
	CreatedAt     string `json:"createdAt"`     // 文件生成时间
	FilePath      string `json:"filePath"`      // 本地文件路径
	ObjectKey     string `json:"objectKey"`     // 统一存储对象 key
	StorageType   string `json:"storageType"`   // 统一存储类型
	ContentType   string `json:"contentType"`   // 文件 MIME
}

// userExportIDShard 表示按用户雪花 ID 有序切分的导出分片。
type userExportIDShard struct {
	StartID int64 // 分片起始用户 ID（包含）
	EndID   int64 // 分片结束用户 ID（包含）；0 表示无上界
}

// userExportRow 表示导出流里携带游标的前台用户行。
type userExportRow struct {
	User     model.User // 当前用户资料
	CursorID int64      // 本批次推进游标，身份表路径等于 identity.user_id
}

// userExportPartResult 表示单个文件分片导出的推进结果。
type userExportPartResult struct {
	RowCount   int64 // 当前文件写入的数据行数
	LastCursor int64 // 当前文件处理到的游标
	HasMore    bool  // 是否仍有下一文件分片
}

// TriggerExport 提交前台用户列表导出任务，并立即返回 jobId 供前端轮询进度。
func (l *Logic) TriggerExport(req *types.UserExportReq) *types.BizResult {
	if err := l.ensureUserExportTaskQueueEnabled(); err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"UserLogic.TriggerExport 任务系统未启用").ToBizResult()
	}
	if err := l.ensureUserExportRedisEnabled(); err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"UserLogic.TriggerExport Redis 未启用").ToBizResult()
	}

	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID <= 0 {
		return types.NewBizResult(401).
			SetI18nMessage(i18n.MsgKeyNeedLogin).
			WithError(errors.Errorf("UserLogic.TriggerExport 当前请求未登录"))
	}
	if resp := l.validateUserExportQueryBoundary(req); resp != nil {
		return resp
	}

	triggerResp, err := l.triggerUserExportWithUniqueLock(req, ctxAdmin.ID, ctxAdmin.Name)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"UserLogic.TriggerExport 提交前台用户导出任务失败").ToBizResult()
	}

	return types.NewBizResult(1).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(triggerResp)
}

// GetExportStatus 查询前台用户导出任务的当前进度。
func (l *Logic) GetExportStatus(req *types.UserExportJobReq) *types.BizResult {
	status, err := l.loadUserExportStatus(req.JobID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NotFound(i18n.MsgKeyNotFound, err,
				"UserLogic.GetExportStatus 前台用户导出任务[%s]不存在", req.JobID).ToBizResult()
		}
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"UserLogic.GetExportStatus 查询前台用户导出任务[%s]失败", req.JobID).ToBizResult()
	}
	if err := l.ensureUserExportStatusOwner(status); err != nil {
		return types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(err, "UserLogic.GetExportStatus 无权查看前台用户导出任务[%s]", req.JobID))
	}
	if err := l.refreshUserExportDownloadAvailability(status); err != nil {
		corelogic.LogWrappedError(l, err, "UserLogic.GetExportStatus 刷新前台用户导出任务[%s]下载可用性失败", req.JobID)
	}
	return types.NewBizResult(1).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(l.publicUserExportStatus(status))
}

// PrepareExportDownload 校验前台用户导出任务状态，并返回用于下载的文件信息。
func (l *Logic) PrepareExportDownload(req *types.UserExportJobReq) (*types.UserExportStatusResp, *types.BizResult) {
	status, err := l.loadUserExportStatus(req.JobID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, types.NotFound(i18n.MsgKeyNotFound, err,
				"UserLogic.PrepareExportDownload 前台用户导出任务[%s]不存在", req.JobID).ToBizResult()
		}
		return nil, types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"UserLogic.PrepareExportDownload 查询前台用户导出任务[%s]失败", req.JobID).ToBizResult()
	}
	if err := l.ensureUserExportStatusOwner(status); err != nil {
		return nil, types.Forbidden(i18n.MsgKeyForbidden).
			ToBizResult().
			WithError(errors.Wrapf(err, "UserLogic.PrepareExportDownload 无权下载前台用户导出任务[%s]", req.JobID))
	}
	if err := l.refreshUserExportDownloadAvailability(status); err != nil {
		corelogic.LogWrappedError(l, err, "UserLogic.PrepareExportDownload 刷新前台用户导出任务[%s]下载可用性失败", req.JobID)
	}
	fileItem, err := selectUserExportDownloadFile(status, req.PartNo)
	if err != nil {
		return nil, types.NewBizResult(0).
			SetI18nMessage(i18n.MsgKeyFail).
			WithError(errors.Wrapf(err, "UserLogic.PrepareExportDownload 前台用户导出任务[%s]文件不可下载", req.JobID))
	}
	downloadStatus := *status
	applyUserExportDownloadFile(&downloadStatus, fileItem)
	return &downloadStatus, nil
}

// OpenExportDownloadObject 打开前台用户导出结果对象流，支持本地文件和统一存储对象。
func (l *Logic) OpenExportDownloadObject(status *types.UserExportStatusResp, rangeHeader string) (*storage.OpenObjectResult, error) {
	if status == nil {
		return nil, errors.Errorf("导出任务状态不能为空")
	}
	if strings.TrimSpace(status.ObjectKey) != "" {
		objectStorage, err := l.userExportObjectStorage()
		if err != nil {
			return nil, errors.Tag(err)
		}
		return objectStorage.Open(l.Ctx, storage.OpenObjectReq{
			ObjectKey:   status.ObjectKey,
			RangeHeader: rangeHeader,
		})
	}
	if strings.TrimSpace(status.FilePath) == "" {
		return nil, errors.Errorf("导出文件不存在")
	}
	file, err := os.Open(status.FilePath)
	if err != nil {
		return nil, errors.Wrap(err, "打开本地导出文件失败")
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, errors.Wrap(err, "读取本地导出文件信息失败")
	}
	return &storage.OpenObjectResult{
		Reader:        file,
		ContentType:   helper.FirstNonEmptyString(status.ContentType, userExportContentType),
		ContentLength: info.Size(),
		FileName:      helper.FirstNonEmptyString(status.FileName, info.Name()),
		LastModified:  info.ModTime(),
		AcceptRanges:  true,
	}, nil
}

// RunExportTask 执行前台用户列表异步导出任务。
func (l *Logic) RunExportTask(payload *types.UserExportTaskPayload) error {
	if payload == nil {
		return errors.Errorf("前台用户导出任务载荷不能为空")
	}
	if strings.TrimSpace(payload.JobID) == "" {
		return errors.Errorf("前台用户导出任务 jobId 不能为空")
	}

	status, err := l.loadUserExportStatus(payload.JobID)
	if err != nil {
		return errors.Wrapf(err, "UserLogic.RunExportTask 查询前台用户导出任务[%s]失败", payload.JobID)
	}

	now := time.Now()
	status.Status = types.UserExportStatusRunning
	status.ErrorMessage = ""
	status.OperatorID = payload.OperatorID
	if status.StartedAt == "" {
		status.StartedAt = excel.FormatDateTime(now)
	}
	if status.SplitRows <= 0 {
		status.SplitRows = l.userExportSplitRows()
	}
	if status.FileName == "" {
		status.FileName = buildUserExportFileName(payload.JobID, now)
	}
	status.FilePath = buildUserExportFilePath(status.FileName)
	status.ContentType = userExportContentType
	status.DownloadURL = buildUserExportDownloadURL(status.JobID)
	if err := l.saveUserExportStatus(status); err != nil {
		return errors.Wrapf(err, "UserLogic.RunExportTask 初始化前台用户导出任务[%s]运行态失败", payload.JobID)
	}

	if err := os.MkdirAll(filepath.Dir(status.FilePath), 0o755); err != nil {
		_ = l.markUserExportFailed(status, errors.Wrap(err, "创建前台用户导出目录失败"))
		return errors.Wrapf(err, "UserLogic.RunExportTask 创建前台用户导出目录失败 jobId=%s", payload.JobID)
	}

	useIdentityPath, err := l.useUserExportIdentityPath(&payload.Request)
	if err != nil {
		_ = l.markUserExportFailed(status, errors.Wrap(err, "校验前台用户导出查询路径失败"))
		return errors.Wrapf(err, "UserLogic.RunExportTask 校验前台用户导出查询路径失败 jobId=%s", payload.JobID)
	}
	cursorID := int64(0)
	partNo := 1
	for {
		partFileName := buildUserExportPartFileName(payload.JobID, now, partNo)
		partFilePath := buildUserExportFilePath(partFileName)
		status.FileName = partFileName
		status.FilePath = partFilePath
		status.ContentType = userExportContentType
		status.DownloadURL = buildUserExportDownloadURL(status.JobID, partNo)
		if err := l.saveUserExportStatus(status); err != nil {
			return errors.Wrapf(err, "UserLogic.RunExportTask 保存前台用户导出分片[%d]运行态失败 jobId=%s", partNo, payload.JobID)
		}

		baseProcessed := status.Processed
		partResult, err := l.runUserExportPart(&payload.Request, status, useIdentityPath, cursorID, partNo, partFilePath)
		if err != nil {
			_ = l.markUserExportFailed(status, err)
			_ = os.Remove(partFilePath)
			return errors.Wrapf(err, "UserLogic.RunExportTask 执行前台用户导出分片[%d]失败 jobId=%s", partNo, payload.JobID)
		}
		if partResult.RowCount <= 0 && partResult.HasMore {
			err := errors.Errorf("前台用户导出分片[%d]未写入数据但仍存在下一分片 jobId=%s cursor_id=%d", partNo, payload.JobID, partResult.LastCursor)
			_ = l.markUserExportFailed(status, err)
			_ = os.Remove(partFilePath)
			return errors.Tag(err)
		}
		if partResult.RowCount <= 0 && len(status.Files) > 0 {
			_ = os.Remove(partFilePath)
			break
		}
		fileItem, err := l.persistUserExportPart(status, partNo, partFileName, partFilePath, baseProcessed, partResult)
		if err != nil {
			_ = l.markUserExportFailed(status, err)
			_ = os.Remove(partFilePath)
			return errors.Wrapf(err, "UserLogic.RunExportTask 持久化前台用户导出分片[%d]失败 jobId=%s", partNo, payload.JobID)
		}
		_ = os.Remove(partFilePath)

		status.Files = append(status.Files, fileItem)
		status.PartCount = len(status.Files)
		status.Processed = max(status.Processed, fileItem.ProcessedTo)
		status.DownloadReady = true
		applyUserExportDownloadFile(status, fileItem)
		if err := l.saveUserExportStatus(status); err != nil {
			return errors.Wrapf(err, "UserLogic.RunExportTask 保存前台用户导出分片[%d]完成态失败 jobId=%s", partNo, payload.JobID)
		}
		if !partResult.HasMore {
			break
		}
		cursorID = partResult.LastCursor
		partNo++
	}

	finishedAt := time.Now()
	status.Status = types.UserExportStatusSucceeded
	status.Progress = 100
	status.Total = max(status.Total, status.Processed)
	status.EstimatedSeconds = 0
	status.DownloadReady = len(userExportReadyFiles(status)) > 0
	status.FinishedAt = excel.FormatDateTime(finishedAt)
	status.LastProcessedAt = excel.FormatDateTime(finishedAt)
	if status.Total <= 0 {
		status.AverageRowsPerSec = status.Processed
	}
	if err := l.saveUserExportStatus(status); err != nil {
		return errors.Wrapf(err, "UserLogic.RunExportTask 保存前台用户导出完成状态失败 jobId=%s", payload.JobID)
	}
	return nil
}

// validateUserExportQueryBoundary 校验当前导出筛选在单表或分表阶段是否可执行。
func (l *Logic) validateUserExportQueryBoundary(req *types.UserExportReq) *types.BizResult {
	db, err := l.userReadDB()
	if err != nil {
		return l.userDBError("UserLogic.TriggerExport 前台用户库未配置", err)
	}
	useIdentity, err := l.useUserIdentityList(db)
	if err != nil {
		return types.DBError(i18n.MsgKeyDBError, err, "UserLogic.TriggerExport 判断业务用户分表状态失败").ToBizResult()
	}
	if !useIdentity {
		return nil
	}
	if err := validateUserIdentityListReq(req.ToUserListReq()); err != nil {
		return types.ParamErrorResult(err).WithError(err)
	}
	return nil
}

// triggerUserExportWithUniqueLock 串行化同条件导出任务创建或复用。
func (l *Logic) triggerUserExportWithUniqueLock(req *types.UserExportReq, operatorID int, operatorName string) (*types.UserExportTriggerResp, error) {
	splitRows := l.userExportSplitRows()
	fingerprint := buildUserExportRequestFingerprint(req, splitRows)
	lockKey := l.userExportRequestLockKey(fingerprint)
	var triggerResp *types.UserExportTriggerResp
	err := redislock.WithLock(l.Ctx, l.Redis(), lockKey, userExportTriggerLockTTL, func(ctx context.Context) error {
		var innerErr error
		triggerResp, innerErr = l.triggerOrReuseUserExportUnderLock(req, fingerprint, splitRows, operatorID, operatorName)
		return errors.Tag(innerErr)
	})
	if err == nil {
		return triggerResp, nil
	}
	if reusedResp, reuseErr := l.tryReuseUserExportJob(fingerprint, operatorID); reuseErr == nil && reusedResp != nil {
		return reusedResp, nil
	}
	return nil, errors.Tag(err)
}

// triggerOrReuseUserExportUnderLock 在持有同条件互斥锁时创建或复用导出任务。
func (l *Logic) triggerOrReuseUserExportUnderLock(req *types.UserExportReq, fingerprint string, splitRows int64, operatorID int, operatorName string) (*types.UserExportTriggerResp, error) {
	if req == nil {
		req = &types.UserExportReq{}
	}
	if reusedResp, err := l.tryReuseUserExportJob(fingerprint, operatorID); err != nil || reusedResp != nil {
		return reusedResp, errors.Tag(err)
	}
	jobID := uuid.NewString()
	now := time.Now()
	fileName := buildUserExportFileName(jobID, now)
	status := &types.UserExportStatusResp{
		JobID:              jobID,
		Queue:              taskqueue.QueueMaintenance,
		Status:             types.UserExportStatusQueued,
		Progress:           0,
		Processed:          0,
		Total:              0,
		EstimatedSeconds:   0,
		FileName:           fileName,
		DownloadReady:      false,
		SplitRows:          splitRows,
		CreatedAt:          corelogic.FormatDateTime(now),
		UpdatedAt:          corelogic.FormatDateTime(now),
		FilePath:           buildUserExportFilePath(fileName),
		OperatorID:         operatorID,
		DownloadURL:        buildUserExportDownloadURL(jobID),
		RequestFingerprint: fingerprint,
		AuthorizedAdminIDs: []int{operatorID},
	}
	if err := l.saveUserExportStatus(status); err != nil {
		return nil, errors.Wrap(err, "初始化前台用户导出任务失败")
	}
	if err := l.saveUserExportRequestIndex(fingerprint, jobID); err != nil {
		return nil, errors.Wrap(err, "保存前台用户导出复用索引失败")
	}
	payloadBody, err := json.Marshal(types.UserExportTaskPayload{
		JobID:        jobID,
		OperatorID:   operatorID,
		OperatorName: operatorName,
		Request:      *req,
	})
	if err != nil {
		_ = l.clearUserExportRequestIndex(fingerprint)
		_ = l.markUserExportFailed(status, errors.Wrap(err, "构造前台用户导出任务载荷失败"))
		return nil, errors.Wrap(err, "构造前台用户导出任务载荷失败")
	}
	timeoutSeconds := l.Svc.CurrentConfig().Task.DefaultTimeoutSeconds
	if timeoutSeconds < userExportTaskTimeoutSeconds {
		timeoutSeconds = userExportTaskTimeoutSeconds
	}
	uniqueTTLSeconds := int(userExportReuseIndexTTL.Seconds())
	enqueueResp, err := l.Svc.Task.EnqueueRegisteredTask(l.Ctx, &types.EnqueueTaskReq{
		TaskType:         types.UserExportTaskType,
		Payload:          payloadBody,
		Queue:            taskqueue.QueueMaintenance,
		TimeoutSeconds:   &timeoutSeconds,
		UniqueTTLSeconds: &uniqueTTLSeconds,
	})
	if err != nil {
		_ = l.clearUserExportRequestIndex(fingerprint)
		_ = l.markUserExportFailed(status, errors.Wrap(err, "投递前台用户导出任务失败"))
		return nil, errors.Wrap(err, "投递前台用户导出任务失败")
	}
	status.TaskID = enqueueResp.TaskID
	status.Queue = helper.FirstNonEmptyString(enqueueResp.Queue, taskqueue.QueueMaintenance)
	status.ProcessAt = enqueueResp.ProcessAt
	if err := l.saveUserExportStatus(status); err != nil {
		return nil, errors.Wrap(err, "更新前台用户导出任务回执失败")
	}
	return l.buildUserExportTriggerResp(status), nil
}

// tryReuseUserExportJob 复用同一导出条件下仍可用的任务，避免重复工作。
func (l *Logic) tryReuseUserExportJob(fingerprint string, operatorID int) (*types.UserExportTriggerResp, error) {
	jobID, err := l.loadUserExportRequestIndex(fingerprint)
	if err != nil || jobID == "" {
		return nil, errors.Tag(err)
	}
	status, err := l.loadUserExportStatus(jobID)
	if err != nil {
		_ = l.clearUserExportRequestIndex(fingerprint)
		return nil, nil
	}
	switch status.Status {
	case types.UserExportStatusQueued, types.UserExportStatusRunning:
		if addUserExportAuthorizedAdmin(status, operatorID) {
			if saveErr := l.saveUserExportStatus(status); saveErr != nil {
				return nil, errors.Wrapf(saveErr, "保存前台用户导出授权人失败 job_id=%s operator_id=%d", status.JobID, operatorID)
			}
		}
		return l.buildUserExportTriggerResp(status), nil
	case types.UserExportStatusSucceeded:
		if !l.hasReusableUserExportDownloadObject(status) {
			_ = l.clearUserExportRequestIndex(fingerprint)
			return nil, nil
		}
		if addUserExportAuthorizedAdmin(status, operatorID) {
			if saveErr := l.saveUserExportStatus(status); saveErr != nil {
				return nil, errors.Wrapf(saveErr, "保存前台用户导出授权人失败 job_id=%s operator_id=%d", status.JobID, operatorID)
			}
		}
		return l.buildUserExportTriggerResp(status), nil
	default:
		_ = l.clearUserExportRequestIndex(fingerprint)
		return nil, nil
	}
}

// useUserExportIdentityPath 判断导出是否应通过身份索引定位物理用户表。
func (l *Logic) useUserExportIdentityPath(req *types.UserExportReq) (bool, error) {
	db, err := l.userReadDB()
	if err != nil {
		return false, errors.Tag(err)
	}
	useIdentity, err := l.useUserIdentityList(db)
	if err != nil {
		return false, errors.Wrap(err, "判断业务用户分表状态失败")
	}
	if useIdentity {
		if err := validateUserIdentityListReq(req.ToUserListReq()); err != nil {
			return false, errors.Tag(err)
		}
	}
	return useIdentity, nil
}

// listUserExportShardBatch 按分片范围和 ID 游标顺序读取一批导出数据。
func (l *Logic) listUserExportShardBatch(ctx context.Context, req *types.UserExportReq, shard userExportIDShard, cursorID int64, limit int, useIdentityPath bool) (*excel.CursorPage[userExportRow, int64], error) {
	if useIdentityPath {
		return l.listUserIdentityShardBatch(ctx, req, shard, cursorID, limit)
	}
	return l.listUserTableShardBatch(ctx, req, shard, cursorID, limit)
}

// listUserTableShardBatch 在单表阶段直接按 user.id 游标读取导出数据。
func (l *Logic) listUserTableShardBatch(ctx context.Context, req *types.UserExportReq, shard userExportIDShard, cursorID int64, limit int) (*excel.CursorPage[userExportRow, int64], error) {
	if limit <= 0 {
		limit = userExportBatchSize
	}
	dbq, err := l.userExportQuery(ctx, req)
	if err != nil {
		return nil, errors.Tag(err)
	}
	users := make([]model.User, 0, limit)
	dbq = dbq.Where("id >= ?", shard.StartID).
		Order("id ASC").
		Limit(limit)
	if shard.EndID > 0 {
		dbq = dbq.Where("id <= ?", shard.EndID)
	}
	if cursorID >= shard.StartID {
		dbq = dbq.Where("id > ?", cursorID)
	}
	if err := dbq.Find(&users).Error; err != nil {
		return nil, errors.Wrap(err, "UserLogic.listUserTableShardBatch 查询前台用户分片批次失败")
	}
	rows := make([]userExportRow, 0, len(users))
	for _, row := range users {
		rows = append(rows, userExportRow{User: row, CursorID: row.ID})
	}
	page := &excel.CursorPage[userExportRow, int64]{Items: rows}
	if len(rows) > 0 {
		page.NextCursor = rows[len(rows)-1].CursorID
		page.HasMore = len(rows) >= limit && (shard.EndID <= 0 || rows[len(rows)-1].CursorID < shard.EndID)
	}
	return page, nil
}

// listUserIdentityShardBatch 在分表阶段通过身份索引读取一批导出数据。
func (l *Logic) listUserIdentityShardBatch(ctx context.Context, req *types.UserExportReq, shard userExportIDShard, cursorID int64, limit int) (*excel.CursorPage[userExportRow, int64], error) {
	if limit <= 0 {
		limit = userExportBatchSize
	}
	dbq, identityType, err := l.userExportIdentityQuery(ctx, req)
	if err != nil {
		return nil, errors.Tag(err)
	}
	identities := make([]model.UserIdentity, 0, limit)
	dbq = dbq.Where("user_id >= ?", shard.StartID).
		Order("user_id ASC").
		Limit(limit)
	if shard.EndID > 0 {
		dbq = dbq.Where("user_id <= ?", shard.EndID)
	}
	if cursorID >= shard.StartID {
		dbq = dbq.Where("user_id > ?", cursorID)
	}
	if err := dbq.Find(&identities).Error; err != nil {
		return nil, errors.Wrap(err, "UserLogic.listUserIdentityShardBatch 查询前台用户身份索引批次失败")
	}
	rows := make([]userExportRow, 0, len(identities))
	userDB, err := l.userReadDB()
	if err != nil {
		return nil, errors.Tag(err)
	}
	userDB = userDB.WithContext(ctx)
	for index := range identities {
		identities[index].IdentityType = identityType
		identity := identities[index]
		row, err := model.FindUserByIdentityRow(userDB, &identity)
		if err != nil {
			return nil, errors.Wrapf(err, "读取前台用户 ID[%d]失败", identity.UserID)
		}
		if row == nil || !userExportMatchedByUsername(*row, req.Username) {
			continue
		}
		rows = append(rows, userExportRow{User: *row, CursorID: identity.UserID})
	}
	page := &excel.CursorPage[userExportRow, int64]{Items: rows}
	if len(identities) > 0 {
		lastIdentity := identities[len(identities)-1]
		page.NextCursor = lastIdentity.UserID
		page.HasMore = len(identities) >= limit && (shard.EndID <= 0 || lastIdentity.UserID < shard.EndID)
	}
	return page, nil
}

// buildUserExportShards 返回单分片导出边界，避免触发阶段为了估算并发分片执行大表 COUNT 或 MIN/MAX。
func buildUserExportShards() []excel.ExportShard[userExportIDShard] {
	return []excel.ExportShard[userExportIDShard]{
		{Meta: userExportIDShard{StartID: 1, EndID: 0}},
	}
}

// runUserExportPart 生成单个用户导出文件分片，达到拆分阈值时停止并保留游标。
func (l *Logic) runUserExportPart(req *types.UserExportReq, status *types.UserExportStatusResp, useIdentityPath bool, startCursor int64, partNo int, filePath string) (userExportPartResult, error) {
	result := userExportPartResult{LastCursor: startCursor}
	if status == nil {
		return result, errors.Errorf("导出任务状态不能为空")
	}
	splitRows := status.SplitRows
	if splitRows <= 0 {
		splitRows = l.userExportSplitRows()
	}
	baseProcessed := status.Processed
	shards := buildUserExportShards()
	exportOpt := excel.ApplyStreamExportShardedOpts(
		excel.StreamExportShardedOptions[userExportRow, int64, userExportIDShard]{
			FilePath: filePath,
			Query: func(ctx context.Context, shard userExportIDShard, cursorID int64, limit int) (*excel.CursorPage[userExportRow, int64], error) {
				for {
					queryLimit := userExportPartQueryLimit(limit, splitRows-result.RowCount)
					page, queryErr := l.listUserExportShardBatch(ctx, req, shard, cursorID, queryLimit, useIdentityPath)
					if queryErr != nil {
						return nil, errors.Wrap(queryErr, "查询前台用户导出批次失败")
					}
					if page == nil {
						return &excel.CursorPage[userExportRow, int64]{}, nil
					}
					nextCursor, skipped, err := nextUserExportCursorAfterEmptyPage(page, cursorID)
					if err != nil {
						return nil, errors.Tag(err)
					}
					if skipped {
						cursorID = nextCursor
						result.LastCursor = nextCursor
						continue
					}
					if len(page.Items) > 0 {
						result.RowCount += int64(len(page.Items))
						result.LastCursor = page.NextCursor
					}
					page.Total = status.Total
					if result.RowCount >= splitRows && page.HasMore {
						hasMore, err := l.hasMoreUserExportRows(ctx, req, result.LastCursor, useIdentityPath)
						if err != nil {
							return nil, errors.Wrap(err, "探测前台用户导出下一分片失败")
						}
						result.HasMore = hasMore
						page.HasMore = false
					} else {
						result.HasMore = page.HasMore
					}
					return page, nil
				}
			},
			BuildRows: buildUserExportRows,
		},
		excel.WithStreamExportSheetName[userExportRow, int64, userExportIDShard](userExportSheetName),
		excel.WithStreamExportHeader[userExportRow, int64, userExportIDShard](buildUserExportHeader()),
		excel.WithStreamExportHeaderStyle[userExportRow, int64, userExportIDShard](&excelize.Style{
			Font:      &excelize.Font{Bold: true},
			Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		}),
		excel.WithStreamExportSheetRowLimit[userExportRow, int64, userExportIDShard](userExportSheetRowLimit(splitRows)),
		excel.WithStreamExportBatchSize[userExportRow, int64, userExportIDShard](userExportBatchSize),
		excel.WithStreamExportMaxConcurrentShards[userExportRow, int64, userExportIDShard](userExportMaxConcurrentShards),
		excel.WithStreamExportChunkBufferSize[userExportRow, int64, userExportIDShard](userExportMaxConcurrentShards*2),
		excel.WithStreamExportRetry[userExportRow, int64, userExportIDShard](3, 100*time.Millisecond),
		excel.WithStreamExportShards[userExportRow, int64, userExportIDShard](shards),
		excel.WithStreamExportInitialCursor[userExportRow, int64, userExportIDShard](func(shard userExportIDShard) int64 {
			return startCursor
		}),
		excel.WithStreamExportProgress[userExportRow, int64, userExportIDShard](func(progress excel.ExportProgress) error {
			status.Processed = baseProcessed + progress.Processed
			status.LastCursorID = result.LastCursor
			status.PartCount = max(status.PartCount, partNo-1)
			if status.Total > 0 {
				status.Total = baseProcessed + progress.Total
				status.Progress = progress.Progress
				status.EstimatedSeconds = progress.EstimatedSeconds
			} else {
				status.Progress = 0
				status.EstimatedSeconds = 0
			}
			status.AverageRowsPerSec = progress.AverageRowsPerSec
			status.LastProcessedAt = excel.FormatDateTime(progress.LastProcessedAt)
			return l.saveUserExportStatus(status)
		}),
		excel.WithStreamExportProgressThrottle[userExportRow, int64, userExportIDShard](userExportProgressMinInterval, userExportProgressMinRows),
	)
	if err := excel.StreamExportSharded(l.Ctx, exportOpt); err != nil {
		return result, errors.Wrapf(err, "执行通用流式导出失败 part_no=%d", partNo)
	}
	return result, nil
}

// userExportPartQueryLimit 返回当前分片剩余可读取数量，避免单文件超过配置阈值。
func userExportPartQueryLimit(limit int, remainingRows int64) int {
	if limit <= 0 {
		limit = userExportBatchSize
	}
	if remainingRows <= 0 {
		return 1
	}
	if remainingRows < int64(limit) {
		return int(remainingRows)
	}
	return limit
}

// nextUserExportCursorAfterEmptyPage 返回空页可跳过时的下一游标，避免身份索引空页截断导出。
func nextUserExportCursorAfterEmptyPage(page *excel.CursorPage[userExportRow, int64], cursorID int64) (int64, bool, error) {
	if page == nil || len(page.Items) > 0 || !page.HasMore {
		return cursorID, false, nil
	}
	if page.NextCursor <= cursorID {
		return cursorID, false, errors.Errorf("前台用户导出空页游标未推进 cursor_id=%d next_cursor=%d", cursorID, page.NextCursor)
	}
	return page.NextCursor, true, nil
}

// userExportSheetRowLimit 返回当前导出文件允许写入的最大 Excel 行数，包含表头。
func userExportSheetRowLimit(splitRows int64) int {
	if splitRows <= 0 || splitRows > userExportMaxSplitRows {
		return excel.MaxExcelSheetRows
	}
	return int(splitRows) + 1
}

// hasMoreUserExportRows 探测拆分边界后是否仍存在下一条数据，避免整除阈值时生成空文件。
func (l *Logic) hasMoreUserExportRows(ctx context.Context, req *types.UserExportReq, cursorID int64, useIdentityPath bool) (bool, error) {
	page, err := l.listUserExportShardBatch(ctx, req, userExportIDShard{StartID: 1, EndID: 0}, cursorID, 1, useIdentityPath)
	if err != nil {
		return false, errors.Tag(err)
	}
	return page != nil && len(page.Items) > 0, nil
}

// userExportQuery 构造单表阶段导出统一查询条件，保持与列表页筛选逻辑一致。
func (l *Logic) userExportQuery(ctx context.Context, req *types.UserExportReq) (*gorm.DB, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	db, err := l.userReadDB()
	if err != nil {
		return nil, errors.Tag(err)
	}
	dbq := db.WithContext(ctx).Model(&model.User{})
	if req == nil {
		return dbq, nil
	}
	if req.ID > 0 {
		dbq = dbq.Where("id = ?", req.ID)
	}
	if req.ShardNo != nil {
		dbq = dbq.Where("shard_no = ?", *req.ShardNo)
	}
	if req.Username != "" {
		dbq = dbq.Where("username LIKE ?", req.Username+"%")
	}
	if req.Email != "" {
		emailHash, err := model.UserContactIdentityHash(model.UserIdentityTypeEmail, req.Email, l.Svc.CurrentConfig().AppKey)
		if err != nil {
			return nil, errors.Tag(err)
		}
		dbq = dbq.Where("email_hash = ?", emailHash)
	}
	if req.Phone != "" {
		phoneHash, err := model.UserContactIdentityHash(model.UserIdentityTypePhone, req.Phone, l.Svc.CurrentConfig().AppKey)
		if err != nil {
			return nil, errors.Tag(err)
		}
		dbq = dbq.Where("phone_hash = ?", phoneHash)
	}
	if req.Status != nil {
		dbq = dbq.Where("status = ?", *req.Status)
	}
	return dbq, nil
}

// userExportIdentityQuery 构造分表阶段身份索引导出查询。
func (l *Logic) userExportIdentityQuery(ctx context.Context, req *types.UserExportReq) (*gorm.DB, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if req == nil {
		req = &types.UserExportReq{}
	}
	identityType, identityHash, err := l.userExportIdentityFilter(req)
	if err != nil {
		return nil, "", errors.Tag(err)
	}
	tableName, err := model.UserIdentityTableName(identityType)
	if err != nil {
		return nil, "", errors.Tag(err)
	}
	db, err := l.userReadDB()
	if err != nil {
		return nil, "", errors.Tag(err)
	}
	db = db.WithContext(ctx)
	dbq := db.Model(&model.UserIdentity{}).Table(userIdentityListTableName(tableName, identityType, req.Username))
	if req.ID > 0 {
		dbq = dbq.Where("user_id = ?", req.ID)
	}
	if req.ShardNo != nil {
		dbq = dbq.Where("user_shard_no = ?", *req.ShardNo)
	}
	if identityHash != "" {
		dbq = dbq.Where("identity_hash = ?", identityHash)
	} else if req.Username != "" {
		dbq = dbq.Where("identity_value LIKE ?", strings.ToLower(req.Username)+"%")
	}
	return dbq, identityType, nil
}

// userExportIdentityFilter 返回导出请求在身份索引表上的查询身份类型和 hash。
func (l *Logic) userExportIdentityFilter(req *types.UserExportReq) (string, string, error) {
	identityType := model.UserIdentityTypeUsername
	if req == nil {
		return identityType, "", nil
	}
	if req.Email != "" {
		identityHash, err := model.UserContactIdentityHash(model.UserIdentityTypeEmail, req.Email, l.Svc.CurrentConfig().AppKey)
		if err != nil {
			return "", "", errors.Tag(err)
		}
		return model.UserIdentityTypeEmail, identityHash, nil
	}
	if req.Phone != "" {
		identityHash, err := model.UserContactIdentityHash(model.UserIdentityTypePhone, req.Phone, l.Svc.CurrentConfig().AppKey)
		if err != nil {
			return "", "", errors.Tag(err)
		}
		return model.UserIdentityTypePhone, identityHash, nil
	}
	return identityType, "", nil
}

// ensureUserExportTaskQueueEnabled 校验任务系统是否可用。
func (l *Logic) ensureUserExportTaskQueueEnabled() error {
	if l == nil || l.Svc == nil || l.Svc.Task == nil || !l.Svc.Task.IsEnabled() {
		return errors.Errorf("任务系统未启用")
	}
	return nil
}

// ensureUserExportRedisEnabled 校验 Redis 是否可用。
func (l *Logic) ensureUserExportRedisEnabled() error {
	if l == nil || l.Redis() == nil {
		return errors.Errorf("Redis 未初始化")
	}
	return nil
}

// userExportSplitRows 返回当前用户导出单文件拆分阈值。
func (l *Logic) userExportSplitRows() int64 {
	if l == nil || l.Svc == nil {
		return userExportDefaultSplitRows
	}
	splitRows := l.Svc.CurrentConfig().User.ExportSplitRows
	if splitRows <= 0 {
		return userExportDefaultSplitRows
	}
	if splitRows > userExportMaxSplitRows {
		return userExportMaxSplitRows
	}
	return splitRows
}

// loadUserExportRequestIndex 读取同条件导出复用索引。
func (l *Logic) loadUserExportRequestIndex(fingerprint string) (string, error) {
	if err := l.ensureUserExportRedisEnabled(); err != nil {
		return "", errors.Tag(err)
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return "", nil
	}
	value, err := l.Redis().Get(l.Ctx, l.userExportRequestIndexKey(fingerprint)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil
		}
		return "", errors.Wrap(err, "读取前台用户导出复用索引失败")
	}
	return strings.TrimSpace(value), nil
}

// saveUserExportRequestIndex 保存同条件导出复用索引。
func (l *Logic) saveUserExportRequestIndex(fingerprint string, jobID string) error {
	if err := l.ensureUserExportRedisEnabled(); err != nil {
		return errors.Tag(err)
	}
	fingerprint = strings.TrimSpace(fingerprint)
	jobID = strings.TrimSpace(jobID)
	if fingerprint == "" || jobID == "" {
		return errors.Errorf("导出复用索引参数不能为空")
	}
	if err := l.Redis().Set(l.Ctx, l.userExportRequestIndexKey(fingerprint), jobID, userExportReuseIndexTTL).Err(); err != nil {
		return errors.Wrap(err, "保存前台用户导出复用索引失败")
	}
	return nil
}

// clearUserExportRequestIndex 清理同条件导出复用索引。
func (l *Logic) clearUserExportRequestIndex(fingerprint string) error {
	if err := l.ensureUserExportRedisEnabled(); err != nil {
		return errors.Tag(err)
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return nil
	}
	if err := l.Redis().Del(l.Ctx, l.userExportRequestIndexKey(fingerprint)).Err(); err != nil {
		return errors.Wrap(err, "清理前台用户导出复用索引失败")
	}
	return nil
}

// loadUserExportStatus 从 Redis 读取前台用户导出任务状态。
func (l *Logic) loadUserExportStatus(jobID string) (*types.UserExportStatusResp, error) {
	if err := l.ensureUserExportRedisEnabled(); err != nil {
		return nil, errors.Tag(err)
	}
	snapshot := &userExportStatusSnapshot{}
	if err := l.userExportStatusStore().Load(l.Ctx, jobID, snapshot); err != nil {
		return nil, errors.Wrapf(err, "UserLogic.loadUserExportStatus 读取前台用户导出任务[%s]失败", jobID)
	}
	status := snapshot.toStatus()
	if status.DownloadURL == "" {
		status.DownloadURL = buildUserExportDownloadURL(status.JobID)
	}
	return status, nil
}

// saveUserExportStatus 将前台用户导出任务状态写回 Redis。
func (l *Logic) saveUserExportStatus(status *types.UserExportStatusResp) error {
	if status == nil {
		return errors.Errorf("前台用户导出任务状态不能为空")
	}
	if err := l.ensureUserExportRedisEnabled(); err != nil {
		return errors.Tag(err)
	}
	now := time.Now()
	if status.CreatedAt == "" {
		status.CreatedAt = corelogic.FormatDateTime(now)
	}
	status.UpdatedAt = corelogic.FormatDateTime(now)
	if status.DownloadURL == "" {
		status.DownloadURL = buildUserExportDownloadURL(status.JobID)
	}
	if err := l.userExportStatusStore().Save(l.Ctx, status.JobID, newUserExportStatusSnapshot(status)); err != nil {
		return errors.Wrapf(err, "UserLogic.saveUserExportStatus 保存前台用户导出任务[%s]失败", status.JobID)
	}
	return nil
}

// markUserExportFailed 把前台用户导出任务更新为失败状态。
func (l *Logic) markUserExportFailed(status *types.UserExportStatusResp, err error) error {
	if status == nil {
		return nil
	}
	status.Status = types.UserExportStatusFailed
	status.DownloadReady = len(userExportReadyFiles(status)) > 0
	status.Progress = excel.ClampProgress(status.Progress)
	if err != nil {
		status.ErrorMessage = err.Error()
	}
	status.FinishedAt = excel.FormatDateTime(time.Now())
	if clearErr := l.clearUserExportRequestIndex(status.RequestFingerprint); clearErr != nil {
		corelogic.LogWrappedError(l, clearErr, "UserLogic.markUserExportFailed 清理前台用户导出复用索引失败")
	}
	saveErr := l.saveUserExportStatus(status)
	l.notifyUserExportFailed(status, userExportFirstNonNilError(err, saveErr))
	return saveErr
}

// notifyUserExportFailed 上报前台用户 Excel 异步导出失败。
func (l *Logic) notifyUserExportFailed(status *types.UserExportStatusResp, err error) {
	if l == nil || l.Svc == nil || status == nil || err == nil {
		return
	}
	svc.NotifyTaskRuntimeAlert(l.Ctx, l.Svc.Task, svc.TaskRuntimeAlert{
		Kind:      svc.TaskRuntimeAlertKindUserExcelExportFailed,
		Title:     "【P1 Excel 异步导出失败】",
		Status:    "导出任务已标记失败，已生成文件分片保留可下载状态",
		Component: "excel_export",
		Operation: "user_export",
		TaskName:  "前台用户列表导出",
		TaskType:  types.UserExportTaskType,
		TaskQueue: helper.FirstNonEmptyString(status.Queue, taskqueue.QueueMaintenance),
		UniqueKey: "user_export",
		Reason:    userExportAlertReason(status, err),
		Advice:    "请在导出任务状态中按 jobId 定位请求条件，并在任务中心按 taskId 查看执行日志；确认用户身份索引、对象存储和 Redis 状态后再重新触发导出。",
	})
}

// ensureUserExportStatusOwner 校验当前管理员只能查看和下载自己发起或被授权复用的导出任务。
func (l *Logic) ensureUserExportStatusOwner(status *types.UserExportStatusResp) error {
	if status == nil {
		return gorm.ErrRecordNotFound
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID <= 0 {
		return errors.Errorf("当前请求未登录")
	}
	if status.OperatorID == ctxAdmin.ID {
		return nil
	}
	for _, adminID := range status.AuthorizedAdminIDs {
		if adminID == ctxAdmin.ID {
			return nil
		}
	}
	if status.OperatorID > 0 && status.OperatorID != ctxAdmin.ID {
		return errors.Errorf("无权访问其他管理员的导出任务")
	}
	return nil
}

// publicUserExportStatus 返回对前端开放的前台用户导出状态快照。
func (l *Logic) publicUserExportStatus(status *types.UserExportStatusResp) *types.UserExportStatusResp {
	if status == nil {
		return &types.UserExportStatusResp{}
	}
	resp := *status
	resp.Progress = excel.ClampProgress(resp.Progress)
	resp.Files = publicUserExportFiles(resp.JobID, resp.Files)
	resp.PartCount = len(resp.Files)
	resp.DownloadReady = len(userExportReadyFiles(&resp)) > 0
	if fileItem, err := selectUserExportDownloadFile(&resp, 0); err == nil {
		applyUserExportDownloadFile(&resp, fileItem)
	} else {
		resp.DownloadURL = buildUserExportDownloadURL(resp.JobID)
	}
	return &resp
}

// userExportStatusStore 返回前台用户导出复用的通用任务状态存储。
func (l *Logic) userExportStatusStore() *excel.RedisStore {
	return excel.NewRedisStore(l.Redis(), l.userExportJobKeyPattern(), userExportStatusTTL)
}

// userExportRequestLockKey 返回当前 app_id 作用域下的前台用户导出条件互斥锁。
func (l *Logic) userExportRequestLockKey(fingerprint string) string {
	return l.AppRedisKey(fmt.Sprintf(keys.UserExportRequestLock, strings.TrimSpace(fingerprint)))
}

// userExportRequestIndexKey 返回当前 app_id 作用域下的前台用户导出条件复用索引。
func (l *Logic) userExportRequestIndexKey(fingerprint string) string {
	return l.AppRedisKey(fmt.Sprintf(keys.UserExportRequestIndex, strings.TrimSpace(fingerprint)))
}

// userExportJobKeyPattern 返回当前 app_id 作用域下的前台用户导出任务状态 key 模板。
func (l *Logic) userExportJobKeyPattern() string {
	return l.AppRedisKey(keys.UserExportJob)
}

// persistUserExportPart 将单个导出文件分片保存到对象存储。
func (l *Logic) persistUserExportPart(status *types.UserExportStatusResp, partNo int, fileName string, filePath string, baseProcessed int64, result userExportPartResult) (types.UserExportFileItem, error) {
	fileItem := types.UserExportFileItem{}
	if status == nil {
		return fileItem, errors.Errorf("导出任务状态不能为空")
	}
	objectStorage, err := l.userExportObjectStorage()
	if err != nil {
		return fileItem, errors.Tag(err)
	}
	storedObject, err := objectStorage.SaveLocalFile(l.Ctx, storage.SaveLocalFileReq{
		BizType:          "user-export",
		ContentType:      userExportContentType,
		LocalPath:        filePath,
		OriginalFileName: fileName,
		StoredFileName:   storage.BuildStoredFileName(buildUserExportPartStorageID(status.JobID, partNo), fileName),
		Visibility:       storage.VisibilityPrivate,
	})
	if err != nil {
		return fileItem, errors.Wrap(err, "上传前台用户导出文件到统一存储失败")
	}
	processedFrom := baseProcessed + 1
	processedTo := baseProcessed + result.RowCount
	if result.RowCount <= 0 {
		processedFrom = baseProcessed
		processedTo = baseProcessed
	}
	return types.UserExportFileItem{
		PartNo:        partNo,
		FileName:      fileName,
		RowCount:      result.RowCount,
		ProcessedFrom: processedFrom,
		ProcessedTo:   processedTo,
		FileSize:      storedObject.Size,
		DownloadReady: true,
		DownloadURL:   buildUserExportDownloadURL(status.JobID, partNo),
		CreatedAt:     excel.FormatDateTime(time.Now()),
		FilePath:      filePath,
		ObjectKey:     storedObject.ObjectKey,
		StorageType:   storedObject.StorageType,
		ContentType:   storedObject.ContentType,
	}, nil
}

// hasReusableUserExportDownloadObject 判断当前导出任务是否仍保有可复用的下载对象。
func (l *Logic) hasReusableUserExportDownloadObject(status *types.UserExportStatusResp) bool {
	if status == nil {
		return false
	}
	for _, fileItem := range userExportReadyFiles(status) {
		if l.probeUserExportFileObject(fileItem) == nil {
			return true
		}
	}
	if len(status.Files) > 0 {
		return false
	}
	return l.probeUserExportDownloadObject(status) == nil
}

// refreshUserExportDownloadAvailability 刷新已完成导出任务的真实下载可用性。
func (l *Logic) refreshUserExportDownloadAvailability(status *types.UserExportStatusResp) error {
	if status == nil || !status.DownloadReady {
		return nil
	}
	changed := false
	for index := range status.Files {
		if !status.Files[index].DownloadReady {
			continue
		}
		if err := l.probeUserExportFileObject(status.Files[index]); err != nil {
			status.Files[index].DownloadReady = false
			changed = true
		}
	}
	if len(status.Files) == 0 {
		if err := l.probeUserExportDownloadObject(status); err != nil {
			status.DownloadReady = false
			changed = true
		}
	}
	status.DownloadReady = len(userExportReadyFiles(status)) > 0 || (len(status.Files) == 0 && status.DownloadReady)
	if status.DownloadReady {
		if changed {
			return l.saveUserExportStatus(status)
		}
		return nil
	}
	if status.Status == types.UserExportStatusSucceeded {
		status.DownloadReady = false
		status.Status = types.UserExportStatusFailed
		status.ErrorMessage = l.Message(i18n.MsgKeyUserExportFileExpired)
		if clearErr := l.clearUserExportRequestIndex(status.RequestFingerprint); clearErr != nil {
			corelogic.LogWrappedError(l, clearErr, "UserLogic.refreshUserExportDownloadAvailability 清理前台用户导出复用索引失败")
		}
		if saveErr := l.saveUserExportStatus(status); saveErr != nil {
			return errors.Wrap(saveErr, "保存导出文件失效状态失败")
		}
		return errors.Errorf("导出文件不存在或已失效")
	}
	if changed {
		return l.saveUserExportStatus(status)
	}
	return nil
}

// probeUserExportFileObject 探测指定导出文件分片是否仍可读取。
func (l *Logic) probeUserExportFileObject(fileItem types.UserExportFileItem) error {
	if strings.TrimSpace(fileItem.ObjectKey) != "" {
		objectStorage, err := l.userExportObjectStorage()
		if err != nil {
			return errors.Tag(err)
		}
		objectStream, err := objectStorage.Open(l.Ctx, storage.OpenObjectReq{
			ObjectKey:   fileItem.ObjectKey,
			RangeHeader: "bytes=0-0",
		})
		if err != nil {
			return errors.Wrap(err, "打开统一存储导出对象失败")
		}
		_ = objectStream.Reader.Close()
		return nil
	}
	if strings.TrimSpace(fileItem.FilePath) == "" || !userExportFileExists(fileItem.FilePath) {
		return errors.Errorf("导出文件不存在")
	}
	return nil
}

// probeUserExportDownloadObject 探测前台用户导出结果对象是否仍可读取。
func (l *Logic) probeUserExportDownloadObject(status *types.UserExportStatusResp) error {
	if status == nil {
		return errors.Errorf("导出任务状态不能为空")
	}
	if strings.TrimSpace(status.ObjectKey) != "" {
		objectStorage, err := l.userExportObjectStorage()
		if err != nil {
			return errors.Tag(err)
		}
		objectStream, err := objectStorage.Open(l.Ctx, storage.OpenObjectReq{
			ObjectKey:   status.ObjectKey,
			RangeHeader: "bytes=0-0",
		})
		if err != nil {
			return errors.Wrap(err, "打开统一存储导出对象失败")
		}
		_ = objectStream.Reader.Close()
		return nil
	}
	if strings.TrimSpace(status.FilePath) == "" || !userExportFileExists(status.FilePath) {
		return errors.Errorf("导出文件不存在")
	}
	return nil
}

// userExportObjectStorage 获取统一对象存储实例。
func (l *Logic) userExportObjectStorage() (storage.ObjectStorage, error) {
	if l == nil || l.Svc == nil {
		return nil, errors.Errorf("前台用户导出服务上下文为空")
	}
	return l.Svc.ObjectStorage()
}

// buildUserExportHeader 返回前台用户导出的标准表头。
func buildUserExportHeader() []any {
	return []any{
		"ID",
		"分片号",
		"用户名",
		"昵称",
		"邮箱(脱敏)",
		"手机号(脱敏)",
		"账号状态",
		"最近登录 IP",
		"最近登录时间",
		"创建时间",
		"更新时间",
		"头像访问地址",
	}
}

// buildUserExportRows 并发构造当前批次前台用户导出数据行。
func buildUserExportRows(rows []userExportRow) ([][]any, error) {
	if len(rows) == 0 {
		return [][]any{}, nil
	}
	return excel.BuildRowsConcurrentlyWithOpt(
		rows,
		func(row userExportRow) ([]any, error) {
			user := row.User
			return []any{
				strconv.FormatInt(user.ID, 10),
				user.ShardNo,
				user.Username,
				user.Nickname,
				user.EmailMasked,
				user.PhoneMasked,
				userExportStatusText(user.Status),
				user.LastLoginIP,
				corelogic.FormatDateTime(user.LastLoginAt),
				corelogic.FormatDateTime(user.CreatedAt),
				corelogic.FormatDateTime(user.UpdatedAt),
				user.Avatar,
			}, nil
		},
		excel.WithConcurrentRowsWorkersRange(userExportMinWorkerCount, userExportMaxWorkerCount),
	)
}

// buildUserExportFileName 构造前台用户导出文件名。
func buildUserExportFileName(jobID string, now time.Time) string {
	return buildUserExportPartFileName(jobID, now, 1)
}

// buildUserExportPartFileName 构造带顺序编号的前台用户导出文件名。
func buildUserExportPartFileName(jobID string, now time.Time, partNo int) string {
	if partNo <= 0 {
		partNo = 1
	}
	return fmt.Sprintf("%s_%s_%s_part_%04d.xlsx",
		userExportDefaultFileNamePrefix,
		now.Format("20060102150405"),
		strings.ReplaceAll(jobID, "-", ""),
		partNo)
}

// buildUserExportPartStorageID 构造分片对象存储文件 ID，避免不同分片覆盖。
func buildUserExportPartStorageID(jobID string, partNo int) string {
	if partNo <= 0 {
		partNo = 1
	}
	return fmt.Sprintf("%s_part_%04d", strings.ReplaceAll(strings.TrimSpace(jobID), "-", ""), partNo)
}

// buildUserExportFilePath 构造前台用户导出文件的本地存储路径。
func buildUserExportFilePath(fileName string) string {
	return filepath.Join(os.TempDir(), "admin", "exports", "user", fileName)
}

// buildUserExportDownloadURL 构造前台用户导出结果下载地址。
func buildUserExportDownloadURL(jobID string, partNo ...int) string {
	baseURL := fmt.Sprintf("/api/users/exports/download/%s", url.PathEscape(strings.TrimSpace(jobID)))
	if len(partNo) == 0 || partNo[0] <= 0 {
		return baseURL
	}
	return fmt.Sprintf("%s?partNo=%d", baseURL, partNo[0])
}

// buildUserExportRequestFingerprint 计算前台用户导出筛选条件的稳定指纹。
func buildUserExportRequestFingerprint(req *types.UserExportReq, splitRows int64) string {
	if req == nil {
		req = &types.UserExportReq{}
	}
	shardNo := ""
	if req.ShardNo != nil {
		shardNo = strconv.Itoa(*req.ShardNo)
	}
	statusText := ""
	if req.Status != nil {
		statusText = strconv.Itoa(*req.Status)
	}
	raw := strings.Join([]string{
		strconv.FormatInt(req.ID, 10),
		shardNo,
		strings.TrimSpace(req.Username),
		strings.ToLower(strings.TrimSpace(req.Email)),
		strings.TrimSpace(req.Phone),
		statusText,
		strconv.FormatInt(splitRows, 10),
	}, "|")
	return utils.SHA256(raw)
}

// buildUserExportTriggerResp 从任务状态构造提交导出后的统一回执。
func (l *Logic) buildUserExportTriggerResp(status *types.UserExportStatusResp) *types.UserExportTriggerResp {
	if status == nil {
		return &types.UserExportTriggerResp{}
	}
	return &types.UserExportTriggerResp{
		JobID:  status.JobID,
		TaskID: status.TaskID,
		Queue:  status.Queue,
		Status: status.Status,
	}
}

// addUserExportAuthorizedAdmin 把管理员加入可复用任务访问名单。
func addUserExportAuthorizedAdmin(status *types.UserExportStatusResp, adminID int) bool {
	if status == nil || adminID <= 0 {
		return false
	}
	for _, item := range status.AuthorizedAdminIDs {
		if item == adminID {
			return false
		}
	}
	status.AuthorizedAdminIDs = append(status.AuthorizedAdminIDs, adminID)
	return true
}

// selectUserExportDownloadFile 选择本次下载的导出文件分片。
func selectUserExportDownloadFile(status *types.UserExportStatusResp, partNo int) (types.UserExportFileItem, error) {
	if status == nil {
		return types.UserExportFileItem{}, errors.Errorf("导出任务状态不能为空")
	}
	for _, fileItem := range status.Files {
		if !fileItem.DownloadReady {
			continue
		}
		if partNo > 0 && fileItem.PartNo != partNo {
			continue
		}
		return fileItem, nil
	}
	if partNo > 0 {
		return types.UserExportFileItem{}, errors.Errorf("导出文件分片[%d]尚未生成或已失效", partNo)
	}
	if len(status.Files) == 0 && status.DownloadReady {
		return types.UserExportFileItem{
			PartNo:        1,
			FileName:      status.FileName,
			FilePath:      status.FilePath,
			ObjectKey:     status.ObjectKey,
			StorageType:   status.StorageType,
			ContentType:   status.ContentType,
			DownloadReady: true,
			DownloadURL:   buildUserExportDownloadURL(status.JobID, 1),
		}, nil
	}
	return types.UserExportFileItem{}, errors.Errorf("暂无可下载的导出文件")
}

// applyUserExportDownloadFile 把指定文件分片映射到旧版顶层下载字段。
func applyUserExportDownloadFile(status *types.UserExportStatusResp, fileItem types.UserExportFileItem) {
	if status == nil {
		return
	}
	status.FileName = fileItem.FileName
	status.FilePath = fileItem.FilePath
	status.ObjectKey = fileItem.ObjectKey
	status.StorageType = fileItem.StorageType
	status.ContentType = helper.FirstNonEmptyString(fileItem.ContentType, userExportContentType)
	status.DownloadURL = buildUserExportDownloadURL(status.JobID, fileItem.PartNo)
	status.DownloadReady = fileItem.DownloadReady
}

// userExportReadyFiles 返回已可下载的文件分片。
func userExportReadyFiles(status *types.UserExportStatusResp) []types.UserExportFileItem {
	if status == nil {
		return nil
	}
	files := make([]types.UserExportFileItem, 0, len(status.Files))
	for _, fileItem := range status.Files {
		if fileItem.DownloadReady {
			files = append(files, fileItem)
		}
	}
	return files
}

// publicUserExportFiles 返回对前端开放的文件分片列表。
func publicUserExportFiles(jobID string, files []types.UserExportFileItem) []types.UserExportFileItem {
	if len(files) == 0 {
		return []types.UserExportFileItem{}
	}
	items := make([]types.UserExportFileItem, 0, len(files))
	for _, fileItem := range files {
		fileItem.DownloadURL = buildUserExportDownloadURL(jobID, fileItem.PartNo)
		fileItem.FilePath = ""
		fileItem.ObjectKey = ""
		fileItem.StorageType = ""
		fileItem.ContentType = ""
		items = append(items, fileItem)
	}
	return items
}

// userExportMatchedByUsername 判断读取出的用户是否满足用户名筛选。
func userExportMatchedByUsername(row model.User, username string) bool {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return true
	}
	return strings.HasPrefix(strings.ToLower(row.Username), username)
}

// userExportStatusText 把前台用户状态转换为导出文本。
func userExportStatusText(status int) string {
	if status == model.UserStatusEnabled {
		return "启用"
	}
	return "禁用"
}

// userExportAlertReason 生成导出告警错误摘要，保留 jobId/taskId 但不参与告警指纹。
func userExportAlertReason(status *types.UserExportStatusResp, err error) string {
	parts := make([]string, 0, 3)
	if status != nil && strings.TrimSpace(status.JobID) != "" {
		parts = append(parts, "jobId="+strings.TrimSpace(status.JobID))
	}
	if status != nil && strings.TrimSpace(status.TaskID) != "" {
		parts = append(parts, "taskId="+strings.TrimSpace(status.TaskID))
	}
	if err != nil {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "；")
}

// userExportFirstNonNilError 返回首个非空错误，便于统一拼接业务错误链。
func userExportFirstNonNilError(values ...error) error {
	for _, err := range values {
		if err != nil {
			return errors.Tag(err)
		}
	}
	return nil
}

// userExportFileExists 判断文件是否存在。
func userExportFileExists(filePath string) bool {
	if strings.TrimSpace(filePath) == "" {
		return false
	}
	_, err := os.Stat(filePath)
	return err == nil
}

// newUserExportStatusSnapshot 把运行态状态对象转换成 Redis 持久化快照。
func newUserExportStatusSnapshot(status *types.UserExportStatusResp) *userExportStatusSnapshot {
	if status == nil {
		return &userExportStatusSnapshot{}
	}
	snapshot := &userExportStatusSnapshot{
		UserExportStatusResp: *status,
		FilePath:             status.FilePath,
		ObjectKey:            status.ObjectKey,
		StorageType:          status.StorageType,
		ContentType:          status.ContentType,
		LastCursorID:         status.LastCursorID,
		OperatorID:           status.OperatorID,
		RequestFingerprint:   status.RequestFingerprint,
	}
	snapshot.UserExportStatusResp.Files = nil
	if len(status.Files) > 0 {
		snapshot.Files = make([]userExportFileSnapshot, 0, len(status.Files))
		for _, fileItem := range status.Files {
			snapshot.Files = append(snapshot.Files, newUserExportFileSnapshot(fileItem))
		}
	}
	if len(status.AuthorizedAdminIDs) > 0 {
		snapshot.AuthorizedAdminIDs = append([]int(nil), status.AuthorizedAdminIDs...)
	}
	return snapshot
}

// newUserExportFileSnapshot 把运行态文件分片转换成 Redis 快照。
func newUserExportFileSnapshot(fileItem types.UserExportFileItem) userExportFileSnapshot {
	return userExportFileSnapshot{
		PartNo:        fileItem.PartNo,
		FileName:      fileItem.FileName,
		RowCount:      fileItem.RowCount,
		ProcessedFrom: fileItem.ProcessedFrom,
		ProcessedTo:   fileItem.ProcessedTo,
		FileSize:      fileItem.FileSize,
		DownloadReady: fileItem.DownloadReady,
		DownloadURL:   fileItem.DownloadURL,
		CreatedAt:     fileItem.CreatedAt,
		FilePath:      fileItem.FilePath,
		ObjectKey:     fileItem.ObjectKey,
		StorageType:   fileItem.StorageType,
		ContentType:   fileItem.ContentType,
	}
}

// toStatus 把 Redis 快照还原成运行态状态对象。
func (s *userExportStatusSnapshot) toStatus() *types.UserExportStatusResp {
	if s == nil {
		return &types.UserExportStatusResp{}
	}
	status := s.UserExportStatusResp
	status.FilePath = s.FilePath
	status.ObjectKey = s.ObjectKey
	status.StorageType = s.StorageType
	status.ContentType = s.ContentType
	status.LastCursorID = s.LastCursorID
	status.OperatorID = s.OperatorID
	status.RequestFingerprint = s.RequestFingerprint
	if len(s.Files) > 0 {
		status.Files = make([]types.UserExportFileItem, 0, len(s.Files))
		for _, fileSnapshot := range s.Files {
			status.Files = append(status.Files, fileSnapshot.toItem())
		}
	}
	if len(s.AuthorizedAdminIDs) > 0 {
		status.AuthorizedAdminIDs = append([]int(nil), s.AuthorizedAdminIDs...)
	} else {
		status.AuthorizedAdminIDs = nil
	}
	return &status
}

// toItem 把 Redis 文件快照还原成运行态文件分片。
func (s userExportFileSnapshot) toItem() types.UserExportFileItem {
	return types.UserExportFileItem{
		PartNo:        s.PartNo,
		FileName:      s.FileName,
		RowCount:      s.RowCount,
		ProcessedFrom: s.ProcessedFrom,
		ProcessedTo:   s.ProcessedTo,
		FileSize:      s.FileSize,
		DownloadReady: s.DownloadReady,
		DownloadURL:   s.DownloadURL,
		CreatedAt:     s.CreatedAt,
		FilePath:      s.FilePath,
		ObjectKey:     s.ObjectKey,
		StorageType:   s.StorageType,
		ContentType:   s.ContentType,
	}
}
