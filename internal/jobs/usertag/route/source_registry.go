package route

import (
	"regexp"
	"strings"
)

// clickHouseIdentPattern 限制 ClickHouse 标识符只能使用安全字符。
var clickHouseIdentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// QuoteClickHouseIdent 返回安全的 ClickHouse 标识符。
func QuoteClickHouseIdent(name string) string {
	name = strings.TrimSpace(name)
	if !clickHouseIdentPattern.MatchString(name) {
		return "``"
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
