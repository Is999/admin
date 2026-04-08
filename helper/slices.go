package helper

import (
	"strings"

	utils "github.com/Is999/go-utils"
)

// FirstNonEmptyString 返回首个去除首尾空白后仍非空的字符串。
// 该方法用于“显式值优先、默认值兜底”的轻量选择场景，返回值会同步 TrimSpace，避免调用方继续重复清洗。
func FirstNonEmptyString(values ...string) string {
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text != "" {
			return text
		}
	}
	return ""
}

// UniqueNonEmptyStrings 清洗字符串列表并按首次出现顺序去重。
// 空白值会被丢弃，去重逻辑复用 go-utils。
func UniqueNonEmptyStrings(items []string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(item)
		if text == "" {
			continue
		}
		result = append(result, text)
	}
	return utils.Unique(result)
}
