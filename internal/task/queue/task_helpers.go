package taskqueue

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// normalizeStrings 去空、去重并排序字符串切片。
func normalizeStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}

// newID 生成新的全局唯一标识。
func newID() string { return uuid.NewString() }

// mustJSON 把任意值序列化为 JSON 字符串；调用方默认传入可序列化结构。
func mustJSON(value any) string { return string(mustJSONBytes(value)) }

// mustJSONBytes 把任意值序列化为 JSON 字节切片。
func mustJSONBytes(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

// toInt 尝试把 Redis 读取值或头信息转换为 int。
func toInt(value any) int {
	switch v := value.(type) {
	case nil:
		return 0
	case int:
		return v
	case int64:
		return int(v)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(v))
		return i
	default:
		return 0
	}
}

// toInt64 尝试把 Redis 读取值或 JSON 字段转换为 int64。
func toInt64(value any) int64 {
	switch v := value.(type) {
	case nil:
		return 0
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		parsed, _ := v.Int64()
		return parsed
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return i
	default:
		return 0
	}
}

// durationBetweenRFC3339 计算两个 RFC3339 时间之间的耗时，结束时间为空时按当前时间计算。
func durationBetweenRFC3339(startText string, endText string) int64 {
	startText = strings.TrimSpace(startText)
	if startText == "" {
		return 0
	}
	start, err := time.Parse(time.RFC3339, startText)
	if err != nil {
		return 0
	}
	end := time.Now()
	if strings.TrimSpace(endText) != "" {
		parsedEnd, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(endText))
		if parseErr != nil {
			return 0
		}
		end = parsedEnd
	}
	if end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}
