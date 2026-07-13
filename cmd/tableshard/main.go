// tableshard 在应用仓库内执行 UID 固定桶物理扩容，不需要部署额外代理服务。
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"admin/internal/bootstrap/configload"
	"admin/internal/config"
	"admin/internal/model"
	"admin/internal/sharding"
	"admin/internal/sharding/migration"

	"github.com/Is999/go-utils/errors"
)

const (
	// actionPlan 只输出扩容桶区间，不连接数据库。
	actionPlan = "plan"
	// actionPrepare 创建并校验空目标物理表。
	actionPrepare = "prepare"
	// actionCopy 在线预复制并等待短暂停写收口。
	actionCopy = "copy"
	// actionCleanup 在观察期结束后分批清理旧表已迁移桶。
	actionCleanup = "cleanup"
	// migrationDSNEnv 是短期专用迁移账号 DSN 的环境变量名。
	migrationDSNEnv = "TABLESHARD_DSN"
	// markerStatusReady 表示在线复制已接近追平，可以进入维护期。
	markerStatusReady = "ready_for_maintenance"
	// markerStatusVerified 表示最终追平和校验已在数据库写围栏内通过。
	markerStatusVerified = "copy_verified"
	// maxMarkerFileSize 覆盖 512→1024 的最大合法计划，并限制异常凭证大小。
	maxMarkerFileSize = 256 * 1024
	// maxCutoverFileSize 限制停写令牌文件大小，避免读取异常文件。
	maxCutoverFileSize = 128
)

// options 定义一次应用内物理拆表命令参数。
type options struct {
	Action         string        // 动作：plan/prepare/copy/cleanup
	ConfigFile     string        // 应用配置文件
	FirstTable     string        // 起始桶物理表
	UIDColumn      string        // 业务用户 ID 字段
	ShardColumn    string        // 固定桶字段
	CursorColumn   string        // 唯一数字游标字段
	FromCount      int           // 当前物理分片数
	ToCount        int           // 目标物理分片数
	ActiveCount    int           // 未注册业务表的当前应用路由数
	BatchSize      uint64        // 在线复制单批行数
	CleanupBatch   int           // 旧数据单批删除行数
	CleanupDelay   time.Duration // 清理批次间隔
	ReadyFile      string        // 可进入维护期的就绪标记
	CutoverFile    string        // 运维确认停写完成的放行标记
	VerifiedFile   string        // 最终追平和校验成功凭证
	CutoverTimeout time.Duration // 等待停写确认的最长时间
	MaxDowntime    time.Duration // 最终追平和校验维护窗口上限
	TLSCA          string        // MySQL TLS CA 文件
	TLSServerName  string        // MySQL TLS 服务端名称
	ConfirmCleanup string        // 清理二次确认文本
}

