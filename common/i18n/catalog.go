package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/active.*.json
var localeFS embed.FS

// localeMessageCatalog 表示从 JSON 语言资产加载的单个语种文案。
type localeMessageCatalog map[string]string

// messageCatalog 缓存已加载的 JSON 语言包，供翻译和完整性测试复用。
var messageCatalog = loadMessageCatalog()

// messageBundle 是进程级多语言文案集合，启动后只读。
var messageBundle = buildMessageBundle(messageCatalog)

// loadMessageCatalog 从 go:embed JSON 资产加载语言包。
func loadMessageCatalog() map[string]localeMessageCatalog {
	catalog := make(map[string]localeMessageCatalog, len(supportedLocales))
	for _, locale := range supportedLocales {
		path := fmt.Sprintf("locales/active.%s.json", locale)
		data, err := localeFS.ReadFile(path)
		if err != nil {
			panic(fmt.Sprintf("加载语言包失败 locale=%s path=%s: %v", locale, path, err))
		}
		messages := localeMessageCatalog{}
		if err := json.Unmarshal(data, &messages); err != nil {
			panic(fmt.Sprintf("解析语言包失败 locale=%s path=%s: %v", locale, path, err))
		}
		catalog[locale] = messages
	}
	return catalog
}

// buildMessageBundle 把 JSON 语言包注册到 go-i18n Bundle。
func buildMessageBundle(catalog map[string]localeMessageCatalog) *goi18n.Bundle {
	bundle := goi18n.NewBundle(language.SimplifiedChinese)
	for _, locale := range supportedLocales {
		messages := catalog[locale]
		ids := make([]string, 0, len(messages))
		for id := range messages {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		items := make([]*goi18n.Message, 0, len(ids))
		for _, id := range ids {
			items = append(items, &goi18n.Message{ID: id, Other: messages[id]})
		}
		tag := language.MustParse(locale)
		if err := bundle.AddMessages(tag, items...); err != nil {
			panic(fmt.Sprintf("注册语言包失败 locale=%s: %v", locale, err))
		}
	}
	return bundle
}
