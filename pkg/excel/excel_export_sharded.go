package excel

import (
	"context"
	"sync"
	"time"

	"github.com/Is999/go-utils/errors"
	"github.com/xuri/excelize/v2"
)

const (
	// DefaultChunkBufferSize 表示分片导出时单分片结果缓冲大小。
	DefaultChunkBufferSize = 2
	// DefaultRetryCount 表示查询失败时默认重试次数。
	DefaultRetryCount = 3
	// MaxPendingExportChunks 表示乱序分片结果在内存中暂存的安全上限，避免前置慢分片导致后续快分片无限堆积。
	MaxPendingExportChunks = MaxExportWorkerCount * MaxChunkBufferSize
	// maxStreamExportRetryBackoff 表示单次查询重试等待的最大时间，避免指数退避在长时间故障下拖慢取消响应。
	maxStreamExportRetryBackoff = 30 * time.Second
)

// streamExportSheetRowLimit 表示单个工作表最大行数。
var streamExportSheetRowLimit = MaxExcelSheetRows

// ExportShard 表示一个独立导出分片。
type ExportShard[S any] struct {
	Meta S // 分片元数据，由业务侧自定义
}

// QueryShardFunc 表示在指定分片内按游标读取一批数据的函数。
type QueryShardFunc[T any, C any, S any] func(ctx context.Context, shard S, cursor C, limit int) (*CursorPage[T, C], error)

// InitialCursorFunc 表示返回某个分片的初始游标。
type InitialCursorFunc[C any, S any] func(shard S) C

// StreamExportShardedOptions 表示分片并发导出的参数。
type StreamExportShardedOptions[T any, C any, S any] struct {
	FilePath            string                  // 目标文件路径
	SheetName           string                  // 工作表名称
	Header              []any                   // 表头
	HeaderStyle         *excelize.Style         // 表头样式
	BatchSize           int                     // 每批读取数量
	MaxConcurrentShards int                     // 最大并发分片数
	ChunkBufferSize     int                     // 单分片结果缓冲大小
	MaxPendingChunks    int                     // 乱序结果暂存上限，用于保护慢分片场景下的导出内存
	RetryCount          int                     // 查询重试次数
	RetryBackoff        time.Duration           // 重试基础退避时间
	Shards              []ExportShard[S]        // 分片列表，顺序即全局写入顺序
	InitialCursor       InitialCursorFunc[C, S] // 分片初始游标
	Query               QueryShardFunc[T, C, S] // 分片查询函数
	BuildRows           BuildRowsFunc[T]        // 行构造函数
	Progress            ProgressFunc            // 进度回调
	ProgressMinInterval time.Duration           // 进度上报最小时间间隔
	ProgressMinRows     int64                   // 进度上报最小行数增量
}

// StreamExportShardedOpt 表示分片导出的函数式配置项。
type StreamExportShardedOpt[T any, C any, S any] func(*StreamExportShardedOptions[T, C, S])

// WithStreamExportSheetName 设置导出工作表名称。
func WithStreamExportSheetName[T any, C any, S any](sheetName string) StreamExportShardedOpt[T, C, S] {
	return func(opt *StreamExportShardedOptions[T, C, S]) {
		opt.SheetName = sheetName
	}
}

// WithStreamExportHeader 设置导出表头。
func WithStreamExportHeader[T any, C any, S any](header []any) StreamExportShardedOpt[T, C, S] {
	return func(opt *StreamExportShardedOptions[T, C, S]) {
		opt.Header = header
	}
}

// WithStreamExportHeaderStyle 设置导出表头样式。
func WithStreamExportHeaderStyle[T any, C any, S any](headerStyle *excelize.Style) StreamExportShardedOpt[T, C, S] {
	return func(opt *StreamExportShardedOptions[T, C, S]) {
		opt.HeaderStyle = headerStyle
	}
}

// WithStreamExportBatchSize 设置每批读取数量。
func WithStreamExportBatchSize[T any, C any, S any](batchSize int) StreamExportShardedOpt[T, C, S] {
	return func(opt *StreamExportShardedOptions[T, C, S]) {
		opt.BatchSize = batchSize
	}
}

