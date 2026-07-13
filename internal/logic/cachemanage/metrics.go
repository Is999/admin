package cachemanage

import (
	"fmt"
	"os"
	"sort"
	"time"

	"admin/common/codes"
	i18n "admin/common/i18n"
	cachelogic "admin/internal/logic/cache"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// tableCacheMetricsStartedAt 记录当前进程表缓存指标的累计起点。
var tableCacheMetricsStartedAt = time.Now()

// Metrics 返回当前管理进程的表缓存运行指标快照。
func (l *SystemCacheLogic) Metrics() *types.BizResult {
	if l == nil || l.Svc == nil || l.Svc.TableCacheMetrics == nil {
		err := errors.Errorf("表缓存运行指标未初始化")
		return types.ServerError(i18n.MsgKeyCacheInfoFail, err,
			"SystemCacheLogic.Metrics 表缓存运行指标未装配").ToBizResult()
	}
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return types.ServerError(i18n.MsgKeyCacheInfoFail, err,
			"SystemCacheLogic.Metrics 采集表缓存运行指标失败").ToBizResult()
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(buildCacheMetricsResp(families, cachelogic.TableCacheItems(l.BaseLogic), time.Now()))
}

// buildCacheMetricsResp 把 Prometheus 指标转换为管理页需要的紧凑快照。
func buildCacheMetricsResp(families []*dto.MetricFamily, items []types.CacheItem, generatedAt time.Time) types.CacheMetricsResp {
	targets := make(map[string]*types.CacheMetricTarget, len(items))
	for _, item := range items {
		item := item
		targets[item.Index] = &types.CacheMetricTarget{
			Index: item.Index, KeyTitle: item.KeyTitle, Remark: item.Remark, Category: item.Category,
		}
	}
	summary := types.CacheMetricSummary{}
	for _, family := range families {
		if family == nil {
			continue
		}
		name := family.GetName()
		if name == cachelogic.TableCacheMetricPrefix+"refresh_batch_success_items_total" {
			summary.BatchSuccessTotal += metricFamilyTotal(family)
			continue
		}
		if name == cachelogic.TableCacheMetricPrefix+"refresh_batch_failed_items_total" {
			summary.BatchFailedTotal += metricFamilyTotal(family)
			continue
		}
		for _, metric := range family.GetMetric() {
			index := metricLabel(metric, "index")
			if index == "" {
				continue
			}
			target := targets[index]
			if target == nil {
				target = &types.CacheMetricTarget{Index: index, Category: "system"}
				targets[index] = target
			}
			value := metricCounterValue(metric)
			switch name {
			case cachelogic.TableCacheMetricPrefix + "cache_hit_total":
				target.HitTotal += value
			case cachelogic.TableCacheMetricPrefix + "cache_miss_total":
				target.MissTotal += value
			case cachelogic.TableCacheMetricPrefix + "refresh_total":
				if result := metricLabel(metric, "result"); result == "success" || result == "wait_success" {
					target.RefreshSuccessTotal += value
				} else {
					target.RefreshErrorTotal += value
				}
			case cachelogic.TableCacheMetricPrefix + "loader_error_total":
				target.LoaderErrorTotal += value
			case cachelogic.TableCacheMetricPrefix + "lock_failed_total":
				target.LockFailedTotal += value
			case cachelogic.TableCacheMetricPrefix + "wait_timeout_total":
				target.WaitTimeoutTotal += value
			case cachelogic.TableCacheMetricPrefix + "scan_fallback_total":
				target.ScanFallbackTotal += value
			}
		}
	}

	rows := make([]types.CacheMetricTarget, 0, len(targets))
	for _, target := range targets {
		target.HitRate = cacheHitRate(target.HitTotal, target.MissTotal)
		summary.HitTotal += target.HitTotal
		summary.MissTotal += target.MissTotal
		summary.RefreshSuccessTotal += target.RefreshSuccessTotal
		summary.RefreshErrorTotal += target.RefreshErrorTotal
		summary.LoaderErrorTotal += target.LoaderErrorTotal
		summary.LockFailedTotal += target.LockFailedTotal
		summary.WaitTimeoutTotal += target.WaitTimeoutTotal
		summary.ScanFallbackTotal += target.ScanFallbackTotal
		rows = append(rows, *target)
	}
	summary.HitRate = cacheHitRate(summary.HitTotal, summary.MissTotal)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Index < rows[j].Index })

	return types.CacheMetricsResp{
		Scope:       "current_process",
		InstanceID:  cacheMetricsInstanceID(),
		StartedAt:   tableCacheMetricsStartedAt.Format(time.RFC3339),
		GeneratedAt: generatedAt.Format(time.RFC3339),
		Summary:     summary,
		Targets:     rows,
	}
}

// metricFamilyTotal 汇总同一指标族内的计数器值。
func metricFamilyTotal(family *dto.MetricFamily) int64 {
	var total int64
	for _, metric := range family.GetMetric() {
		total += metricCounterValue(metric)
	}
	return total
}

// metricCounterValue 读取 Prometheus Counter 当前值。
func metricCounterValue(metric *dto.Metric) int64 {
	if metric == nil || metric.Counter == nil {
		return 0
	}
	return int64(metric.GetCounter().GetValue())
}

// metricLabel 读取 Prometheus 指标标签。
func metricLabel(metric *dto.Metric, name string) string {
	if metric == nil {
		return ""
	}
	for _, label := range metric.GetLabel() {
		if label.GetName() == name {
			return label.GetValue()
		}
	}
	return ""
}

// cacheHitRate 计算百分比命中率。
func cacheHitRate(hit, miss int64) float64 {
	total := hit + miss
	if total <= 0 {
		return 0
	}
	return float64(hit) * 100 / float64(total)
}

// cacheMetricsInstanceID 返回便于区分多实例的进程标识。
func cacheMetricsInstanceID() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s:%d", hostname, os.Getpid())
}
