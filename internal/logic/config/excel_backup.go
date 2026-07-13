package config

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	keys "admin/common/rediskeys"
	redislock "admin/internal/infra/redsync"
	cachelogic "admin/internal/logic/cache"
	filelogic "admin/internal/logic/file"
	"admin/internal/model"
	"admin/internal/svc"
	taskqueue "admin/internal/task/queue"
	"admin/internal/types"
	pkgexcel "admin/pkg/excel"
	"admin/pkg/storage"

	"github.com/Is999/go-utils/errors"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	// sysConfigExcelBackupRetention 表示字典导入前备份在服务器保留 30 天。
	sysConfigExcelBackupRetention = 30 * 24 * time.Hour
	// sysConfigExcelContentType 表示字典 Excel 文件的标准 MIME。
	sysConfigExcelContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	// sysConfigExcelBackupBizType 表示字典导入前备份的私有对象目录。
	sysConfigExcelBackupBizType = "sys-config-excel-backup"
)

var (
	// errSysConfigBackupTaskUnavailable 表示字典备份清理任务无法投递。
	errSysConfigBackupTaskUnavailable = errors.New("字典备份清理任务不可用")
)

// sysConfigImportBackupState 表示 Redis 中保存的字典导入前备份状态。
type sysConfigImportBackupState struct {
	BackupID       string    `json:"backupId"`       // 备份 ID
	UploadID       string    `json:"uploadId"`       // 绑定的待导入上传会话 ID
	OperatorID     int       `json:"operatorId"`     // 创建备份的管理员 ID
	ImportFileHash string    `json:"importFileHash"` // 待导入 Excel 文件摘要
	FileName       string    `json:"fileName"`       // 用户下载文件名
	ObjectKey      string    `json:"objectKey"`      // 私有存储对象 key
	SnapshotHash   string    `json:"snapshotHash"`   // 备份时完整字典数据摘要
	RowCount       int64     `json:"rowCount"`       // 备份时字典行数
	ExpiresAt      time.Time `json:"expiresAt"`      // 过期时间
	DownloadedAt   time.Time `json:"downloadedAt"`   // 完整下载响应完成时间
}

// PrepareImportBackup 校验待导入文件并生成全量字典备份。
func (l *SysConfigLogic) PrepareImportBackup(req *types.SysConfigExcelBackupReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(errors.Errorf("备份请求不能为空"))
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	if err := l.ensureSysConfigBackupDependencies(); err != nil {
		return sysConfigInfrastructureResult(err,
			"SysConfigLogic.PrepareImportBackup 备份依赖不可用")
	}

	fileTransferLogic := filelogic.NewFileTransferLogicWithContext(l.Ctx, l.Svc)
	importFilePath, _, cleanup, resolveResult := l.resolveImportExcelFile(req.UploadID, fileTransferLogic)
	if cleanup != nil {
		defer cleanup()
	}
	if resolveResult != nil {
		return resolveResult
	}
	if err := validateSysConfigImportFile(l.Ctx, importFilePath); err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) ||
			errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return sysConfigInfrastructureResult(err,
				"SysConfigLogic.PrepareImportBackup 读取导入文件失败")
		}
		return types.ParamErrorResult(err).
			WithError(errors.Wrap(err, "SysConfigLogic.PrepareImportBackup 预检导入文件失败"))
	}
	importFileHash, err := fileSHA256(importFilePath)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"SysConfigLogic.PrepareImportBackup 计算导入文件摘要失败").ToBizResult()
	}

	state, err := l.createImportBackup(l.Ctx, req.UploadID, importFileHash)
	if err != nil {
		return sysConfigInfrastructureResult(err,
			"SysConfigLogic.PrepareImportBackup 生成导入前备份失败")
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeySuccess).
		WithData(types.SysConfigExcelBackupResp{
			BackupID:  state.BackupID,
			FileName:  state.FileName,
			ExpiresAt: state.ExpiresAt.Format(time.RFC3339),
		})
}

