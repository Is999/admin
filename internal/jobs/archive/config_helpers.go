package archive

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	"admin/internal/config"
	"admin/internal/infra/loggerx"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

// splitArchiveTarget 解析归档目标及动作后缀。
// 支持 `job#archive` 和 `job#delete` 分别调度归档、删除；无后缀时按 all 执行。
func splitArchiveTarget(target string) (string, string) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", archiveRunModeAll
	}
	name := target
	mode := archiveRunModeAll
	if idx := strings.LastIndex(target, "#"); idx >= 0 {
		name = strings.TrimSpace(target[:idx])
		mode = strings.ToLower(strings.TrimSpace(target[idx+1:]))
	}
	switch mode {
	case archiveRunModeArchive, archiveRunModeDelete:
	default:
		mode = archiveRunModeAll
	}
	return name, mode
}

// normalizeSplitUnit 标准化拆分粒度配置；非法值统一回退为按月拆分。
func normalizeSplitUnit(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case SplitUnitNone:
		return SplitUnitNone
	case SplitUnitYear:
		return SplitUnitYear
	case SplitUnitQuarter:
		return SplitUnitQuarter
	case SplitUnitWeek:
		return SplitUnitWeek
	case SplitUnitDay:
		return SplitUnitDay
	case SplitUnitCustomDays:
		return SplitUnitCustomDays
	default:
		return SplitUnitMonth
	}
}

// normalizeTimeColumnType 标准化时间列类型，未配置时按 time 处理。
func normalizeTimeColumnType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", TimeColumnTypeTime:
		return TimeColumnTypeTime
	case TimeColumnTypeString:
		return TimeColumnTypeString
	case TimeColumnTypeUnix:
		return TimeColumnTypeUnix
	default:
		return ""
	}
}

// normalizeTimeColumnUnixUnit 标准化 Unix 整数时间单位，未配置时默认按秒。
func normalizeTimeColumnUnixUnit(timeColumnType string, value string) string {
	if normalizeTimeColumnType(timeColumnType) != TimeColumnTypeUnix {
		return ""
	}
	normalizedValue := strings.ToLower(strings.TrimSpace(value))
	switch normalizedValue {
	case "", TimeColumnUnixUnitSeconds:
		return TimeColumnUnixUnitSeconds
	case TimeColumnUnixUnitMilliseconds:
		return TimeColumnUnixUnitMilliseconds
	default:
		return ""
	}
}

// defaultArchiveStringTimeFormat 返回字符串时间列的默认 Go layout。
func defaultArchiveStringTimeFormat(timeColumnType string) string {
	if normalizeTimeColumnType(timeColumnType) == TimeColumnTypeString {
		return defaultArchiveStringTimeLayout
	}
	return ""
}

