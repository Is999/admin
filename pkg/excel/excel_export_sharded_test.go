package excel

import (
	"context"
	stdErrors "errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

type shardedExportRow struct {
	ID int
}

type shardedExportShard struct {
	ID int
}

func TestApplyStreamExportShardedOpts(t *testing.T) {
	base := StreamExportShardedOptions[shardedExportRow, int, shardedExportShard]{
		FilePath: "test.xlsx",
	}
	got := ApplyStreamExportShardedOpts(
		base,
		WithStreamExportSheetName[shardedExportRow, int, shardedExportShard]("导出表"),
		WithStreamExportBatchSize[shardedExportRow, int, shardedExportShard](300),
		WithStreamExportMaxConcurrentShards[shardedExportRow, int, shardedExportShard](6),
		WithStreamExportChunkBufferSize[shardedExportRow, int, shardedExportShard](8),
		WithStreamExportMaxPendingChunks[shardedExportRow, int, shardedExportShard](12),
		WithStreamExportProgressThrottle[shardedExportRow, int, shardedExportShard](time.Second, 500),
	)
	if got.SheetName != "导出表" {
		t.Fatalf("SheetName 未正确应用: got=%s", got.SheetName)
	}
	if got.BatchSize != 300 {
		t.Fatalf("BatchSize 未正确应用: got=%d", got.BatchSize)
	}
	if got.MaxConcurrentShards != 6 {
		t.Fatalf("MaxConcurrentShards 未正确应用: got=%d", got.MaxConcurrentShards)
	}
	if got.ChunkBufferSize != 8 {
		t.Fatalf("ChunkBufferSize 未正确应用: got=%d", got.ChunkBufferSize)
	}
	if got.MaxPendingChunks != 12 {
		t.Fatalf("MaxPendingChunks 未正确应用: got=%d", got.MaxPendingChunks)
	}
	if got.ProgressMinInterval != time.Second || got.ProgressMinRows != 500 {
		t.Fatalf("进度节流配置未正确应用: got=%v/%d", got.ProgressMinInterval, got.ProgressMinRows)
	}
}

// TestExportResultBufferSizeUsesConcurrentShardLimit 验证结果通道容量按并发分片数计算，避免海量分片放大内存。
func TestExportResultBufferSizeUsesConcurrentShardLimit(t *testing.T) {
	tests := []struct {
		name                string
		shardCount          int
		maxConcurrentShards int
		chunkBufferSize     int
		want                int
	}{
		{name: "limited by concurrent shards", shardCount: 1000, maxConcurrentShards: 4, chunkBufferSize: 2, want: 8},
		{name: "limited by shard count", shardCount: 2, maxConcurrentShards: 10, chunkBufferSize: 3, want: 6},
		{name: "limited by safety cap", shardCount: 1000, maxConcurrentShards: 1000, chunkBufferSize: 1000, want: MaxExportWorkerCount * MaxChunkBufferSize},
		{name: "fallback minimum", shardCount: 0, maxConcurrentShards: 0, chunkBufferSize: 0, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exportResultBufferSize(tt.shardCount, tt.maxConcurrentShards, tt.chunkBufferSize); got != tt.want {
				t.Fatalf("exportResultBufferSize() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestDefaultPendingExportChunks 验证乱序缓存默认上限只随并发缓冲扩张，避免总分片数放大内存。
func TestDefaultPendingExportChunks(t *testing.T) {
	tests := []struct {
		name                string
		shardCount          int
		maxConcurrentShards int
		chunkBufferSize     int
		want                int
	}{
		{name: "default bounded by concurrent buffer", shardCount: 1000, maxConcurrentShards: 4, chunkBufferSize: 2, want: 16},
		{name: "minimum", shardCount: 0, maxConcurrentShards: 0, chunkBufferSize: 0, want: 2},
		{name: "safety cap", shardCount: 1000, maxConcurrentShards: 1000, chunkBufferSize: 1000, want: MaxPendingExportChunks},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultPendingExportChunks(tt.shardCount, tt.maxConcurrentShards, tt.chunkBufferSize); got != tt.want {
				t.Fatalf("defaultPendingExportChunks() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestStreamExportShardedKeepsGlobalOrder(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "sharded.xlsx")
	source := map[int][]int{
		1: {1, 2},
		2: {3, 4},
		3: {5, 6},
	}
	delays := map[int]time.Duration{
		1: 120 * time.Millisecond,
		2: 20 * time.Millisecond,
		3: 40 * time.Millisecond,
	}

	err := StreamExportSharded(context.Background(), StreamExportShardedOptions[shardedExportRow, int, shardedExportShard]{
		FilePath:            filePath,
		SheetName:           "Sheet1",
		Header:              []any{"ID"},
		BatchSize:           1,
		MaxConcurrentShards: 3,
		ChunkBufferSize:     2,
		RetryCount:          1,
		Shards: []ExportShard[shardedExportShard]{
			{Meta: shardedExportShard{ID: 1}},
			{Meta: shardedExportShard{ID: 2}},
			{Meta: shardedExportShard{ID: 3}},
		},
		InitialCursor: func(shard shardedExportShard) int {
			return 0
		},
		Query: func(ctx context.Context, shard shardedExportShard, cursor int, limit int) (*CursorPage[shardedExportRow, int], error) {
			time.Sleep(delays[shard.ID])
			items := source[shard.ID]
			start := cursor
			if start >= len(items) {
				return &CursorPage[shardedExportRow, int]{
					Total: int64(len(items) * 3),
					Items: nil,
				}, nil
			}
			next := start + limit
			if next > len(items) {
				next = len(items)
			}
			rows := make([]shardedExportRow, 0, next-start)
			for _, id := range items[start:next] {
				rows = append(rows, shardedExportRow{ID: id})
			}
			return &CursorPage[shardedExportRow, int]{
				Total:      6,
				Items:      rows,
				NextCursor: next,
				HasMore:    next < len(items),
			}, nil
		},
		BuildRows: func(items []shardedExportRow) ([][]any, error) {
			rows := make([][]any, 0, len(items))
			for _, item := range items {
				rows = append(rows, []any{item.ID})
			}
			return rows, nil
		},
	})
	if err != nil {
		t.Fatalf("StreamExportSharded 返回错误: %v", err)
	}

	workbook, err := excelize.OpenFile(filePath)
	if err != nil {
		t.Fatalf("打开导出文件失败: %v", err)
	}
	defer func() {
		_ = workbook.Close()
	}()

	rows, err := workbook.GetRows("Sheet1")
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
	expected := []string{"ID", "1", "2", "3", "4", "5", "6"}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("导出顺序不正确: got=%v want=%v", got, expected)
	}
}

// TestStreamExportShardedRejectsPendingChunkOverflow 确保前置慢分片不会让后续快分片无限堆积乱序缓存。
func TestStreamExportShardedRejectsPendingChunkOverflow(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "pending-overflow.xlsx")
	err := StreamExportSharded(context.Background(), StreamExportShardedOptions[shardedExportRow, int, shardedExportShard]{
		FilePath:            filePath,
		SheetName:           "Sheet1",
		BatchSize:           1,
		MaxConcurrentShards: 2,
		ChunkBufferSize:     2,
		MaxPendingChunks:    1,
		RetryCount:          1,
		Shards: []ExportShard[shardedExportShard]{
			{Meta: shardedExportShard{ID: 1}},
			{Meta: shardedExportShard{ID: 2}},
		},
		InitialCursor: func(shard shardedExportShard) int {
			_ = shard
			return 0
		},
		Query: func(ctx context.Context, shard shardedExportShard, cursor int, limit int) (*CursorPage[shardedExportRow, int], error) {
			if shard.ID == 1 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(200 * time.Millisecond):
					return &CursorPage[shardedExportRow, int]{
						Total:   1,
						Items:   []shardedExportRow{{ID: 1}},
						HasMore: false,
					}, nil
				}
			}
			if cursor >= 3 {
				return &CursorPage[shardedExportRow, int]{Total: 3}, nil
			}
			next := cursor + limit
			if next > 3 {
				next = 3
			}
			return &CursorPage[shardedExportRow, int]{
				Total:      3,
				Items:      []shardedExportRow{{ID: next + 10}},
				NextCursor: next,
				HasMore:    next < 3,
			}, nil
		},
		BuildRows: func(items []shardedExportRow) ([][]any, error) {
			rows := make([][]any, 0, len(items))
			for _, item := range items {
				rows = append(rows, []any{item.ID})
			}
			return rows, nil
		},
	})
	if err == nil {
		t.Fatal("期望乱序缓存超限错误被返回")
	}
	if !strings.Contains(err.Error(), "乱序缓存超限") {
		t.Fatalf("错误信息未包含缓存超限上下文: %v", err)
	}
}

// TestStreamExportShardedSavesPartialFileWhenSheetLimitExceeded 验证行数保护触发后仍会保存已写入的有效 Excel 文件。
func TestStreamExportShardedSavesPartialFileWhenSheetLimitExceeded(t *testing.T) {
	originLimit := streamExportSheetRowLimit
	streamExportSheetRowLimit = 3
	defer func() {
		streamExportSheetRowLimit = originLimit
	}()

	filePath := filepath.Join(t.TempDir(), "partial-limit.xlsx")
	err := StreamExportSharded(context.Background(), StreamExportShardedOptions[shardedExportRow, int, shardedExportShard]{
		FilePath:            filePath,
		SheetName:           "Sheet1",
		Header:              []any{"ID"},
		BatchSize:           10,
		MaxConcurrentShards: 1,
		ChunkBufferSize:     1,
		RetryCount:          1,
		Shards: []ExportShard[shardedExportShard]{
			{Meta: shardedExportShard{ID: 1}},
		},
		InitialCursor: func(shard shardedExportShard) int {
			_ = shard
			return 0
		},
		Query: func(ctx context.Context, shard shardedExportShard, cursor int, limit int) (*CursorPage[shardedExportRow, int], error) {
			_ = ctx
			_ = shard
			_ = cursor
			_ = limit
			return &CursorPage[shardedExportRow, int]{
				Total:   3,
				Items:   []shardedExportRow{{ID: 1}, {ID: 2}, {ID: 3}},
				HasMore: false,
			}, nil
		},
		BuildRows: func(items []shardedExportRow) ([][]any, error) {
			rows := make([][]any, 0, len(items))
			for _, item := range items {
				rows = append(rows, []any{item.ID})
			}
			return rows, nil
		},
	})
	if err == nil {
		t.Fatal("期望单表行数超限错误被返回")
	}
	if !strings.Contains(err.Error(), "已保留前[3]行") {
		t.Fatalf("错误信息未说明部分文件已保留: %v", err)
	}

	workbook, err := excelize.OpenFile(filePath)
	if err != nil {
		t.Fatalf("打开部分导出文件失败: %v", err)
	}
	defer func() {
		_ = workbook.Close()
	}()

	rows, err := workbook.GetRows("Sheet1")
	if err != nil {
		t.Fatalf("读取部分导出文件失败: %v", err)
	}
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) > 0 {
			got = append(got, row[0])
		}
	}
	expected := []string{"ID", "1", "2"}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("部分导出内容不正确: got=%v want=%v", got, expected)
	}
}

// TestStreamExportShardedWritesRowsAfterEmptyShard 确保空分片不会阻断后续分片写入。
func TestStreamExportShardedWritesRowsAfterEmptyShard(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "empty-shard.xlsx")
	err := StreamExportSharded(context.Background(), StreamExportShardedOptions[shardedExportRow, int, shardedExportShard]{
		FilePath:            filePath,
		SheetName:           "Sheet1",
		Header:              []any{"ID"},
		BatchSize:           10,
		MaxConcurrentShards: 2,
		ChunkBufferSize:     2,
		RetryCount:          1,
		Shards: []ExportShard[shardedExportShard]{
			{Meta: shardedExportShard{ID: 1}},
			{Meta: shardedExportShard{ID: 2}},
		},
		InitialCursor: func(shard shardedExportShard) int {
			return 0
		},
		Query: func(ctx context.Context, shard shardedExportShard, cursor int, limit int) (*CursorPage[shardedExportRow, int], error) {
			_ = ctx
			_ = cursor
			_ = limit
			if shard.ID == 1 {
				return &CursorPage[shardedExportRow, int]{Total: 1}, nil
			}
			return &CursorPage[shardedExportRow, int]{
				Total:   1,
				Items:   []shardedExportRow{{ID: 2}},
				HasMore: false,
			}, nil
		},
		BuildRows: func(items []shardedExportRow) ([][]any, error) {
			rows := make([][]any, 0, len(items))
			for _, item := range items {
				rows = append(rows, []any{item.ID})
			}
			return rows, nil
		},
	})
	if err != nil {
		t.Fatalf("StreamExportSharded 返回错误: %v", err)
	}

	workbook, err := excelize.OpenFile(filePath)
	if err != nil {
		t.Fatalf("打开导出文件失败: %v", err)
	}
	defer func() {
		_ = workbook.Close()
	}()

	rows, err := workbook.GetRows("Sheet1")
	if err != nil {
		t.Fatalf("读取导出结果失败: %v", err)
	}
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) > 0 {
			got = append(got, row[0])
		}
	}
	expected := []string{"ID", "2"}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("空分片后的导出数据不正确: got=%v want=%v", got, expected)
	}
}

