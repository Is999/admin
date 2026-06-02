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

// TestNextIDSharesWorkerGeneratorAcrossNamespaces 验证同一 worker 不会因命名空间拆出独立序列。
func TestNextIDSharesWorkerGeneratorAcrossNamespaces(t *testing.T) {
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
	if _, ok := workerGenerators.Load(int64(12)); !ok {
		t.Fatal("worker generator for worker_id=12 missing")
	}
	count := 0
	workerGenerators.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 1 {
		t.Fatalf("worker generator count = %d want=1", count)
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
	if ShardNo(1000) == 0 {
		t.Fatal("ShardNo(1000) should not fall back to id%1000")
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
