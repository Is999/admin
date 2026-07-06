package keys

import (
	"fmt"
	"strings"
)

// SnowflakeNodeLeaseKey 返回跨 admin/api 共享的雪花 node_id 租约 key。
func SnowflakeNodeLeaseKey(scope string, namespace string, nodeID int64) string {
	scope = strings.TrimSpace(scope)
	namespace = strings.TrimSpace(namespace)
	if scope == "" || namespace == "" {
		return ""
	}
	return fmt.Sprintf(SnowflakeNodeLease, scope, namespace, nodeID)
}

// IDSegmentCounterKey 返回跨 admin/api 共享的业务号段高水位 key。
func IDSegmentCounterKey(scope string, namespace string) string {
	scope = strings.TrimSpace(scope)
	namespace = strings.TrimSpace(namespace)
	if scope == "" || namespace == "" {
		return ""
	}
	return fmt.Sprintf(IDSegmentCounter, scope, namespace)
}