// marker 保存复制阶段写入磁盘的机器可读状态。
type marker struct {
	Status              string          `json:"status"`               // 当前状态
	Token               string          `json:"token"`                // 本次维护切换一次性令牌
	Table               string          `json:"table"`                // 起始桶物理表
	UIDColumn           string          `json:"uid_column"`           // 业务用户 ID 字段
	ShardColumn         string          `json:"shard_column"`         // 固定桶字段
	CursorColumn        string          `json:"cursor_column"`        // 唯一数字游标字段
	FromCount           int             `json:"from_count"`           // 当前物理分片数
	ToCount             int             `json:"to_count"`             // 目标物理分片数
	Moves               []sharding.Move `json:"moves"`                // 待切换桶区间
	DatabaseFingerprint string          `json:"database_fingerprint"` // 不含凭据的数据库目标指纹
	CreatedAt           time.Time       `json:"created_at"`           // 标记创建时间
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run 解析参数并执行单一运维动作。
func run(args []string, output io.Writer) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	moves, err := sharding.ExpandMoves(opts.FirstTable, opts.FromCount, opts.ToCount)
	if err != nil {
		return err
	}
	if opts.Action == actionPlan {
		return writeJSON(output, moves)
	}
	cfg, err := configload.Load(opts.ConfigFile)
	if err != nil {
		return errors.Wrap(err, "加载应用配置失败")
	}
	activeCount, err := activeRouteCount(cfg, opts)
	if err != nil {
		return err
	}
	if activeCount != opts.FromCount && opts.Action != actionCleanup {
		return errors.Errorf("当前应用路由数必须等于 from_count active=%d from=%d", activeCount, opts.FromCount)
	}
	if activeCount != opts.ToCount && opts.Action == actionCleanup {
		return errors.Errorf("清理前必须先完成应用路由切换 active=%d target=%d", activeCount, opts.ToCount)
	}
	migrationDSN, err := loadMigrationDSN(cfg.MySQL.WriteDataSource)
	if err != nil {
		return err
	}
	databaseFingerprint, err := migration.DatabaseFingerprint(migrationDSN)
	if err != nil {
		return err
	}
	database, err := migration.OpenDatabase(migrationDSN)
	if err != nil {
		return err
	}
	defer database.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	prepareOptions := migration.PrepareOptions{
		FirstTable:   opts.FirstTable,
		UIDColumn:    opts.UIDColumn,
		ShardColumn:  opts.ShardColumn,
		CursorColumn: opts.CursorColumn,
		FromCount:    opts.FromCount,
		ToCount:      opts.ToCount,
	}
	switch opts.Action {
	case actionPrepare:
		prepared, err := migration.Prepare(ctx, database, prepareOptions)
		if err != nil {
			return err
		}
		return writeJSON(output, prepared)
	case actionCopy:
		release, err := migration.AcquireLock(ctx, database, prepareOptions)
		if err != nil {
			return err
		}
		defer release()
		if _, err := migration.ValidateCopy(ctx, database, prepareOptions); err != nil {
			return err
		}
		if err := validateCopyMarkerPaths(opts); err != nil {
			return err
		}
		cutoverToken, err := newCutoverToken()
		if err != nil {
			return err
		}
		result, err := migration.RunCopy(ctx, migration.CopyOptions{
			DSN:          migrationDSN,
			TLS:          migration.TLSOptions{CAPath: opts.TLSCA, ServerName: opts.TLSServerName},
			FirstTable:   opts.FirstTable,
			UIDColumn:    opts.UIDColumn,
			ShardColumn:  opts.ShardColumn,
			CursorColumn: opts.CursorColumn,
			FromCount:    opts.FromCount,
			ToCount:      opts.ToCount,
			BatchSize:    opts.BatchSize,
			MaxDowntime:  opts.MaxDowntime,
			Ready: func(readyMoves []sharding.Move) error {
				return createMarker(opts.ReadyFile, newMarker(opts, markerStatusReady, cutoverToken, readyMoves, databaseFingerprint))
			},
			CutoverWait: func() error {
				return waitForCutoverFile(ctx, opts.CutoverFile, cutoverToken, opts.CutoverTimeout)
			},
			CutoverFence: func(fenceCtx context.Context) (func(), error) {
				return migration.AcquireSourceReadLock(fenceCtx, database, prepareOptions)
			},
			Verified: func(verifiedMoves []sharding.Move) error {
				return createMarker(opts.VerifiedFile, newMarker(opts, markerStatusVerified, cutoverToken, verifiedMoves, databaseFingerprint))
			},
		})
		if err != nil {
			return err
		}
		return writeJSON(output, result)
	case actionCleanup:
		verified, err := validateVerifiedMarker(opts.VerifiedFile, opts, moves, databaseFingerprint)
		if err != nil {
			return err
		}
		want := cleanupConfirmation(opts, verified.Token)
		if opts.ConfirmCleanup != want {
			return errors.Errorf("清理确认不匹配，必须显式传入 -confirm-cleanup %q", want)
		}
		deleted, err := migration.Cleanup(ctx, database, migration.CleanupOptions{
			PrepareOptions: prepareOptions,
			BatchSize:      opts.CleanupBatch,
			Delay:          opts.CleanupDelay,
		})
		if err != nil {
			return err
		}
		return writeJSON(output, map[string]any{"deleted_rows": deleted, "moves": moves})
	default:
		return errors.New("action 必须是 plan/prepare/copy/cleanup")
	}
}

