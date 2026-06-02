package runtimeconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	corelogic "admin/internal/logic"
)

// sha256Hex 返回快照 JSON 的 SHA256 十六进制摘要。
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// formatUnix 把 Unix 秒时间戳格式化为管理端展示时间。
func formatUnix(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return corelogic.FormatDateTime(time.Unix(ts, 0))
}

// uniqueStrings 去重并过滤空字符串，保持配置项原有顺序。
func uniqueStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}
