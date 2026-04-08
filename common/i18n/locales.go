package i18n

import "strings"

const (
	// LocaleZHCN 表示管理后台默认简体中文语言标签。
	LocaleZHCN = "zh-CN"
	// LocaleENUS 表示管理后台英文语言标签。
	LocaleENUS = "en-US"
)

// NormalizeLocale 把请求语言标准化为当前系统支持的语言标签。
func NormalizeLocale(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return LocaleZHCN
	}

	parts := strings.Split(s, ",")
	primary := strings.TrimSpace(parts[0])
	if i := strings.Index(primary, ";"); i >= 0 {
		primary = strings.TrimSpace(primary[:i])
	}
	primary = strings.ToLower(primary)

	switch {
	case strings.HasPrefix(primary, "en"):
		return LocaleENUS
	case strings.HasPrefix(primary, "zh"):
		return LocaleZHCN
	default:
		return LocaleZHCN
	}
}
