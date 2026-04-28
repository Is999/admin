package taskqueue

import (
	"admin/internal/infra/loggerx"
	"fmt"

	"github.com/zeromicro/go-zero/core/logx"
)

// asynqLogger 把 Asynq 内部日志转成项目统一的 logx 输出。
type asynqLogger struct{}

// logger 返回跳过当前适配层后的 logger，避免 caller 永远落在 `taskqueue/logger.go`。
// 对 Asynq 这类第三方库日志，跳过一层后能定位到实际触发日志的 Asynq 源码位置。
func (asynqLogger) logger() logx.Logger { return loggerx.LoggerWithCallerSkip(1) }

// Debug 转发 Asynq 调试日志。
func (l asynqLogger) Debug(args ...interface{}) { l.logger().Debug(fmt.Sprint(args...)) }

// Info 转发 Asynq 常规运行日志。
func (l asynqLogger) Info(args ...interface{}) { l.logger().Info(fmt.Sprint(args...)) }

// Warn 转发 Asynq 告警日志；当前按 Info 级别输出，避免被基础设施告警误判为任务失败。
func (l asynqLogger) Warn(args ...interface{}) { l.logger().Info(fmt.Sprint(args...)) }

// Error 转发 Asynq 错误日志。
func (l asynqLogger) Error(args ...interface{}) { l.logger().Error(fmt.Sprint(args...)) }

// Fatal 转发 Asynq 致命日志；是否退出进程由上层启动入口统一决定。
func (l asynqLogger) Fatal(args ...interface{}) { l.logger().Error(fmt.Sprint(args...)) }
