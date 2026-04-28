package loggerx

import (
	"context"
	"fmt"
	"time"

	"github.com/Is999/go-utils/errors"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// GormLogger 把 GORM 日志接入统一结构化日志体系，并复用请求上下文中的 trace 与用户字段。
type GormLogger struct {
	level         gormlogger.LogLevel // 当前日志级别，决定是否输出 info/warn/error SQL 日志
	slowThreshold time.Duration       // 慢 SQL 阈值，超过后按 slow 日志输出
}

// NewGormLogger 创建自定义 GORM logger，slowThreshold 用于慢 SQL 告警。
func NewGormLogger(slowThreshold time.Duration) gormlogger.Interface {
	return &GormLogger{
		level:         gormlogger.Warn,
		slowThreshold: slowThreshold,
	}
}

// LogMode 返回一份新的 logger 副本，符合 GORM logger.Interface 的链式设计。
func (g *GormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	cp := *g
	cp.level = level
	return &cp
}

// Info 输出普通 SQL 信息级日志。
func (g *GormLogger) Info(ctx context.Context, format string, args ...interface{}) {
	if g.level < gormlogger.Info {
		return
	}
	InfowSkip(ctx, 1, "数据库 信息日志", logx.Field("detail", fmt.Sprintf(format, args...)))
}

// Warn 输出警告级 SQL 日志。
func (g *GormLogger) Warn(ctx context.Context, format string, args ...interface{}) {
	if g.level < gormlogger.Warn {
		return
	}
	InfowSkip(ctx, 1, "数据库 警告日志", logx.Field("detail", fmt.Sprintf(format, args...)))
}

// Error 输出错误级 SQL 日志。
func (g *GormLogger) Error(ctx context.Context, format string, args ...interface{}) {
	if g.level < gormlogger.Error {
		return
	}
	detail := fmt.Sprintf(format, args...)
	ErrorTextwSkip(ctx, 1, "数据库 错误日志", detail, logx.Field("detail", detail))
}

// Trace 是 GORM 的核心回调：统一记录错误 SQL、慢 SQL 和调试 SQL。
func (g *GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if g.level == gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()
	fields := []logx.LogField{
		logx.Field("latency_ms", elapsed.Milliseconds()),
		logx.Field("rows", rows),
		logx.Field("sql", sql),
	}

	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		ErrorwSkip(ctx, 1, "数据库 查询失败", err, fields...)
	case g.slowThreshold > 0 && elapsed > g.slowThreshold:
		SlowwSkip(ctx, 1, "数据库 慢查询", fields...)
	case g.level >= gormlogger.Info:
		InfowSkip(ctx, 1, "数据库 查询", fields...)
	}
}
