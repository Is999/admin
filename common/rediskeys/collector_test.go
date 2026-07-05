package keys_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	keys "admin/common/rediskeys"
)

// TestCollectorRedisKey 验证 Collector 自管 key 统一收敛到 collector 二级前缀。
func TestCollectorRedisKey(t *testing.T) {
	tests := []struct {
		name  string // name 表示测试场景名称。
		appID string // appID 表示测试应用 ID。
		key   string // key 表示待验证 key。
		want  string // want 表示期望结果。
	}{
		{name: "逻辑key追加collector前缀", appID: "215", key: "idempotency:event-1", want: "app:215:collector:idempotency:event-1"},
		{name: "已有collector前缀不重复追加", appID: "215", key: "collector:idempotency:event-1", want: "app:215:collector:idempotency:event-1"},
		{name: "collector根段只保留一次", appID: "215", key: "collector", want: "app:215:collector"},
		{name: "当前app完整key保持不变", appID: "215", key: "app:215:collector:idempotency:event-1", want: "app:215:collector:idempotency:event-1"},
		{name: "其它app完整key失败闭合", appID: "102", key: "app:215:collector:idempotency:event-1", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useAppID(t, tt.appID)
			if got := keys.CollectorRedisKey(tt.key); got != tt.want {
				t.Fatalf("keys.CollectorRedisKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCollectorIdempotencyRedisKey 验证单任务 EventID 幂等 key 带 app_id 且空值失败闭合。
func TestCollectorIdempotencyRedisKey(t *testing.T) {
	useAppID(t, "215")
	want := "app:215:collector:idempotency:" + collectorTestDigest("admin_log.audit", "event-1")
	if got := keys.CollectorIdempotencyRedisKey(" admin_log.audit ", " event-1 "); got != want {
		t.Fatalf("keys.CollectorIdempotencyRedisKey() = %q", got)
	}
	if got := keys.CollectorIdempotencyRedisKey(" ", "event-1"); got != "" {
		t.Fatalf("keys.CollectorIdempotencyRedisKey(empty bizType) = %q, want empty", got)
	}
	if got := keys.CollectorIdempotencyRedisKey("admin_log.audit", " "); got != "" {
		t.Fatalf("keys.CollectorIdempotencyRedisKey(empty eventID) = %q, want empty", got)
	}
}

// TestCollectorIdempotencyRedisKeyIsolatesBizType 验证不同任务使用相同 EventID 不会共享去重 key。
func TestCollectorIdempotencyRedisKeyIsolatesBizType(t *testing.T) {
	useAppID(t, "215")
	adminLogKey := keys.CollectorIdempotencyRedisKey("admin_log.audit", "event-1")
	userStatKey := keys.CollectorIdempotencyRedisKey("user_stat", "event-1")
	if adminLogKey == "" || userStatKey == "" {
		t.Fatal("Collector 幂等 key 不应为空")
	}
	if adminLogKey == userStatKey {
		t.Fatalf("不同 bizType 不应共享 Collector 幂等 key: %s", adminLogKey)
	}
}

// TestCollectorRedisKeyFailsClosedWithoutAppID 验证缺少 app_id 时不会生成裸 Collector key。
func TestCollectorRedisKeyFailsClosedWithoutAppID(t *testing.T) {
	useAppID(t, "")
	if got := keys.CollectorIdempotencyRedisKey("admin_log.audit", "event-1"); got != "" {
		t.Fatalf("keys.CollectorIdempotencyRedisKey(empty app) = %q, want empty", got)
	}
}

// collectorTestDigest 返回测试期望使用的单任务 EventID 摘要。
func collectorTestDigest(bizType string, eventID string) string {
	sum := sha256.Sum256([]byte(bizType + "\x00" + eventID))
	return hex.EncodeToString(sum[:16])
}
