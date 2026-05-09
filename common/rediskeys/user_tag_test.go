package keys_test

import (
	"testing"

	keys "admin/common/rediskeys"
)

// TestUserTagRedisKeys 验证用户标签锁和屏障 key 统一使用 user_tag 二级前缀。
func TestUserTagRedisKeys(t *testing.T) {
	useAppID(t, "215")
	if got := keys.UserTagWorkflowLeaseRedisKey(); got != "app:215:user_tag:workflow:write_lock" {
		t.Fatalf("keys.UserTagWorkflowLeaseRedisKey() = %q", got)
	}
	if got := keys.UserTagWorkflowFinalDoneRedisKey("wf-1"); got != "app:215:user_tag:workflow:final_done:wf-1" {
		t.Fatalf("keys.UserTagWorkflowFinalDoneRedisKey() = %q", got)
	}
	if got := keys.UserTagRuntimeCleanupRedisKey(); got != "app:215:user_tag:runtime:cleanup:lock" {
		t.Fatalf("keys.UserTagRuntimeCleanupRedisKey() = %q", got)
	}
	if got := keys.UserTagEventOutboxRetryScanRedisKey(); got != "app:215:user_tag:event_outbox:retry_scan:lock" {
		t.Fatalf("keys.UserTagEventOutboxRetryScanRedisKey() = %q", got)
	}
}