// WithStreamExportMaxConcurrentShards 设置最大并发分片数。
func WithStreamExportMaxConcurrentShards[T any, C any, S any](maxConcurrentShards int) StreamExportShardedOpt[T, C, S] {
	return func(opt *StreamExportShardedOptions[T, C, S]) {
		opt.MaxConcurrentShards = maxConcurrentShards
	}
}

// WithStreamExportChunkBufferSize 设置单分片结果缓冲大小。
func WithStreamExportChunkBufferSize[T any, C any, S any](chunkBufferSize int) StreamExportShardedOpt[T, C, S] {
	return func(opt *StreamExportShardedOptions[T, C, S]) {
		opt.ChunkBufferSize = chunkBufferSize
	}
}

// WithStreamExportMaxPendingChunks 设置乱序结果暂存上限。
func WithStreamExportMaxPendingChunks[T any, C any, S any](maxPendingChunks int) StreamExportShardedOpt[T, C, S] {
	return func(opt *StreamExportShardedOptions[T, C, S]) {
		opt.MaxPendingChunks = maxPendingChunks
	}
}

// WithStreamExportRetry 设置查询重试次数和基础退避时间。
func WithStreamExportRetry[T any, C any, S any](retryCount int, retryBackoff time.Duration) StreamExportShardedOpt[T, C, S] {
	return func(opt *StreamExportShardedOptions[T, C, S]) {
		opt.RetryCount = retryCount
		opt.RetryBackoff = retryBackoff
	}
}

// WithStreamExportShards 设置分片列表。
func WithStreamExportShards[T any, C any, S any](shards []ExportShard[S]) StreamExportShardedOpt[T, C, S] {
	return func(opt *StreamExportShardedOptions[T, C, S]) {
		opt.Shards = shards
	}
}

// WithStreamExportInitialCursor 设置分片初始游标函数。
func WithStreamExportInitialCursor[T any, C any, S any](fn InitialCursorFunc[C, S]) StreamExportShardedOpt[T, C, S] {
	return func(opt *StreamExportShardedOptions[T, C, S]) {
		opt.InitialCursor = fn
	}
}

// WithStreamExportProgress 设置导出进度回调。
func WithStreamExportProgress[T any, C any, S any](progress ProgressFunc) StreamExportShardedOpt[T, C, S] {
	return func(opt *StreamExportShardedOptions[T, C, S]) {
		opt.Progress = progress
	}
}

// WithStreamExportProgressThrottle 设置导出进度上报节流阈值。
func WithStreamExportProgressThrottle[T any, C any, S any](minInterval time.Duration, minRows int64) StreamExportShardedOpt[T, C, S] {
	return func(opt *StreamExportShardedOptions[T, C, S]) {
		opt.ProgressMinInterval = minInterval
		opt.ProgressMinRows = minRows
	}
}

// ApplyStreamExportShardedOpts 将函数式配置项应用到基础配置。
func ApplyStreamExportShardedOpts[T any, C any, S any](base StreamExportShardedOptions[T, C, S], opts ...StreamExportShardedOpt[T, C, S]) StreamExportShardedOptions[T, C, S] {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&base)
	}
	return base
}

// exportChunk 表示单个分片查询完成后等待顺序写入的结果块。
type exportChunk struct {
	shardIndex int     // 分片序号
	sequence   int     // 分片内块序号
	rows       [][]any // 已转换好的 Excel 行
	itemCount  int     // 当前块对应的原始数据条数
	total      int64   // 分片或全局总数快照
	done       bool    // 当前分片是否已经读完
}

// progressReporter 记录最近一次进度上报时间和行数，用于节流。
type progressReporter struct {
	lastReportedAt time.Time // 最近一次上报时间
	lastRows       int64     // 最近一次上报时的已处理行数
}

