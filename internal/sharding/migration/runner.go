package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"admin/internal/sharding"

	"github.com/Is999/go-utils/errors"
	"github.com/Shopify/ghostferry"
	"github.com/go-sql-driver/mysql"
)

const (
	// defaultMySQLPort 是 MySQL TCP 默认端口。
	defaultMySQLPort = 3306
	// onlineCopyReadRetries 为在线写入造成的 NOWAIT 行锁冲突保留一分钟重试窗口。
	onlineCopyReadRetries = 60
)

// TLSOptions 定义在线复制连接使用的数据库证书。
type TLSOptions struct {
	CAPath     string // CA 证书文件
	ServerName string // 证书服务端名称
}

// CopyOptions 定义一次物理扩容在线复制。
type CopyOptions struct {
	DSN          string                                // 应用写库 DSN
	TLS          TLSOptions                            // 数据库 TLS 参数
	FirstTable   string                                // 起始桶物理表
	UIDColumn    string                                // 业务用户 ID 字段
	ShardColumn  string                                // 固定桶字段
	CursorColumn string                                // 唯一分页游标字段
	FromCount    int                                   // 当前物理分片数
	ToCount      int                                   // 目标物理分片数
	BatchSize    uint64                                // 行复制单批上限
	CutoverWait  func() error                          // 停写完成后的放行函数
	CutoverFence func(context.Context) (func(), error) // 最终追平期间的数据库写围栏
	Ready        func([]sharding.Move) error           // 全量完成且增量接近追平后的通知函数
	Verified     func([]sharding.Move) error           // 最终校验通过且写围栏仍持有时的持久化函数
	MaxDowntime  time.Duration                         // 允许的维护窗口上限
}

// CopyResult 表示在线复制和最终校验结果。
type CopyResult struct {
	Moves []sharding.Move // 已完成迁移的桶区间
}

// ferryBatch 绑定一组源表互不重复的迁移区间及其 Ghostferry 实例。
type ferryBatch struct {
	moves []sharding.Move   // 本通道迁移桶区间
	ferry *ghostferry.Ferry // 嵌入式复制实例
	done  chan struct{}     // 复制主循环结束信号
}

// ValidateSameDatabase 校验应用账号和迁移账号指向完全相同的 MySQL 网络端点与数据库。
func ValidateSameDatabase(applicationDSN string, migrationDSN string) error {
	applicationTarget, err := mysqlTargetFromDSN(applicationDSN)
	if err != nil {
		return errors.Wrap(err, "解析应用 MySQL DSN 失败")
	}
	migrationTarget, err := mysqlTargetFromDSN(migrationDSN)
	if err != nil {
		return errors.Wrap(err, "解析迁移 MySQL DSN 失败")
	}
	if applicationTarget != migrationTarget {
		return errors.Errorf(
			"迁移账号必须与应用写库指向同一目标 application=%s/%s/%s migration=%s/%s/%s",
			applicationTarget.Network,
			applicationTarget.Address,
			applicationTarget.Database,
			migrationTarget.Network,
			migrationTarget.Address,
			migrationTarget.Database,
		)
	}
	return nil
}

// DatabaseFingerprint 返回不含账号密码的数据库目标指纹，用于绑定本轮迁移凭证。
func DatabaseFingerprint(dsn string) (string, error) {
	target, err := mysqlTargetFromDSN(dsn)
	if err != nil {
		return "", errors.Wrap(err, "生成数据库目标指纹失败")
	}
	digest := sha256.Sum256([]byte(target.Network + "\x00" + target.Address + "\x00" + target.Database))
	return hex.EncodeToString(digest[:]), nil
}