// parseOptions 使用独立 FlagSet 解析并校验资源边界。
func parseOptions(args []string) (options, error) {
	opts := options{}
	flags := flag.NewFlagSet("tableshard", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.Action, "action", "", "动作：plan/prepare/copy/cleanup")
	flags.StringVar(&opts.ConfigFile, "f", "./etc/config.yaml", "应用配置文件")
	flags.StringVar(&opts.FirstTable, "first-table", "", "起始桶物理表，例如 user 或 user_tag_0")
	flags.StringVar(&opts.UIDColumn, "uid-column", "", "业务用户 ID 字段；user 默认 id，user_tag_0 默认 uid")
	flags.StringVar(&opts.ShardColumn, "shard-column", "shard_no", "固定桶字段")
	flags.StringVar(&opts.CursorColumn, "cursor-column", "id", "单调唯一数字游标字段")
	flags.IntVar(&opts.FromCount, "from-count", 0, "当前物理分片数")
	flags.IntVar(&opts.ToCount, "to-count", 0, "目标物理分片数")
	flags.IntVar(&opts.ActiveCount, "active-count", 0, "自定义表当前应用路由数")
	flags.Uint64Var(&opts.BatchSize, "batch-size", 500, "在线复制单批行数，1..5000")
	flags.IntVar(&opts.CleanupBatch, "cleanup-batch-size", 1000, "清理单批行数，1..10000")
	flags.DurationVar(&opts.CleanupDelay, "cleanup-delay", 100*time.Millisecond, "清理批次间隔")
	flags.StringVar(&opts.ReadyFile, "ready-file", "", "维护就绪标记绝对路径")
	flags.StringVar(&opts.CutoverFile, "cutover-file", "", "停写完成放行标记绝对路径")
	flags.StringVar(&opts.VerifiedFile, "verified-file", "", "最终追平和校验成功凭证绝对路径")
	flags.DurationVar(&opts.CutoverTimeout, "cutover-timeout", 24*time.Hour, "等待停写放行标记上限")
	flags.DurationVar(&opts.MaxDowntime, "max-downtime", 10*time.Minute, "最终追平和校验窗口上限")
	flags.StringVar(&opts.TLSCA, "tls-ca", "", "MySQL TLS CA 文件")
	flags.StringVar(&opts.TLSServerName, "tls-server-name", "", "MySQL TLS 服务端名称")
	flags.StringVar(&opts.ConfirmCleanup, "confirm-cleanup", "", "旧数据清理二次确认")
	if err := flags.Parse(args); err != nil {
		return options{}, errors.Wrap(err, "解析参数失败")
	}
	if flags.NArg() != 0 {
		return options{}, errors.Errorf("存在未识别参数 %q", flags.Arg(0))
	}
	opts.FirstTable = strings.TrimSpace(opts.FirstTable)
	opts.UIDColumn = strings.TrimSpace(opts.UIDColumn)
	opts.ShardColumn = strings.TrimSpace(opts.ShardColumn)
	opts.CursorColumn = strings.TrimSpace(opts.CursorColumn)
	if opts.UIDColumn == "" {
		switch opts.FirstTable {
		case model.TableNameUser:
			opts.UIDColumn = "id"
		case model.TableNameUserTag:
			opts.UIDColumn = "uid"
		default:
			return options{}, errors.New("自定义 UID 表必须通过 -uid-column 指定业务用户 ID 字段")
		}
	}
	switch opts.Action {
	case actionPlan, actionPrepare, actionCopy, actionCleanup:
	default:
		return options{}, errors.New("action 必须是 plan/prepare/copy/cleanup")
	}
	for name, value := range map[string]string{
		"first-table":   opts.FirstTable,
		"uid-column":    opts.UIDColumn,
		"shard-column":  opts.ShardColumn,
		"cursor-column": opts.CursorColumn,
	} {
		if err := sharding.ValidateIdentifier(value); err != nil {
			return options{}, errors.Wrapf(err, "%s 无效", name)
		}
	}
	if _, err := sharding.ExpandMoves(opts.FirstTable, opts.FromCount, opts.ToCount); err != nil {
		return options{}, err
	}
	if opts.BatchSize == 0 || opts.BatchSize > 5000 {
		return options{}, errors.New("batch-size 必须位于 1..5000")
	}
	if opts.CutoverTimeout <= 0 || opts.CutoverTimeout > 7*24*time.Hour {
		return options{}, errors.New("cutover-timeout 必须位于 0..168h")
	}
	if opts.MaxDowntime <= 0 || opts.MaxDowntime > time.Hour {
		return options{}, errors.New("max-downtime 必须位于 0..1h")
	}
	return opts, nil
}

