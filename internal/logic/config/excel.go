package config

import (
	corelogic "admin/internal/logic"
	"admin/internal/svc"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	keys "admin/common/rediskeys"
	redislock "admin/internal/infra/redsync"
	cachelogic "admin/internal/logic/cache"
	filelogic "admin/internal/logic/file"
	"admin/internal/model"
	"admin/internal/types"
	pkgexcel "admin/pkg/excel"
	"admin/pkg/transfer"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// sysConfigExcelSheetName 表示字典 Excel 导入导出的工作表名称。
	sysConfigExcelSheetName = "字典配置"
	// sysConfigExcelBatchSize 表示字典 Excel 导出游标批次大小。
	sysConfigExcelBatchSize = 200
	// sysConfigExcelImportMaxRows 表示字典 Excel 单次最大导入行数。
	sysConfigExcelImportMaxRows = 5000
	// sysConfigLockTTL 表示字典导出和写入业务锁的保留时间。
	sysConfigLockTTL = 2 * time.Minute
)

// sysConfigTxOutcome 表示字典写事务的最终可确认状态。
type sysConfigTxOutcome uint8

const (
	// sysConfigTxNotStarted 表示事务尚未成功开启。
	sysConfigTxNotStarted sysConfigTxOutcome = iota
	// sysConfigTxCommitted 表示数据库已确认提交。
	sysConfigTxCommitted
	// sysConfigTxRolledBack 表示数据库已确认回滚。
	sysConfigTxRolledBack
	// sysConfigTxUncertain 表示提交或回滚结果无法确认。
	sysConfigTxUncertain
)

// sysConfigExcelHeaders 定义系统配置 Excel 导入导出的固定列顺序。
var sysConfigExcelHeaders = []any{
	"配置ID",
	"配置UUID",
	"配置标题",
	"配置类型",
	"配置值",
	"示例值",
	"页面路径",
	"上级ID",
	"备注",
}

// ExportExcel 导出字典配置 Excel 文件并返回本地结果路径。
func (l *SysConfigLogic) ExportExcel(req *types.SysConfigExcelExportReq) (string, string, *types.BizResult) {
	if req == nil {
		req = &types.SysConfigExcelExportReq{}
	}
	lockKey := l.AppRedisKey(fmt.Sprintf(keys.SysConfigExcelExportLock, buildSysConfigExcelFingerprint(req)))
	var exportPath string
	var fileName string
	err := redislock.WithLock(l.Ctx, l.Redis(), lockKey, sysConfigLockTTL, func(ctx context.Context) error {
		now := time.Now()
		fileName = fmt.Sprintf("sys_config_%s.xlsx", now.Format("20060102150405"))
		exportPath = filepath.Join(os.TempDir(), "admin", "exports", "sys-config", uuid.NewString()+".xlsx")
		if err := os.MkdirAll(filepath.Dir(exportPath), 0o755); err != nil {
			return errors.Wrap(err, "创建字典导出目录失败")
		}
		return pkgexcel.StreamExport(ctx, pkgexcel.StreamExportOptions[model.SysConfig, int]{
			FilePath:      exportPath,
			SheetName:     sysConfigExcelSheetName,
			Header:        sysConfigExcelHeaders,
			BatchSize:     sysConfigExcelBatchSize,
			InitialCursor: 0,
			Query: func(ctx context.Context, cursor int, limit int) (*pkgexcel.CursorPage[model.SysConfig, int], error) {
				return l.querySysConfigExportPage(ctx, req, cursor, limit)
			},
			BuildRows: buildSysConfigExportRows,
		})
	})
	if err != nil {
		if exportPath != "" {
			_ = os.Remove(exportPath)
		}
		return "", "", sysConfigInfrastructureResult(err,
			"SysConfigLogic.ExportExcel 导出字典配置失败")
	}
	return exportPath, fileName, nil
}