// TestStreamExportRetryDelayUsesExponentialBackoff 验证查询重试使用指数退避并受最大等待时间保护。
func TestStreamExportRetryDelayUsesExponentialBackoff(t *testing.T) {
	base := 100 * time.Millisecond
	tests := []struct {
		name    string
		attempt int
		want    time.Duration
	}{
		{name: "first retry", attempt: 0, want: base},
		{name: "second retry", attempt: 1, want: 2 * base},
		{name: "third retry", attempt: 2, want: 4 * base},
		{name: "capped retry", attempt: 20, want: maxStreamExportRetryBackoff},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := streamExportRetryDelay(base, tt.attempt); got != tt.want {
				t.Fatalf("streamExportRetryDelay() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestStreamExportShardedReturnsQueryError 确保分片查询错误不会被 resultCh 关闭路径吞掉。
func TestStreamExportShardedReturnsQueryError(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "query-error.xlsx")
	err := StreamExportSharded(context.Background(), StreamExportShardedOptions[shardedExportRow, int, shardedExportShard]{
		FilePath:            filePath,
		SheetName:           "Sheet1",
		BatchSize:           1,
		MaxConcurrentShards: 1,
		ChunkBufferSize:     1,
		RetryCount:          1,
		Shards: []ExportShard[shardedExportShard]{
			{Meta: shardedExportShard{ID: 1}},
		},
		InitialCursor: func(shard shardedExportShard) int {
			_ = shard
			return 0
		},
		Query: func(ctx context.Context, shard shardedExportShard, cursor int, limit int) (*CursorPage[shardedExportRow, int], error) {
			_ = ctx
			_ = shard
			_ = cursor
			_ = limit
			return nil, stdErrors.New("query failed")
		},
		BuildRows: func(items []shardedExportRow) ([][]any, error) {
			return nil, nil
		},
	})
	if err == nil {
		t.Fatal("期望分片查询错误被返回")
	}
}
