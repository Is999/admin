package i18n

import (
	"strconv"
	"strings"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
)

// MessageByCode 按业务码返回本地化消息，适合作为默认响应文案。
func MessageByCode(code int, locale string) string {
	key, ok := codeToMessageKey[code]
	if !ok {
		return MessageByKey(MsgKeyFail, locale)
	}
	return MessageByKey(key, locale)
}

// MessageByKey 按消息 key 返回本地化文案，支持模板参数。
func MessageByKey(key, locale string, args ...any) string {
	normalizedLocale := NormalizeLocale(locale)
	defaultText := defaultMessage(key)
	if defaultText == "" {
		return key
	}
	cfg := &goi18n.LocalizeConfig{
		MessageID: key,
		DefaultMessage: &goi18n.Message{
			ID:    key,
			Other: defaultText,
		},
	}
	if len(args) > 0 {
		cfg.TemplateData = templateData(args)
	}
	text, err := goi18n.NewLocalizer(messageBundle, normalizedLocale, LocaleZHCN).Localize(cfg)
	if err != nil {
		return key
	}
	return text
}

// MessageTemplateHasArgs 判断消息模板是否包含格式化占位符。
// 该方法用于区分“用于对外文案填充的参数”和“仅供内部错误链补充的上下文参数”。
func MessageTemplateHasArgs(key string) bool {
	return strings.Contains(defaultMessage(key), "{{.")
}

// defaultMessage 返回中文优先的默认文案模板。
func defaultMessage(key string) string {
	if tpl := messageCatalog[LocaleZHCN][key]; tpl != "" {
		return tpl
	}
	for _, catalog := range messageCatalog {
		if tpl := catalog[key]; tpl != "" {
			return tpl
		}
	}
	return ""
}

// templateData 把位置参数映射为 Arg0、Arg1，供 JSON 文案模板引用。
func templateData(args []any) map[string]any {
	data := make(map[string]any, len(args))
	for i, arg := range args {
		data["Arg"+strconv.Itoa(i)] = arg
	}
	return data
}
