package routealias

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// TestDocsAliasForPath 验证文档站优先映射到单篇文档权限。
func TestDocsAliasForPath(t *testing.T) {
	cases := []struct {
		name string // name 表示测试场景名称。
		path string // path 表示请求路径。
		want Alias  // want 表示期望结果。
	}{
		{name: "首页", path: "/api/docs", want: DocsIndex},
		{name: "站点公共资源", path: "/api/docs/_sidebar.md", want: DocsIndex},
		{name: "文档首页", path: "/api/docs/文档首页.md", want: docsFileAlias("文档首页.md")},
		{name: "运维角色文档", path: "/api/docs/角色文档/运维/部署发布指南.md", want: docsFileAlias("角色文档/运维/部署发布指南.md")},
		{name: "后端开发文档", path: "/api/docs/角色文档/后端开发/AI开发规范.md", want: docsFileAlias("角色文档/后端开发/AI开发规范.md")},
		{name: "前端测试文档", path: "/api/docs/角色文档/前端与测试/接口联调与验收说明.md", want: docsFileAlias("角色文档/前端与测试/接口联调与验收说明.md")},
		{name: "任务系统功能文档", path: "/api/docs/功能模块/任务系统/任务系统首页.md", want: docsFileAlias("功能模块/任务系统/任务系统首页.md")},
		{name: "用户标签功能文档", path: "/api/docs/功能模块/用户标签/用户标签首页.md", want: docsFileAlias("功能模块/用户标签/用户标签首页.md")},
		{name: "接口文档首页", path: "/api/docs/接口文档/接口文档首页.md", want: docsFileAlias("接口文档/接口文档首页.md")},
		{name: "后台系统接口文档", path: "/api/docs/接口文档/后台系统/权限管理接口.md", want: docsFileAlias("接口文档/后台系统/权限管理接口.md")},
		{name: "任务系统接口文档", path: "/api/docs/接口文档/任务系统/任务列表接口.md", want: docsFileAlias("接口文档/任务系统/任务列表接口.md")},
		{name: "用户标签接口文档", path: "/api/docs/接口文档/用户标签/用户标签接口.md", want: docsFileAlias("接口文档/用户标签/用户标签接口.md")},
		{name: "前台 API 文档入口", path: "/api/docs/api", want: DocsAPIServiceIndex},
		{name: "前台 API 侧边栏", path: "/api/docs/api/_sidebar.md", want: DocsAPIServiceIndex},
		{name: "前台API接口规范", path: "/api/docs/api/接口文档/接口文档统一规范.md", want: docsFileAlias("api/接口文档/接口文档统一规范.md")},
		{name: "前台API接口文档", path: "/api/docs/api/接口文档/前台系统/认证接口.md", want: docsFileAlias("api/接口文档/前台系统/认证接口.md")},
		{name: "前台API角色文档", path: "/api/docs/api/角色文档/后端开发/AI开发规范.md", want: docsFileAlias("api/角色文档/后端开发/AI开发规范.md")},
		{name: "编码路径", path: "/api/docs/%E6%8E%A5%E5%8F%A3%E6%96%87%E6%A1%A3/%E5%90%8E%E5%8F%B0%E7%B3%BB%E7%BB%9F/%E6%9D%83%E9%99%90%E7%AE%A1%E7%90%86%E6%8E%A5%E5%8F%A3.md", want: docsFileAlias("接口文档/后台系统/权限管理接口.md")},
		{name: "路径穿越回退", path: "/api/docs/../secret.md", want: DocsIndex},
	}
	for _, tc := range cases {
		if got := DocsAliasForPath(tc.path); got != tc.want {
			t.Fatalf("%s DocsAliasForPath(%q) = %q, want %q", tc.name, tc.path, got, tc.want)
		}
	}
}

// TestDocsCandidateAliases 验证单篇文档只匹配自身文件权限。
func TestDocsCandidateAliases(t *testing.T) {
	cases := []struct {
		name  string  // name 表示测试场景名称。
		alias Alias   // alias 表示文档权限别名。
		want  []Alias // want 表示候选权限别名。
	}{
		{name: "后台具体接口文档", alias: docsFileAlias("接口文档/后台系统/权限管理接口.md"), want: []Alias{docsFileAlias("接口文档/后台系统/权限管理接口.md")}},
		{name: "前台 API 具体接口文档", alias: docsFileAlias("api/接口文档/前台系统/认证接口.md"), want: []Alias{docsFileAlias("api/接口文档/前台系统/认证接口.md")}},
		{name: "目录权限", alias: DocsRoleBackend, want: []Alias{DocsRoleBackend}},
	}
	for _, tc := range cases {
		got := DocsCandidateAliases(tc.alias)
		if len(got) != len(tc.want) {
			t.Fatalf("%s DocsCandidateAliases() len = %d, want %d, got=%v", tc.name, len(got), len(tc.want), got)
		}
		for index := range tc.want {
			if got[index] != tc.want[index] {
				t.Fatalf("%s DocsCandidateAliases()[%d] = %q, want %q", tc.name, index, got[index], tc.want[index])
			}
		}
	}
}

// TestDocsContentPathsMatchWorkspace 确保当前 Markdown 文档都纳入单篇文档权限清单。
func TestDocsContentPathsMatchWorkspace(t *testing.T) {
	adminRoot := routealiasTestAdminRoot(t)
	got := DocsContentPaths()
	sort.Strings(got)
	want := append(
		workspaceDocsContentPaths(t, filepath.Join(adminRoot, "docs/site"), ""),
		workspaceDocsContentPaths(t, filepath.Join(adminRoot, "../api/docs/site"), "api/")...,
	)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("DocsContentPaths() mismatch\n got=%v\nwant=%v", got, want)
	}
}

// routealiasTestAdminRoot 返回 admin 仓库根目录，避免测试依赖执行目录。
func routealiasTestAdminRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../.."))
}

// workspaceDocsContentPaths 返回指定文档站目录下需要授权的 Markdown 文档。
func workspaceDocsContentPaths(t *testing.T, root string, prefix string) []string {
	t.Helper()
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("docs root %s stat error: %v", root, err)
	}
	paths := make([]string, 0)
	err := filepath.WalkDir(root, func(itemPath string, item os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if item.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(item.Name()), ".md") {
			return nil
		}
		switch item.Name() {
		case "_sidebar.md", "_navbar.md", "404.md":
			return nil
		}
		relativePath, err := filepath.Rel(root, itemPath)
		if err != nil {
			return err
		}
		paths = append(paths, prefix+filepath.ToSlash(relativePath))
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(%s) error = %v", root, err)
	}
	return paths
}