// RunCopy 在线预复制、持续追踪 binlog，并在外部停写后完成最终追平和校验。
func RunCopy(ctx context.Context, opts CopyOptions) (CopyResult, error) {
	if opts.CutoverWait == nil {
		return CopyResult{}, errors.New("缺少停写切换放行函数")
	}
	if opts.CutoverFence == nil {
		return CopyResult{}, errors.New("缺少最终追平数据库写围栏")
	}
	if opts.Verified == nil {
		return CopyResult{}, errors.New("缺少最终校验成功持久化函数")
	}
	if err := sharding.ValidateIdentifier(opts.UIDColumn); err != nil {
		return CopyResult{}, errors.Wrap(err, "业务用户 ID 字段无效")
	}
	if err := sharding.ValidateIdentifier(opts.CursorColumn); err != nil {
		return CopyResult{}, errors.Wrap(err, "唯一分页游标字段无效")
	}
	if opts.BatchSize == 0 {
		opts.BatchSize = 500
	}
	if opts.MaxDowntime <= 0 {
		opts.MaxDowntime = 10 * time.Minute
	}
	moves, err := sharding.ExpandMoves(opts.FirstTable, opts.FromCount, opts.ToCount)
	if err != nil {
		return CopyResult{}, err
	}
	databaseName, databaseConfig, err := ghostferryConfigFromDSN(opts.DSN, opts.TLS)
	if err != nil {
		return CopyResult{}, err
	}
	runner, err := newFerryBatch(databaseName, databaseConfig, opts, moves)
	if err != nil {
		return CopyResult{}, err
	}
	defer closeFerry(runner)
	if err := runner.ferry.Start(); err != nil {
		return CopyResult{}, errors.Wrapf(err, "启动在线复制失败 targets=%s", moveTargets(moves))
	}
	go func() {
		defer close(runner.done)
		runner.ferry.Run()
	}()
	runner.ferry.WaitUntilRowCopyIsComplete()
	runner.ferry.WaitUntilBinlogStreamerCatchesUp()
	if opts.Ready != nil {
		if err := opts.Ready(moves); err != nil {
			return CopyResult{}, errors.Wrap(err, "发布维护就绪状态失败")
		}
	}
	if err := waitCutover(ctx, opts.CutoverWait); err != nil {
		return CopyResult{}, err
	}
	downtimeCtx, cancel := context.WithTimeout(ctx, opts.MaxDowntime)
	defer cancel()
	releaseFence, err := opts.CutoverFence(downtimeCtx)
	if err != nil {
		return CopyResult{}, errors.Wrap(err, "获取最终追平数据库写围栏失败")
	}
	defer releaseFence()
	runner.ferry.FlushBinlogAndStopStreaming()
	select {
	case <-downtimeCtx.Done():
		return CopyResult{}, errors.Wrapf(downtimeCtx.Err(), "维护期最终增量追平超时 targets=%s", moveTargets(moves))
	case <-runner.done:
	}
	resultChannel := make(chan ghostferry.VerificationResult, 1)
	errorChannel := make(chan error, 1)
	go func() {
		result, verifyErr := runner.ferry.Verifier.VerifyDuringCutover()
		if verifyErr != nil {
			errorChannel <- verifyErr
			return
		}
		resultChannel <- result
	}()
	select {
	case <-downtimeCtx.Done():
		return CopyResult{}, errors.Wrapf(downtimeCtx.Err(), "维护期最终校验超时 targets=%s", moveTargets(moves))
	case verifyErr := <-errorChannel:
		return CopyResult{}, errors.Wrapf(verifyErr, "最终校验失败 targets=%s", moveTargets(moves))
	case result := <-resultChannel:
		if !result.DataCorrect {
			return CopyResult{}, errors.Errorf("最终校验不一致 targets=%s: %s", moveTargets(moves), result.Message)
		}
	}
	if err := opts.Verified(moves); err != nil {
		return CopyResult{}, errors.Wrap(err, "持久化最终校验成功状态失败")
	}
	return CopyResult{Moves: moves}, nil
}