// OpenImportBackup 校验备份归属并打开私有对象下载流。
func (l *SysConfigLogic) OpenImportBackup(req *types.SysConfigExcelBackupDownloadReq, rangeHeader string) (*storage.OpenObjectResult, string, *types.BizResult) {
	if req == nil {
		return nil, "", types.ParamErrorResult(errors.Errorf("备份下载请求不能为空"))
	}
	if err := req.Validate(); err != nil {
		return nil, "", types.ParamErrorResult(err)
	}
	state, err := l.loadImportBackup(req.BackupID)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, "", types.NotFound(i18n.MsgKeyNotFound, err,
				"SysConfigLogic.OpenImportBackup 字典备份[%s]不存在", req.BackupID).ToBizResult()
		}
		return nil, "", sysConfigInfrastructureResult(err,
			fmt.Sprintf("SysConfigLogic.OpenImportBackup 读取字典备份[%s]失败", req.BackupID))
	}
	if err := l.validateImportBackupOwner(state); err != nil {
		return nil, "", types.Forbidden(i18n.MsgKeyForbidden).ToBizResult().
			WithError(errors.Wrap(err, "SysConfigLogic.OpenImportBackup 无权下载字典备份"))
	}
	objectStorage, err := l.Svc.ObjectStorage()
	if err != nil {
		return nil, "", types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"SysConfigLogic.OpenImportBackup 获取对象存储失败").ToBizResult()
	}
	objectStream, err := objectStorage.Open(l.Ctx, storage.OpenObjectReq{
		ObjectKey:   state.ObjectKey,
		RangeHeader: rangeHeader,
	})
	if err != nil {
		return nil, "", types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"SysConfigLogic.OpenImportBackup 打开字典备份[%s]失败", req.BackupID).ToBizResult()
	}
	return objectStream, state.FileName, nil
}

// MarkImportBackupDownloaded 标记完整备份响应已经输出，正式导入必须经过该状态。
func (l *SysConfigLogic) MarkImportBackupDownloaded(backupID string) error {
	state, err := l.loadImportBackup(backupID)
	if err != nil {
		return errors.Tag(err)
	}
	if err := l.validateImportBackupOwner(state); err != nil {
		return errors.Tag(err)
	}
	state.DownloadedAt = time.Now()
	return l.saveImportBackup(state, time.Until(state.ExpiresAt))
}

// CleanupImportBackup 删除到期的字典备份对象和 Redis 状态。
func (l *SysConfigLogic) CleanupImportBackup(payload *types.SysConfigExcelBackupCleanupTaskPayload) error {
	if payload == nil || strings.TrimSpace(payload.BackupID) == "" || strings.TrimSpace(payload.ObjectKey) == "" {
		return errors.Errorf("字典备份清理任务参数不完整")
	}
	if l == nil || l.Svc == nil {
		return errors.Errorf("字典配置服务未初始化")
	}
	objectStorage, err := l.Svc.ObjectStorage()
	if err != nil {
		return errors.Tag(err)
	}
	objectKey, err := objectStorage.ResolveObjectKey(payload.ObjectKey)
	if err != nil {
		return errors.Wrap(err, "解析字典备份对象 key 失败")
	}
	pathPrefix := ""
	if objectStorage.Type() == storage.TypeS3 {
		pathPrefix = l.Svc.CurrentConfig().FileStorage.S3.PathPrefix
	}
	expectedPrefix := storage.ObjectKeyPrefix(pathPrefix, sysConfigExcelBackupBizType)
	if !isSysConfigBackupObjectKey(objectKey, expectedPrefix, payload.BackupID) {
		return errors.Errorf("字典备份清理对象与 backupId 不匹配")
	}
	if err := objectStorage.Delete(l.Ctx, objectKey); err != nil {
		return errors.Wrap(err, "删除字典备份对象失败")
	}
	if l.Redis() == nil {
		return cachelogic.WrapRedisUnavailable(nil, "清理字典备份状态失败")
	}
	backupKey := l.importBackupKey(payload.BackupID)
	usedKey := l.importBackupUsedKey(payload.BackupID)
	if backupKey == "" || usedKey == "" {
		return errors.Errorf("字典备份缓存 key 为空")
	}
	if err := l.Redis().Del(l.Ctx, backupKey).Err(); err != nil {
		return cachelogic.WrapRedisUnavailable(err, "删除字典备份状态失败")
	}
	if err := l.Redis().Del(l.Ctx, usedKey).Err(); err != nil {
		return cachelogic.WrapRedisUnavailable(err, "删除字典备份消费状态失败")
	}
	return nil
}

