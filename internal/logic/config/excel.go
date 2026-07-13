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

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	keys "admin/common/rediskeys"
	redislock "admin/internal/infra/redsync"
	filelogic "admin/internal/logic/file"
	"admin/internal/model"
	"admin/internal/types"
	pkgexcel "admin/pkg/excel"
	"admin/pkg/transfer"

	"gorm.io/gorm"
)

const (
	// sysConfigExcelSheetName 表示字典 Excel 导入导出的工作表名称。
	sysConfigExcelSheetName = "字典配置"
	// sysConfigExcelBatchSize 表示字典 Excel 导出游标批次大小。
	sysConfigExcelBatchSize = 200
	// sysConfigExcelImportMaxRows 表示字典 Excel 单次最大导入行数。
	sysConfigExcelImportMaxRows = 5000
	// sysConfigExcelLockTTL 表示字典 Excel 导入导出的业务锁保留时间。
	sysConfigExcelLockTTL = 2 * time.Minute
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
	err := redislock.WithLock(l.Ctx, l.Redis(), lockKey, sysConfigExcelLockTTL, func(ctx context.Context) error {
		now := time.Now()
		fileName = fmt.Sprintf("sys_config_%s.xlsx", now.Format("20060102150405"))
		exportPath = filepath.Join(os.TempDir(), "admin", "exports", "sys-config", fileName)
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
		return "", "", types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"SysConfigLogic.ExportExcel 导出字典配置失败").ToBizResult()
	}
	return exportPath, fileName, nil
}

// ImportExcel 从已上传的 Excel 文件导入字典配置。
func (l *SysConfigLogic) ImportExcel(req *types.SysConfigExcelImportReq) *types.BizResult {
	fileTransferLogic := filelogic.NewFileTransferLogicWithContext(l.Ctx, l.Svc)
	importFilePath, importSession, cleanup, err := l.resolveImportExcelFile(req, fileTransferLogic)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"SysConfigLogic.ImportExcel 解析导入文件失败").ToBizResult()
	}

	lockKey := l.AppRedisKey(fmt.Sprintf(keys.SysConfigExcelImportLock, l.GetCtxAdmin().ID))
	var result *types.SysConfigExcelImportResp
	changedUUIDs := map[string]struct{}{}
	err = redislock.WithLock(l.Ctx, l.Redis(), lockKey, sysConfigExcelLockTTL, func(ctx context.Context) error {
		return l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
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
	})
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
			"SysConfigLogic.ImportExcel 导入字典配置失败").ToBizResult()
	}
	if cleanupErr := fileTransferLogic.DeleteImportedObject(importSession); cleanupErr != nil {
		// 数据库导入已经提交，删除失败交给上传时预投递的延迟任务重试，不能把成功导入误报为失败。
		corelogic.LogWrappedError(l.Logger, cleanupErr,
			"SysConfigLogic.ImportExcel 删除已消费上传对象失败 upload_id=%s", importSession.UploadID)
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

// resolveImportExcelFile 解析导入 Excel 文件，并返回已校验的上传会话供成功后删除源对象。
func (l *SysConfigLogic) resolveImportExcelFile(req *types.SysConfigExcelImportReq, fileTransferLogic *filelogic.FileTransferLogic) (string, *transfer.UploadSession, func(), error) {
	if req == nil {
		return "", nil, nil, errors.Errorf("导入请求不能为空")
	}
	if strings.TrimSpace(req.UploadID) != "" {
		session, err := fileTransferLogic.GetSession(req.UploadID)
		if err != nil {
			return "", nil, nil, errors.Wrapf(err, "读取导入文件会话[%s]失败", req.UploadID)
		}
		if err := fileTransferLogic.EnsureSessionOwner(session); err != nil {
			return "", nil, nil, errors.Tag(err)
		}
		if !fileTransferLogic.IsCompletedSession(session) {
			return "", nil, nil, errors.Errorf("导入文件尚未上传完成")
		}
		if strings.TrimSpace(session.BizType) != filelogic.FileTransferBizSysConfigExcelImport {
			return "", nil, nil, errors.Errorf("导入文件业务类型不合法")
		}
		filePath, cleanup, err := fileTransferLogic.MaterializeSessionObject(session)
		return filePath, session, cleanup, errors.Tag(err)
	}
	if strings.TrimSpace(req.FileURL) == "" {
		return "", nil, nil, errors.Errorf("导入文件地址不能为空")
	}
	session, err := fileTransferLogic.ResolveManagedSessionByFileURL(req.FileURL)
	if err != nil {
		return "", nil, nil, errors.Wrap(err, "根据文件地址反查上传会话失败")
	}
	if err := fileTransferLogic.EnsureSessionOwner(session); err != nil {
		return "", nil, nil, errors.Tag(err)
	}
	if !fileTransferLogic.IsCompletedSession(session) {
		return "", nil, nil, errors.Errorf("导入文件尚未上传完成")
	}
	if strings.TrimSpace(session.BizType) != filelogic.FileTransferBizSysConfigExcelImport {
		return "", nil, nil, errors.Errorf("导入文件业务类型不合法")
	}
	filePath, cleanup, err := fileTransferLogic.MaterializeSessionObject(session)
	return filePath, session, cleanup, errors.Tag(err)
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
		if err := tx.Create(&cfg).Error; err != nil {
			return errors.Wrap(err, "创建系统配置失败")
		}
		summary.Created++
		if changedUUIDs != nil {
			changedUUIDs[cfg.UUID] = struct{}{}
		}
		return nil
	}
	if existing.Type != row.Type {
		return errors.Errorf("配置UUID[%s]的类型不允许从[%d]改为[%d]", row.UUID, existing.Type, row.Type)
	}
	nextPid := row.Pid
	if row.Pid <= 0 && existing.Pid > 0 {
		nextPid = existing.Pid
	}
	pids, err := l.sysConfigPidsTx(tx, nextPid, existing.ID)
	if err != nil {
		return errors.Tag(err)
	}
	if err := tx.Model(&model.SysConfig{}).Where("id = ?", existing.ID).Updates(map[string]any{
		"title":      row.Title,
		"value":      value,
		"example":    example,
		"remark":     row.Remark,
		"page":       row.Page,
		"pid":        nextPid,
		"pids":       pids,
		"version":    gorm.Expr("version + 1"),
		"updated_at": time.Now(),
	}).Error; err != nil {
		return errors.Wrap(err, "更新系统配置失败")
	}
	summary.Updated++
	if changedUUIDs != nil {
		changedUUIDs[existing.UUID] = struct{}{}
		changedUUIDs[row.UUID] = struct{}{}
	}
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
