package keys

import "testing"

// TestArchiveRedisKeys 验证归档锁 key 统一使用 archive 二级前缀。
func TestArchiveRedisKeys(t *testing.T) {
	if got := ArchiveJobPlanRedisKey("215", "admin_log"); got != "app:215:archive:job:admin_log:plan" {
		t.Fatalf("ArchiveJobPlanRedisKey() = %q", got)
	}
	if got := ArchiveJobWatermarkRedisKey("215", "admin_log"); got != "app:215:archive:job:admin_log:watermark" {
		t.Fatalf("ArchiveJobWatermarkRedisKey() = %q", got)
	}
	if got := ArchiveJobCleanupRedisKey("215", "admin_log"); got != "app:215:archive:job:admin_log:cleanup" {
		t.Fatalf("ArchiveJobCleanupRedisKey() = %q", got)
	}
}