// StreamExportSharded 以“分片并发查询 + 单线程顺序写入”方式生成 Excel 文件。
func StreamExportSharded[T any, C any, S any](ctx context.Context, opt StreamExportShardedOptions[T, C, S]) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if opt.Query == nil {
		return errors.Errorf("分片导出查询函数不能为空")
	}
	if opt.BuildRows == nil {
		return errors.Errorf("分片导出行构造函数不能为空")
	}
	if len(opt.Shards) == 0 {
		return errors.Errorf("分片导出至少需要一个分片")
	}
	if opt.InitialCursor == nil {
		return errors.Errorf("分片导出初始游标函数不能为空")
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
	if opt.MaxConcurrentShards <= 0 {
		opt.MaxConcurrentShards = DefaultMaxWorkerCount
	} else if opt.MaxConcurrentShards > MaxExportWorkerCount {
		opt.MaxConcurrentShards = MaxExportWorkerCount
	}
	if opt.ChunkBufferSize <= 0 {
		opt.ChunkBufferSize = DefaultChunkBufferSize
	} else if opt.ChunkBufferSize > MaxChunkBufferSize {
		opt.ChunkBufferSize = MaxChunkBufferSize
	}
	if opt.MaxPendingChunks <= 0 {
		opt.MaxPendingChunks = defaultPendingExportChunks(len(opt.Shards), opt.MaxConcurrentShards, opt.ChunkBufferSize)
	} else if opt.MaxPendingChunks > MaxPendingExportChunks {
		opt.MaxPendingChunks = MaxPendingExportChunks
	}
	if opt.RetryCount <= 0 {
		opt.RetryCount = DefaultRetryCount
	}
	if opt.RetryBackoff <= 0 {
		opt.RetryBackoff = 100 * time.Millisecond
	}
	if opt.ProgressMinInterval < 0 {
		opt.ProgressMinInterval = 0
	}
	if opt.ProgressMinRows < 0 {
		opt.ProgressMinRows = 0
	}
	// sheetRowLimit 表示本次导出的单表行数边界，异常配置回退到官方上限，避免突破 Excel 文件格式限制。
	sheetRowLimit := streamExportSheetRowLimit
	if sheetRowLimit <= 0 || sheetRowLimit > MaxExcelSheetRows {
		sheetRowLimit = MaxExcelSheetRows
	}

	workbook := excelize.NewFile()
	defer func() {
		_ = workbook.Close()
	}()

	defaultSheetName := workbook.GetSheetName(workbook.GetActiveSheetIndex())
	if defaultSheetName != opt.SheetName {
		workbook.SetSheetName(defaultSheetName, opt.SheetName)
	}
	streamWriter, err := workbook.NewStreamWriter(opt.SheetName)
	if err != nil {
		return errors.Wrap(err, "创建 Excel 流式写入器失败")
	}
	if err := writeExportHeader(workbook, streamWriter, opt.Header, opt.HeaderStyle); err != nil {
		return errors.Tag(err)
	}

	producerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resultCh := make(chan exportChunk, exportResultBufferSize(len(opt.Shards), opt.MaxConcurrentShards, opt.ChunkBufferSize))
	errCh := make(chan error, 1)
	sem := make(chan struct{}, opt.MaxConcurrentShards)
	var producerWG sync.WaitGroup

	for index, shard := range opt.Shards {
		producerWG.Add(1)
		go func(shardIndex int, shard ExportShard[S]) {
			defer producerWG.Done()

			select {
			case sem <- struct{}{}:
			case <-producerCtx.Done():
				return
			}
			defer func() {
				<-sem
			}()

			cursor := opt.InitialCursor(shard.Meta)
			sequence := 0
			for {
				page, err := queryShardWithRetry(producerCtx, shard.Meta, cursor, opt.BatchSize, opt.RetryCount, opt.RetryBackoff, opt.Query)
				if err != nil {
					sendExportError(errCh, errors.Wrapf(err, "导出分片[%d]查询失败", shardIndex))
					cancel()
					return
				}
				if page == nil || len(page.Items) == 0 {
					sendExportDone(producerCtx, resultCh, shardIndex, sequence)
					return
				}
				rows, err := opt.BuildRows(page.Items)
				if err != nil {
					sendExportError(errCh, errors.Wrapf(err, "导出分片[%d]构造数据行失败", shardIndex))
					cancel()
					return
				}
				select {
				case resultCh <- exportChunk{
					shardIndex: shardIndex,
					sequence:   sequence,
					rows:       rows,
					itemCount:  len(page.Items),
					total:      page.Total,
				}:
				case <-producerCtx.Done():
					return
				}
				sequence++
				if !page.HasMore {
					select {
					case resultCh <- exportChunk{
						shardIndex: shardIndex,
						sequence:   sequence,
						done:       true,
					}:
					case <-producerCtx.Done():
					}
					return
				}
				cursor = page.NextCursor
			}
		}(index, shard)
	}

	go func() {
		producerWG.Wait()
		close(resultCh)
	}()

	rowIndex := 2
	processed := int64(0)
	total := int64(0)
	startedAt := timeNow()
	reporter := &progressReporter{}
	pending := make(map[int]map[int]exportChunk, len(opt.Shards))
	completed := make(map[int]bool, len(opt.Shards))
	expectedShard := 0
	expectedSequence := 0

	for expectedShard < len(opt.Shards) {
		select {
		case err := <-errCh:
			cancel()
			for range resultCh {
			}
			return errors.Tag(err)
		case <-producerCtx.Done():
			for range resultCh {
			}
			if err := receiveExportError(errCh); err != nil {
				return errors.Tag(err)
			}
			if producerCtx.Err() != nil {
				return errors.Wrap(producerCtx.Err(), "分片导出已取消")
			}
			return nil
		case chunk, ok := <-resultCh:
			if !ok {
				if err := receiveExportError(errCh); err != nil {
					return errors.Tag(err)
				}
				expectedShard = len(opt.Shards)
				break
			}
			if chunk.done {
				completed[chunk.shardIndex] = true
			} else {
				if chunk.total > total {
					total = chunk.total
				}
				if pending[chunk.shardIndex] == nil {
					pending[chunk.shardIndex] = make(map[int]exportChunk, opt.ChunkBufferSize)
				}
				// isOutOfOrderChunk 表示当前结果暂时不能写入 Excel，只能先留在内存等待前置分片。
				isOutOfOrderChunk := chunk.shardIndex != expectedShard || chunk.sequence != expectedSequence
				pending[chunk.shardIndex][chunk.sequence] = chunk
				// 前置分片卡住时，后续分片可能持续返回结果；这里按 chunk 数量限流，避免大导出把乱序缓存撑到 OOM。
				if isOutOfOrderChunk {
					// pendingCount 表示当前乱序缓存压力，超过上限时主动取消生产协程并返回可追踪错误。
					pendingCount := pendingExportChunkCount(pending)
					if pendingCount > opt.MaxPendingChunks {
						cancel()
						for range resultCh {
						}
						return errors.Errorf("分片导出乱序缓存超限: pending_chunks=%d, limit=%d, shard_index=%d, sequence=%d", pendingCount, opt.MaxPendingChunks, chunk.shardIndex, chunk.sequence)
					}
				}
			}
			for expectedShard < len(opt.Shards) {
				chunkMap := pending[expectedShard]
				nextChunk, exists := chunkMap[expectedSequence]
				if exists {
					for _, row := range nextChunk.rows {
						if rowIndex > sheetRowLimit {
							cancel()
							for range resultCh {
							}
							limitErr := errors.Errorf("导出行数超过 Excel 单工作表最大限制[%d]行，已保留前[%d]行到文件[%s]", sheetRowLimit, rowIndex-1, opt.FilePath)
							return savePartialStreamExportWorkbook(workbook, streamWriter, opt.FilePath, limitErr)
						}
						cellName, err := excelize.CoordinatesToCellName(1, rowIndex)
						if err != nil {
							cancel()
							for range resultCh {
							}
							return errors.Wrap(err, "计算导出单元格坐标失败")
						}
						if err := streamWriter.SetRow(cellName, row); err != nil {
							cancel()
							for range resultCh {
							}
							return errors.Wrap(err, "写入导出数据行失败")
						}
						rowIndex++
					}
					delete(chunkMap, expectedSequence)
					if len(chunkMap) == 0 {
						delete(pending, expectedShard)
					}
					processed += int64(nextChunk.itemCount)
					if err := reportStreamExportProgress(opt, reporter, processed, total, startedAt, false); err != nil {
						cancel()
						for range resultCh {
						}
						return errors.Tag(err)
					}
					expectedSequence++
					continue
				}
				if completed[expectedShard] {
					delete(completed, expectedShard)
					delete(pending, expectedShard)
					expectedShard++
					expectedSequence = 0
					continue
				}
				break
			}
		}
	}

	if err := receiveExportError(errCh); err != nil {
		return errors.Tag(err)
	}
	if err := reportStreamExportProgress(opt, reporter, processed, total, startedAt, true); err != nil {
		return errors.Tag(err)
	}

	return saveStreamExportWorkbook(workbook, streamWriter, opt.FilePath)
}