// validateArchiveJobConfig 校验单个归档任务基础配置，并返回归一化后的任务名和表名。
func validateArchiveJobConfig(item config.ArchiveJobConfig) error {
	// jobName 表示归档任务唯一名称，空值会导致 target 匹配和 checkpoint 都无法定位。
	jobName := strings.TrimSpace(item.Name)
	if jobName == "" {
		return errors.New("归档任务名称不能为空")
	}
	// tableName 表示归档热表名，空值会导致动态 SQL 无法安全构造。
	tableName := strings.TrimSpace(item.TableName)
	if tableName == "" {
		return errors.New("归档热表名不能为空")
	}
	if !isKnownDatabase(item.Database) {
		return errors.Errorf("归档数据库名称不合法: %s", strings.TrimSpace(item.Database))
	}
	if normalizeTimeColumnType(item.TimeColumnType) == "" {
		return errors.Errorf("归档时间列类型不合法: %s", strings.TrimSpace(item.TimeColumnType))
	}
	if normalizeTimeColumnType(item.TimeColumnType) == TimeColumnTypeString {
		if err := validateArchiveStringTimeFormat(item.TimeColumnFormat); err != nil {
			return errors.Tag(err)
		}
	}
	if normalizeTimeColumnType(item.TimeColumnType) == TimeColumnTypeUnix &&
		normalizeTimeColumnUnixUnit(item.TimeColumnType, item.TimeColumnUnixUnit) == "" {
		return errors.Errorf("归档 Unix 时间单位不合法: %s", strings.TrimSpace(item.TimeColumnUnixUnit))
	}
	if normalizeArchiveWindowMode(item.ArchiveWindowMode) == "" {
		return errors.Errorf("归档窗口推进模式不合法: %s", strings.TrimSpace(item.ArchiveWindowMode))
	}
	if strings.TrimSpace(item.StartAt) != "" {
		if _, err := parseArchiveStartAt(item.StartAt); err != nil {
			return errors.Tag(err)
		}
	}
	// timeColumn 表示归档时间列，允许为空并在归一化阶段回退到 created_at。
	timeColumn := strings.TrimSpace(item.TimeColumn)
	if timeColumn == "" {
		timeColumn = "created_at"
	}
	// primaryKey 表示热表主键列，允许为空并在归一化阶段回退到 id。
	primaryKey := strings.TrimSpace(item.PrimaryKey)
	if primaryKey == "" {
		primaryKey = "id"
	}
	// historyPrefix 表示历史表前缀，允许为空并在归一化阶段回退到 `{table}_archive`。
	historyPrefix := strings.TrimSpace(item.HistoryTablePrefix)
	if historyPrefix == "" {
		historyPrefix = fmt.Sprintf("%s_archive", tableName)
	}
	// identifierItems 表示需要进入动态 SQL 的标识符集合，逐个校验后才能参与拼接。
	identifierItems := []struct {
		fieldName string // fieldName 表示配置字段名，便于错误日志定位
		value     string // value 表示字段对应的标识符值
	}{
		{fieldName: "history_table_prefix", value: historyPrefix},
		{fieldName: "primary_key", value: primaryKey},
		{fieldName: "table_name", value: tableName},
		{fieldName: "time_column", value: timeColumn},
	}
	for _, item := range identifierItems {
		if !identifierPattern.MatchString(item.value) {
			return errors.Errorf("归档标识符不合法: field=%s value=%s", item.fieldName, item.value)
		}
	}
	// 自定义 SQL 条件来自运行期配置，只允许作为 WHERE 谓词片段使用；非法条件直接跳过当前归档 job。
	conditionItems := []struct {
		fieldName string // fieldName 表示配置字段名，便于定位是哪一类条件配置错误
		value     string // value 表示配置里的原始 SQL 条件片段
	}{
		{fieldName: "archive_condition", value: item.ArchiveCondition},
		{fieldName: "delete_condition", value: item.DeleteCondition},
	}
	for _, conditionItem := range conditionItems {
		if _, err := normalizeArchiveSQLCondition(conditionItem.value); err != nil {
			return errors.Wrapf(err, "归档自定义条件不合法: field=%s", conditionItem.fieldName)
		}
	}
	if strings.TrimSpace(item.HistoryTableNameRule) != "" {
		probeStart := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.Local)
		probeEnd := probeStart.Add(time.Hour)
		probe := buildHistoryTableName(jobConfig{
			TableName:            tableName,
			SplitUnit:            normalizeSplitUnit(item.SplitUnit),
			CustomDays:           item.CustomDays,
			HistoryTablePrefix:   historyPrefix,
			HistoryTableNameRule: strings.TrimSpace(item.HistoryTableNameRule),
		}, probeStart, probeEnd)
		if !identifierPattern.MatchString(probe) {
			return errors.Errorf("归档历史表命名规则渲染后不合法: rule=%s sample=%s", strings.TrimSpace(item.HistoryTableNameRule), probe)
		}
	}
	return nil
}

// notifyArchiveJobConfigInvalid 记录归档任务配置异常，保证错误配置可被日志检索和告警系统发现。
func notifyArchiveJobConfigInvalid(index int, item config.ArchiveJobConfig, err error) {
	loggerx.Errorw(context.Background(), "归档任务 配置无效", err,
		logx.Field("index", index),
		logx.Field("archive_job_name", strings.TrimSpace(item.Name)),
		logx.Field("database", strings.TrimSpace(item.Database)),
		logx.Field("table_name", strings.TrimSpace(item.TableName)),
		logx.Field("failure_reason", strings.TrimSpace(err.Error())),
	)
}

