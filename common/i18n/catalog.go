package i18n

// localeMessageCatalog 表示单个语言包；map key 是 MsgKey* 多语言 key，value 是当前语言的展示文案或格式化模板。
type localeMessageCatalog map[string]string

// messageCatalog 按语言标签收口后端管理后台语言包；结构保持与前端 langs/{locale} 的按语种拆分思路一致。
var messageCatalog = map[string]localeMessageCatalog{
	LocaleZHCN: zhCNMessageCatalog,
	LocaleENUS: enUSMessageCatalog,
}
