package docs

import "embed"

// FS 内嵌打包文档站点静态资源，供生产环境直接读取。
//
//go:embed all:site
var FS embed.FS
