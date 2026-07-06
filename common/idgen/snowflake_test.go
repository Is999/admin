package idgen

import (
	"hash/crc32"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestSnowflakeNextID 验证雪花 ID 在同一生成器内单调递增。
func TestSnowflakeNextID(t *testing.T) {
	generator, err := NewSnowflake(1)
	if err != nil {
		t.Fatalf("NewSnowflake() error = %v", err)
	}
	first, err := generator.NextID()
	if err != nil {
		t.Fatalf("NextID() first error = %v", err)
	}
	second, err := generator.NextID()
	if err != nil {
		t.Fatalf("NextID() second error = %v", err)
	}
	if second <= first {
		t.Fatalf("NextID() not increasing first=%d second=%d", first, second)
	}
}

// TestSnowflakeConcurrentUniqueIDs 验证成熟库节点在同一 worker 内并发生成 ID 不重复。
func TestSnowflakeConcurrentUniqueIDs(t *testing.T) {
	generator, err := NewSnowflake(31)
	if err != nil {
		t.Fatalf("NewSnowflake() error = %v", err)
	}

	const (
		workers       = 8
		perWorkerIDs  = 512
		expectedTotal = workers * perWorkerIDs
	)
	ids := make(chan int64, expectedTotal)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorkerIDs; j++ {
				id, nextErr := generator.NextID()
				if nextErr != nil {
					t.Errorf("NextID() error = %v", nextErr)
					return
				}
				ids <- id
			}
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[int64]struct{}, expectedTotal)
	for id := range ids {
		if got := snowflakeWorkerIDFromID(id); got != 31 {
			t.Fatalf("worker id in snowflake = %d want=31", got)
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicated snowflake id: %d", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != expectedTotal {
		t.Fatalf("generated id count = %d want=%d", len(seen), expectedTotal)
	}
}

// TestNextIDRequiresWorkerID 验证进程级 ID 生成必须先显式配置 worker_id。
func TestNextIDRequiresWorkerID(t *testing.T) {
	resetWorkerIDForTest()
	t.Cleanup(resetWorkerIDForTest)
	t.Setenv(snowflakeWorkerIDEnv, "")

	if _, err := NextID("user"); err == nil || !strings.Contains(err.Error(), "worker_id 未配置") {
		t.Fatalf("NextID() error = %v, want missing worker_id", err)
	}
}

// TestNextIDRejectsEmptyNamespace 验证调用方必须声明业务命名空间。
func TestNextIDRejectsEmptyNamespace(t *testing.T) {
	resetWorkerIDForTest()
	t.Cleanup(resetWorkerIDForTest)

	if err := ConfigureWorkerID(12); err != nil {
		t.Fatalf("ConfigureWorkerID() error = %v", err)
	}
	if _, err := NextID(" "); err == nil || !strings.Contains(err.Error(), "namespace 不能为空") {
		t.Fatalf("NextID(empty namespace) error = %v, want namespace error", err)
	}
}

// TestNextIDUsesConfiguredWorkerID 验证配置 worker_id 会进入雪花 ID worker 段。
func TestNextIDUsesConfiguredWorkerID(t *testing.T) {
	resetWorkerIDForTest()
	t.Cleanup(resetWorkerIDForTest)

	if err := ConfigureWorkerID(12); err != nil {
		t.Fatalf("ConfigureWorkerID() error = %v", err)
	}
	id, err := NextID("user")
	if err != nil {
		t.Fatalf("NextID() error = %v", err)
	}
	if got := snowflakeWorkerIDFromID(id); got != 12 {
		t.Fatalf("worker id in snowflake = %d want=12", got)
	}
}

// TestReleaseWorkerIDStopsNextID 验证 Redis 租约释放后进程不会继续使用旧 worker 发号。
func TestReleaseWorkerIDStopsNextID(t *testing.T) {
	resetWorkerIDForTest()
	t.Cleanup(resetWorkerIDForTest)

	if err := ConfigureWorkerID(12); err != nil {
		t.Fatalf("ConfigureWorkerID() error = %v", err)
	}
	ReleaseWorkerID(12)
	if _, err := NextID("user"); err == nil || !strings.Contains(err.Error(), "租约已释放") {
		t.Fatalf("NextID() error = %v, want released lease error", err)
	}
}

// TestConfigureWorkerIDClearsWorkerCache 验证 worker 变更会清空旧进程生成器缓存。
func TestConfigureWorkerIDClearsWorkerCache(t *testing.T) {
	resetWorkerIDForTest()
	t.Cleanup(resetWorkerIDForTest)

	if err := ConfigureWorkerID(12); err != nil {
		t.Fatalf("ConfigureWorkerID(12) error = %v", err)
	}
	if _, err := NextID("user"); err != nil {
		t.Fatalf("NextID() first error = %v", err)
	}
	if err := ConfigureWorkerID(13); err != nil {
		t.Fatalf("ConfigureWorkerID(13) error = %v", err)
	}
	id, err := NextID("user")
	if err != nil {
		t.Fatalf("NextID() second error = %v", err)
	}
	if got := snowflakeWorkerIDFromID(id); got != 13 {
		t.Fatalf("worker id after reconfigure = %d want=13", got)
	}
}

// TestNextIDIsolatesWorkerGeneratorAcrossNamespaces 验证不同业务命名空间使用独立本地生成器。
func TestNextIDIsolatesWorkerGeneratorAcrossNamespaces(t *testing.T) {
	resetWorkerIDForTest()
	t.Cleanup(resetWorkerIDForTest)

	if err := ConfigureWorkerID(12); err != nil {
		t.Fatalf("ConfigureWorkerID() error = %v", err)
	}
	if _, err := NextID("user"); err != nil {
		t.Fatalf("NextID(user) error = %v", err)
	}
	if _, err := NextID("order"); err != nil {
		t.Fatalf("NextID(order) error = %v", err)
	}
	if _, ok := workerGenerators.Load(snowflakeGeneratorKey{namespace: "user", workerID: 12}); !ok {
		t.Fatal("worker generator for user namespace missing")
	}
	if _, ok := workerGenerators.Load(snowflakeGeneratorKey{namespace: "order", workerID: 12}); !ok {
		t.Fatal("worker generator for order namespace missing")
	}
	count := 0
	workerGenerators.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 2 {
		t.Fatalf("worker generator count = %d want=2", count)
	}
}

// TestConfigureWorkerIDForNamespace 验证单业务命名空间可独立绑定 worker_id。
func TestConfigureWorkerIDForNamespace(t *testing.T) {
	resetWorkerIDForTest()
	t.Cleanup(resetWorkerIDForTest)

	if err := ConfigureWorkerIDForNamespace("recharge.order", 21); err != nil {
		t.Fatalf("ConfigureWorkerIDForNamespace() error = %v", err)
	}
	id, err := NextID("recharge.order")
	if err != nil {
		t.Fatalf("NextID(recharge.order) error = %v", err)
	}
	if got := snowflakeWorkerIDFromID(id); got != 21 {
		t.Fatalf("worker id = %d want=21", got)
	}
	if _, err = NextID("withdraw.order"); err == nil || !strings.Contains(err.Error(), "worker_id 未配置") {
		t.Fatalf("NextID(withdraw.order) error = %v, want missing worker", err)
	}
}

// TestNextIDUsesSegmentResolver 验证启用 Segment 的业务 namespace 不依赖雪花 worker_id。
func TestNextIDUsesSegmentResolver(t *testing.T) {
	resetWorkerIDForTest()
	t.Cleanup(resetWorkerIDForTest)

	resolver := &fakeSegmentResolver{
		enabled: map[string]bool{"recharge.order": true},
		next:    1000,
	}
	token := ConfigureSegmentResolver(resolver)
	t.Cleanup(func() { ClearSegmentResolver(token) })

	first, err := NextID("recharge.order")
	if err != nil {
		t.Fatalf("NextID(segment first) error = %v", err)
	}
	second, err := NextID("recharge.order")
	if err != nil {
		t.Fatalf("NextID(segment second) error = %v", err)
	}
	if first != 1001 || second != 1002 {
		t.Fatalf("segment ids = %d,%d want 1001,1002", first, second)
	}
	if _, ok := CurrentWorkerID("recharge.order"); ok {
		t.Fatal("segment namespace should not configure snowflake worker")
	}
}

// TestNextIDFallsBackToSnowflakeForNonSegmentNamespace 验证未配置 Segment 的业务仍走默认雪花策略。
func TestNextIDFallsBackToSnowflakeForNonSegmentNamespace(t *testing.T) {
	resetWorkerIDForTest()
	t.Cleanup(resetWorkerIDForTest)

	resolver := &fakeSegmentResolver{enabled: map[string]bool{"recharge.order": true}}
	token := ConfigureSegmentResolver(resolver)
	t.Cleanup(func() { ClearSegmentResolver(token) })
	if err := ConfigureWorkerID(12); err != nil {
		t.Fatalf("ConfigureWorkerID() error = %v", err)
	}
	id, err := NextID("withdraw.order")
	if err != nil {
		t.Fatalf("NextID(non segment) error = %v", err)
	}
	if got := snowflakeWorkerIDFromID(id); got != 12 {
		t.Fatalf("worker id = %d want=12", got)
	}
}

// TestShardNo 验证用户分片号固定来源于 ID 字符串 CRC32 哈希。
func TestShardNo(t *testing.T) {
	tests := []struct {
		id   int64 // id 表示待计算分片号的用户 ID。
		want int   // want 表示按 CRC32 规则期望得到的分片号。
	}{
		{id: 0, want: stableShardNoForTest(0)},
		{id: 999, want: stableShardNoForTest(999)},
		{id: 1000, want: stableShardNoForTest(1000)},
		{id: 1001, want: stableShardNoForTest(1001)},
	}
	for _, tt := range tests {
		if got := ShardNo(tt.id); got != tt.want {
			t.Fatalf("ShardNo(%d)=%d want=%d", tt.id, got, tt.want)
		}
	}
	if ShardNo(1024) == 0 {
		t.Fatal("ShardNo(1024) should not fall back to raw id%1024")
	}
}

// TestResolveWorkerIDEnv 验证显式 worker_id 环境变量优先生效。
func TestResolveWorkerIDEnv(t *testing.T) {
	resetWorkerIDForTest()
	t.Cleanup(resetWorkerIDForTest)
	t.Setenv(snowflakeWorkerIDEnv, "12")
	if got, err := ResolveWorkerID(SnowflakeWorkerIDUnset); err != nil || got != 12 {
		t.Fatalf("ResolveWorkerID()=%d err=%v want=12", got, err)
	}
}

// snowflakeWorkerIDFromID 从雪花 ID 中解析 worker_id 位段。
func snowflakeWorkerIDFromID(id int64) int64 {
	return (id >> snowflakeSequenceBits) & snowflakeMaxWorkerID
}

// stableShardNoForTest 使用与生产一致的 CRC32 规则计算测试期望分片号。
func stableShardNoForTest(id int64) int {
	return int(crc32.ChecksumIEEE([]byte(strconv.FormatInt(id, 10))) % ShardMod)
}

// fakeSegmentResolver 为 NextID 策略切换测试提供内存 Segment 解析器。
type fakeSegmentResolver struct {
	enabled map[string]bool // enabled 标记哪些业务 namespace 启用 Segment
	next    int64           // next 保存最后一次返回的 ID
}

// SegmentEnabled 判断测试 namespace 是否启用 Segment。
func (f *fakeSegmentResolver) SegmentEnabled(namespace string) bool {
	return f.enabled[namespace]
}

// SegmentID 返回递增的测试 Segment ID。
func (f *fakeSegmentResolver) SegmentID(string) (int64, error) {
	f.next++
	return f.next, nil
}
