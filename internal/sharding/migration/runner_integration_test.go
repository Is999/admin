package migration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"admin/common/idgen"
	"admin/internal/sharding"
)

// TestRunCopySameDatabase 验证同库逐级翻倍的全量、增量、写围栏、最终校验与清理闭环。
func TestRunCopySameDatabase(t *testing.T) {
	dsn := os.Getenv("INTEGRATION_MYSQL_DSN")
	if dsn == "" {
		t.Skip("未设置 INTEGRATION_MYSQL_DSN，跳过 MySQL 集成测试")
	}
	for _, counts := range [][2]int{{1, 2}, {2, 4}} {
		t.Run(fmt.Sprintf("%d_to_%d", counts[0], counts[1]), func(t *testing.T) {
			testRunCopySameDatabase(t, dsn, counts[0], counts[1])
		})
	}
}

// testRunCopySameDatabase 执行一个指定扩容档位的真实 MySQL 迁移用例。
func testRunCopySameDatabase(t *testing.T, dsn string, fromCount int, toCount int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database, err := OpenDatabase(dsn)
	if err != nil {
		t.Fatalf("OpenDatabase() error = %v", err)
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		t.Fatalf("PingContext() error = %v", err)
	}
	toPlan, err := sharding.NewPlan("user", toCount)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	if err := dropIntegrationTables(ctx, database, toPlan); err != nil {
		t.Fatalf("dropIntegrationTables(before) error = %v", err)
	}
	fromPlan, err := sharding.NewPlan("user", fromCount)
	if err != nil {
		t.Fatalf("NewPlan(from) error = %v", err)
	}
	for _, table := range fromPlan.Tables() {
		query := "CREATE TABLE " + quoteIdentifier(table.Name) + " (`id` bigint NOT NULL, `shard_no` int NOT NULL, `payload` varchar(64) NOT NULL, PRIMARY KEY (`id`), KEY `idx_shard_id` (`shard_no`,`id`)) ENGINE=InnoDB"
		if _, err := database.ExecContext(ctx, query); err != nil {
			t.Fatalf("create source table=%s error = %v", table.Name, err)
		}
	}
	defer func() {
		_ = dropIntegrationTables(context.Background(), database, toPlan)
	}()
	moves, err := sharding.ExpandMoves("user", fromCount, toCount)
	if err != nil {
		t.Fatalf("ExpandMoves() error = %v", err)
	}
	updateIDs := make([]int64, len(moves))
	updateShards := make([]int, len(moves))
	updateTables := make([]string, len(moves))
	inserts := make(map[string]*sql.Stmt, fromCount)
	for _, table := range fromPlan.Tables() {
		statement, prepareErr := database.PrepareContext(
			ctx,
			"INSERT INTO "+quoteIdentifier(table.Name)+" (`id`,`shard_no`,`payload`) VALUES (?,?,?)",
		)
		if prepareErr != nil {
			t.Fatalf("PrepareContext(insert table=%s) error = %v", table.Name, prepareErr)
		}
		inserts[table.Name] = statement
	}
	for id := int64(1); id <= 5000; id++ {
		shardNo := idgen.ShardNo(id)
		source, sourceErr := fromPlan.TableForBucket(shardNo)
		if sourceErr != nil {
			t.Fatalf("TableForBucket(%d) error = %v", shardNo, sourceErr)
		}
		if _, err := inserts[source.Name].ExecContext(ctx, id, shardNo, "initial"); err != nil {
			t.Fatalf("insert id=%d error = %v", id, err)
		}
		for index, move := range moves {
			if updateIDs[index] == 0 && shardNo >= move.BucketStart && shardNo <= move.BucketEnd {
				updateIDs[index] = id
				updateShards[index] = shardNo
				updateTables[index] = move.Source
			}
		}
	}
	for table, statement := range inserts {
		if err := statement.Close(); err != nil {
			t.Fatalf("close insert statement table=%s error = %v", table, err)
		}
	}
	for index, id := range updateIDs {
		if id == 0 {
			t.Fatalf("迁移区间没有增量测试行 move=%+v", moves[index])
		}
	}
	opts := PrepareOptions{
		FirstTable:   "user",
		UIDColumn:    "id",
		ShardColumn:  "shard_no",
		CursorColumn: "id",
		FromCount:    fromCount,
		ToCount:      toCount,
	}
	if fromCount == 1 {
		correctShard := idgen.ShardNo(1)
		wrongShard := (correctShard + 1) % sharding.BucketTotal
		source, sourceErr := fromPlan.TableForBucket(correctShard)
		if sourceErr != nil {
			t.Fatalf("TableForBucket(wrong source) error = %v", sourceErr)
		}
		if _, err := database.ExecContext(ctx, "UPDATE "+quoteIdentifier(source.Name)+" SET `shard_no` = ? WHERE `id` = 1", wrongShard); err != nil {
			t.Fatalf("write wrong source shard error = %v", err)
		}
		if _, err := Prepare(ctx, database, opts); err == nil {
			t.Fatal("Prepare() 应拒绝仍在源表范围内但不符合 UID 公式的固定桶")
		}
		if _, err := database.ExecContext(ctx, "UPDATE "+quoteIdentifier(source.Name)+" SET `shard_no` = ? WHERE `id` = 1", correctShard); err != nil {
			t.Fatalf("restore source shard error = %v", err)
		}
		if _, err := database.ExecContext(ctx, "UPDATE "+quoteIdentifier(source.Name)+" SET `shard_no` = -1 WHERE `id` = 1"); err != nil {
			t.Fatalf("write source shard outside range error = %v", err)
		}
		if _, err := Prepare(ctx, database, opts); err == nil {
			t.Fatal("Prepare() 应拒绝物理表桶范围外数据")
		}
		if _, err := database.ExecContext(ctx, "UPDATE "+quoteIdentifier(source.Name)+" SET `shard_no` = ? WHERE `id` = 1", correctShard); err != nil {
			t.Fatalf("restore source shard after range check error = %v", err)
		}
	}
	if _, err := Prepare(ctx, database, opts); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	release, err := AcquireLock(ctx, database, opts)
	if err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	lockHeld := true
	defer func() {
		if lockHeld {
			release()
		}
	}()
	if _, err := ValidateCopy(ctx, database, opts); err != nil {
		t.Fatalf("ValidateCopy() error = %v", err)
	}
	writerStop := make(chan struct{})
	writerDone := make(chan struct{})
	writerErr := make(chan error, 1)
	var stopOnce sync.Once
	var revision atomic.Uint64
	go func() {
		defer close(writerDone)
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-writerStop:
				return
			case <-ticker.C:
				next := revision.Add(1)
				index := int(next-1) % len(updateIDs)
				if _, err := database.ExecContext(
					ctx,
					"UPDATE "+quoteIdentifier(updateTables[index])+" SET `payload` = ? WHERE `shard_no` = ? AND `id` = ?",
					next,
					updateShards[index],
					updateIDs[index],
				); err != nil {
					writerErr <- err
					return
				}
			}
		}
	}()
	var verified atomic.Bool
	var fenceReleased atomic.Bool
	result, err := RunCopy(ctx, CopyOptions{
		DSN:          dsn,
		FirstTable:   opts.FirstTable,
		UIDColumn:    opts.UIDColumn,
		ShardColumn:  opts.ShardColumn,
		CursorColumn: opts.CursorColumn,
		FromCount:    opts.FromCount,
		ToCount:      opts.ToCount,
		BatchSize:    100,
		MaxDowntime:  30 * time.Second,
		CutoverWait: func() error {
			stopOnce.Do(func() { close(writerStop) })
			<-writerDone
			return nil
		},
		CutoverFence: func(fenceCtx context.Context) (func(), error) {
			releaseFence, fenceErr := AcquireSourceReadLock(fenceCtx, database, opts)
			if fenceErr != nil {
				return nil, fenceErr
			}
			return func() {
				fenceReleased.Store(true)
				releaseFence()
			}, nil
		},
		Verified: func(_ []sharding.Move) error {
			if fenceReleased.Load() {
				return fmt.Errorf("最终校验凭证创建前写围栏已释放")
			}
			verified.Store(true)
			return nil
		},
	})
	stopOnce.Do(func() { close(writerStop) })
	<-writerDone
	select {
	case err := <-writerErr:
		t.Fatalf("concurrent source update error = %v", err)
	default:
	}
	if err != nil {
		t.Fatalf("RunCopy() error = %v", err)
	}
	if len(result.Moves) != len(moves) || !verified.Load() || !fenceReleased.Load() || revision.Load() < uint64(len(moves)) {
		t.Fatalf(
			"RunCopy() result=%+v verified=%t fence_released=%t revisions=%d",
			result,
			verified.Load(),
			fenceReleased.Load(),
			revision.Load(),
		)
	}
	var targetRows int64
	for _, move := range moves {
		var mismatches int
		query := fmt.Sprintf(`
SELECT COUNT(*)
FROM %s AS source
LEFT JOIN %s AS target ON target.id = source.id
WHERE source.shard_no BETWEEN ? AND ?
  AND (target.id IS NULL OR target.shard_no <> source.shard_no OR target.payload <> source.payload)
`, quoteIdentifier(move.Source), quoteIdentifier(move.Target))
		if err := database.QueryRowContext(ctx, query, move.BucketStart, move.BucketEnd).Scan(&mismatches); err != nil {
			t.Fatalf("compare source=%s target=%s error = %v", move.Source, move.Target, err)
		}
		if mismatches != 0 {
			t.Fatalf("source=%s target=%s mismatches=%d", move.Source, move.Target, mismatches)
		}
		var rows int64
		countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdentifier(move.Target))
		if err := database.QueryRowContext(ctx, countQuery).Scan(&rows); err != nil {
			t.Fatalf("count target=%s rows error = %v", move.Target, err)
		}
		targetRows += rows
	}
	release()
	lockHeld = false
	guardMove := moves[0]
	guardID := updateIDs[0]
	correctTargetShard := updateShards[0]
	wrongTargetShard := correctTargetShard + 1
	if wrongTargetShard > guardMove.BucketEnd {
		wrongTargetShard = correctTargetShard - 1
	}
	if _, err := database.ExecContext(
		ctx,
		"UPDATE "+quoteIdentifier(guardMove.Target)+" SET `shard_no` = ? WHERE `id` = ?",
		wrongTargetShard,
		guardID,
	); err != nil {
		t.Fatalf("write wrong target shard error = %v", err)
	}
	cleanupOptions := CleanupOptions{
		PrepareOptions: opts,
		BatchSize:      73,
	}
	if _, err := Cleanup(ctx, database, cleanupOptions); err == nil {
		t.Fatal("Cleanup() 应在删除前拒绝目标表 UID 固定桶公式错误")
	}
	if _, err := database.ExecContext(
		ctx,
		"UPDATE "+quoteIdentifier(guardMove.Target)+" SET `shard_no` = ? WHERE `id` = ?",
		correctTargetShard,
		guardID,
	); err != nil {
		t.Fatalf("restore target shard error = %v", err)
	}
	deleted, err := Cleanup(ctx, database, cleanupOptions)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if deleted != targetRows {
		t.Fatalf("Cleanup() deleted=%d target_rows=%d", deleted, targetRows)
	}
	for _, move := range moves {
		var remaining int64
		query := fmt.Sprintf(
			"SELECT COUNT(*) FROM %s WHERE `shard_no` BETWEEN ? AND ?",
			quoteIdentifier(move.Source),
		)
		if err := database.QueryRowContext(ctx, query, move.BucketStart, move.BucketEnd).Scan(&remaining); err != nil {
			t.Fatalf("count remaining source=%s error = %v", move.Source, err)
		}
		if remaining != 0 {
			t.Fatalf("migrated source=%s range=%d-%d rows remain=%d", move.Source, move.BucketStart, move.BucketEnd, remaining)
		}
	}
}

// dropIntegrationTables 清理当前集成测试扩容计划的全部物理表。
func dropIntegrationTables(ctx context.Context, database *sql.DB, plan sharding.Plan) error {
	tables := plan.Tables()
	for index := len(tables) - 1; index >= 0; index-- {
		table := tables[index]
		if _, err := database.ExecContext(ctx, "DROP TABLE IF EXISTS "+quoteIdentifier(table.Name)); err != nil {
			return err
		}
	}
	return nil
}
