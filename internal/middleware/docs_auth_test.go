package middleware

import (
	"testing"

	"admin/internal/routealias"
)

// TestDocsRouteAliasForPath 验证文档站按目录映射到更细的权限别名。
func TestDocsRouteAliasForPath(t *testing.T) {
	cases := []struct {
		name string           // name 表示测试场景名称。
		path string           // path 表示请求路径。
		want routealias.Alias // want 表示期望结果。
	}{
		{name: "首页", path: "/api/docs", want: routealias.DocsIndex},
		{name: "站点公共资源", path: "/api/docs/_sidebar.md", want: routealias.DocsIndex},
		{name: "文档首页", path: "/api/docs/文档首页.md", want: routealias.DocsIndex},
		{name: "运维角色文档", path: "/api/docs/角色文档/运维/部署发布指南.md", want: routealias.DocsRoleOps},
		{name: "后端开发文档", path: "/api/docs/角色文档/后端开发/AI开发规范.md", want: routealias.DocsRoleBackend},
		{name: "前端测试文档", path: "/api/docs/角色文档/前端与测试/接口联调与验收说明.md", want: routealias.DocsRoleFrontend},
		{name: "任务系统功能文档", path: "/api/docs/功能模块/任务系统/任务系统首页.md", want: routealias.DocsFeatureTask},
		{name: "用户标签功能文档", path: "/api/docs/功能模块/用户标签/用户标签首页.md", want: routealias.DocsFeatureUserTag},
		{name: "接口文档首页", path: "/api/docs/接口文档/接口文档首页.md", want: routealias.DocsAPIIndex},
		{name: "后台系统接口文档", path: "/api/docs/接口文档/后台系统/权限管理接口.md", want: routealias.DocsAPIAdmin},
		{name: "任务系统接口文档", path: "/api/docs/接口文档/任务系统/任务列表接口.md", want: routealias.DocsAPITask},
		{name: "用户标签接口文档", path: "/api/docs/接口文档/用户标签/用户标签接口.md", want: routealias.DocsUserTag},
		{name: "前台API文档入口", path: "/api/docs/api", want: routealias.DocsAPIServiceIndex},
		{name: "前台API侧边栏", path: "/api/docs/api/_sidebar.md", want: routealias.DocsAPIServiceIndex},
		{name: "前台API接口规范", path: "/api/docs/api/接口文档/接口文档统一规范.md", want: routealias.DocsAPIServiceIndex},
		{name: "前台API接口文档", path: "/api/docs/api/接口文档/前台系统/认证接口.md", want: routealias.DocsAPIServiceFront},
		{name: "前台API角色文档", path: "/api/docs/api/角色文档/后端开发/AI开发规范.md", want: routealias.DocsAPIServiceIndex},
		{name: "编码路径", path: "/api/docs/%E6%8E%A5%E5%8F%A3%E6%96%87%E6%A1%A3/%E5%90%8E%E5%8F%B0%E7%B3%BB%E7%BB%9F/%E6%9D%83%E9%99%90%E7%AE%A1%E7%90%86%E6%8E%A5%E5%8F%A3.md", want: routealias.DocsAPIAdmin},
		{name: "路径穿越回退", path: "/api/docs/../secret.md", want: routealias.DocsIndex},
	}
	for _, tc := range cases {
		if got := docsRouteAliasForPath(tc.path); got != tc.want {
			t.Fatalf("%s docsRouteAliasForPath(%q) = %q, want %q", tc.name, tc.path, got, tc.want)
		}
	}
}