// newFerryBatch 为一组源表互不重复的迁移区间创建嵌入式复制实例。
func newFerryBatch(databaseName string, databaseConfig *ghostferry.DatabaseConfig, opts CopyOptions, moves []sharding.Move) (*ferryBatch, error) {
	if len(moves) == 0 {
		return nil, errors.New("在线复制迁移区间不能为空")
	}
	ranges := make(map[string]BucketRange, len(moves))
	rewrites := make(map[string]string, len(moves))
	paginationColumns := make(map[string]string, len(moves))
	for _, move := range moves {
		if _, exists := ranges[move.Source]; exists {
			return nil, errors.Errorf("同一复制通道不能重复源物理表 source=%s", move.Source)
		}
		ranges[move.Source] = BucketRange{
			Start: int64(move.BucketStart),
			End:   int64(move.BucketEnd),
		}
		rewrites[move.Source] = move.Target
		paginationColumns[move.Source] = opts.CursorColumn
	}
	filter := BucketRangeFilter{
		UIDColumn:   opts.UIDColumn,
		ShardColumn: opts.ShardColumn,
		Ranges:      ranges,
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}
	config := &ghostferry.Config{
		Source:                             cloneDatabaseConfig(databaseConfig),
		Target:                             cloneDatabaseConfig(databaseConfig),
		TableRewrites:                      rewrites,
		TableFilter:                        exactTableFilter{Database: databaseName, Tables: sourceSet(moves)},
		CopyFilter:                         filter,
		AutomaticCutover:                   true,
		DumpStateOnSignal:                  true,
		DumpStateToStdoutOnError:           true,
		VerifierType:                       ghostferry.VerifierTypeInline,
		SkipTargetVerification:             true,
		DataIterationConcurrency:           1,
		BinlogEventBatchSize:               100,
		DBReadRetries:                      onlineCopyReadRetries,
		DBWriteRetries:                     10,
		DoNotIncludeSchemaCacheInStateDump: true,
		UpdatableConfig: ghostferry.UpdatableConfig{
			DataIterationBatchSize: opts.BatchSize,
		},
		InlineVerifierConfig: ghostferry.InlineVerifierConfig{
			MaxExpectedDowntime:        opts.MaxDowntime.String(),
			VerifyBinlogEventsInterval: "1s",
		},
		CascadingPaginationColumnConfig: &ghostferry.CascadingPaginationColumnConfig{
			PerTable: map[string]map[string]string{
				databaseName: paginationColumns,
			},
		},
	}
	if err := config.ValidateConfig(); err != nil {
		return nil, errors.Wrapf(err, "Ghostferry 配置无效 targets=%s", moveTargets(moves))
	}
	ferry := &ghostferry.Ferry{Config: config}
	if err := ferry.Initialize(); err != nil {
		closeFerry(&ferryBatch{moves: moves, ferry: ferry})
		return nil, errors.Wrapf(err, "初始化在线复制失败 targets=%s", moveTargets(moves))
	}
	return &ferryBatch{moves: moves, ferry: ferry, done: make(chan struct{})}, nil
}

// ghostferryConfigFromDSN 把应用写库 DSN 转成 Ghostferry 的内嵌连接配置。
func ghostferryConfigFromDSN(dsn string, tlsOptions TLSOptions) (string, *ghostferry.DatabaseConfig, error) {
	parsed, err := mysql.ParseDSN(strings.TrimSpace(dsn))
	if err != nil {
		return "", nil, errors.Wrap(err, "解析 MySQL DSN 失败")
	}
	if parsed.DBName == "" {
		return "", nil, errors.New("MySQL DSN 必须包含数据库名")
	}
	if parsed.Net != "tcp" {
		return "", nil, errors.Errorf("在线拆表当前只支持 MySQL TCP DSN net=%s", parsed.Net)
	}
	if parsed.ReadTimeout <= 0 || parsed.WriteTimeout <= 0 {
		return "", nil, errors.New("在线拆表迁移 DSN 必须显式配置非零 readTimeout 和 writeTimeout")
	}
	host, portText, err := net.SplitHostPort(parsed.Addr)
	if err != nil {
		return "", nil, errors.Wrapf(err, "解析 MySQL 地址失败 addr=%s", parsed.Addr)
	}
	port := defaultMySQLPort
	if portText != "" {
		port, err = strconv.Atoi(portText)
		if err != nil || port <= 0 || port > 65535 {
			return "", nil, errors.Errorf("MySQL 端口无效 port=%s", portText)
		}
	}
	tlsConfig := (*ghostferry.TLSConfig)(nil)
	if parsed.TLSConfig != "" && parsed.TLSConfig != "false" {
		if strings.TrimSpace(tlsOptions.CAPath) == "" || strings.TrimSpace(tlsOptions.ServerName) == "" {
			return "", nil, errors.New("DSN 启用了 TLS，必须同时提供 tls_ca 和 tls_server_name")
		}
		tlsConfig = &ghostferry.TLSConfig{
			CertPath:   strings.TrimSpace(tlsOptions.CAPath),
			ServerName: strings.TrimSpace(tlsOptions.ServerName),
		}
	}
	sessionParams, err := ghostferrySessionParams(parsed)
	if err != nil {
		return "", nil, err
	}
	return parsed.DBName, &ghostferry.DatabaseConfig{
		Host:         host,
		Port:         uint16(port),
		Net:          parsed.Net,
		User:         parsed.User,
		Pass:         parsed.Passwd,
		Collation:    parsed.Collation,
		Params:       sessionParams,
		TLS:          tlsConfig,
		ReadTimeout:  durationSeconds(parsed.ReadTimeout),
		WriteTimeout: durationSeconds(parsed.WriteTimeout),
	}, nil
}

// ghostferrySessionParams 保留 MySQL 会话变量和驱动单独解析的 charset 参数。
func ghostferrySessionParams(parsed *mysql.Config) (map[string]string, error) {
	params := cloneStringMap(parsed.Params)
	_, rawQuery, exists := strings.Cut(parsed.FormatDSN(), "?")
	if !exists {
		return params, nil
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, errors.Wrap(err, "解析 MySQL DSN 会话参数失败")
	}
	if charset := strings.TrimSpace(values.Get("charset")); charset != "" {
		params["charset"] = charset
	}
	return params, nil
}