// ImportExcel 从已上传的 Excel 文件导入字典配置。
func (l *SysConfigLogic) ImportExcel(req *types.SysConfigExcelImportReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(errors.Errorf("导入请求不能为空"))
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	backupState, err := l.loadImportBackup(req.BackupID)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			err = errors.Errorf("导入前备份不存在或已过期")
			return types.ParamErrorResult(err).
				WithError(errors.Wrap(err, "SysConfigLogic.ImportExcel 导入前备份已失效"))
		}
		return sysConfigInfrastructureResult(err,
			"SysConfigLogic.ImportExcel 读取导入前备份失败")
	}
	if err := l.validateImportBackupForImport(req, backupState); err != nil {
		return types.ParamErrorResult(err).
			WithError(errors.Wrap(err, "SysConfigLogic.ImportExcel 导入前备份校验失败"))
	}

	fileTransferLogic := filelogic.NewFileTransferLogicWithContext(l.Ctx, l.Svc)
	importFilePath, importSession, cleanup, resolveResult := l.resolveImportExcelFile(req.UploadID, fileTransferLogic)
	if cleanup != nil {
		defer cleanup()
	}
	if resolveResult != nil {
		return resolveResult
	}
	importFileHash, err := fileSHA256(importFilePath)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"SysConfigLogic.ImportExcel 计算导入文件摘要失败").ToBizResult()
	}
	if backupState.ImportFileHash != importFileHash {
		err := errors.Errorf("待导入文件与生成备份时的文件不一致，请重新选择文件")
		return types.ParamErrorResult(err).
			WithError(errors.Wrap(err, "SysConfigLogic.ImportExcel 导入文件摘要不匹配"))
	}

	var result *types.SysConfigExcelImportResp
	var backupErr error
	claimed := false
	var txOutcome sysConfigTxOutcome
	changedUUIDs := map[string]struct{}{}
	err = l.withSysConfigMutationLock(l.Ctx, func(ctx context.Context) error {
		writeDB := l.Svc.WriteDB(svc.DatabaseMain)
		if writeDB == nil {
			return errors.Errorf("字典配置主库未初始化")
		}
		var txErr error
		txOutcome, txErr = runSysConfigTransaction(writeDB.WithContext(ctx), func(tx *gorm.DB) error {
			state, err := l.loadImportBackup(req.BackupID)
			if err != nil {
				if errors.Is(err, redis.Nil) {
					backupErr = errors.Errorf("导入前备份不存在或已过期")
					return errors.Tag(backupErr)
				}
				return errors.Tag(err)
			}
			if err := l.validateImportBackupForImport(req, state); err != nil {
				backupErr = err
				return errors.Tag(err)
			}
			if state.ImportFileHash != importFileHash {
				backupErr = errors.Errorf("待导入文件与生成备份时的文件不一致，请重新选择文件")
				return errors.Tag(backupErr)
			}
			snapshotHash, rowCount, err := l.currentSysConfigSnapshot(ctx, tx)
			if err != nil {
				return errors.Tag(err)
			}
			if state.SnapshotHash != snapshotHash || state.RowCount != rowCount {
				backupErr = errors.Errorf("字典数据在备份后已发生变化，请重新生成并下载备份")
				return errors.Tag(backupErr)
			}
			if err := l.claimImportBackup(state); err != nil {
				backupErr = err
				return errors.Tag(err)
			}
			claimed = true

			summary := &types.SysConfigExcelImportResp{}
			streamErr := pkgexcel.StreamImport(ctx, pkgexcel.StreamImportOptions{
				FilePath:  importFilePath,
				SheetName: sysConfigExcelSheetName,
				HeaderRow: pkgexcel.DefaultHeaderRowIndex,
				StartRow:  pkgexcel.DefaultDataStartRowIndex,
				MaxRows:   sysConfigExcelImportMaxRows,
				TrimSpace: true,
				OnHeader:  validateSysConfigImportHeaders,
				OnRow: func(rowIndex int, values []string) error {
					return l.importSysConfigRowTx(tx, rowIndex, values, summary, changedUUIDs)
				},
			})
			if streamErr != nil {
				return errors.Wrap(streamErr, "导入系统配置 Excel 流失败")
			}
			result = summary
			return nil
		})
		return errors.Tag(txErr)
	})
	if err != nil {
		if claimed && txOutcome == sysConfigTxRolledBack {
			if releaseErr := l.releaseImportBackupClaim(req.BackupID); releaseErr != nil {
				err = errors.Join(err, releaseErr)
			}
		}
		if txOutcome == sysConfigTxUncertain {
			if cacheErr := l.invalidateSysConfigCaches(changedUUIDs); cacheErr != nil {
				err = errors.Join(err, cacheErr)
			}
			return types.DBError(i18n.MsgKeyDBError, err,
				"SysConfigLogic.ImportExcel 字典导入事务结果不确定，已保留备份消费标记").ToBizResult()
		}
		if txOutcome == sysConfigTxCommitted {
			// 数据库已经提交时，锁释放异常只能记录告警，不能把成功导入误报为失败或释放一次性消费标记。
			corelogic.LogWrappedError(l.Logger, err,
				"SysConfigLogic.ImportExcel 导入已提交但字典写入锁释放异常 backup_id=%s", req.BackupID)
		} else {
			if errors.Is(err, cachelogic.ErrRedisUnavailable) {
				return sysConfigInfrastructureResult(err,
					"SysConfigLogic.ImportExcel 字典导入 Redis 依赖不可用")
			}
			if backupErr != nil {
				return types.ParamErrorResult(backupErr).
					WithError(errors.Wrap(backupErr, "SysConfigLogic.ImportExcel 导入前备份状态失效"))
			}
			if inputResult := sysConfigInputResult(err,
				"SysConfigLogic.ImportExcel 导入字典配置数据校验失败"); inputResult != nil {
				return inputResult
			}
			return sysConfigInfrastructureResult(err,
				"SysConfigLogic.ImportExcel 导入字典配置失败")
		}
	}
	if cleanupErr := fileTransferLogic.ConsumeImportedObject(importSession); cleanupErr != nil {
		// 数据库导入已经提交，删除失败交给上传时预投递的延迟任务重试，不能把成功导入误报为失败。
		corelogic.LogWrappedError(l.Logger, cleanupErr,
			"SysConfigLogic.ImportExcel 清理已消费上传会话和对象失败 upload_id=%s", importSession.UploadID)
	}
	var cacheErr error
	for uuid := range changedUUIDs {
		if err := l.RenewByUUID(uuid); err != nil && cacheErr == nil {
			cacheErr = errors.Wrapf(err, "刷新配置UUID[%s]缓存失败", uuid)
		}
	}
	if cacheErr != nil {
		result.SyncPending = true
		return corelogic.CacheSyncPendingResult(l.Logger, codes.UpdateSuccess, i18n.MsgKeyCacheSyncPending, cacheErr,
			"SysConfigLogic.ImportExcel 批量配置缓存同步失败").WithData(result)
	}
	return types.NewBizResult(codes.UpdateSuccess).
		SetI18nMessage(i18n.MsgKeyUpdateSuccess).
		WithData(result)
}

