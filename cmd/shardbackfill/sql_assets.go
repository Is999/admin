package main

import (
	"embed"
	"strings"

	"admin/common/embedasset"

	"github.com/Is999/go-utils/errors"
)

// backfillAssets 嵌入回填命令使用的 MySQL 代码资产。
//
//go:embed assets/*.sql.tmpl
var backfillAssets embed.FS

// sqlAsset 读取 SQL 资产并剥离仅供代码审查的文件头说明。
func sqlAsset(name string) (string, error) {
	data, err := backfillAssets.ReadFile("assets/" + name)
	if err != nil {
		return "", errors.Wrapf(err, "读取回填 SQL 资产失败 asset=%s", name)
	}
	return strings.TrimSpace(embedasset.StripLeadingLineComments(string(data), "--")), nil
}

// renderSQL 只把已校验标识符写入 SQL 结构，其余值继续使用预编译参数。
func renderSQL(name string, identifiers map[string]string) (string, error) {
	query, err := sqlAsset(name)
	if err != nil {
		return "", errors.Tag(err)
	}
	for placeholder, identifier := range identifiers {
		if _, err = validateName(identifier, "SQL标识符"); err != nil {
			return "", errors.Tag(err)
		}
		query = strings.ReplaceAll(query, placeholder, quoted(identifier))
	}
	if strings.Contains(query, "__TABLE__") || strings.Contains(query, "__PRIMARY_KEY__") || strings.Contains(query, "__UID_COLUMN__") {
		return "", errors.Errorf("回填 SQL 资产缺少标识符 asset=%s", name)
	}
	return query, nil
}
