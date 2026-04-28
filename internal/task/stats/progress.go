package taskstats

import (
	"math"
	"strings"
)

const (
	// ProgressUnitShard 表示按工作流分片统计进度。
	ProgressUnitShard = "shard"
	// ProgressUnitNodeInstance 表示按工作流节点实例统计进度。
	ProgressUnitNodeInstance = "node_instance"

	// ProgressStatusPending 表示进度对象尚未执行。
	ProgressStatusPending = "pending"
	// ProgressStatusRunning 表示进度对象正在执行。
	ProgressStatusRunning = "running"
	// ProgressStatusSuccess 表示进度对象已成功。
	ProgressStatusSuccess = "success"
	// ProgressStatusFailed 表示进度对象已失败。
	ProgressStatusFailed = "failed"
	// ProgressStatusSkipped 表示进度对象已跳过。
	ProgressStatusSkipped = "skipped"
)

// Progress 表示任务或工作流的执行进度。
type Progress struct {
	Unit           string  `json:"unit,omitempty"`           // 进度单位，如 shard、node_instance
	Status         string  `json:"status,omitempty"`         // 当前状态，如 pending/running/success/failed
	Total          int64   `json:"total,omitempty"`          // 计划执行总量
	Finished       int64   `json:"finished,omitempty"`       // 已进入终态数量
	Succeeded      int64   `json:"succeeded,omitempty"`      // 成功数量
	Failed         int64   `json:"failed,omitempty"`         // 失败数量
	Skipped        int64   `json:"skipped,omitempty"`        // 跳过数量
	Running        int64   `json:"running,omitempty"`        // 运行中数量
	Pending        int64   `json:"pending,omitempty"`        // 等待执行数量
	Remaining      int64   `json:"remaining,omitempty"`      // 未进入终态数量
	Percent        float64 `json:"percent,omitempty"`        // 终态完成比例，0~100
	SuccessPercent float64 `json:"successPercent,omitempty"` // 成功比例，0~100
	Indeterminate  bool    `json:"indeterminate,omitempty"`  // 是否缺少总量，无法计算百分比
}

// NewProgress 根据状态和计数构造执行进度。
func NewProgress(unit string, status string, total int64, succeeded int64, failed int64, skipped int64) *Progress {
	status = normalizeProgressStatus(status)
	total = nonNegativeProgressCount(total)
	succeeded = nonNegativeProgressCount(succeeded)
	failed = nonNegativeProgressCount(failed)
	skipped = nonNegativeProgressCount(skipped)
	finished := succeeded + failed + skipped
	if total < finished {
		total = finished
	}
	remaining := maxProgressCount(total-finished, 0)
	running := int64(0)
	pending := int64(0)
	switch status {
	case ProgressStatusRunning:
		running = remaining
	case ProgressStatusPending:
		pending = remaining
	}
	return buildProgressSnapshot(unit, status, total, succeeded, failed, skipped, running, pending)
}

// MergeProgress 合并多个进度快照。
func MergeProgress(unit string, status string, items ...*Progress) *Progress {
	merged := Progress{Unit: strings.TrimSpace(unit), Status: normalizeProgressStatus(status)}
	for _, item := range items {
		if item == nil {
			continue
		}
		merged.Total += nonNegativeProgressCount(item.Total)
		merged.Succeeded += nonNegativeProgressCount(item.Succeeded)
		merged.Failed += nonNegativeProgressCount(item.Failed)
		merged.Skipped += nonNegativeProgressCount(item.Skipped)
		merged.Running += nonNegativeProgressCount(item.Running)
		merged.Pending += nonNegativeProgressCount(item.Pending)
		if item.Indeterminate {
			merged.Indeterminate = true
		}
	}
	if merged.Total == 0 && merged.Succeeded == 0 && merged.Failed == 0 && merged.Skipped == 0 && merged.Running == 0 && merged.Pending == 0 {
		return nil
	}
	progress := buildProgressSnapshot(merged.Unit, merged.Status, merged.Total, merged.Succeeded, merged.Failed, merged.Skipped, merged.Running, merged.Pending)
	if progress != nil && merged.Indeterminate {
		progress.Indeterminate = true
	}
	return progress
}

// buildProgressSnapshot 按归一化计数构造进度快照。
func buildProgressSnapshot(unit string, status string, total int64, succeeded int64, failed int64, skipped int64, running int64, pending int64) *Progress {
	unit = strings.TrimSpace(unit)
	status = normalizeProgressStatus(status)
	total = nonNegativeProgressCount(total)
	succeeded = nonNegativeProgressCount(succeeded)
	failed = nonNegativeProgressCount(failed)
	skipped = nonNegativeProgressCount(skipped)
	running = nonNegativeProgressCount(running)
	pending = nonNegativeProgressCount(pending)
	finished := succeeded + failed + skipped
	if total < finished {
		total = finished
	}
	remaining := maxProgressCount(total-finished, 0)
	if running+pending > remaining {
		if running > remaining {
			running = remaining
			pending = 0
		} else {
			pending = remaining - running
		}
	}
	progress := &Progress{
		Unit:      unit,
		Status:    status,
		Total:     total,
		Finished:  finished,
		Succeeded: succeeded,
		Failed:    failed,
		Skipped:   skipped,
		Running:   running,
		Pending:   pending,
		Remaining: remaining,
	}
	if total <= 0 {
		progress.Indeterminate = true
		return progress
	}
	progress.Percent = progressPercent(finished, total)
	progress.SuccessPercent = progressPercent(succeeded, total)
	return progress
}

// normalizeProgressStatus 把外部状态收敛为进度模型的稳定状态。
func normalizeProgressStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "succeeded", "completed":
		return ProgressStatusSuccess
	case "failure":
		return ProgressStatusFailed
	default:
		return status
	}
}

// progressPercent 计算一位小数的百分比，返回值限制在 0~100。
func progressPercent(value int64, total int64) float64 {
	if total <= 0 || value <= 0 {
		return 0
	}
	result := float64(value) * 100 / float64(total)
	if result > 100 {
		result = 100
	}
	return math.Round(result*10) / 10
}

// nonNegativeProgressCount 把异常负数进度计数收敛为 0。
func nonNegativeProgressCount(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

// maxProgressCount 返回较大的进度计数。
func maxProgressCount(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
