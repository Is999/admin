package sharding

import "strings"

// ValidateIdentifier 校验可进入迁移 SQL 结构位置的标识符。
func ValidateIdentifier(name string) error {
	return validateTableName(strings.TrimSpace(name))
}

// Tables 返回迁移计划中按桶范围升序排列的全部物理表。
func (p Plan) Tables() []Table {
	tables := make([]Table, 0, p.count)
	for index := 0; index < p.count; index++ {
		table, _ := p.TableAt(index)
		tables = append(tables, table)
	}
	return tables
}
