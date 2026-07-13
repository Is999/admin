package main

import (
	"context"
	"database/sql"
	"embed"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"admin/common/embedasset"

	_ "github.com/go-sql-driver/mysql"
)

// integrationAssets 嵌入真实 MySQL 集成测试使用的固定 SQL 资产。
//
//go:embed testdata/*.sql.tmpl
var integrationAssets embed.FS

// TestBackfillResumesAndVerifiesOnMySQL 使用真实 MySQL 验证批次提交、断点恢复、全量校验和重新修复。
func TestBackfillResumesAndVerifiesOnMySQL(t *testing.T) {
	dsn := os.Getenv("SHARD_BACKFILL_TEST_DSN")
	if dsn == "" {
		t.Skip("SHARD_BACKFILL_TEST_DSN 未配置，跳过真实 MySQL 回填验证")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	db.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		t.Fatalf("PingContext() error = %v", err)
	}

	const table = "shard_backfill_case"
	const job = "shard_backfill_case"
	const insertTrigger = "shard_backfill_case_bi"
	const updateTrigger = "shard_backfill_case_bu"
	var testTableExists bool
	if err = db.QueryRowContext(ctx, integrationSQL(t, "test-table-exists.sql.tmpl")).Scan(&testTableExists); err != nil {
		t.Fatalf("check test table error = %v", err)
	}
	if testTableExists {
		t.Fatal("test table shard_backfill_case already exists; refusing destructive cleanup")
	}
	var checkpointExisted bool
	if err = db.QueryRowContext(ctx, integrationSQL(t, "checkpoint-exists.sql.tmpl")).Scan(&checkpointExisted); err != nil {
		t.Fatalf("check checkpoint table error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(integrationSQL(t, "delete-checkpoint.sql.tmpl"), job)
		_, _ = db.Exec(integrationSQL(t, "drop-table.sql.tmpl"))
		if !checkpointExisted {
			_, _ = db.Exec(integrationSQL(t, "drop-checkpoint.sql.tmpl"))
		}
	})
	if err = initCheckpointTable(ctx, db, io.Discard); err != nil {
		t.Fatalf("initCheckpointTable() error = %v", err)
	}
	if _, err = db.ExecContext(ctx, integrationSQL(t, "delete-checkpoint.sql.tmpl"), job); err != nil {
		t.Fatalf("delete test checkpoint error = %v", err)
	}
	if _, err = db.ExecContext(ctx, integrationSQL(t, "create-table.sql.tmpl")); err != nil {
		t.Fatalf("create test table error = %v", err)
	}
	if _, err = db.ExecContext(ctx, integrationSQL(t, "seed.sql.tmpl")); err != nil {
		t.Fatalf("insert test rows error = %v", err)
	}
	createSourceGuards(t, ctx, db)
	for _, asset := range []string{"insert-zero-primary-key.sql.tmpl", "insert-zero-uid.sql.tmpl"} {
		if _, insertErr := db.ExecContext(ctx, integrationSQL(t, asset)); insertErr == nil {
			t.Fatalf("%s should be rejected by source insert guard", asset)
		}
	}

	opts := options{
		Action:        actionRun,
		Job:           job,
		Table:         table,
		PrimaryKey:    "id",
		UIDColumn:     "uid",
		InsertTrigger: insertTrigger,
		UpdateTrigger: updateTrigger,
		RangeEnd:      6,
		BatchSize:     2,
		BatchTimeout:  5 * time.Second,
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("db.Conn() error = %v", err)
	}
	if err = ensureCheckpoint(ctx, conn, opts); err != nil {
		conn.Close()
		t.Fatalf("ensureCheckpoint() error = %v", err)
	}
	first, err := backfillBatch(ctx, conn, opts)
	if closeErr := conn.Close(); closeErr != nil {
		t.Fatalf("conn.Close() error = %v", closeErr)
	}
	if err != nil {
		t.Fatalf("backfillBatch() error = %v", err)
	}
	if first.Checkpoint.Cursor != 2 || first.Rows != 2 {
		t.Fatalf("first batch = %+v, want cursor=2 rows=2", first)
	}
	if err = runBatches(ctx, db, opts, io.Discard); err != nil {
		t.Fatalf("resumed runBatches() error = %v", err)
	}
	assertMismatchCount(t, ctx, db, table, 0)

	verify := opts
	verify.Action = actionVerify
	if err = runBatches(ctx, db, verify, io.Discard); err != nil {
		t.Fatalf("verify runBatches() error = %v", err)
	}
	current, err := loadCheckpoint(ctx, db, job, false)
	if err != nil {
		t.Fatalf("loadCheckpoint() error = %v", err)
	}
	if current.Status != statusVerified || current.VerifiedRows != 6 {
		t.Fatalf("verified checkpoint = %+v, want status=verified rows=6", current)
	}

	if _, err = db.ExecContext(ctx, integrationSQL(t, "drop-update-trigger.sql.tmpl")); err != nil {
		t.Fatalf("drop update guard error = %v", err)
	}
	if _, err = db.ExecContext(ctx, integrationSQL(t, "corrupt-row.sql.tmpl")); err != nil {
		t.Fatalf("corrupt test row error = %v", err)
	}
	createUpdateGuard(t, ctx, db)
	verify.Restart = true
	if err = runBatches(ctx, db, verify, io.Discard); err == nil {
		t.Fatal("verify should report formula mismatch")
	}
	current, err = loadCheckpoint(ctx, db, job, false)
	if err != nil {
		t.Fatalf("load mismatch checkpoint error = %v", err)
	}
	if current.Status != statusMismatch || current.MismatchRows != 1 {
		t.Fatalf("mismatch checkpoint = %+v, want one mismatch", current)
	}

	opts.Restart = true
	if err = runBatches(ctx, db, opts, io.Discard); err != nil {
		t.Fatalf("restart backfill error = %v", err)
	}
	if err = runBatches(ctx, db, verify, io.Discard); err != nil {
		t.Fatalf("restart verify error = %v", err)
	}
	assertMismatchCount(t, ctx, db, table, 0)
}

// createSourceGuards 创建和生产模板同语义的测试源表保护触发器。
func createSourceGuards(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, integrationSQL(t, "create-insert-trigger.sql.tmpl")); err != nil {
		t.Fatalf("create insert guard error = %v", err)
	}
	createUpdateGuard(t, ctx, db)
}

// createUpdateGuard 创建和生产模板同语义的测试更新保护触发器。
func createUpdateGuard(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, integrationSQL(t, "create-update-trigger.sql.tmpl")); err != nil {
		t.Fatalf("create update guard error = %v", err)
	}
}

// assertMismatchCount 使用 MySQL 公式检查真实测试表中的不一致数量。
func assertMismatchCount(t *testing.T, ctx context.Context, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, integrationSQL(t, "mismatch-count.sql.tmpl")).Scan(&got); err != nil {
		t.Fatalf("query mismatch count error = %v", err)
	}
	if got != want {
		t.Fatalf("mismatch count = %d, want %d", got, want)
	}
}

// integrationSQL 读取并剥离真实 MySQL 集成测试 SQL 的文件头说明。
func integrationSQL(t *testing.T, name string) string {
	t.Helper()
	data, err := integrationAssets.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read integration SQL %s error = %v", name, err)
	}
	return strings.TrimSpace(embedasset.StripLeadingLineComments(string(data), "--"))
}