// resolveImportExcelFile 解析导入 Excel 文件，并按参数、权限和依赖故障返回准确业务结果。
func (l *SysConfigLogic) resolveImportExcelFile(uploadID string, fileTransferLogic *filelogic.FileTransferLogic) (string, *transfer.UploadSession, func(), *types.BizResult) {
	uploadID = strings.TrimSpace(uploadID)
	if uploadID == "" {
		err := errors.Errorf("uploadId 不能为空")
		return "", nil, nil, types.ParamErrorResult(err)
	}
	if fileTransferLogic == nil {
		err := errors.Errorf("文件传输服务未初始化")
		return "", nil, nil, sysConfigInfrastructureResult(err,
			"SysConfigLogic.resolveImportExcelFile 文件传输服务不可用")
	}
	session, err := fileTransferLogic.GetSession(uploadID)
	if err != nil {
		if errors.Is(err, transfer.ErrUploadSessionNotFound) {
			return "", nil, nil, types.ParamErrorResult(err).
				WithError(errors.Wrapf(err, "SysConfigLogic.resolveImportExcelFile 导入文件会话[%s]不存在", uploadID))
		}
		if errors.Is(err, transfer.ErrUploadSessionStoreUnavailable) {
			err = cachelogic.WrapRedisUnavailable(err, "读取导入文件上传会话失败")
		}
		return "", nil, nil, sysConfigInfrastructureResult(err,
			"SysConfigLogic.resolveImportExcelFile 读取导入文件会话失败")
	}
	if err := fileTransferLogic.EnsureSessionOwner(session); err != nil {
		return "", nil, nil, types.Forbidden(i18n.MsgKeyForbidden).ToBizResult().
			WithError(errors.Wrap(err, "SysConfigLogic.resolveImportExcelFile 导入文件归属校验失败"))
	}
	if !fileTransferLogic.IsCompletedSession(session) {
		err := errors.Errorf("导入文件尚未上传完成")
		return "", nil, nil, types.ParamErrorResult(err).
			WithError(errors.Wrap(err, "SysConfigLogic.resolveImportExcelFile 导入文件状态无效"))
	}
	if strings.TrimSpace(session.BizType) != filelogic.FileTransferBizSysConfigExcelImport {
		err := errors.Errorf("导入文件业务类型不合法")
		return "", nil, nil, types.ParamErrorResult(err).
			WithError(errors.Wrap(err, "SysConfigLogic.resolveImportExcelFile 导入文件业务类型无效"))
	}
	filePath, cleanup, err := fileTransferLogic.MaterializeSessionObject(session)
	if err != nil {
		return "", nil, cleanup, sysConfigInfrastructureResult(err,
			"SysConfigLogic.resolveImportExcelFile 读取导入文件对象失败")
	}
	return filePath, session, cleanup, nil
}

