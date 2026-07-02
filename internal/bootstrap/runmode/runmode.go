package runmode

import (
	"fmt"
	"strings"

	"github.com/Is999/go-utils/errors"
)

const (
	// API 启动 HTTP API（bit=1）。
	API = 1 << iota
	// Worker 启动异步任务 Worker（bit=2）。
	Worker
	// Scheduler 启动周期调度器 leader（bit=4）。
	Scheduler
	// All 同时启动 API、Worker 和调度器（bit=7）。
	All = API | Worker | Scheduler
)

// Normalize 规范化位掩码模式，传入 0 时默认回退为全量模式。
func Normalize(mode int) (int, error) {
	if mode == 0 {
		return All, nil
	}
	if mode < 0 || mode&^All != 0 {
		return 0, errors.Errorf("不支持的启动模式: %d，仅支持 1(API)、2(Worker)、4(Scheduler) 及 3/5/6/7 组合", mode)
	}
	return mode, nil
}

// Resolve 按“命令行优先、配置兜底、默认全量”的规则决定最终启动模式。
func Resolve(cliMode *int, configMode int) (int, error) {
	if cliMode != nil && *cliMode != 0 {
		return Normalize(*cliMode)
	}
	return Normalize(configMode)
}

// Has 判断当前运行模式是否包含指定能力位。
func Has(mode, flag int) bool {
	return mode&flag == flag
}

// ShouldStartWorker 判断当前模式是否允许启动 Worker 类后台消费者。
func ShouldStartWorker(mode int) bool {
	return Has(mode, Worker)
}

// ShouldStartTask 判断当前模式是否允许启动任务系统后台生命周期。
func ShouldStartTask(mode int) bool {
	return Has(mode, Worker) || Has(mode, Scheduler)
}

// Format 把位掩码模式转换成便于日志阅读的文字描述。
func Format(mode int) string {
	parts := make([]string, 0, 3)
	if Has(mode, API) {
		parts = append(parts, "api")
	}
	if Has(mode, Worker) {
		parts = append(parts, "worker")
	}
	if Has(mode, Scheduler) {
		parts = append(parts, "scheduler")
	}
	if len(parts) == 0 {
		return "none"
	}
	return fmt.Sprintf("%d(%s)", mode, strings.Join(parts, "+"))
}