// withSysConfigMutationLock 串行化字典新增、编辑和导入，保证正式导入的快照校验与写入闭环。
func (l *SysConfigLogic) withSysConfigMutationLock(ctx context.Context, fn func(context.Context) error) error {
	if l == nil || l.Redis() == nil {
		return cachelogic.WrapRedisUnavailable(nil, "获取字典配置写入锁失败")
	}
	lockKey := l.AppRedisKey(keys.SysConfigMutationLock)
	if lockKey == "" {
		return errors.Errorf("字典配置写入锁 key 为空")
	}
	executed := false
	err := redislock.WithLock(ctx, l.Redis(), lockKey, sysConfigLockTTL, func(lockCtx context.Context) error {
		executed = true
		if fn == nil {
			return nil
		}
		return fn(lockCtx)
	})
	if err != nil && (errors.Is(err, redislock.ErrLockLost) || (!executed && !redislock.IsLockTaken(err))) {
		return cachelogic.WrapRedisUnavailable(err, "字典配置写入锁 Redis 操作失败")
	}
	return errors.Tag(err)
}

// validateImportBackupForImport 校验已读取的备份可用于当前管理员和当前上传文件。
func (l *SysConfigLogic) validateImportBackupForImport(req *types.SysConfigExcelImportReq, state *sysConfigImportBackupState) error {
	if req == nil {
		return errors.Errorf("导入请求不能为空")
	}
	if err := l.validateImportBackupOwner(state); err != nil {
		return errors.Tag(err)
	}
	if state.UploadID != strings.TrimSpace(req.UploadID) {
		return errors.Errorf("导入前备份与当前上传文件不匹配")
	}
	if state.DownloadedAt.IsZero() {
		return errors.Errorf("请先完整下载导入前备份")
	}
	return nil
}

// claimImportBackup 原子占用备份，避免重复请求执行两次导入。
func (l *SysConfigLogic) claimImportBackup(state *sysConfigImportBackupState) error {
	if state == nil {
		return errors.Errorf("字典备份消费状态不可用")
	}
	if l == nil || l.Redis() == nil {
		return cachelogic.WrapRedisUnavailable(nil, "占用字典导入备份失败")
	}
	ttl := time.Until(state.ExpiresAt)
	if ttl <= 0 {
		return errors.Errorf("导入前备份已过期")
	}
	usedKey := l.importBackupUsedKey(state.BackupID)
	if usedKey == "" {
		return errors.Errorf("字典备份消费 key 为空")
	}
	ok, err := l.Redis().SetNX(l.Ctx, usedKey, time.Now().Unix(), ttl).Result()
	if err != nil {
		return cachelogic.WrapRedisUnavailable(err, "占用字典导入备份失败")
	}
	if !ok {
		return errors.Errorf("导入前备份已使用，请重新选择文件并生成备份")
	}
	return nil
}

// releaseImportBackupClaim 在导入事务明确回滚后释放占用，允许使用同一备份重试。
func (l *SysConfigLogic) releaseImportBackupClaim(backupID string) error {
	if strings.TrimSpace(backupID) == "" {
		return errors.Errorf("backupId 不能为空")
	}
	if l == nil || l.Redis() == nil {
		return cachelogic.WrapRedisUnavailable(nil, "释放字典备份消费标记失败")
	}
	usedKey := l.importBackupUsedKey(backupID)
	if usedKey == "" {
		return errors.Errorf("字典备份消费 key 为空")
	}
	if err := l.Redis().Del(l.Ctx, usedKey).Err(); err != nil {
		return cachelogic.WrapRedisUnavailable(err, "释放字典备份消费标记失败")
	}
	return nil
}

