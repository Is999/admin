package keys

import "strings"

// KeyTemplatePrefix 返回 Redis key 模板中第一个占位符之前的固定片段。
func KeyTemplatePrefix(template string) string {
	template = strings.TrimSpace(template)
	braceIndex := strings.Index(template, "{")
	percentIndex := strings.Index(template, "%")
	switch {
	case braceIndex >= 0 && percentIndex >= 0:
		if braceIndex < percentIndex {
			return template[:braceIndex]
		}
		return template[:percentIndex]
	case braceIndex >= 0:
		return template[:braceIndex]
	case percentIndex >= 0:
		return template[:percentIndex]
	default:
		return template
	}
}
