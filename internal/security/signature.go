package security

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"admin/helper"

	utils "github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
)

// SignFieldAll 表示所有首层字段参与排序签名。
const SignFieldAll = "*"

// BuildSignString 使用版本化长度前缀协议生成无歧义签名串。
// traceID 对应 X-Trace-Id，timestamp 对应 X-Timestamp，AppID 和字段值均按 UTF-8 字节长度编码。
func BuildSignString(data map[string]any, signParams []string, traceID, timestamp, appID string) string {
	params := resolveSignParams(data, signParams)
	sort.Strings(params)

	var builder strings.Builder
	builder.WriteString("v2|app=")
	writeSignStringPart(&builder, appID)
	builder.WriteString("|trace=")
	writeSignStringPart(&builder, traceID)
	builder.WriteString("|timestamp=")
	writeSignStringPart(&builder, timestamp)
	for _, key := range params {
		value, ok := SignFieldValue(data, key)
		if !ok || isEmptySignValue(value) {
			continue
		}
		builder.WriteString("|field=")
		writeSignStringPart(&builder, key)
		writeSignStringPart(&builder, SignValueString(value))
	}
	return builder.String()
}

// SignFieldValue 按点路径读取参与签名的首层或嵌套字段值。
func SignFieldValue(data map[string]any, field string) (any, bool) {
	path := strings.Split(strings.TrimSpace(field), ".")
	if len(path) == 0 {
		return nil, false
	}
	var current any = data
	for _, segment := range path {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return nil, false
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// writeSignStringPart 写入 UTF-8 字节长度和原文，避免字段分隔符出现在值中时产生碰撞。
func writeSignStringPart(builder *strings.Builder, value string) {
	builder.WriteString(strconv.Itoa(len(value)))
	builder.WriteByte(':')
	builder.WriteString(value)
}

// resolveSignParams 解析签名字段列表；配置了 * 时，对当前 map 的所有首层字段签名。
func resolveSignParams(data map[string]any, signParams []string) []string {
	params := helper.UniqueNonEmptyStrings(signParams)
	if !utils.Contains(SignFieldAll, params) {
		return params
	}
	result := make([]string, 0, len(data))
	for key := range data {
		switch strings.TrimSpace(key) {
		case "", "sign", "ciphertext":
			continue
		default:
			result = append(result, key)
		}
	}
	return result
}

// SignValueString 把参与签名的值转换为稳定字符串。
func SignValueString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", v), "0"), ".")
	case float32:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", v), "0"), ".")
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, bool:
		return fmt.Sprint(v)
	default:
		body, err := stableJSONMarshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(body)
	}
}

// stableJSONMarshal 对复杂值执行递归稳定 JSON 序列化，确保对象 key 顺序在前后端完全一致。
func stableJSONMarshal(value any) ([]byte, error) {
	normalized, err := normalizeStableJSONValue(value)
	if err != nil {
		return nil, errors.Tag(err)
	}
	var builder strings.Builder
	if err := writeStableJSON(&builder, normalized); err != nil {
		return nil, errors.Tag(err)
	}
	return []byte(builder.String()), nil
}

// normalizeStableJSONValue 先把任意复杂值收敛成 map/slice/json.Number 基础结构，避免 struct 等类型直接参与排序。
func normalizeStableJSONValue(value any) (any, error) {
	switch v := value.(type) {
	case nil, string, bool, json.Number,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return v, nil
	case map[string]any:
		return v, nil
	case []any:
		return v, nil
	default:
		body, err := json.Marshal(v)
		if err != nil {
			return nil, errors.Tag(err)
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		var normalized any
		if err := decoder.Decode(&normalized); err != nil {
			return nil, errors.Tag(err)
		}
		return normalized, nil
	}
}

// writeStableJSON 递归输出稳定 JSON，map key 统一按字典序排序。
func writeStableJSON(builder *strings.Builder, value any) error {
	switch v := value.(type) {
	case nil:
		builder.WriteString("null")
		return nil
	case string:
		body, err := json.Marshal(v)
		if err != nil {
			return errors.Tag(err)
		}
		builder.Write(body)
		return nil
	case bool:
		if v {
			builder.WriteString("true")
		} else {
			builder.WriteString("false")
		}
		return nil
	case json.Number:
		builder.WriteString(v.String())
		return nil
	case float64:
		builder.WriteString(strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", v), "0"), "."))
		return nil
	case float32:
		builder.WriteString(strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", v), "0"), "."))
		return nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		builder.WriteString(fmt.Sprint(v))
		return nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		builder.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				builder.WriteByte(',')
			}
			keyBody, err := json.Marshal(key)
			if err != nil {
				return errors.Tag(err)
			}
			builder.Write(keyBody)
			builder.WriteByte(':')
			if err := writeStableJSON(builder, v[key]); err != nil {
				return errors.Tag(err)
			}
		}
		builder.WriteByte('}')
		return nil
	case []any:
		builder.WriteByte('[')
		for index, item := range v {
			if index > 0 {
				builder.WriteByte(',')
			}
			if err := writeStableJSON(builder, item); err != nil {
				return errors.Tag(err)
			}
		}
		builder.WriteByte(']')
		return nil
	default:
		normalized, err := normalizeStableJSONValue(v)
		if err != nil {
			return errors.Tag(err)
		}
		return writeStableJSON(builder, normalized)
	}
}

// isEmptySignValue 判断字段是否应跳过签名，空字符串和 nil
func isEmptySignValue(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return text == ""
	}
	return false
}