// saveStreamExportWorkbook 将流式写入缓冲刷新并保存到目标路径。
// StreamWriter 只有在 Flush 后才会把已写入行合入工作簿，SaveAs 负责最终生成可被 Excel 打开的文件。
func saveStreamExportWorkbook(workbook *excelize.File, streamWriter *excelize.StreamWriter, filePath string) error {
	if err := streamWriter.Flush(); err != nil {
		return errors.Wrap(err, "刷新 Excel 流式缓冲失败")
	}
	if err := workbook.SaveAs(filePath); err != nil {
		return errors.Wrap(err, "保存导出文件失败")
	}
	return nil
}

// savePartialStreamExportWorkbook 在导出被行数保护中断时保存已完成的数据行。
// 如果部分文件保存失败，错误中同时保留原始中断原因，便于排查是数据越界还是落盘失败。
func savePartialStreamExportWorkbook(workbook *excelize.File, streamWriter *excelize.StreamWriter, filePath string, cause error) error {
	if err := saveStreamExportWorkbook(workbook, streamWriter, filePath); err != nil {
		return errors.Wrapf(err, "导出已中断且保存部分文件失败: cause=%v", cause)
	}
	return errors.Tag(cause)
}

// defaultPendingExportChunks 计算乱序结果暂存的默认上限。
// 默认值随当前并发和通道缓冲扩大，但不会随总分片数无限扩大，避免海量分片放大内存边界。
func defaultPendingExportChunks(shardCount int, maxConcurrentShards int, chunkBufferSize int) int {
	// size 来源于结果通道容量，保留 2 倍余量用于吸收正常的分片速度抖动。
	size := exportResultBufferSize(shardCount, maxConcurrentShards, chunkBufferSize) * 2
	if size < 1 {
		return 1
	}
	if size > MaxPendingExportChunks {
		return MaxPendingExportChunks
	}
	return size
}