// sysConfigInfrastructureResult 把锁竞争和依赖故障映射为对应 503，其余服务故障映射为内部错误。
func sysConfigInfrastructureResult(err error, operation string) *types.BizResult {
	if redislock.IsLockTaken(err) {
		return types.NewBizResult(codes.ServiceBusy).
			SetI18nMessage(i18n.MsgKeyServiceBusy).
			WithError(errors.Wrap(err, operation))
	}
	if errors.Is(err, cachelogic.ErrRedisUnavailable) {
		return types.NewBizResult(codes.RedisUnavailable).
			SetI18nMessage(i18n.MsgKeyRedisUnavailable).
			WithError(errors.Wrap(err, operation))
	}
	if errors.Is(err, errSysConfigBackupTaskUnavailable) {
		return types.NewBizResult(codes.TaskQueueUnavailable).
			SetI18nMessage(i18n.MsgKeyTaskQueueUnavailable).
			WithError(errors.Wrap(err, operation))
	}
	return types.ServerError(i18n.MsgKeyInternalErrorFormat, err, operation).ToBizResult()
}

// runSysConfigTransaction 执行字典写事务，并保留提交、回滚和结果不确定三种状态。
func runSysConfigTransaction(db *gorm.DB, work func(*gorm.DB) error) (sysConfigTxOutcome, error) {
	if db == nil {
		return sysConfigTxNotStarted, errors.Errorf("字典配置数据库未初始化")
	}
	tx := db.Begin()
	if tx.Error != nil {
		return sysConfigTxNotStarted, errors.Wrap(tx.Error, "开启字典配置事务失败")
	}
	// 兜底回滚覆盖 panic 等非正常退出；正常提交或显式回滚后的重复回滚可安全忽略。
	defer func() {
		_ = tx.Rollback().Error
	}()
	if work == nil {
		return finishSysConfigTransaction(
			errors.Errorf("字典配置事务方法未初始化"),
			func() error { return tx.Rollback().Error },
			func() error { return tx.Commit().Error },
		)
	}
	return finishSysConfigTransaction(
		work(tx),
		func() error { return tx.Rollback().Error },
		func() error { return tx.Commit().Error },
	)
}

