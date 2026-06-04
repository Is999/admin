package middleware

import "testing"

// TestDocsRouteAliasForPath 验证文档站按具体文档映射权限别名。
func TestDocsRouteAliasForPath(t *testing.T) {
	cases := []struct {
		name string // name 表示测试场景名称。
		path string // path 表示请求路径。
		want string // want 表示期望结果。
	}{
		{name: "首页", path: "/api/docs", want: "docs.index"},
		{name: "后端开发文档", path: "/api/docs/角色文档/后端开发/AI开发规范.md", want: "docs.file.角色文档/后端开发/AI开发规范.md"},
		{name: "前台API接口文档", path: "/api/docs/api/接口文档/前台系统/认证接口.md", want: "docs.file.api/接口文档/前台系统/认证接口.md"},
	}
	for _, tc := range cases {
		if got := string(docsRouteAliasForPath(tc.path)); got != tc.want {
			t.Fatalf("%s docsRouteAliasForPath(%q) = %q, want %q", tc.name, tc.path, got, tc.want)
		}
	}
}
