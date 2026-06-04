package routealias

import (
	"net/url"
	pathpkg "path"
	"strings"
)

const (
	// docsFileAliasPrefix 表示单篇 Markdown 文档权限 module 前缀。
	docsFileAliasPrefix = "docs.file."
)

// docsPermissionRule 描述文档目录前缀与权限别名的关系。
type docsPermissionRule struct {
	Prefix string // /api/docs 下的相对目录前缀，使用文档站真实目录名
	Alias  Alias  // 命中目录后用于后台权限表校验的稳定别名
}

// docsPermissionRules 维护会随文档目录变动调整的访问规则；更具体的二级目录放在前面。
var docsPermissionRules = []docsPermissionRule{
	{Prefix: "角色文档/运维", Alias: DocsRoleOps},
	{Prefix: "角色文档/后端开发", Alias: DocsRoleBackend},
	{Prefix: "角色文档/前端与测试", Alias: DocsRoleFrontend},
	{Prefix: "功能模块/任务系统", Alias: DocsFeatureTask},
	{Prefix: "功能模块/用户标签", Alias: DocsFeatureUserTag},
	{Prefix: "api/接口文档/前台系统", Alias: DocsAPIServiceFront},
	{Prefix: "api/接口文档", Alias: DocsAPIServiceIndex},
	{Prefix: "api", Alias: DocsAPIServiceIndex},
	{Prefix: "接口文档/后台系统", Alias: DocsAPIAdmin},
	{Prefix: "接口文档/任务系统", Alias: DocsAPITask},
	{Prefix: "接口文档/用户标签", Alias: DocsUserTag},
	{Prefix: "接口文档", Alias: DocsAPIIndex},
}

// docsContentPaths 维护当前文档站可直接阅读的 Markdown 文档路径；docsify 资源不进入角色授权。
var docsContentPaths = []string{
	"功能模块/任务系统/任务系统使用手册.md",
	"功能模块/任务系统/任务系统首页.md",
	"功能模块/用户标签/任务系统与用户标签排障手册.md",
	"功能模块/用户标签/用户标签实现与调度说明.md",
	"功能模块/用户标签/用户标签操作手册.md",
	"功能模块/用户标签/用户标签首页.md",
	"功能模块/用户标签/用户标签骨架验收指南.md",
	"功能模块/用户标签/用户标签骨架验证指南.md",
	"接口文档/任务系统/任务列表接口.md",
	"接口文档/任务系统/任务总控接口.md",
	"接口文档/任务系统/任务注册表接口.md",
	"接口文档/任务系统/任务监控接口.md",
	"接口文档/任务系统/任务系统接口.md",
	"接口文档/任务系统/任务队列接口.md",
	"接口文档/任务系统/收集器任务接口.md",
	"接口文档/任务系统/运行配置管理接口.md",
	"接口文档/后台系统/个人信息接口.md",
	"接口文档/后台系统/内网初始化管理员接口.md",
	"接口文档/后台系统/前台用户管理接口.md",
	"接口文档/后台系统/后台系统接口.md",
	"接口文档/后台系统/基础规范与认证接口.md",
	"接口文档/后台系统/安全调试接口.md",
	"接口文档/后台系统/接口文档访问接口.md",
	"接口文档/后台系统/文件传输接口.md",
	"接口文档/后台系统/日志管理接口.md",
	"接口文档/后台系统/权限管理接口.md",
	"接口文档/后台系统/消息中心接口.md",
	"接口文档/后台系统/秘钥管理接口.md",
	"接口文档/后台系统/管理员管理接口.md",
	"接口文档/后台系统/系统配置接口.md",
	"接口文档/后台系统/缓存管理接口.md",
	"接口文档/后台系统/角色管理接口.md",
	"接口文档/接口文档统一规范.md",
	"接口文档/接口文档首页.md",
	"接口文档/用户标签/内网接口.md",
	"接口文档/用户标签/指定标签重算接口.md",
	"接口文档/用户标签/用户标签工作流接口.md",
	"接口文档/用户标签/用户标签接口.md",
	"文档首页.md",
	"角色文档/前端与测试/前端权限码与管理员权限映射说明.md",
	"角色文档/前端与测试/接口联调与验收说明.md",
	"角色文档/后端开发/AI开发提示词.md",
	"角色文档/后端开发/AI开发规范.md",
	"角色文档/后端开发/任务队列与工作流指南.md",
	"角色文档/后端开发/多因素认证与管理员角色规则.md",
	"角色文档/后端开发/库表路由规范.md",
	"角色文档/后端开发/开发扩展指南.md",
	"角色文档/后端开发/组件注册清单.md",
	"角色文档/后端开发/配置字段说明.md",
	"角色文档/运维/数据库迁移治理.md",
	"角色文档/运维/部署发布指南.md",
	"api/接口文档/前台系统/健康检查接口.md",
	"api/接口文档/前台系统/用户接口.md",
	"api/接口文档/前台系统/系统接口.md",
	"api/接口文档/前台系统/认证接口.md",
	"api/接口文档/接口文档统一规范.md",
	"api/角色文档/后端开发/AI开发提示词.md",
	"api/角色文档/后端开发/AI开发规范.md",
	"api/角色文档/后端开发/前端安全清单同步.md",
	"api/角色文档/后端开发/开发扩展指南.md",
	"api/角色文档/后端开发/组件注册清单.md",
	"api/角色文档/后端开发/认证安全指标与告警.md",
	"api/角色文档/运维/数据库迁移治理.md",
	"api/角色文档/运维/部署发布指南.md",
}

