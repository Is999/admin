package batchprocessor

import (
	"context"
	"strings"
	"time"
)

const (
	runtimeAlertKindFlushFailed    = "batch_processor_flush_failed"    // 批处理收集器 flush 失败
	runtimeAlertKindFallbackFailed = "batch_processor_fallback_failed" // 批处理收集器必达兜底失败
	runtimeAlertKindProcessFailed  = "batch_processor_process_failed"  // 批处理后台 process 失败
)

// RuntimeAlert 描述批处理框架后台协程中不直接返回给调用方的运行异常。
type RuntimeAlert struct {
	Kind       string    // 异常类型，用于上层告警指纹和排障归类
	Title      string    // 告警标题
	Status     string    // 当前处理状态
	Component  string    // 发生异常的组件
	Operation  string    // 发生异常的运行操作
	BizType    string    // 关联业务类型
	UniqueKey  string    // 关联幂等或业务键
	Reason     string    // 异常原因摘要
	Advice     string    // 处理建议
	OccurredAt time.Time // 发现异常的时间
}

// AlertHook 接收批处理后台运行异常；上层负责限频、日志和外部通知。
type AlertHook func(ctx context.Context, alert RuntimeAlert)

// normalizeRuntimeAlert 清理告警字段，保证上层通知文本稳定。
func normalizeRuntimeAlert(alert RuntimeAlert) RuntimeAlert {
	alert.Kind = strings.TrimSpace(alert.Kind)
	alert.Title = strings.TrimSpace(alert.Title)
	alert.Status = strings.TrimSpace(alert.Status)
	alert.Component = strings.TrimSpace(alert.Component)
	alert.Operation = strings.TrimSpace(alert.Operation)
	alert.BizType = strings.TrimSpace(alert.BizType)
	alert.UniqueKey = strings.TrimSpace(alert.UniqueKey)
	alert.Reason = strings.TrimSpace(alert.Reason)
	alert.Advice = strings.TrimSpace(alert.Advice)
	if alert.OccurredAt.IsZero() {
		alert.OccurredAt = time.Now()
	}
	return alert
}
