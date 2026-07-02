package larkx

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// cardMarkdown 生成 Lark Markdown 文本块。
func cardMarkdown(format string, args ...any) messageCardElement {
	return messageCardElement{
		Tag: "div",
		Text: &messageCardText{
			Tag:     "lark_md",
			Content: strings.TrimSpace(fmt.Sprintf(format, args...)),
		},
	}
}

// cardFields 生成保留空值占位的双列字段块。
func cardFields(items [][2]string) messageCardElement {
	fields := make([]messageCardField, 0, len(items))
	for _, item := range items {
		label := strings.TrimSpace(item[0])
		value := cardValue(item[1], "-")
		if label == "" {
			continue
		}
		fields = append(fields, messageCardField{
			IsShort: true,
			Text: messageCardText{
				Tag:     "lark_md",
				Content: "**" + label + "**\n" + value,
			},
		})
	}
	return messageCardElement{Tag: "div", Fields: fields}
}

// cardFieldsCompact 生成忽略空字段的双列字段块。
func cardFieldsCompact(items [][2]string) messageCardElement {
	fields := make([]messageCardField, 0, len(items))
	for _, item := range items {
		label := strings.TrimSpace(item[0])
		value := strings.TrimSpace(item[1])
		if label == "" || value == "" {
			continue
		}
		fields = append(fields, messageCardField{
			IsShort: true,
			Text: messageCardText{
				Tag:     "lark_md",
				Content: "**" + label + "**\n" + value,
			},
		})
	}
	return messageCardElement{Tag: "div", Fields: fields}
}

// cardValue 返回非空字段值，空值使用指定占位文本。
func cardValue(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return fallback
}

// cardDivider 生成 Lark 卡片分隔线。
func cardDivider() messageCardElement {
	return messageCardElement{Tag: "hr"}
}

// formatCardWindow 格式化 Lark 卡片统计窗口。
func formatCardWindow(start, end time.Time) string {
	startText := formatCardTime(start)
	endText := formatCardTime(end)
	if startText == "" || endText == "" {
		return "-"
	}
	return startText + " ~ " + endText
}

// formatCardTime 使用分钟粒度展示 Lark 卡片时间。
func formatCardTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02 15:04")
}

// joinNonEmpty 拼接非空字段，避免卡片出现多余分隔符。
func joinNonEmpty(sep string, values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, sep)
}

// shortCardText 对卡片字段做空值占位和长度保护。
func shortCardText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return truncateByBytes(value, limit)
}

// truncateError 规范化错误摘要并按配置限制长度。
func (n *Notifier) truncateError(err error) string {
	if err == nil {
		return ""
	}
	return n.truncateText(err.Error())
}

// truncateText 规范化文本摘要并按配置限制长度。
func (n *Notifier) truncateText(text string) string {
	msg := strings.Join(strings.Fields(text), " ")
	if msg == "" {
		return ""
	}
	limit := n.maxErrorByte
	if limit <= 0 {
		limit = defaultMaxErrorBytes
	}
	if len(msg) <= limit {
		return msg
	}
	return truncateByBytes(msg, limit)
}

// truncateByBytes 按字节限制截断文本，并尽量保留省略号空间。
func truncateByBytes(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	const ellipsis = "..."
	if limit <= len(ellipsis) {
		return utf8PrefixByBytes(text, limit)
	}
	return utf8PrefixByBytes(text, limit-len(ellipsis)) + ellipsis
}

// utf8PrefixByBytes 返回不破坏 UTF-8 字符边界的前缀。
func utf8PrefixByBytes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	end := 0
	for i, r := range text {
		size := utf8.RuneLen(r)
		if size < 0 {
			size = 1
		}
		if i+size > limit {
			break
		}
		end = i + size
	}
	return text[:end]
}
