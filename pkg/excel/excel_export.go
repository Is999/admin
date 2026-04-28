package excel

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/Is999/go-utils/errors"
	"github.com/xuri/excelize/v2"
)

// singleExportShard 表示单分片流式导出的占位分片。
type singleExportShard struct{}

const (
	// DefaultBatchSize 表示默认导出批量大小。
	DefaultBatchSize = 500
	// DefaultMinWorkerCount 表示默认最小并发 worker 数。
	DefaultMinWorkerCount = 2
	// DefaultMaxWorkerCount 表示默认最大并发 worker 数。
	DefaultMaxWorkerCount = 4
	// MaxExportBatchSize 表示单批导出读取的安全上限，避免误配置导致单批行构造占用过多内存。
	MaxExportBatchSize = 5000
	// MaxExportWorkerCount 表示导出内部 worker 或分片并发的安全上限。
	MaxExportWorkerCount = 32
	// MaxChunkBufferSize 表示分片导出结果通道单分片缓冲的安全上限。
	MaxChunkBufferSize = 32
	// MaxExcelSheetRows 表示 XLSX 单个工作表允许的最大行数。
	MaxExcelSheetRows = 1048576
	// concurrentRowsJobBufferMultiplier 表示行构造任务通道相对 worker 数的缓冲倍数，避免按数据量分配大 channel。
	concurrentRowsJobBufferMultiplier = 2
)

// CursorPage 表示一次游标分页查询结果。
type CursorPage[T any, C any] struct {
	Total      int64 // 总条数
	Items      []T   // 当前批次数据
	NextCursor C     // 下一批次游标
	HasMore    bool  // 是否还有后续批次
}

// QueryCursorFunc 表示按游标读取一批数据的函数。
type QueryCursorFunc[T any, C any] func(ctx context.Context, cursor C, limit int) (*CursorPage[T, C], error)

// BuildRowsFunc 表示把一批业务数据转换成 Excel 行数据的函数。
type BuildRowsFunc[T any] func(items []T) ([][]any, error)

// ProgressFunc 表示导出进度回调函数。
type ProgressFunc func(progress ExportProgress) error

// ConcurrentRowsOptions 表示并发构造数据行的配置。
type ConcurrentRowsOptions struct {
	MinWorkerCount int // 最小并发 worker 数
	MaxWorkerCount int // 最大并发 worker 数
}

// ConcurrentRowsOpt 表示并发构造数据行的函数式配置项。
type ConcurrentRowsOpt func(*ConcurrentRowsOptions)

// WithConcurrentRowsMinWorkers 设置最小并发 worker 数。
func WithConcurrentRowsMinWorkers(minWorkerCount int) ConcurrentRowsOpt {
	return func(opt *ConcurrentRowsOptions) {
		opt.MinWorkerCount = minWorkerCount
	}
}

// WithConcurrentRowsMaxWorkers 设置最大并发 worker 数。
func WithConcurrentRowsMaxWorkers(maxWorkerCount int) ConcurrentRowsOpt {
	return func(opt *ConcurrentRowsOptions) {
		opt.MaxWorkerCount = maxWorkerCount
	}
}

// WithConcurrentRowsWorkersRange 同时设置最小和最大并发 worker 数。
func WithConcurrentRowsWorkersRange(minWorkerCount int, maxWorkerCount int) ConcurrentRowsOpt {
	return func(opt *ConcurrentRowsOptions) {
		opt.MinWorkerCount = minWorkerCount
		opt.MaxWorkerCount = maxWorkerCount
	}
}

// ApplyConcurrentRowsOpts 把函数式配置项应用到并发构造配置。
func ApplyConcurrentRowsOpts(base ConcurrentRowsOptions, opts ...ConcurrentRowsOpt) ConcurrentRowsOptions {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&base)
	}
	return base
}

// StreamExportOptions 表示通用 Excel 流式导出参数。
type StreamExportOptions[T any, C any] struct {
	FilePath      string                // 目标文件路径
	SheetName     string                // 工作表名称
	Header        []any                 // 表头
	HeaderStyle   *excelize.Style       // 表头样式
	BatchSize     int                   // 每批读取数量
	InitialCursor C                     // 初始游标
	Query         QueryCursorFunc[T, C] // 查询函数
	BuildRows     BuildRowsFunc[T]      // 行构造函数
	Progress      ProgressFunc          // 进度回调
}

