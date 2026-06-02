package idgen

import "testing"

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

// TestShardNo 验证用户分片号固定按 1000 取模。
func TestShardNo(t *testing.T) {
	tests := []struct {
		id   int64
		want int
	}{
		{id: 0, want: 0},
		{id: 999, want: 999},
		{id: 1000, want: 0},
		{id: 1001, want: 1},
	}
	for _, tt := range tests {
		if got := ShardNo(tt.id); got != tt.want {
			t.Fatalf("ShardNo(%d)=%d want=%d", tt.id, got, tt.want)
		}
	}
}

// TestAutoWorkerIDEnv 验证显式 worker_id 环境变量优先生效。
func TestAutoWorkerIDEnv(t *testing.T) {
	t.Setenv(snowflakeWorkerIDEnv, "12")
	if got := AutoWorkerID("user"); got != 12 {
		t.Fatalf("AutoWorkerID()=%d want=12", got)
	}
}
