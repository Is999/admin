package i18n

import (
	"strings"

	"golang.org/x/text/language"
)

const (
	// LocaleZHCN 表示管理后台默认简体中文语言标签。
	LocaleZHCN = "zh-CN"
	// LocaleENUS 表示管理后台英文语言标签。
	LocaleENUS = "en-US"
)

// supportedLocales 表示后端响应文案当前维护的语种。
var supportedLocales = []string{LocaleZHCN, LocaleENUS}

// NormalizeLocale 把请求语言标准化为当前系统支持的语言标签。
func NormalizeLocale(locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return LocaleZHCN
	}
	tags, _, err := language.ParseAcceptLanguage(locale)
	if err != nil || len(tags) == 0 {
		tag, parseErr := language.Parse(locale)
		if parseErr != nil {
			return LocaleZHCN
		}
		tags = []language.Tag{tag}
	}
	for _, tag := range tags {
		if locale := supportedLocale(tag); locale != "" {
			return locale
		}
	}
	return LocaleZHCN
}

// supportedLocale 将标准语言标签映射到当前后端支持的语言。
func supportedLocale(tag language.Tag) string {
	if strings.EqualFold(tag.String(), LocaleENUS) {
		return LocaleENUS
	}
	base, _ := tag.Base()
	switch base.String() {
	case "en":
		return LocaleENUS
	case "zh":
		return LocaleZHCN
	default:
		return ""
	}
}