// currentSysConfigSnapshot 计算当前主库字典数据摘要。
func (l *SysConfigLogic) currentSysConfigSnapshot(ctx context.Context, db *gorm.DB) (string, int64, error) {
	if db == nil {
		return "", 0, errors.Errorf("字典配置数据库未初始化")
	}
	var total int64
	if err := db.WithContext(ctx).Model(&model.SysConfig{}).Count(&total).Error; err != nil {
		return "", 0, errors.Wrap(err, "统计字典配置数量失败")
	}
	if total > sysConfigExcelImportMaxRows {
		return "", 0, errors.Errorf("字典配置数量超过备份上限 %d", sysConfigExcelImportMaxRows)
	}
	digest := sha256.New()
	cursor := 0
	for {
		items, err := querySysConfigBatch(ctx, db, cursor, sysConfigExcelBatchSize)
		if err != nil {
			return "", 0, errors.Tag(err)
		}
		if len(items) == 0 {
			break
		}
		if err := writeSysConfigSnapshot(digest, items); err != nil {
			return "", 0, errors.Tag(err)
		}
		cursor = items[len(items)-1].ID
		if len(items) < sysConfigExcelBatchSize {
			break
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), total, nil
}

// createImportBackup 生成 Excel、持久化私有对象并保存可校验状态。
func (l *SysConfigLogic) createImportBackup(ctx context.Context, uploadID string, importFileHash string) (*sysConfigImportBackupState, error) {
	backupID := uuid.NewString()
	now := time.Now()
	fileName := fmt.Sprintf("sys_config_backup_%s.xlsx", now.Format("20060102150405"))
	filePath := filepath.Join(os.TempDir(), "admin", "exports", "sys-config-backups", backupID+".xlsx")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return nil, errors.Wrap(err, "创建字典备份临时目录失败")
	}
	defer os.Remove(filePath)

	var total int64
	digest := sha256.New()
	db := l.Svc.WriteDB(svc.DatabaseMain).WithContext(ctx)
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.SysConfig{}).Count(&total).Error; err != nil {
			return errors.Wrap(err, "统计字典备份数量失败")
		}
		if total > sysConfigExcelImportMaxRows {
			return errors.Errorf("字典配置数量超过备份上限 %d", sysConfigExcelImportMaxRows)
		}
		return pkgexcel.StreamExport(ctx, pkgexcel.StreamExportOptions[model.SysConfig, int]{
			FilePath:      filePath,
			SheetName:     sysConfigExcelSheetName,
			Header:        sysConfigExcelHeaders,
			BatchSize:     sysConfigExcelBatchSize,
			InitialCursor: 0,
			Query: func(ctx context.Context, cursor int, limit int) (*pkgexcel.CursorPage[model.SysConfig, int], error) {
				items, err := querySysConfigBatch(ctx, tx, cursor, limit)
				if err != nil {
					return nil, errors.Tag(err)
				}
				nextCursor := cursor
				if len(items) > 0 {
					nextCursor = items[len(items)-1].ID
				}
				return &pkgexcel.CursorPage[model.SysConfig, int]{
					Total:      total,
					Items:      items,
					NextCursor: nextCursor,
					HasMore:    len(items) >= limit,
				}, nil
			},
			BuildRows: func(items []model.SysConfig) ([][]any, error) {
				if err := writeSysConfigSnapshot(digest, items); err != nil {
					return nil, errors.Tag(err)
				}
				return buildSysConfigExportRows(items)
			},
		})
	}, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	}); err != nil {
		return nil, errors.Wrap(err, "生成字典备份 Excel 失败")
	}

	objectStorage, err := l.Svc.ObjectStorage()
	if err != nil {
		return nil, errors.Tag(err)
	}
	storedObject, err := objectStorage.SaveLocalFile(ctx, storage.SaveLocalFileReq{
		BizType:          sysConfigExcelBackupBizType,
		ContentType:      sysConfigExcelContentType,
		LocalPath:        filePath,
		OriginalFileName: fileName,
		StoredFileName:   storage.BuildStoredFileName(strings.ReplaceAll(backupID, "-", ""), fileName),
		Visibility:       storage.VisibilityPrivate,
	})
	if err != nil {
		return nil, errors.Wrap(err, "持久化字典备份失败")
	}
	state := &sysConfigImportBackupState{
		BackupID:       backupID,
		UploadID:       strings.TrimSpace(uploadID),
		OperatorID:     l.GetCtxAdmin().ID,
		ImportFileHash: strings.TrimSpace(importFileHash),
		FileName:       fileName,
		ObjectKey:      storedObject.ObjectKey,
		SnapshotHash:   hex.EncodeToString(digest.Sum(nil)),
		RowCount:       total,
		ExpiresAt:      now.Add(sysConfigExcelBackupRetention),
	}
	if err := l.scheduleImportBackupCleanup(state); err != nil {
		if cleanupErr := objectStorage.Delete(ctx, storedObject.ObjectKey); cleanupErr != nil {
			err = errors.Join(err, errors.Wrap(cleanupErr, "回收未登记字典备份对象失败"))
		}
		return nil, errors.Tag(err)
	}
	if err := l.saveImportBackup(state, sysConfigExcelBackupRetention); err != nil {
		if cleanupErr := objectStorage.Delete(ctx, storedObject.ObjectKey); cleanupErr != nil {
			err = errors.Join(err, errors.Wrap(cleanupErr, "回收未保存状态的字典备份对象失败"))
		}
		return nil, errors.Tag(err)
	}
	return state, nil
}

