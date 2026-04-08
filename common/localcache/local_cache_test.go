package localcache

import (
	"testing"
	"time"
)

// TestCacheSetGetAndMetrics 验证基础读写、删除和指标快照。
func TestCacheSetGetAndMetrics(t *testing.T) {
	cache, err := New[string, string](Options{
		NumCounters: 1_000,
		MaxCost:     1_000,
		Metrics:     true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer cache.Close()

	if ok := cache.Set("local:cache:key", "value"); !ok {
		t.Fatal("Set() = false")
	}
	cache.Wait()

	value, ok := cache.Get("local:cache:key")
	if !ok || value != "value" {
		t.Fatalf("Get() = %q, %v; want value, true", value, ok)
	}
	if _, ok = cache.GetTTL("local:cache:key"); !ok {
		t.Fatal("GetTTL() should find key without expiration")
	}

	cache.Del("local:cache:key")
	cache.Wait()
	if _, ok = cache.Get("local:cache:key"); ok {
		t.Fatal("Get() after Del() should miss")
	}

	metrics := cache.Metrics()
	if metrics.Hits == 0 || metrics.Misses == 0 {
		t.Fatalf("Metrics() hits=%d misses=%d, want both positive", metrics.Hits, metrics.Misses)
	}
}

// TestCacheSetWithTTL 验证 TTL 到期后 Get 不返回过期值。
func TestCacheSetWithTTL(t *testing.T) {
	cache, err := New[string, string](Options{
		NumCounters: 1_000,
		MaxCost:     1_000,
		TTL:         20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer cache.Close()

	if ok := cache.Set("local:cache:ttl", "value"); !ok {
		t.Fatal("Set() = false")
	}
	cache.Wait()

	if _, ok := cache.Get("local:cache:ttl"); !ok {
		t.Fatal("Get() before expiration should hit")
	}
	time.Sleep(30 * time.Millisecond)
	if _, ok := cache.Get("local:cache:ttl"); ok {
		t.Fatal("Get() after expiration should miss")
	}
}

// TestCacheCostFallback 验证非法成本会回退为默认成本。
func TestCacheCostFallback(t *testing.T) {
	cache, err := New[string, string](Options{
		NumCounters:        1_000,
		MaxCost:            1_000,
		ItemCost:           3,
		Metrics:            true,
		IgnoreInternalCost: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer cache.Close()

	if ok := cache.SetWithCost("local:cache:cost", "value", 0); !ok {
		t.Fatal("SetWithCost() = false")
	}
	cache.Wait()

	if got := cache.Metrics().CostAdded; got != 3 {
		t.Fatalf("Metrics().CostAdded = %d, want 3", got)
	}
}

// TestCacheRejectNegativeTTL 验证负 TTL 不写入缓存。
func TestCacheRejectNegativeTTL(t *testing.T) {
	cache, err := New[string, string](Options{
		NumCounters: 1_000,
		MaxCost:     1_000,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer cache.Close()

	if ok := cache.SetWithTTL("local:cache:bad-ttl", "value", -time.Second); ok {
		t.Fatal("SetWithTTL() with negative TTL should return false")
	}
	cache.Wait()
	if _, ok := cache.Get("local:cache:bad-ttl"); ok {
		t.Fatal("Get() should miss negative TTL write")
	}
}

// TestCacheCloseIdempotent 验证 Close 可以重复调用且关闭后读写安全返回。
func TestCacheCloseIdempotent(t *testing.T) {
	cache, err := New[string, string](Options{
		NumCounters: 1_000,
		MaxCost:     1_000,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	cache.Close()
	cache.Close()
	if ok := cache.Set("local:cache:closed", "value"); ok {
		t.Fatal("Set() after Close() should return false")
	}
	if _, ok := cache.Get("local:cache:closed"); ok {
		t.Fatal("Get() after Close() should miss")
	}
	cache.Wait()
	cache.Del("local:cache:closed")
	cache.Clear()
}