// activeRouteCount 返回已注册业务表的启动路由数。
func activeRouteCount(cfg config.Config, opts options) (int, error) {
	switch opts.FirstTable {
	case model.TableNameUser:
		return cfg.User.RouteShardCount, nil
	case model.TableNameUserTag:
		count := cfg.Workflows.UserTag.ResultShardTotal
		if count <= 0 {
			count = 1
		}
		return count, nil
	default:
		if !sharding.ValidCount(opts.ActiveCount) {
			return 0, errors.New("自定义 UID 表必须通过 -active-count 提供当前应用路由数")
		}
		return opts.ActiveCount, nil
	}
}

// validateCopyMarkerPaths 校验维护握手文件不会被旧文件误触发。
func validateCopyMarkerPaths(opts options) error {
	paths := map[string]string{
		"ready-file":    opts.ReadyFile,
		"cutover-file":  opts.CutoverFile,
		"verified-file": opts.VerifiedFile,
	}
	cleaned := make(map[string]string, len(paths))
	for name, path := range paths {
		if !filepath.IsAbs(path) {
			return errors.Errorf("%s 必须是绝对路径", name)
		}
		cleaned[name] = filepath.Clean(path)
		if _, err := os.Lstat(path); err == nil {
			return errors.Errorf("%s 已存在，拒绝复用旧标记 path=%s", name, path)
		} else if !os.IsNotExist(err) {
			return errors.Wrapf(err, "检查 %s 失败 path=%s", name, path)
		}
	}
	if cleaned["ready-file"] == cleaned["cutover-file"] ||
		cleaned["ready-file"] == cleaned["verified-file"] ||
		cleaned["cutover-file"] == cleaned["verified-file"] {
		return errors.New("ready-file、cutover-file 和 verified-file 不能相同")
	}
	return nil
}