// DocsAliases 返回所有可配置的文档访问权限别名。
func DocsAliases() []Alias {
	aliases := []Alias{DocsIndex}
	seen := map[Alias]struct{}{DocsIndex: {}}
	for _, rule := range docsPermissionRules {
		if _, ok := seen[rule.Alias]; ok {
			continue
		}
		seen[rule.Alias] = struct{}{}
		aliases = append(aliases, rule.Alias)
	}
	for _, docsPath := range docsContentPaths {
		alias := docsFileAlias(docsPath)
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		aliases = append(aliases, alias)
	}
	return aliases
}

// DocsContentPaths 返回当前纳入权限表的全部可阅读文档路径。
func DocsContentPaths() []string {
	result := make([]string, len(docsContentPaths))
	copy(result, docsContentPaths)
	return result
}

// DocsAliasForPath 按文档站请求路径返回权限别名；可阅读 Markdown 优先返回单篇文档权限。
func DocsAliasForPath(requestPath string) Alias {
	docsPath := normalizedDocsPath(requestPath)
	if docsPath == "" {
		return DocsIndex
	}
	if alias, ok := DocsFileAliasForPath(docsPath); ok {
		return alias
	}
	return DocsParentAliasForPath(docsPath)
}

// DocsAliasForAssetPath 按 docsify 文档资源路径返回权限别名，basePath 用于前台 API 代理文档。
func DocsAliasForAssetPath(basePath string, assetPath string) Alias {
	parts := []string{strings.Trim(strings.TrimSpace(basePath), "/"), strings.Trim(strings.TrimSpace(assetPath), "/")}
	docsPath := strings.Trim(strings.Join(parts, "/"), "/")
	if docsPath == "" {
		return DocsIndex
	}
	return DocsAliasForPath("/api/docs/" + docsPath)
}

// DocsFileAliasForPath 返回单篇文档权限别名；非内容文档返回 false。
func DocsFileAliasForPath(assetPath string) (Alias, bool) {
	docsPath := normalizedDocsAssetPath(assetPath)
	if docsPath == "" {
		return "", false
	}
	for _, item := range docsContentPaths {
		if item == docsPath {
			return docsFileAlias(docsPath), true
		}
	}
	return "", false
}

// DocsFilePathFromAlias 从单篇文档权限别名还原文档路径。
func DocsFilePathFromAlias(alias Alias) (string, bool) {
	value := string(alias)
	if !strings.HasPrefix(value, docsFileAliasPrefix) {
		return "", false
	}
	docsPath := normalizedDocsAssetPath(strings.TrimPrefix(value, docsFileAliasPrefix))
	return docsPath, docsPath != ""
}

// DocsParentAliasForPath 返回文档所属目录权限别名；非具体目录命中时使用文档入口。
func DocsParentAliasForPath(assetPath string) Alias {
	docsPath := normalizedDocsAssetPath(assetPath)
	if docsPath == "" {
		return DocsIndex
	}
	for _, rule := range docsPermissionRules {
		if docsPathMatchesRule(docsPath, rule.Prefix) {
			return rule.Alias
		}
	}
	return DocsIndex
}

// DocsCandidateAliases 返回访问指定文档别名时允许匹配的权限别名集合。
func DocsCandidateAliases(alias Alias) []Alias {
	docsPath, ok := DocsFilePathFromAlias(alias)
	if !ok {
		return []Alias{alias}
	}
	parentAlias := DocsParentAliasForPath(docsPath)
	if parentAlias == alias || parentAlias == "" {
		return []Alias{alias}
	}
	return []Alias{alias, parentAlias}
}

// normalizedDocsPath 返回 /api/docs 下的相对路径，空值表示文档首页或站点公共资源。
func normalizedDocsPath(requestPath string) string {
	if text, err := url.PathUnescape(strings.TrimSpace(requestPath)); err == nil {
		requestPath = text
	}
	cleanPath := pathpkg.Clean("/" + strings.TrimLeft(strings.TrimSpace(requestPath), "/"))
	if cleanPath == "/api/docs" || cleanPath == "/api/docs/" {
		return ""
	}
	if !strings.HasPrefix(cleanPath, "/api/docs/") {
		return ""
	}
	return strings.TrimPrefix(cleanPath, "/api/docs/")
}

// normalizedDocsAssetPath 规整文档站内部资源路径，避免同一文档出现多种 module。
func normalizedDocsAssetPath(assetPath string) string {
	if text, err := url.PathUnescape(strings.TrimSpace(assetPath)); err == nil {
		assetPath = text
	}
	cleanPath := pathpkg.Clean("/" + strings.TrimLeft(strings.TrimSpace(assetPath), "/"))
	if cleanPath == "/" || strings.HasPrefix(cleanPath, "/../") {
		return ""
	}
	return strings.TrimPrefix(cleanPath, "/")
}

// docsFileAlias 返回单篇 Markdown 文档对应的权限 module。
func docsFileAlias(docsPath string) Alias {
	return Alias(docsFileAliasPrefix + normalizedDocsAssetPath(docsPath))
}

// docsPathMatchesRule 判断文档相对路径是否命中目录规则，目录本身和目录下文件都算命中。
func docsPathMatchesRule(docsPath string, prefix string) bool {
	docsPath = strings.Trim(strings.TrimSpace(docsPath), "/")
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	return docsPath == prefix || strings.HasPrefix(docsPath, prefix+"/")
}
