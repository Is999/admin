package excel

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestBuildRowsConcurrentlyKeepsOrder(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	rows, err := BuildRowsConcurrently(items, 2, 4, func(item int) ([]any, error) {
		return []any{fmt.Sprintf("row-%d", item)}, nil
	})
	if err != nil {
		t.Fatalf("BuildRowsConcurrently 返回错误: %v", err)
	}
	if len(rows) != len(items) {
		t.Fatalf("BuildRowsConcurrently 行数不正确: got=%d want=%d", len(rows), len(items))
	}
	for index, item := range items {
		if got := rows[index][0]; got != fmt.Sprintf("row-%d", item) {
			t.Fatalf("BuildRowsConcurrently 顺序错误: index=%d got=%v want=%s", index, got, fmt.Sprintf("row-%d", item))
		}
	}
}

// TestBuildRowsConcurrentlyReturnsOnBuilderErrorWithoutDeadlock 验证行构造失败后不会卡死。
func TestBuildRowsConcurrentlyReturnsOnBuilderErrorWithoutDeadlock(t *testing.T) {
	items := make([]int, 1000)
	buildErr := errors.New("build row failed")
	done := make(chan error, 1)
	go func() {
		_, err := BuildRowsConcurrentlyWithOpt(items, func(item int) ([]any, error) {
			return nil, buildErr
		}, WithConcurrentRowsWorkersRange(1, 1))
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, buildErr) {
			t.Fatalf("BuildRowsConcurrentlyWithOpt error = %v, want %v", err, buildErr)
		}
	case <-time.After(time.Second):
		t.Fatal("BuildRowsConcurrentlyWithOpt 在行构造失败时发生阻塞")
	}
}

// TestConcurrentRowsJobBufferSizeUsesWorkerBound 验证并发行构造不会按数据量创建超大 channel 缓冲。
func TestConcurrentRowsJobBufferSizeUsesWorkerBound(t *testing.T) {
	if got := concurrentRowsJobBufferSize(4, 5000); got != 8 {
		t.Fatalf("buffer size = %d, want 8", got)
	}
	if got := concurrentRowsJobBufferSize(4, 3); got != 3 {
		t.Fatalf("small item buffer size = %d, want 3", got)
	}
}

func TestStreamExportUsesUnifiedCore(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "stream_export.xlsx")
	source := []int{1, 2, 3, 4, 5}
	err := StreamExport(context.Background(), StreamExportOptions[int, int]{
		FilePath:      filePath,
		SheetName:     "数据",
		Header:        []any{"ID"},
		BatchSize:     2,
		InitialCursor: 0,
		Query: func(ctx context.Context, cursor int, limit int) (*CursorPage[int, int], error) {
			if cursor >= len(source) {
				return &CursorPage[int, int]{Total: int64(len(source))}, nil
			}
			next := cursor + limit
			if next > len(source) {
				next = len(source)
			}
			return &CursorPage[int, int]{
				Total:      int64(len(source)),
				Items:      source[cursor:next],
				NextCursor: next,
				HasMore:    next < len(source),
			}, nil
		},
		BuildRows: func(items []int) ([][]any, error) {
			rows := make([][]any, 0, len(items))
			for _, item := range items {
				rows = append(rows, []any{item})
			}
			return rows, nil
		},
	})
	if err != nil {
		t.Fatalf("StreamExport 返回错误: %v", err)
	}

	workbook, err := excelize.OpenFile(filePath)
	if err != nil {
		t.Fatalf("打开导出文件失败: %v", err)
	}
	defer func() {
		_ = workbook.Close()
	}()

	rows, err := workbook.GetRows("数据")
	if err != nil {
		t.Fatalf("读取导出结果失败: %v", err)
	}
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		got = append(got, row[0])
	}
	expected := []string{"ID", "1", "2", "3", "4", "5"}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("StreamExport 导出结果不正确: got=%v want=%v", got, expected)
	}
}

// TestStreamExportCapsBatchSize 验证过大的导出批次会被夹到安全上限，避免单批行构造占用过多内存。
func TestStreamExportCapsBatchSize(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "stream_export_cap.xlsx")
	var gotLimit int
	err := StreamExport(context.Background(), StreamExportOptions[int, int]{
		FilePath:      filePath,
		SheetName:     "数据",
		BatchSize:     MaxExportBatchSize + 1,
		InitialCursor: 0,
		Query: func(ctx context.Context, cursor int, limit int) (*CursorPage[int, int], error) {
			gotLimit = limit
			return &CursorPage[int, int]{Total: 0}, nil
		},
		BuildRows: func(items []int) ([][]any, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("StreamExport 返回错误: %v", err)
	}
	if gotLimit != MaxExportBatchSize {
		t.Fatalf("query limit = %d, want %d", gotLimit, MaxExportBatchSize)
	}
}