// validateVerifiedMarker 校验并返回 cleanup 使用的同一轮扩容最终成功凭证。
func validateVerifiedMarker(path string, opts options, moves []sharding.Move, databaseFingerprint string) (marker, error) {
	if !filepath.IsAbs(path) {
		return marker{}, errors.New("verified-file 必须是绝对路径")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return marker{}, errors.Wrapf(err, "读取最终校验凭证失败 path=%s", path)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxMarkerFileSize {
		return marker{}, errors.Errorf("最终校验凭证必须是 1..%d 字节的普通文件 path=%s", maxMarkerFileSize, path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return marker{}, errors.Wrapf(err, "读取最终校验凭证失败 path=%s", path)
	}
	var value marker
	if err := json.Unmarshal(content, &value); err != nil {
		return marker{}, errors.Wrapf(err, "解析最终校验凭证失败 path=%s", path)
	}
	rawToken, err := hex.DecodeString(value.Token)
	if err != nil || len(rawToken) != 16 {
		return marker{}, errors.Errorf("最终校验凭证令牌无效 path=%s", path)
	}
	if value.Status != markerStatusVerified ||
		value.Table != opts.FirstTable ||
		value.UIDColumn != opts.UIDColumn ||
		value.ShardColumn != opts.ShardColumn ||
		value.CursorColumn != opts.CursorColumn ||
		value.FromCount != opts.FromCount ||
		value.ToCount != opts.ToCount ||
		value.DatabaseFingerprint != databaseFingerprint ||
		value.CreatedAt.IsZero() ||
		!sameMoves(value.Moves, moves) {
		return marker{}, errors.Errorf("最终校验凭证与本次清理计划不一致 path=%s", path)
	}
	return value, nil
}

// sameMoves 判断两个扩容桶迁移计划是否逐项一致。
func sameMoves(left []sharding.Move, right []sharding.Move) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// newMarker 构造绑定本轮扩容计划和数据库目标的维护标记。
func newMarker(opts options, status string, token string, moves []sharding.Move, databaseFingerprint string) marker {
	return marker{
		Status:              status,
		Token:               token,
		Table:               opts.FirstTable,
		UIDColumn:           opts.UIDColumn,
		ShardColumn:         opts.ShardColumn,
		CursorColumn:        opts.CursorColumn,
		FromCount:           opts.FromCount,
		ToCount:             opts.ToCount,
		Moves:               moves,
		DatabaseFingerprint: databaseFingerprint,
		CreatedAt:           time.Now(),
	}
}

// createMarker 以排他方式创建维护就绪标记。
func createMarker(path string, value marker) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return errors.Wrap(err, "编码维护标记失败")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.Wrapf(err, "创建维护标记失败 path=%s", path)
	}
	if _, err := file.Write(append(content, '\n')); err != nil {
		_ = file.Close()
		return errors.Wrapf(err, "写入维护标记失败 path=%s", path)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return errors.Wrapf(err, "同步维护标记失败 path=%s", path)
	}
	if err := file.Close(); err != nil {
		return errors.Wrapf(err, "关闭维护标记失败 path=%s", path)
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return errors.Wrapf(err, "打开维护标记目录失败 path=%s", path)
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return errors.Wrapf(err, "同步维护标记目录失败 path=%s", path)
	}
	if err := directory.Close(); err != nil {
		return errors.Wrapf(err, "关闭维护标记目录失败 path=%s", path)
	}
	return nil
}

// waitForCutoverFile 等待运维停止入口写流量并排空应用事务。
func waitForCutoverFile(ctx context.Context, path string, token string, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		matched, err := cutoverFileMatches(path, token)
		if err != nil {
			return err
		}
		if matched {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "等待停写标记取消")
		case <-timer.C:
			return errors.Errorf("等待停写标记超时 path=%s", path)
		case <-ticker.C:
		}
	}
}

// cutoverFileMatches 校验停写标记只包含本轮 ready 标记下发的令牌。
func cutoverFileMatches(path string, token string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrapf(err, "读取停写标记失败 path=%s", path)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxCutoverFileSize {
		return false, errors.Errorf("停写标记必须是 1..%d 字节的普通文件 path=%s", maxCutoverFileSize, path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return false, errors.Wrapf(err, "读取停写标记失败 path=%s", path)
	}
	if strings.TrimSpace(string(content)) != token {
		return false, errors.Errorf("停写标记令牌不匹配 path=%s", path)
	}
	return true, nil
}

// newCutoverToken 生成只能从本轮 ready 标记取得的一次性切换令牌。
func newCutoverToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.Wrap(err, "生成切换令牌失败")
	}
	return hex.EncodeToString(raw), nil
}

// cleanupConfirmation 返回绑定最终校验令牌的旧数据清理确认文本。
func cleanupConfirmation(opts options, token string) string {
	return fmt.Sprintf("%s:%d->%d:%s", opts.FirstTable, opts.FromCount, opts.ToCount, token)
}

// loadMigrationDSN 读取短期专用迁移账号并确认不会误操作其他 MySQL 库。
func loadMigrationDSN(applicationDSN string) (string, error) {
	migrationDSN := strings.TrimSpace(os.Getenv(migrationDSNEnv))
	if migrationDSN == "" {
		return "", errors.Errorf("必须通过环境变量 %s 注入短期专用迁移账号 DSN", migrationDSNEnv)
	}
	if err := migration.ValidateSameDatabase(applicationDSN, migrationDSN); err != nil {
		return "", err
	}
	return migrationDSN, nil
}

// writeJSON 输出稳定的机器可读执行结果。
func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return errors.Wrap(err, "输出 JSON 失败")
	}
	return nil
}