// exportResultBufferSize 计算分片导出结果通道容量。
// 容量按并发分片数而非总分片数扩张，避免海量分片时分配过大的 channel 缓冲。
func exportResultBufferSize(shardCount int, maxConcurrentShards int, chunkBufferSize int) int {
	if shardCount <= 0 {
		return 1
	}
	if maxConcurrentShards <= 0 {
		maxConcurrentShards = DefaultMaxWorkerCount
	} else if maxConcurrentShards > MaxExportWorkerCount {
		maxConcurrentShards = MaxExportWorkerCount
	}
	if chunkBufferSize <= 0 {
		chunkBufferSize = DefaultChunkBufferSize
	} else if chunkBufferSize > MaxChunkBufferSize {
		chunkBufferSize = MaxChunkBufferSize
	}
	if maxConcurrentShards > shardCount {
		maxConcurrentShards = shardCount
	}
	size := maxConcurrentShards * chunkBufferSize
	if size < 1 {
		return 1
	}
	return size
}

// pendingExportChunkCount 统计当前乱序暂存的 chunk 数量。
// 统计维度使用 chunk 而不是行数，避免每次遍历所有行，同时与 BatchSize 的内存上限形成组合保护。
func pendingExportChunkCount(pending map[int]map[int]exportChunk) int {
	total := 0
	for _, chunkMap := range pending {
		total += len(chunkMap)
	}
	return total
}

// reportStreamExportProgress 按节流策略上报流式导出进度。
func reportStreamExportProgress[T any, C any, S any](opt StreamExportShardedOptions[T, C, S], reporter *progressReporter, processed int64, total int64, startedAt time.Time, force bool) error {
	if opt.Progress == nil {
		return nil
	}
	now := timeNow()
	if !force && !shouldReportStreamExportProgress(opt, reporter, processed, now) {
		return nil
	}
	progress, averageRowsPerSec, estimatedSeconds := BuildMetrics(total, processed, startedAt, now)
	if err := opt.Progress(ExportProgress{
		Processed:         processed,
		Total:             max(total, processed),
		Progress:          progress,
		AverageRowsPerSec: averageRowsPerSec,
		EstimatedSeconds:  estimatedSeconds,
		LastProcessedAt:   now,
	}); err != nil {
		return errors.Wrap(err, "保存导出进度失败")
	}
	reporter.lastReportedAt = now
	reporter.lastRows = processed
	return nil
}

