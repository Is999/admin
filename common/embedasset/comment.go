package embedasset

import "strings"

const (
	// unicodeBOM 表示 UTF-8 BOM，部分编辑器保存模板文件时可能写入，需要在剥离文件头前统一去掉。
	unicodeBOM = "\ufeff"
)

// StripLeadingLineComments 删除 go:embed 文本资产开头的连续行注释。
// 仅剥离文件头注释，遇到第一行真实 SQL/Lua 后停止。
func StripLeadingLineComments(source string, prefixes ...string) string {
	// normalizedSource 保存去除 BOM 后的资产内容，避免首行注释前缀匹配失败。
	normalizedSource := strings.TrimPrefix(source, unicodeBOM)
	// commentPrefixes 保存调用方允许剥离的行注释前缀，例如 SQL/Lua 资产的 "--"。
	commentPrefixes := normalizeCommentPrefixes(prefixes)
	if len(commentPrefixes) == 0 {
		return normalizedSource
	}
	// lines 保留每行原始换行符，确保剥离文件头后不改写真实脚本内容。
	lines := strings.SplitAfter(normalizedSource, "\n")
	// bodyIndex 表示第一行真实可执行内容所在位置。
	bodyIndex := 0
	// strippedComment 标记是否已经剥离过注释；没有命中文件头注释时返回原文，避免意外删除普通文件开头空行。
	strippedComment := false
	for bodyIndex < len(lines) {
		// currentLine 保存当前行去掉左右空白后的内容，用于判断是否仍处于文件头注释块。
		currentLine := strings.TrimSpace(lines[bodyIndex])
		if currentLine == "" {
			bodyIndex++
			continue
		}
		if hasLineCommentPrefix(currentLine, commentPrefixes) {
			strippedComment = true
			bodyIndex++
			continue
		}
		break
	}
	if !strippedComment {
		return normalizedSource
	}
	// executableText 保存真正传给数据库或 Redis 的脚本内容；只裁掉注释块后的空白行，不压缩业务 SQL/Lua。
	executableText := strings.Join(lines[bodyIndex:], "")
	return strings.TrimLeft(executableText, "\r\n")
}

// normalizeCommentPrefixes 清洗调用方声明的行注释前缀，空前缀会被忽略。
func normalizeCommentPrefixes(prefixes []string) []string {
	// normalized 保存去重前的有效前缀；通常只有 "--"，容量按输入长度预分配即可。
	normalized := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		// item 表示当前待清洗的注释前缀。
		item := strings.TrimSpace(prefix)
		if item == "" {
			continue
		}
		normalized = append(normalized, item)
	}
	return normalized
}

// hasLineCommentPrefix 判断当前行是否命中允许剥离的文件头行注释前缀。
func hasLineCommentPrefix(line string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}
