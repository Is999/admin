package keys

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// CollectorRedisKey 返回带当前 app_id 命名空间的 Collector 自管 Redis key。
func CollectorRedisKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return key
	}
	if HasPrefix(key) {
		return WithPrefix(key)
	}
	if key == collectorRedisRoot {
		return WithPrefix(collectorRedisRoot)
	}
	key = strings.TrimPrefix(key, collectorRedisRoot+":")
	if key == "" {
		return WithPrefix(collectorRedisRoot)
	}
	return WithPrefix(collectorRedisRoot + ":" + key)
}

// CollectorIdempotencyRedisKey 返回 Collector 单任务 EventID 幂等去重 Redis key。
func CollectorIdempotencyRedisKey(bizType string, eventID string) string {
	bizType = strings.TrimSpace(bizType)
	eventID = strings.TrimSpace(eventID)
	if bizType == "" || eventID == "" {
		return ""
	}
	return CollectorRedisKey(joinKeyParts(collectorIdempotencySegment, collectorIdempotencyDigest(bizType, eventID)))
}

// collectorIdempotencyDigest 返回 bizType 和 EventID 摘要，避免不同任务间互相去重。
func collectorIdempotencyDigest(bizType string, eventID string) string {
	sum := sha256.Sum256([]byte(bizType + "\x00" + eventID))
	return hex.EncodeToString(sum[:16])
}
