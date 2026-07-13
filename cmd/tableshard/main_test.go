package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"admin/internal/sharding"
)

// TestParseOptionsAcceptsDoubling 验证计划动作允许逐级翻倍扩容。
func TestParseOptionsAcceptsDoubling(t *testing.T) {
	opts, err := parseOptions([]string{
		"-action", "plan",
		"-first-table", "user_tag_0",
		"-from-count", "2",
		"-to-count", "4",
	})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if opts.FirstTable != "user_tag_0" || opts.UIDColumn != "uid" || opts.ToCount != 4 {
		t.Fatalf("unexpected options: %+v", opts)
	}
}

// TestParseOptionsRejectsDirectFourfoldExpansion 验证同一源表不能并发拆到三个目标表。
func TestParseOptionsRejectsDirectFourfoldExpansion(t *testing.T) {
	if _, err := parseOptions([]string{
		"-action", "plan",
		"-first-table", "user_tag_0",
		"-from-count", "1",
		"-to-count", "4",
	}); err == nil {
		t.Fatal("期望单次四倍扩容被拒绝")
	}
}

// TestParseOptionsRejectsInvalidIdentifiers 验证 SQL 标识符不能由任意文本注入。
func TestParseOptionsRejectsInvalidIdentifiers(t *testing.T) {
	if _, err := parseOptions([]string{
		"-action", "plan",
		"-first-table", "user;drop",
		"-from-count", "1",
		"-to-count", "2",
	}); err == nil {
		t.Fatal("期望非法物理表名返回错误")
	}
}

// TestCleanupConfirmationIsExplicit 验证清理确认文本绑定表名和扩容档位。
func TestCleanupConfirmationIsExplicit(t *testing.T) {
	got := cleanupConfirmation(options{FirstTable: "user", FromCount: 2, ToCount: 4}, "001122")
	if got != "user:2->4:001122" {
		t.Fatalf("cleanupConfirmation() = %q", got)
	}
}

