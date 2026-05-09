package keys_test

import (
	"testing"

	keys "admin/common/rediskeys"
)

// TestArchiveRedisKeys 验证归档锁 key 统一使用 archive 二级前缀。
func TestArchiveRedisKeys(t *testing.T) {
	useAppID(t, "215")
	if got := keys.ArchiveJobPlanRedisKey("admin_log"); got != "app:215:archive:job:admin_log:plan" {
		t.Fatalf("keys.ArchiveJobPlanRedisKey() = %q", got)
	}
	if got := keys.ArchiveJobWatermarkRedisKey("admin_log"); got != "app:215:archive:job:admin_log:watermark" {
		t.Fatalf("keys.ArchiveJobWatermarkRedisKey() = %q", got)
	}
	if got := keys.ArchiveJobCleanupRedisKey("admin_log"); got != "app:215:archive:job:admin_log:cleanup" {
		t.Fatalf("keys.ArchiveJobCleanupRedisKey() = %q", got)
	}
}