// mysqlTarget 表示不包含账号密码的 MySQL 目标身份。
type mysqlTarget struct {
	Network  string // 连接网络
	Address  string // 网络地址
	Database string // 数据库名
}

// mysqlTargetFromDSN 提取不包含凭据的 MySQL 目标身份。
func mysqlTargetFromDSN(dsn string) (mysqlTarget, error) {
	parsed, err := mysql.ParseDSN(strings.TrimSpace(dsn))
	if err != nil {
		return mysqlTarget{}, errors.Wrap(err, "解析 MySQL DSN 失败")
	}
	if parsed.DBName == "" {
		return mysqlTarget{}, errors.New("MySQL DSN 必须包含数据库名")
	}
	return mysqlTarget{
		Network:  parsed.Net,
		Address:  parsed.Addr,
		Database: parsed.DBName,
	}, nil
}

// cloneDatabaseConfig 为每个 Ferry 创建独立可归一化的连接配置。
func cloneDatabaseConfig(source *ghostferry.DatabaseConfig) *ghostferry.DatabaseConfig {
	if source == nil {
		return nil
	}
	cloned := *source
	cloned.Params = cloneStringMap(source.Params)
	return &cloned
}

// cloneStringMap 复制连接会话参数，避免 Ghostferry 归一化源配置时污染其他通道。
func cloneStringMap(source map[string]string) map[string]string {
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

// durationSeconds 向上取整 Ghostferry 只支持秒级的数据库读写超时。
func durationSeconds(value time.Duration) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64((value-1)/time.Second) + 1
}

// waitCutover 等待运维确认所有写流量和在途事务已经停止。
func waitCutover(ctx context.Context, wait func() error) error {
	done := make(chan error, 1)
	go func() {
		done <- wait()
	}()
	select {
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "等待停写确认超时")
	case err := <-done:
		if err != nil {
			return errors.Wrap(err, "等待停写确认失败")
		}
		return nil
	}
}

// closeFerry 释放嵌入式复制建立的数据库连接。
func closeFerry(runner *ferryBatch) {
	if runner == nil || runner.ferry == nil {
		return
	}
	if runner.ferry.SourceDB != nil {
		_ = runner.ferry.SourceDB.Close()
	}
	if runner.ferry.TargetDB != nil {
		_ = runner.ferry.TargetDB.Close()
	}
}

// exactTableFilter 只允许迁移一组明确的源物理表。
type exactTableFilter struct {
	Database string              // 源数据库
	Tables   map[string]struct{} // 源物理表集合
}

// ApplicableDatabases 过滤非目标数据库。
func (f exactTableFilter) ApplicableDatabases(databases []string) ([]string, error) {
	for _, database := range databases {
		if database == f.Database {
			return []string{database}, nil
		}
	}
	return nil, errors.Errorf("源数据库不存在 database=%s", f.Database)
}

// ApplicableTables 过滤非目标物理表。
func (f exactTableFilter) ApplicableTables(tables []*ghostferry.TableSchema) ([]*ghostferry.TableSchema, error) {
	selected := make([]*ghostferry.TableSchema, 0, len(f.Tables))
	for _, table := range tables {
		if table.Schema == f.Database {
			if _, exists := f.Tables[table.Name]; exists {
				selected = append(selected, table)
			}
		}
	}
	if len(selected) != len(f.Tables) {
		return nil, errors.Errorf("部分源物理表不存在 database=%s expected=%d actual=%d", f.Database, len(f.Tables), len(selected))
	}
	return selected, nil
}

// sourceSet 返回迁移区间中的源物理表集合。
func sourceSet(moves []sharding.Move) map[string]struct{} {
	sources := make(map[string]struct{}, len(moves))
	for _, move := range moves {
		sources[move.Source] = struct{}{}
	}
	return sources
}

// moveTargets 返回用于日志定位的目标物理表列表。
func moveTargets(moves []sharding.Move) string {
	if len(moves) == 0 {
		return ""
	}
	if len(moves) > 4 {
		return fmt.Sprintf("count=%d first=%s last=%s", len(moves), moves[0].Target, moves[len(moves)-1].Target)
	}
	targets := make([]string, 0, len(moves))
	for _, move := range moves {
		targets = append(targets, move.Target)
	}
	return strings.Join(targets, ",")
}
