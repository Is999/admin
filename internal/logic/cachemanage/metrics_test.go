package cachemanage

import (
	"context"
	"testing"
	"time"

	"admin/common/codes"
	keys "admin/common/rediskeys"
	"admin/internal/config"
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	"admin/internal/svc"
	"admin/internal/types"

	tablecache "github.com/Is999/table-cache"
	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

// TestTableCacheMetricPrefixContract 固定启动注册与管理页消费的指标前缀，避免两端一起漂移后测试仍误通过。
func TestTableCacheMetricPrefixContract(t *testing.T) {
	if cachelogic.TableCacheMetricPrefix != "tcache_" {
		t.Fatalf("TableCacheMetricPrefix = %q, want %q", cachelogic.TableCacheMetricPrefix, "tcache_")
	}
}

// TestBuildCacheMetricsResp 验证真实表缓存指标可被管理页读取并正确汇总。
func TestBuildCacheMetricsResp(t *testing.T) {
	generatedAt := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	registry := prometheus.NewRegistry()
	metrics, err := tablecache.NewPrometheusMetrics(
		tablecache.WithPrometheusRegisterer(registry),
		tablecache.WithPrometheusSubsystem(cachelogic.TableCacheMetricsSubsystem),
	)
	if err != nil {
		t.Fatalf("NewPrometheusMetrics() error = %v", err)
	}
	for index := 0; index < 8; index++ {
		metrics.RecordCacheHit(ctx, "admin_info")
	}
	for index := 0; index < 2; index++ {
		metrics.RecordCacheMiss(ctx, "admin_info")
	}
	// 细分读取状态不参与基础命中率，避免与 cache_hit/cache_miss 重复计数。
	metrics.RecordLookupState(ctx, "admin_info", tablecache.LookupStateEmpty)
	for index := 0; index < 3; index++ {
		metrics.RecordRefresh(ctx, "admin_info", "success", time.Millisecond)
	}
	metrics.RecordRefresh(ctx, "admin_info", "error", time.Millisecond)
	metrics.RecordLoaderError(ctx, "admin_info", context.Canceled)
	metrics.RecordLockFailed(ctx, "admin_info")
	metrics.RecordWaitTimeout(ctx, "admin_info")
	metrics.RecordScanFallback(ctx, "admin_info", "admin:info:")
	metrics.RecordRefreshBatch(ctx, "all", "partial_failed", 7, 5, 2)

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("registry.Gather() error = %v", err)
	}

	result := buildCacheMetricsResp(families, []types.CacheItem{
		{Index: "admin_info", KeyTitle: "admin_info:%d", Remark: "管理员信息", Category: "session"},
		{Index: "permission", KeyTitle: "permission", Remark: "权限", Category: "auth"},
	}, generatedAt)

	if result.Scope != "current_process" || result.GeneratedAt != generatedAt.Format(time.RFC3339) {
		t.Fatalf("unexpected metadata: %+v", result)
	}
	if len(result.Targets) != 2 {
		t.Fatalf("len(Targets) = %d, want 2", len(result.Targets))
	}
	if result.Summary.HitTotal != 8 || result.Summary.MissTotal != 2 || result.Summary.HitRate != 80 {
		t.Fatalf("unexpected lookup summary: %+v", result.Summary)
	}
	if result.Summary.RefreshSuccessTotal != 3 || result.Summary.RefreshErrorTotal != 1 || result.Summary.LoaderErrorTotal != 1 {
		t.Fatalf("unexpected refresh summary: %+v", result.Summary)
	}
	if result.Summary.BatchSuccessTotal != 5 || result.Summary.BatchFailedTotal != 2 {
		t.Fatalf("unexpected batch summary: %+v", result.Summary)
	}
	if result.Summary.LockFailedTotal != 1 || result.Summary.WaitTimeoutTotal != 1 || result.Summary.ScanFallbackTotal != 1 {
		t.Fatalf("unexpected contention summary: %+v", result.Summary)
	}
	if result.Targets[0].Index != "admin_info" || result.Targets[0].HitTotal != 8 {
		t.Fatalf("unexpected active target: %+v", result.Targets[0])
	}
	if result.Targets[1].Index != "permission" || result.Targets[1].HitTotal != 0 {
		t.Fatalf("unexpected zero target: %+v", result.Targets[1])
	}
}

// TestTableCacheManagerRecordsLookupMetrics 验证请求作用域中的真实 Manager 会复用启动期指标记录器。
func TestTableCacheManagerRecordsLookupMetrics(t *testing.T) {
	ctx := context.Background()
	registry := prometheus.NewRegistry()
	metrics, err := tablecache.NewPrometheusMetrics(
		tablecache.WithPrometheusRegisterer(registry),
		tablecache.WithPrometheusSubsystem(cachelogic.TableCacheMetricsSubsystem),
	)
	if err != nil {
		t.Fatalf("NewPrometheusMetrics() error = %v", err)
	}
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	svcCtx := svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{
		Rds:               client,
		TableCacheMetrics: metrics,
	})
	base := corelogic.NewBaseLogicWithContext(ctx, svcCtx.ScopedWithContext(ctx))
	manager, err := cachelogic.TableCacheManager(base)
	if err != nil {
		t.Fatalf("TableCacheManager() error = %v", err)
	}
	key := cachelogic.TableCachePhysicalKey(base, keys.RoleTree)
	if err = client.Set(ctx, key, "[]", time.Minute).Err(); err != nil {
		t.Fatalf("Set(%s) error = %v", key, err)
	}
	var value []any
	result, err := manager.GetState(ctx, key, &value)
	if err != nil {
		t.Fatalf("GetState(%s) error = %v", key, err)
	}
	if result.State != tablecache.LookupStateHit {
		t.Fatalf("GetState(%s) state = %s, want hit", key, result.State)
	}
	_, summary, refreshErr := manager.RefreshByKeysWithSummary(ctx, []string{key, key})
	if refreshErr == nil || summary.Total != 1 || summary.Failed != 1 {
		t.Fatalf("RefreshByKeysWithSummary() summary = %+v error = %v, want one failed deduplicated key", summary, refreshErr)
	}

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("registry.Gather() error = %v", err)
	}
	metricsResp := buildCacheMetricsResp(families, cachelogic.TableCacheItems(base), time.Now())
	if metricsResp.Summary.BatchFailedTotal != 1 {
		t.Fatalf("batch failed metrics = %d, want 1", metricsResp.Summary.BatchFailedTotal)
	}
	for _, target := range metricsResp.Targets {
		if target.Index == keys.RoleTree {
			if target.HitTotal != 1 || target.MissTotal != 0 || target.RefreshErrorTotal != 1 || target.LoaderErrorTotal != 1 {
				t.Fatalf("role tree metrics = %+v, want hit=1 and one failed refresh", target)
			}
			return
		}
	}
	t.Fatalf("metrics target %s not found", keys.RoleTree)
}

// TestMetricsRequiresRecorder 验证指标装配断链时接口明确失败，避免返回误导性的全零快照。
func TestMetricsRequiresRecorder(t *testing.T) {
	logicObj := &SystemCacheLogic{
		BaseLogic: corelogic.NewBaseLogicWithContext(
			context.Background(),
			svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{}),
		),
	}
	result := logicObj.Metrics()
	if result == nil || result.Code != codes.ServerError || result.Error == nil {
		t.Fatalf("Metrics() = %+v, want server error", result)
	}
}
