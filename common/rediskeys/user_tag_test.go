package keys

import "testing"

// TestUserTagRedisKeys 验证用户标签锁和屏障 key 统一使用 user_tag 二级前缀。
func TestUserTagRedisKeys(t *testing.T) {
	if got := UserTagWorkflowLeaseRedisKey("215"); got != "app:215:user_tag:workflow:write_lock" {
		t.Fatalf("UserTagWorkflowLeaseRedisKey() = %q", got)
	}
	if got := UserTagWorkflowFinalDoneRedisKey("215", "wf-1"); got != "app:215:user_tag:workflow:final_done:wf-1" {
		t.Fatalf("UserTagWorkflowFinalDoneRedisKey() = %q", got)
	}
	if got := UserTagRuntimeCleanupRedisKey("215"); got != "app:215:user_tag:runtime:cleanup:lock" {
		t.Fatalf("UserTagRuntimeCleanupRedisKey() = %q", got)
	}
	if got := UserTagEventOutboxRetryScanRedisKey("215"); got != "app:215:user_tag:event_outbox:retry_scan:lock" {
		t.Fatalf("UserTagEventOutboxRetryScanRedisKey() = %q", got)
	}
}