// TestCutoverFileMatchesOneTimeToken 验证维护放行文件必须携带本轮一次性令牌。
func TestCutoverFileMatchesOneTimeToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cutover")
	if matched, err := cutoverFileMatches(path, "expected"); err != nil || matched {
		t.Fatalf("missing marker result = (%t, %v)", matched, err)
	}
	if err := os.WriteFile(path, []byte("wrong\n"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	if _, err := cutoverFileMatches(path, "expected"); err == nil {
		t.Fatal("期望错误切换令牌被拒绝")
	}
	if err := os.WriteFile(path, []byte("expected\n"), 0o600); err != nil {
		t.Fatalf("rewrite marker: %v", err)
	}
	if matched, err := cutoverFileMatches(path, "expected"); err != nil || !matched {
		t.Fatalf("valid marker result = (%t, %v)", matched, err)
	}
}

// TestCutoverFileRejectsUnsafeTypes 验证停写标记不会跟随符号链接或读取异常大文件。
func TestCutoverFileRejectsUnsafeTypes(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("expected\n"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(directory, "cutover-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := cutoverFileMatches(link, "expected"); err == nil {
		t.Fatal("期望符号链接停写标记被拒绝")
	}
	oversized := filepath.Join(directory, "cutover-oversized")
	if err := os.WriteFile(oversized, make([]byte, maxCutoverFileSize+1), 0o600); err != nil {
		t.Fatalf("write oversized marker: %v", err)
	}
	if _, err := cutoverFileMatches(oversized, "expected"); err == nil {
		t.Fatal("期望超大停写标记被拒绝")
	}
}

// TestValidateCopyMarkerPathsRejectsDanglingSymlink 验证启动门禁不会把悬空链接当作未使用路径。
func TestValidateCopyMarkerPathsRejectsDanglingSymlink(t *testing.T) {
	directory := t.TempDir()
	ready := filepath.Join(directory, "ready")
	if err := os.Symlink(filepath.Join(directory, "missing"), ready); err != nil {
		t.Fatalf("create dangling symlink: %v", err)
	}
	if err := validateCopyMarkerPaths(options{
		ReadyFile:    ready,
		CutoverFile:  filepath.Join(directory, "cutover"),
		VerifiedFile: filepath.Join(directory, "verified"),
	}); err == nil {
		t.Fatal("期望悬空符号链接路径被拒绝")
	}
}

// TestNewCutoverTokenUses128Bits 验证维护切换令牌包含 128 位随机值。
func TestNewCutoverTokenUses128Bits(t *testing.T) {
	token, err := newCutoverToken()
	if err != nil {
		t.Fatalf("newCutoverToken() error = %v", err)
	}
	raw, err := hex.DecodeString(token)
	if err != nil || len(raw) != 16 {
		t.Fatalf("unexpected token %q bytes=%d err=%v", token, len(raw), err)
	}
}

// TestValidateVerifiedMarkerBindsPlanAndDatabase 验证 cleanup 凭证不能跨数据库或扩容计划复用。
func TestValidateVerifiedMarkerBindsPlanAndDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "verified")
	opts := options{
		FirstTable:   "user",
		UIDColumn:    "id",
		ShardColumn:  "shard_no",
		CursorColumn: "id",
		FromCount:    1,
		ToCount:      2,
	}
	moves, err := sharding.ExpandMoves(opts.FirstTable, opts.FromCount, opts.ToCount)
	if err != nil {
		t.Fatalf("ExpandMoves() error = %v", err)
	}
	if err := createMarker(path, marker{
		Status:              markerStatusVerified,
		Token:               "00112233445566778899aabbccddeeff",
		Table:               opts.FirstTable,
		UIDColumn:           opts.UIDColumn,
		ShardColumn:         opts.ShardColumn,
		CursorColumn:        opts.CursorColumn,
		FromCount:           opts.FromCount,
		ToCount:             opts.ToCount,
		Moves:               moves,
		DatabaseFingerprint: "database-a",
		CreatedAt:           time.Now(),
	}); err != nil {
		t.Fatalf("createMarker() error = %v", err)
	}
	value, err := validateVerifiedMarker(path, opts, moves, "database-a")
	if err != nil {
		t.Fatalf("validateVerifiedMarker() error = %v", err)
	}
	if value.Token != "00112233445566778899aabbccddeeff" {
		t.Fatalf("validateVerifiedMarker() token = %q", value.Token)
	}
	if _, err := validateVerifiedMarker(path, opts, moves, "database-b"); err == nil {
		t.Fatal("期望跨数据库复用最终校验凭证被拒绝")
	}
	opts.UIDColumn = "uid"
	if _, err := validateVerifiedMarker(path, opts, moves, "database-a"); err == nil {
		t.Fatal("期望不同 UID 字段复用最终校验凭证被拒绝")
	}
}

// TestValidateVerifiedMarkerSupportsLargestPlan 验证最大扩容计划生成的合法凭证仍可用于清理。
func TestValidateVerifiedMarkerSupportsLargestPlan(t *testing.T) {
	opts := options{
		FirstTable:   strings.Repeat("a", 58),
		UIDColumn:    "id",
		ShardColumn:  "shard_no",
		CursorColumn: "id",
		FromCount:    512,
		ToCount:      1024,
	}
	moves, err := sharding.ExpandMoves(opts.FirstTable, opts.FromCount, opts.ToCount)
	if err != nil {
		t.Fatalf("ExpandMoves() error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "verified")
	if err := createMarker(path, newMarker(
		opts,
		markerStatusVerified,
		"00112233445566778899aabbccddeeff",
		moves,
		"database-a",
	)); err != nil {
		t.Fatalf("createMarker() error = %v", err)
	}
	if _, err := validateVerifiedMarker(path, opts, moves, "database-a"); err != nil {
		t.Fatalf("validateVerifiedMarker() error = %v", err)
	}
}

// TestParseOptionsRequiresUIDColumnForCustomTable 验证自定义 UID 表必须显式绑定公式字段。
func TestParseOptionsRequiresUIDColumnForCustomTable(t *testing.T) {
	if _, err := parseOptions([]string{
		"-action", "plan",
		"-first-table", "user_log",
		"-from-count", "1",
		"-to-count", "2",
	}); err == nil {
		t.Fatal("期望自定义 UID 表缺少 uid-column 时返回错误")
	}
}
