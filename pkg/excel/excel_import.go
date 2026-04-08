package excel

import (
	"context"
	"strings"

	"github.com/Is999/go-utils/errors"
	"github.com/xuri/excelize/v2"
)

const (
	// DefaultHeaderRowIndex 表示默认表头所在行号。
	DefaultHeaderRowIndex = 1
	// DefaultDataStartRowIndex 表示默认数据起始行号。
	DefaultDataStartRowIndex = 2
)

// ImportHeaderFunc 表示导入时的表头处理函数。
type ImportHeaderFunc func(headers []string) error

// ImportRowFunc 表示导入时的单行处理函数。
type ImportRowFunc func(rowIndex int, values []string) error

// StreamImportOptions 表示通用 Excel 流式导入参数。
type StreamImportOptions struct {
	FilePath  string           // 待导入文件路径
	SheetName string           // 工作表名称；为空时取第一个工作表
	HeaderRow int              // 表头行号
	StartRow  int              // 数据起始行号
	MaxRows   int              // 最大允许导入行数；0 表示不限制
	TrimSpace bool             // 是否自动裁剪单元格空白
	OnHeader  ImportHeaderFunc // 表头校验/处理函数
	OnRow     ImportRowFunc    // 单行处理函数
}

// StreamImport 以逐行流式方式读取 Excel，适合大文件导入场景。
func StreamImport(ctx context.Context, opt StreamImportOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(opt.FilePath) == "" {
		return errors.Errorf("导入文件路径不能为空")
	}
	if opt.OnRow == nil {
		return errors.Errorf("导入行处理函数不能为空")
	}
	if opt.HeaderRow <= 0 {
		opt.HeaderRow = DefaultHeaderRowIndex
	}
	if opt.StartRow <= 0 {
		opt.StartRow = DefaultDataStartRowIndex
	}

	workbook, err := excelize.OpenFile(opt.FilePath)
	if err != nil {
		return errors.Wrap(err, "打开导入文件失败")
	}
	defer func() {
		_ = workbook.Close()
	}()

	sheetName := strings.TrimSpace(opt.SheetName)
	if sheetName == "" {
		sheets := workbook.GetSheetList()
		if len(sheets) == 0 {
			return errors.Errorf("导入文件缺少工作表")
		}
		sheetName = sheets[0]
	}

	rows, err := workbook.Rows(sheetName)
	if err != nil {
		return errors.Wrapf(err, "读取工作表[%s]失败", sheetName)
	}
	defer func() {
		_ = rows.Close()
	}()

	rowIndex := 0
	dataRows := 0
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return errors.Wrap(err, "导入上下文已取消")
		}
		rowIndex++
		values, err := rows.Columns()
		if err != nil {
			return errors.Wrapf(err, "读取第[%d]行数据失败", rowIndex)
		}
		if opt.TrimSpace {
			for index := range values {
				values[index] = strings.TrimSpace(values[index])
			}
		}
		if rowIndex == opt.HeaderRow && opt.OnHeader != nil {
			if err := opt.OnHeader(values); err != nil {
				return errors.Wrap(err, "导入表头校验失败")
			}
			continue
		}
		if rowIndex < opt.StartRow {
			continue
		}
		dataRows++
		if opt.MaxRows > 0 && dataRows > opt.MaxRows {
			return errors.Errorf("导入数据超过最大限制[%d]行", opt.MaxRows)
		}
		if isEmptyImportRow(values) {
			continue
		}
		if err := opt.OnRow(rowIndex, values); err != nil {
			return errors.Wrapf(err, "处理导入第[%d]行失败", rowIndex)
		}
	}
	if err := rows.Error(); err != nil {
		return errors.Wrap(err, "遍历导入文件失败")
	}
	return nil
}

// isEmptyImportRow 判断一行是否为空白行。
func isEmptyImportRow(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}