// fileSHA256 计算文件内容摘要，确保最终导入文件与生成备份时完全一致。
func fileSHA256(filePath string) (string, error) {
	file, err := os.Open(strings.TrimSpace(filePath))
	if err != nil {
		return "", errors.Wrap(err, "打开待导入文件失败")
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", errors.Wrap(err, "读取待导入文件失败")
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// validateSysConfigImportFile 在生成备份前校验 Excel 结构和值格式，错误文件不进入备份和写库阶段。
func validateSysConfigImportFile(ctx context.Context, filePath string) error {
	seenUUIDs := make(map[string]struct{})
	return pkgexcel.StreamImport(ctx, pkgexcel.StreamImportOptions{
		FilePath:  filePath,
		SheetName: sysConfigExcelSheetName,
		HeaderRow: pkgexcel.DefaultHeaderRowIndex,
		StartRow:  pkgexcel.DefaultDataStartRowIndex,
		MaxRows:   sysConfigExcelImportMaxRows,
		TrimSpace: true,
		OnHeader:  validateSysConfigImportHeaders,
		OnRow: func(rowIndex int, values []string) error {
			row, err := parseSysConfigImportRow(values)
			if err != nil {
				return errors.Wrapf(err, "解析第[%d]行字典配置失败", rowIndex)
			}
			if row.UUID == "" {
				return nil
			}
			if err := row.Validate(); err != nil {
				return errors.Wrapf(err, "校验第[%d]行基础字段失败", rowIndex)
			}
			normalizedUUID := strings.ToLower(row.UUID)
			if _, exists := seenUUIDs[normalizedUUID]; exists {
				return errors.Errorf("第[%d]行配置UUID[%s]重复", rowIndex, row.UUID)
			}
			seenUUIDs[normalizedUUID] = struct{}{}
			valueRaw, err := row.ValueRawMessage()
			if err != nil {
				return errors.Wrapf(err, "校验第[%d]行配置值失败", rowIndex)
			}
			exampleRaw, err := row.ExampleRawMessage()
			if err != nil {
				return errors.Wrapf(err, "校验第[%d]行示例值失败", rowIndex)
			}
			if _, _, err := normalizeSysConfigJSON(row.Type, valueRaw, exampleRaw); err != nil {
				return errors.Wrapf(err, "校验第[%d]行字典配置失败", rowIndex)
			}
			return nil
		},
	})
}

// querySysConfigBatch 按主键游标读取一批完整字典数据。
func querySysConfigBatch(ctx context.Context, db *gorm.DB, cursor int, limit int) ([]model.SysConfig, error) {
	if limit <= 0 {
		limit = sysConfigExcelBatchSize
	}
	dbq := db.WithContext(ctx).Model(&model.SysConfig{}).Order("id ASC").Limit(limit)
	if cursor > 0 {
		dbq = dbq.Where("id > ?", cursor)
	}
	items := make([]model.SysConfig, 0, limit)
	if err := dbq.Find(&items).Error; err != nil {
		return nil, errors.Wrap(err, "查询字典配置备份批次失败")
	}
	return items, nil
}

// writeSysConfigSnapshot 把完整字典行按稳定顺序写入摘要。
func writeSysConfigSnapshot(digest hash.Hash, items []model.SysConfig) error {
	for _, item := range items {
		body, err := json.Marshal(struct {
			ID        int    `json:"id"`        // ID 表示配置主键。
			UUID      string `json:"uuid"`      // UUID 表示配置唯一标识。
			Title     string `json:"title"`     // Title 表示配置标题。
			Type      int    `json:"type"`      // Type 表示配置类型。
			Value     string `json:"value"`     // Value 表示配置值。
			Example   string `json:"example"`   // Example 表示配置示例。
			Remark    string `json:"remark"`    // Remark 表示备注。
			Page      string `json:"page"`      // Page 表示页面路径。
			Pid       int    `json:"pid"`       // Pid 表示上级配置 ID。
			Pids      string `json:"pids"`      // Pids 表示上级族谱。
			Version   int    `json:"version"`   // Version 表示乐观锁版本。
			CreatedAt int64  `json:"createdAt"` // CreatedAt 表示创建时间戳。
			UpdatedAt int64  `json:"updatedAt"` // UpdatedAt 表示更新时间戳。
		}{
			ID:        item.ID,
			UUID:      item.UUID,
			Title:     item.Title,
			Type:      item.Type,
			Value:     item.Value,
			Example:   item.Example,
			Remark:    item.Remark,
			Page:      item.Page,
			Pid:       item.Pid,
			Pids:      item.Pids,
			Version:   item.Version,
			CreatedAt: item.CreatedAt.UnixNano(),
			UpdatedAt: item.UpdatedAt.UnixNano(),
		})
		if err != nil {
			return errors.Wrap(err, "序列化字典快照失败")
		}
		if _, err := digest.Write(append(body, '\n')); err != nil {
			return errors.Wrap(err, "计算字典快照摘要失败")
		}
	}
	return nil
}

// scheduleImportBackupCleanup 投递备份 30 天后的对象清理任务。
func (l *SysConfigLogic) scheduleImportBackupCleanup(state *sysConfigImportBackupState) error {
	if state == nil || l.Svc == nil || l.Svc.Task == nil || !l.Svc.Task.IsEnabled() {
		return errSysConfigBackupTaskUnavailable
	}
	body, err := json.Marshal(types.SysConfigExcelBackupCleanupTaskPayload{
		BackupID:  state.BackupID,
		ObjectKey: state.ObjectKey,
	})
	if err != nil {
		return errors.Wrap(err, "序列化字典备份清理任务失败")
	}
	if err := l.Svc.Task.EnqueueTask(l.Ctx, types.SysConfigExcelBackupCleanupTaskType, body,
		svc.WithTaskQueue(taskqueue.QueueMaintenance),
		svc.WithTaskRetry(5),
		svc.WithTaskTimeout(2*time.Minute),
		svc.WithTaskDelay(sysConfigExcelBackupRetention),
	); err != nil {
		return errors.Join(errSysConfigBackupTaskUnavailable, errors.Wrap(err, "投递字典备份清理任务失败"))
	}
	return nil
}

// ensureSysConfigBackupDependencies 校验备份依赖的 Redis、对象存储和任务系统。
func (l *SysConfigLogic) ensureSysConfigBackupDependencies() error {
	if l == nil || l.Svc == nil {
		return errors.Errorf("字典配置服务未初始化")
	}
	if l.Redis() == nil {
		return cachelogic.WrapRedisUnavailable(nil, "字典导入备份依赖检查失败")
	}
	if l.GetCtxAdmin().ID <= 0 {
		return errors.Errorf("管理员上下文未初始化")
	}
	if l.Svc.Task == nil || !l.Svc.Task.IsEnabled() {
		return errSysConfigBackupTaskUnavailable
	}
	_, err := l.Svc.ObjectStorage()
	return errors.Tag(err)
}

// saveImportBackup 保存字典备份状态并设置明确 TTL。
func (l *SysConfigLogic) saveImportBackup(state *sysConfigImportBackupState, ttl time.Duration) error {
	if state == nil {
		return errors.Errorf("字典备份状态不可用")
	}
	if l == nil || l.Redis() == nil {
		return cachelogic.WrapRedisUnavailable(nil, "保存字典备份状态失败")
	}
	if ttl <= 0 {
		return errors.Errorf("字典备份已过期")
	}
	body, err := json.Marshal(state)
	if err != nil {
		return errors.Wrap(err, "序列化字典备份状态失败")
	}
	backupKey := l.importBackupKey(state.BackupID)
	if backupKey == "" {
		return errors.Errorf("字典备份缓存 key 为空")
	}
	if err := l.Redis().Set(l.Ctx, backupKey, body, ttl).Err(); err != nil {
		return cachelogic.WrapRedisUnavailable(err, "保存字典备份状态失败")
	}
	return nil
}

// loadImportBackup 读取字典备份状态。
func (l *SysConfigLogic) loadImportBackup(backupID string) (*sysConfigImportBackupState, error) {
	if l == nil || l.Redis() == nil {
		return nil, cachelogic.WrapRedisUnavailable(nil, "读取字典备份状态失败")
	}
	backupKey := l.importBackupKey(backupID)
	if backupKey == "" {
		return nil, errors.Errorf("字典备份缓存 key 为空")
	}
	body, err := l.Redis().Get(l.Ctx, backupKey).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, redis.Nil
		}
		return nil, cachelogic.WrapRedisUnavailable(err, "读取字典备份状态失败")
	}
	state := &sysConfigImportBackupState{}
	if err := json.Unmarshal(body, state); err != nil {
		return nil, errors.Wrap(err, "解析字典备份状态失败")
	}
	if time.Now().After(state.ExpiresAt) {
		return nil, redis.Nil
	}
	return state, nil
}