// isKnownDatabase 校验归档任务数据库名称格式是否合法。
func isKnownDatabase(database string) bool {
	name := normalizeArchiveDatabaseName(database)
	return identifierPattern.MatchString(string(name))
}

// normalizeArchiveDatabaseName 规范化归档配置中的数据库名称。
func normalizeArchiveDatabaseName(database string) svc.DBName {
	return svc.NormalizeDBName(svc.DBName(strings.TrimSpace(database)))
}

// jobConfig 是运行期归一化后的归档任务配置。
type jobConfig struct {
	Name                    string        // 归档任务名
	Database                svc.DBName    // 归属库名
	TableName               string        // 热表名
	TimeColumn              string        // 时间列
	TimeColumnType          string        // 时间列类型
	TimeColumnFormat        string        // 字符串时间格式
	TimeColumnUnixUnit      string        // Unix 整数时间单位
	PrimaryKey              string        // 主键列
	ArchiveCondition        string        // 自定义归档过滤条件
	DeleteCondition         string        // 自定义清理过滤条件
	SplitUnit               string        // 拆分粒度
	CustomDays              int           // 自定义分段天数
	HotKeepDays             int           // 热表保留天数
	ArchiveDelayDays        int           // 归档延迟天数
	ArchiveWindowSeconds    int           // 单个归档窗口秒数
	ArchiveWindowMode       string        // 归档窗口推进模式
	ArchiveMaxWindowsPerRun int           // 单次最多归档窗口数
	ArchiveAutoMaxWindows   int           // auto 模式单次最多追赶窗口数
	ArchiveAutoLightRows    int           // auto 模式轻量窗口行数阈值
	ArchiveAutoLightElapsed time.Duration // auto 模式轻量窗口耗时阈值
	DeleteDisabled          bool          // 是否禁用热表删除
	DeleteDelayDays         int           // 删除延迟天数
	DeleteWindowSeconds     int           // 单个删除窗口秒数
	DeleteMaxWindowsPerRun  int           // 单次最多删除窗口数
	BatchSize               int           // 单批归档条数
	DeleteBatchSize         int           // 单批删除条数
	MaxHistoryTables        int           // 最大历史表数
	HistoryTablePrefix      string        // 历史表名前缀
	HistoryTableNameRule    string        // 历史表命名规则
	StartAt                 sql.NullTime  // 首次归档起点
	QueryWriteDB            bool          // 查询是否强制走主库
}

// jobRunConfig 表示一次工作流 target 解析后的执行目标。
type jobRunConfig struct {
	Job  jobConfig // 归档任务配置
	Mode string    // 执行动作：all/archive/delete
}

// isStringTimeColumn 判断当前任务是否使用字符串时间列做归档游标。
func (j jobConfig) isStringTimeColumn() bool {
	return j.TimeColumnType == TimeColumnTypeString
}

// isDateOnlyStringTimeColumn 判断字符串时间列是否只有日期粒度。
func (j jobConfig) isDateOnlyStringTimeColumn() bool {
	return j.isStringTimeColumn() && !archiveStringTimeFormatHasClock(j.TimeColumnFormat)
}

// hasArchiveSecondWindow 判断归档窗口是否能使用秒级切片。
func (j jobConfig) hasArchiveSecondWindow() bool {
	return j.ArchiveWindowSeconds > 0 && !j.isDateOnlyStringTimeColumn()
}

// hasDeleteSecondWindow 判断删除窗口是否能使用秒级切片。
func (j jobConfig) hasDeleteSecondWindow() bool {
	return j.DeleteWindowSeconds > 0 && !j.isDateOnlyStringTimeColumn()
}

// isUnixSecondsTimeColumn 判断当前任务是否使用 Unix int64 做归档游标。
func (j jobConfig) isUnixSecondsTimeColumn() bool {
	return j.TimeColumnType == TimeColumnTypeUnix
}
