package excel

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"
)

// TestStreamImportSkipsBlankRows 验证对应场景符合预期。
func TestStreamImportSkipsBlankRows(t *testing.T) {
	workbook := excelize.NewFile()
	filePath := filepath.Join(t.TempDir(), "import.xlsx")
	if err := workbook.SetSheetRow("Sheet1", "A1", &[]string{"用户名", "邮箱"}); err != nil {
		t.Fatalf("写入表头失败: %v", err)
	}
	if err := workbook.SetSheetRow("Sheet1", "A2", &[]string{"alice", "alice@example.com"}); err != nil {
		t.Fatalf("写入第1行数据失败: %v", err)
	}
	if err := workbook.SetSheetRow("Sheet1", "A3", &[]string{"", ""}); err != nil {
		t.Fatalf("写入空白行失败: %v", err)
	}
	if err := workbook.SetSheetRow("Sheet1", "A4", &[]string{"bob", "bob@example.com"}); err != nil {
		t.Fatalf("写入第2行数据失败: %v", err)
	}
	if err := workbook.SaveAs(filePath); err != nil {
		t.Fatalf("保存测试 Excel 失败: %v", err)
	}
	defer func() {
		_ = workbook.Close()
	}()

	var (
		gotHeader []string
		gotRows   [][]string
	)
	if err := StreamImport(context.Background(), StreamImportOptions{
		FilePath:  filePath,
		TrimSpace: true,
		OnHeader: func(headers []string) error {
			gotHeader = append([]string{}, headers...)
			return nil
		},
		OnRow: func(rowIndex int, values []string) error {
			gotRows = append(gotRows, append([]string{}, values...))
			return nil
		},
	}); err != nil {
		t.Fatalf("StreamImport 返回错误: %v", err)
	}

	if !reflect.DeepEqual(gotHeader, []string{"用户名", "邮箱"}) {
		t.Fatalf("表头不匹配: got=%v", gotHeader)
	}
	expectedRows := [][]string{
		{"alice", "alice@example.com"},
		{"bob", "bob@example.com"},
	}
	if !reflect.DeepEqual(gotRows, expectedRows) {
		t.Fatalf("导入数据不匹配: got=%v want=%v", gotRows, expectedRows)
	}
}