// validateImportBackupOwner 校验备份归属当前管理员。
func (l *SysConfigLogic) validateImportBackupOwner(state *sysConfigImportBackupState) error {
	if state == nil {
		return errors.Errorf("字典备份不存在")
	}
	adminID := l.GetCtxAdmin().ID
	if adminID <= 0 || state.OperatorID != adminID {
		return errors.Errorf("无权访问其他管理员的字典备份")
	}
	return nil
}

// importBackupKey 返回带 app_id 命名空间的字典备份状态 key。
func (l *SysConfigLogic) importBackupKey(backupID string) string {
	return l.AppRedisKey(fmt.Sprintf(keys.SysConfigImportBackup, strings.TrimSpace(backupID)))
}

// importBackupUsedKey 返回带 app_id 命名空间的字典备份消费标记 key。
func (l *SysConfigLogic) importBackupUsedKey(backupID string) string {
	return l.AppRedisKey(fmt.Sprintf(keys.SysConfigImportBackupUsed, strings.TrimSpace(backupID)))
}

// isSysConfigBackupObjectKey 校验清理对象位于固定备份目录且文件名由 backupId 唯一派生。
func isSysConfigBackupObjectKey(objectKey string, expectedPrefix string, backupID string) bool {
	parsedBackupID, err := uuid.Parse(strings.TrimSpace(backupID))
	if err != nil {
		return false
	}
	objectKey = strings.Trim(strings.TrimSpace(objectKey), "/")
	expectedPrefix = strings.Trim(strings.TrimSpace(expectedPrefix), "/")
	if objectKey == "" || expectedPrefix == "" || !strings.HasPrefix(objectKey, expectedPrefix+"/") {
		return false
	}
	expectedFileName := strings.ReplaceAll(parsedBackupID.String(), "-", "") + ".xlsx"
	return filepath.Base(objectKey) == expectedFileName
}
