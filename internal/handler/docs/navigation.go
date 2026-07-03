package docs

import (
	"io/fs"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"

	sitedocs "admin/docs"
	securitylogic "admin/internal/logic/security"
	"admin/internal/requestctx"
	"admin/internal/routealias"
	"admin/internal/svc"

	"github.com/Is999/go-utils/errors"
)

// docsAccessSet 保存当前账号可访问的文档权限别名集合。
type docsAccessSet struct {
	all     bool                          // 是否允许查看全部文档
	aliases map[routealias.Alias]struct{} // 可查看的文档权限别名集合
}

// docsNavItem 表示 docsify Markdown 导航中的一行及其子级。
type docsNavItem struct {
	line     string         // 原始导航行内容
	indent   int            // 缩进层级，用于恢复父子关系
	allowed  bool           // 当前行是否允许输出
	children []*docsNavItem // 子导航项，按源文件顺序保留
}

// serveDocsNavigation 按当前账号文档权限输出 docsify 导航资源。
func serveDocsNavigation(w http.ResponseWriter, r *http.Request, svcCtx *svc.ServiceContext, assetPath string, docsBase string) {
	content, err := readDocsSiteAsset(assetPath)
	if err != nil {
		http.Error(w, "文档导航资源不可用", http.StatusInternalServerError)
		return
	}
	access, err := docsAccessForRequest(r, svcCtx)
	if err != nil {
		http.Error(w, "文档权限不可用", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(filterDocsNavigation(content, docsBase, access))
}

// readDocsSiteAsset 读取文档站资源；开发环境优先本地文件，生产环境回退内嵌资源。
func readDocsSiteAsset(assetPath string) ([]byte, error) {
	assetPath = strings.Trim(strings.TrimSpace(assetPath), "/")
	localPath := filepath.Join("docs", "site", filepath.FromSlash(assetPath))
	if content, err := os.ReadFile(localPath); err == nil {
		return content, nil
	}
	return fs.ReadFile(sitedocs.FS, pathpkg.Join("site", assetPath))
}

// docsAccessForRequest 根据当前请求账号计算可见文档权限；无登录身份时返回空权限集合。
func docsAccessForRequest(r *http.Request, svcCtx *svc.ServiceContext) (docsAccessSet, error) {
	if svcCtx == nil {
		return docsAccessSet{all: true}, nil
	}
	meta := requestctx.FromContext(r.Context())
	if meta == nil || meta.UserID <= 0 {
		return docsAccessSet{aliases: map[routealias.Alias]struct{}{}}, nil
	}

	access := docsAccessSet{aliases: map[routealias.Alias]struct{}{}}
	security := securitylogic.NewSecurityLogic(r.Context(), svcCtx)
	for _, alias := range routealias.DocsAliases() {
		allowed, err := security.CheckRoutePermission(meta.UserID, string(alias))
		if err != nil {
			return docsAccessSet{}, errors.Wrapf(err, "查询文档权限失败 alias=%s", alias)
		}
		if !allowed {
			continue
		}
		access.aliases[alias] = struct{}{}
	}
	return access, nil
}

// allows 判断当前账号是否允许查看指定文档别名。
func (s docsAccessSet) allows(alias routealias.Alias) bool {
	if s.all {
		return true
	}
	for _, candidate := range routealias.DocsCandidateAliases(alias) {
		if _, ok := s.aliases[candidate]; ok {
			return true
		}
	}
	return false
}

// filterDocsNavigation 按权限过滤 Markdown 导航树，避免侧边栏和搜索暴露未授权文档。
func filterDocsNavigation(content []byte, docsBase string, access docsAccessSet) []byte {
	if access.all {
		return content
	}
	nodes := parseDocsNavigation(content)
	var builder strings.Builder
	for _, node := range nodes {
		if markDocsNavAllowed(node, docsBase, access) {
			writeDocsNavItem(&builder, node)
		}
	}
	return []byte(builder.String())
}

// parseDocsNavigation 把 docsify Markdown 列表解析为层级结构。
func parseDocsNavigation(content []byte) []*docsNavItem {
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	var roots []*docsNavItem
	var stack []*docsNavItem
	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		item := &docsNavItem{line: line, indent: docsNavIndent(line)}
		for len(stack) > 0 && stack[len(stack)-1].indent >= item.indent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			roots = append(roots, item)
		} else {
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, item)
		}
		stack = append(stack, item)
	}
	return roots
}

// docsNavIndent 返回导航行前导空格数量。
func docsNavIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// markDocsNavAllowed 递归标记导航节点是否应该展示。
func markDocsNavAllowed(item *docsNavItem, docsBase string, access docsAccessSet) bool {
	childAllowed := false
	for _, child := range item.children {
		if markDocsNavAllowed(child, docsBase, access) {
			childAllowed = true
		}
	}

	ownAllowed := false
	if href, ok := docsNavHref(item.line); ok {
		if assetPath, isDoc := docsAssetPathFromHref(href); isDoc {
			ownAllowed = access.allows(routealias.DocsAliasForAssetPath(docsBase, assetPath))
		} else {
			ownAllowed = true
		}
	}
	item.allowed = ownAllowed || childAllowed
	return item.allowed
}

// writeDocsNavItem 输出已授权的导航节点。
func writeDocsNavItem(builder *strings.Builder, item *docsNavItem) {
	if !item.allowed {
		return
	}
	builder.WriteString(item.line)
	builder.WriteByte('\n')
	for _, child := range item.children {
		writeDocsNavItem(builder, child)
	}
}

// docsNavHref 解析 Markdown 导航链接地址。
func docsNavHref(line string) (string, bool) {
	closeTitle := strings.Index(line, "](")
	if closeTitle < 0 {
		return "", false
	}
	openTitle := strings.LastIndex(line[:closeTitle], "[")
	if openTitle < 0 {
		return "", false
	}
	start := closeTitle + len("](")
	end := strings.Index(line[start:], ")")
	if end < 0 {
		return "", false
	}
	href := strings.TrimSpace(line[start : start+end])
	return href, href != ""
}

// docsAssetPathFromHref 把 docsify 链接转换成 Markdown 资源路径。
func docsAssetPathFromHref(href string) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") || isExternalDocsHref(href) {
		return "", false
	}
	href = strings.TrimPrefix(href, "/api/docs#/")
	if anchorIndex := strings.Index(href, "#"); anchorIndex >= 0 {
		href = href[:anchorIndex]
	}
	if queryIndex := strings.Index(href, "?"); queryIndex >= 0 {
		href = href[:queryIndex]
	}
	href = strings.TrimLeft(strings.TrimSpace(href), "/")
	if !strings.HasSuffix(strings.ToLower(href), ".md") {
		return "", false
	}
	return href, true
}

// isExternalDocsHref 判断链接是否是外部协议地址。
func isExternalDocsHref(href string) bool {
	href = strings.ToLower(strings.TrimSpace(href))
	return strings.HasPrefix(href, "http://") ||
		strings.HasPrefix(href, "https://") ||
		strings.HasPrefix(href, "mailto:") ||
		strings.HasPrefix(href, "tel:") ||
		strings.HasPrefix(href, "file:")
}