// finishSysConfigTransaction 只在提交或回滚得到明确结果时返回对应状态。
func finishSysConfigTransaction(workErr error, rollback func() error, commit func() error) (sysConfigTxOutcome, error) {
	if workErr != nil {
		if rollback == nil {
			return sysConfigTxUncertain, errors.Join(workErr, errors.Errorf("字典配置事务回滚方法未初始化"))
		}
		if rollbackErr := rollback(); rollbackErr != nil {
			return sysConfigTxUncertain, errors.Join(workErr, errors.Wrap(rollbackErr, "回滚字典配置事务失败"))
		}
		return sysConfigTxRolledBack, errors.Tag(workErr)
	}
	if commit == nil {
		return sysConfigTxUncertain, errors.Errorf("字典配置事务提交方法未初始化")
	}
	if commitErr := commit(); commitErr != nil {
		return sysConfigTxUncertain, errors.Wrap(commitErr, "提交字典配置事务失败")
	}
	return sysConfigTxCommitted, nil
}

// invalidateSysConfigCaches 在事务结果不确定时删除已触达配置缓存，避免继续读取可能过期的数据。
func (l *SysConfigLogic) invalidateSysConfigCaches(changedUUIDs map[string]struct{}) error {
	var cacheErr error
	for uuid := range changedUUIDs {
		physicalKeys := cachelogic.TableCachePhysicalKeys(l.BaseLogic, fmt.Sprintf(keys.SysConfigUUID, uuid))
		if err := l.RdsDelKeys(physicalKeys...); err != nil {
			cacheErr = errors.Join(cacheErr, errors.Wrapf(err, "删除配置UUID[%s]缓存失败", uuid))
		}
	}
	return errors.Tag(cacheErr)
}

// querySysConfigExportPage 查询字典配置导出分页数据。
func (l *SysConfigLogic) querySysConfigExportPage(ctx context.Context, req *types.SysConfigExcelExportReq, cursor int, limit int) (*pkgexcel.CursorPage[model.SysConfig, int], error) {
	readDB := l.Svc.ReadDB(svc.DatabaseMain)
	dbq := readDB.WithContext(ctx).Model(&model.SysConfig{}).Order("id ASC")
	if cursor > 0 {
		dbq = dbq.Where("id > ?", cursor)
	}
	if req != nil {
		if req.UUID != "" {
			dbq = dbq.Where("uuid LIKE ?", "%"+strings.TrimSpace(req.UUID)+"%")
		}
		if req.Title != "" {
			dbq = dbq.Where("title LIKE ?", "%"+strings.TrimSpace(req.Title)+"%")
		}
		if req.PagePath != "" {
			dbq = dbq.Where("page LIKE ?", "%"+strings.TrimSpace(req.PagePath)+"%")
		}
	}
	var total int64
	countQ := readDB.WithContext(ctx).Model(&model.SysConfig{})
	if req != nil {
		if req.UUID != "" {
			countQ = countQ.Where("uuid LIKE ?", "%"+strings.TrimSpace(req.UUID)+"%")
		}
		if req.Title != "" {
			countQ = countQ.Where("title LIKE ?", "%"+strings.TrimSpace(req.Title)+"%")
		}
		if req.PagePath != "" {
			countQ = countQ.Where("page LIKE ?", "%"+strings.TrimSpace(req.PagePath)+"%")
		}
	}
	if err := countQ.Count(&total).Error; err != nil {
		return nil, errors.Wrap(err, "统计字典导出总数失败")
	}
	var items []model.SysConfig
	if err := dbq.Limit(limit).Find(&items).Error; err != nil {
		return nil, errors.Wrap(err, "查询字典导出数据失败")
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
}

// buildSysConfigExportRows 构建字典配置导出行数据。
func buildSysConfigExportRows(items []model.SysConfig) ([][]any, error) {
	rows := make([][]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, []any{
			item.ID,
			item.UUID,
			item.Title,
			item.Type,
			item.Value,
			item.Example,
			item.Page,
			item.Pid,
			item.Remark,
		})
	}
	return rows, nil
}