// shouldReportStreamExportProgress 判断本次处理量或时间间隔是否达到进度上报条件。
func shouldReportStreamExportProgress[T any, C any, S any](opt StreamExportShardedOptions[T, C, S], reporter *progressReporter, processed int64, now time.Time) bool {
	if reporter == nil {
		return true
	}
	if reporter.lastReportedAt.IsZero() {
		return true
	}
	if opt.ProgressMinRows > 0 && processed-reporter.lastRows >= opt.ProgressMinRows {
		return true
	}
	if opt.ProgressMinInterval > 0 && now.Sub(reporter.lastReportedAt) >= opt.ProgressMinInterval {
		return true
	}
	return opt.ProgressMinRows <= 0 && opt.ProgressMinInterval <= 0
}

// queryShardWithRetry 在单个分片内执行带重试的游标查询。
// 重试只包裹读取动作，游标由调用方在成功后推进，避免失败重试时跳过或重复推进数据边界。
func queryShardWithRetry[T any, C any, S any](ctx context.Context, shard S, cursor C, limit int, retryCount int, retryBackoff time.Duration, query QueryShardFunc[T, C, S]) (*CursorPage[T, C], error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var lastErr error
	for index := 0; index < retryCount; index++ {
		if err := ctx.Err(); err != nil {
			return nil, errors.Tag(err)
		}
		page, err := query(ctx, shard, cursor, limit)
		if err == nil {
			return page, nil
		}
		lastErr = err
		if index == retryCount-1 {
			break
		}
		timer := time.NewTimer(streamExportRetryDelay(retryBackoff, index))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, errors.Tag(ctx.Err())
		case <-timer.C:
		}
	}
	if lastErr == nil {
		return nil, errors.Errorf("导出查询失败")
	}
	return nil, errors.Tag(lastErr)
}

// streamExportRetryDelay 按指数退避计算下一次重试等待时间。
// attempt 从 0 开始，首轮失败后等待 base，后续翻倍，并通过上限保护取消响应和任务时延。
func streamExportRetryDelay(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = 100 * time.Millisecond
	}
	if attempt < 0 {
		attempt = 0
	}
	// delay 表示本轮失败后的等待时间，按指数增长但不会超过全局保护上限。
	delay := base
	for i := 0; i < attempt; i++ {
		if delay >= maxStreamExportRetryBackoff/2 {
			return maxStreamExportRetryBackoff
		}
		delay *= 2
	}
	if delay > maxStreamExportRetryBackoff {
		return maxStreamExportRetryBackoff
	}
	return delay
}

// writeExportHeader 写入导出表头，并在需要时应用统一表头样式。
func writeExportHeader(workbook *excelize.File, streamWriter *excelize.StreamWriter, header []any, headerStyle *excelize.Style) error {
	if len(header) == 0 {
		return nil
	}
	if headerStyle != nil {
		headerStyleID, err := workbook.NewStyle(headerStyle)
		if err != nil {
			return errors.Wrap(err, "创建导出表头样式失败")
		}
		styledHeader := make([]any, 0, len(header))
		for _, column := range header {
			styledHeader = append(styledHeader, excelize.Cell{StyleID: headerStyleID, Value: column})
		}
		header = styledHeader
	}
	if err := streamWriter.SetRow("A1", header); err != nil {
		return errors.Wrap(err, "写入导出表头失败")
	}
	return nil
}

// sendExportError 非阻塞发送导出错误，避免多个 goroutine 同时失败时阻塞。
func sendExportError(errCh chan error, err error) {
	if err == nil {
		return
	}
	select {
	case errCh <- err:
	default:
	}
}

// receiveExportError 非阻塞读取导出错误，避免 resultCh 关闭时遗漏已上报错误。
func receiveExportError(errCh <-chan error) error {
	select {
	case err := <-errCh:
		return errors.Tag(err)
	default:
		return nil
	}
}

// sendExportDone 发送分片完成标记，确保空分片不会阻塞后续分片写入。
func sendExportDone(ctx context.Context, resultCh chan<- exportChunk, shardIndex int, sequence int) {
	select {
	case resultCh <- exportChunk{
		shardIndex: shardIndex,
		sequence:   sequence,
		done:       true,
	}:
	case <-ctx.Done():
	}
}
