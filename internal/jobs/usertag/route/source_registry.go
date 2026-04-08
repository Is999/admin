package route

import (
	"regexp"
	"strings"
)

var clickHouseIdentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// QuoteClickHouseIdent 返回安全的 ClickHouse 标识符。
func QuoteClickHouseIdent(name string) string {
	name = strings.TrimSpace(name)
	if !clickHouseIdentPattern.MatchString(name) {
		return "``"
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