// validateSysConfigImportHeaders 验证字典配置导入表头。
func validateSysConfigImportHeaders(headers []string) error {
	expected := make([]string, 0, len(sysConfigExcelHeaders))
	for _, header := range sysConfigExcelHeaders {
		expected = append(expected, strings.TrimSpace(fmt.Sprint(header)))
	}
	if len(headers) < len(expected) {
		return errors.Errorf("导入表头数量不正确")
	}
	for index, header := range expected {
		if strings.TrimSpace(headers[index]) != header {
			return errors.Errorf("第[%d]列表头应为[%s]", index+1, header)
		}
	}
	return nil
}

// importSysConfigRowTx 导入字典配置行数据。
func (l *SysConfigLogic) importSysConfigRowTx(tx *gorm.DB, rowIndex int, values []string, summary *types.SysConfigExcelImportResp, changedUUIDs map[string]struct{}) error {
	if summary == nil {
		return errors.Errorf("导入结果对象不能为空")
	}
	row, err := parseSysConfigImportRow(values)
	if err != nil {
		return errors.Wrapf(err, "解析第[%d]行字典配置失败", rowIndex)
	}
	if row.UUID == "" {
		summary.Skipped++
		return nil
	}
	if err := row.Validate(); err != nil {
		return errors.Wrapf(err, "校验第[%d]行基础字段失败", rowIndex)
	}
	var existing model.SysConfig
	queryErr := tx.Where("uuid = ?", row.UUID).First(&existing).Error
	if queryErr != nil && !errors.Is(queryErr, gorm.ErrRecordNotFound) {
		return errors.Wrapf(queryErr, "查询字典配置UUID[%s]失败", row.UUID)
	}
	valueRaw, err := row.ValueRawMessage()
	if err != nil {
		return errors.Wrapf(err, "校验字典配置UUID[%s]失败", row.UUID)
	}
	exampleRaw, err := row.ExampleRawMessage()
	if err != nil {
		return errors.Wrapf(err, "校验字典配置UUID[%s]失败", row.UUID)
	}
	value, example, err := normalizeSysConfigJSON(row.Type, valueRaw, exampleRaw)
	if err != nil {
		return errors.Wrapf(err, "校验字典配置UUID[%s]失败", row.UUID)
	}
	if errors.Is(queryErr, gorm.ErrRecordNotFound) {
		cfg := model.SysConfig{
			UUID:      row.UUID,
			Title:     row.Title,
			Type:      row.Type,
			Value:     value,
			Example:   example,
			Remark:    row.Remark,
			Page:      row.Page,
			Pid:       row.Pid,
			Version:   0,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		pids, err := l.sysConfigPidsTx(tx, row.Pid, 0)
		if err != nil {
			return errors.Tag(err)
		}
		cfg.Pids = pids
		if err := l.ensureSysConfigUUIDUniqueTx(tx, cfg.UUID, 0); err != nil {
			return errors.Tag(err)
		}
		if changedUUIDs != nil {
			// 写入前登记缓存目标；即使数据库响应丢失，也能在事务结果不确定时安全失效。
			changedUUIDs[cfg.UUID] = struct{}{}
		}
		if err := tx.Create(&cfg).Error; err != nil {
			return errors.Wrap(err, "创建系统配置失败")
		}
		summary.Created++
		return nil
	}
	if existing.Type != row.Type {
		return errors.Tag(types.BizError(
			fmt.Sprintf("配置UUID[%s]的类型不允许从[%d]改为[%d]", row.UUID, existing.Type, row.Type),
		))
	}
	pids, err := l.sysConfigPidsTx(tx, row.Pid, existing.ID)
	if err != nil {
		return errors.Tag(err)
	}
	if changedUUIDs != nil {
		// 查询条件可能忽略大小写，旧值和导入值都登记，确保缓存失效覆盖真实物理键。
		changedUUIDs[existing.UUID] = struct{}{}
		changedUUIDs[row.UUID] = struct{}{}
	}
	if err := tx.Model(&model.SysConfig{}).Where("id = ?", existing.ID).Updates(map[string]any{
		"title":      row.Title,
		"value":      value,
		"example":    example,
		"remark":     row.Remark,
		"page":       row.Page,
		"pid":        row.Pid,
		"pids":       pids,
		"version":    gorm.Expr("version + 1"),
		"updated_at": time.Now(),
	}).Error; err != nil {
		return errors.Wrap(err, "更新系统配置失败")
	}
	summary.Updated++
	return nil
}

// parseSysConfigImportRow 解析字典配置导入行数据。
func parseSysConfigImportRow(values []string) (*types.SaveSysConfigReq, error) {
	get := func(index int) string {
		if index >= len(values) {
			return ""
		}
		return strings.TrimSpace(values[index])
	}
	typ, err := strconv.Atoi(defaultString(get(3), "3"))
	if err != nil {
		return nil, errors.Errorf("配置类型必须是数字")
	}
	pid, err := strconv.Atoi(defaultString(get(7), "0"))
	if err != nil {
		return nil, errors.Errorf("上级ID必须是数字")
	}
	valueRaw, err := buildSysConfigImportJSON(typ, get(4), false)
	if err != nil {
		return nil, errors.Wrap(err, "配置值格式不合法")
	}
	exampleRaw, err := buildSysConfigImportJSON(typ, get(5), true)
	if err != nil {
		return nil, errors.Wrap(err, "示例值格式不合法")
	}
	return &types.SaveSysConfigReq{
		UUID:    get(1),
		Title:   get(2),
		Type:    typ,
		Value:   valueRaw,
		Example: exampleRaw,
		Page:    get(6),
		Pid:     pid,
		Remark:  get(8),
	}, nil
}

// buildSysConfigImportJSON 构建字典配置导入 JSON 数据。
func buildSysConfigImportJSON(typ int, text string, allowEmpty bool) (json.RawMessage, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		if allowEmpty {
			return nil, nil
		}
		if typ == 0 {
			return json.RawMessage("null"), nil
		}
		return nil, errors.Errorf("配置值不能为空")
	}
	switch typ {
	case 0, 1, 2:
		return json.RawMessage(text), nil
	case 3:
		body, _ := json.Marshal(text)
		return body, nil
	case 4, 5:
		return json.RawMessage(text), nil
	case 6:
		if strings.EqualFold(text, "true") || text == "1" {
			return json.RawMessage("true"), nil
		}
		if strings.EqualFold(text, "false") || text == "0" {
			return json.RawMessage("false"), nil
		}
		return nil, errors.Errorf("布尔值仅支持 true/false/1/0")
	default:
		return nil, errors.Errorf("配置类型不合法")
	}
}

// buildSysConfigExcelFingerprint 构建字典配置导出指纹。
func buildSysConfigExcelFingerprint(req *types.SysConfigExcelExportReq) string {
	if req == nil {
		return "all"
	}
	return strings.Join([]string{
		strings.TrimSpace(req.UUID),
		strings.TrimSpace(req.Title),
		strings.TrimSpace(req.PagePath),
	}, "|")
}

// defaultString 默认字符串值。
func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
