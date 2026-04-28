package keys

import "testing"

// TestUserTagRedisKeys 验证用户标签锁和屏障 key 统一使用 user_tag 二级前缀。
func TestUserTagRedisKeys(t *testing.T) {
	if got := UserTagWorkflowLeaseRedisKey("215"); got != "app:215:user_tag:workflow:write_lock" {
		t.Fatalf("UserTagWorkflowLeaseRedisKey() = %q", got)
	}
	if got := UserTagWorkflowSyncDoneRedisKey("215", "wf-1"); got != "app:215:user_tag:workflow:sync_done:wf-1" {
		t.Fatalf("UserTagWorkflowSyncDoneRedisKey() = %q", got)
	}
	if got := UserTagRuntimeCleanupRedisKey("215"); got != "app:215:user_tag:runtime:cleanup:lock" {
		t.Fatalf("UserTagRuntimeCleanupRedisKey() = %q", got)
	}
	if got := UserTagKafkaOutboxRetryScanRedisKey("215"); got != "app:215:user_tag:kafka_outbox:retry_scan:lock" {
		t.Fatalf("UserTagKafkaOutboxRetryScanRedisKey() = %q", got)
	}
}
