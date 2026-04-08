package localcache

import "testing"

// BenchmarkCacheGet 度量封装层热 key 读取开销。
func BenchmarkCacheGet(b *testing.B) {
	cache, err := New[string, string](Options{
		NumCounters: 10_000,
		MaxCost:     10_000,
	})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	defer cache.Close()

	if ok := cache.Set("local:cache:bench", "value"); !ok {
		b.Fatal("Set() = false")
	}
	cache.Wait()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := cache.Get("local:cache:bench"); !ok {
			b.Fatal("Get() should hit")
		}
	}
}