// StreamExport 按“游标查询 -> 行转换 -> 流式写入”模式生成 Excel 文件。
func StreamExport[T any, C any](ctx context.Context, opt StreamExportOptions[T, C]) error {
	if opt.Query == nil {
		return errors.Errorf("导出查询函数不能为空")
	}
	if opt.BuildRows == nil {
		return errors.Errorf("导出行构造函数不能为空")
	}
	if opt.FilePath == "" {
		return errors.Errorf("导出文件路径不能为空")
	}
	if opt.SheetName == "" {
		opt.SheetName = "Sheet1"
	}
	if opt.BatchSize <= 0 {
		opt.BatchSize = DefaultBatchSize
	} else if opt.BatchSize > MaxExportBatchSize {
		opt.BatchSize = MaxExportBatchSize
	}
	return StreamExportSharded(ctx, StreamExportShardedOptions[T, C, singleExportShard]{
		FilePath:            opt.FilePath,
		SheetName:           opt.SheetName,
		Header:              opt.Header,
		HeaderStyle:         opt.HeaderStyle,
		BatchSize:           opt.BatchSize,
		MaxConcurrentShards: 1,
		ChunkBufferSize:     1,
		RetryCount:          DefaultRetryCount,
		RetryBackoff:        100 * time.Millisecond,
		Shards: []ExportShard[singleExportShard]{
			{Meta: singleExportShard{}},
		},
		InitialCursor: func(shard singleExportShard) C {
			return opt.InitialCursor
		},
		Query: func(ctx context.Context, shard singleExportShard, cursor C, limit int) (*CursorPage[T, C], error) {
			return opt.Query(ctx, cursor, limit)
		},
		BuildRows: opt.BuildRows,
		Progress:  opt.Progress,
	})
}

// BuildRowsConcurrently 并发把一批业务对象转换为 Excel 行数据。
func BuildRowsConcurrently[T any](items []T, minWorkerCount int, maxWorkerCount int, rowBuilder func(item T) ([]any, error)) ([][]any, error) {
	return BuildRowsConcurrentlyWithOpt(items, rowBuilder, WithConcurrentRowsWorkersRange(minWorkerCount, maxWorkerCount))
}

// BuildRowsConcurrentlyWithOpt 按函数式配置并发把一批业务对象转换为 Excel 行数据。
func BuildRowsConcurrentlyWithOpt[T any](items []T, rowBuilder func(item T) ([]any, error), opts ...ConcurrentRowsOpt) ([][]any, error) {
	if len(items) == 0 {
		return [][]any{}, nil
	}
	if rowBuilder == nil {
		return nil, errors.Errorf("行构造函数不能为空")
	}
	rowOpt := ApplyConcurrentRowsOpts(ConcurrentRowsOptions{
		MinWorkerCount: DefaultMinWorkerCount,
		MaxWorkerCount: DefaultMaxWorkerCount,
	}, opts...)
	if rowOpt.MinWorkerCount <= 0 {
		rowOpt.MinWorkerCount = DefaultMinWorkerCount
	}
	if rowOpt.MaxWorkerCount <= 0 {
		rowOpt.MaxWorkerCount = DefaultMaxWorkerCount
	}
	if rowOpt.MinWorkerCount > MaxExportWorkerCount {
		rowOpt.MinWorkerCount = MaxExportWorkerCount
	}
	if rowOpt.MaxWorkerCount > MaxExportWorkerCount {
		rowOpt.MaxWorkerCount = MaxExportWorkerCount
	}
	if rowOpt.MinWorkerCount > rowOpt.MaxWorkerCount {
		rowOpt.MinWorkerCount = rowOpt.MaxWorkerCount
	}

	workerCount := runtime.NumCPU()
	if workerCount < rowOpt.MinWorkerCount {
		workerCount = rowOpt.MinWorkerCount
	}
	if workerCount > rowOpt.MaxWorkerCount {
		workerCount = rowOpt.MaxWorkerCount
	}
	if workerCount > len(items) {
		workerCount = len(items)
	}

	rows := make([][]any, len(items))
	jobs := make(chan int, concurrentRowsJobBufferSize(workerCount, len(items)))
	done := make(chan struct{})
	var (
		waitGroup sync.WaitGroup
		firstErr  error
		errOnce   sync.Once
		doneOnce  sync.Once
	)
	cancelBuild := func() {
		doneOnce.Do(func() {
			close(done)
		})
	}

	for i := 0; i < workerCount; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for index := range jobs {
				row, err := rowBuilder(items[index])
				if err != nil {
					errOnce.Do(func() {
						firstErr = err
						cancelBuild()
					})
					return
				}
				rows[index] = row
			}
		}()
	}
sendLoop:
	for index := range items {
		select {
		case <-done:
			break sendLoop
		case jobs <- index:
		}
	}
	close(jobs)
	waitGroup.Wait()
	if firstErr != nil {
		return nil, errors.Tag(firstErr)
	}
	return rows, nil
}

// concurrentRowsJobBufferSize 计算行构造任务通道缓冲大小。
// 主协程允许被 worker 反压，避免大批量导出时为每个 item 都分配 channel 槽位。
func concurrentRowsJobBufferSize(workerCount int, itemCount int) int {
	if itemCount <= 0 {
		return 0
	}
	if workerCount <= 0 {
		return 1
	}
	size := workerCount * concurrentRowsJobBufferMultiplier
	if size > itemCount {
		return itemCount
	}
	return size
}

// timeNow 统一封装当前时间，便于后续测试替换。
var timeNow = func() time.Time {
	return time.Now()
}
