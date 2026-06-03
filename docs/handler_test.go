package docs

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandlerServesChineseMarkdown 校验中文路径文档能被静态处理器正确读取。
func TestHandlerServesChineseMarkdown(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/docs/%E8%A7%92%E8%89%B2%E6%96%87%E6%A1%A3/%E5%90%8E%E7%AB%AF%E5%BC%80%E5%8F%91/AI%E5%BC%80%E5%8F%91%E8%A7%84%E8%8C%83.md", nil)
	recorder := httptest.NewRecorder()

	Handler()(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("http status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

// TestIndexKeepsFastDocsConfig 校验文档首页保留根路径、异步搜索和本地静态依赖配置。
func TestIndexKeepsFastDocsConfig(t *testing.T) {
	content, err := fs.ReadFile(FS, "site/index.html")
	if err != nil {
		t.Fatalf("读取文档首页失败: %v", err)
	}
	html := string(content)
	if !strings.Contains(html, "relativePath: false") {
		t.Fatal("docsify relativePath must be false")
	}
	if !strings.Contains(html, "startSearchIndex") || !strings.Contains(html, "window.fetch(docFetchURL") {
		t.Fatal("docs search must build index asynchronously in current page")
	}
	if !strings.Contains(html, "function isApiDocsRoute") ||
		!strings.Contains(html, "segments[0] === 'api' && segments[1] === '接口文档'") {
		t.Fatal("api docs hash must keep api namespace before loading markdown")
	}
	if !strings.Contains(html, "/api/docs/vendor/docsify/docsify.min.js") ||
		!strings.Contains(html, "/api/docs/vendor/docsify/vue.css") {
		t.Fatal("docsify assets must use local vendor paths")
	}
	if strings.Contains(html, "cdn.jsdelivr.net") ||
		strings.Contains(html, "MutationObserver") ||
		strings.Contains(html, "window.location.reload") ||
		strings.Contains(html, "search.min.js") {
		t.Fatal("docs page must not depend on blocking search plugin or CDN")
	}
}

// TestDocsifyCSSUsesLocalAssets 校验 docsify 样式不再运行时请求外部字体资源。
func TestDocsifyCSSUsesLocalAssets(t *testing.T) {
	content, err := fs.ReadFile(FS, "site/vendor/docsify/vue.css")
	if err != nil {
		t.Fatalf("读取 docsify 样式失败: %v", err)
	}
	css := string(content)
	for _, text := range []string{"fonts.googleapis.com", "fonts.gstatic.com", "@import url(\"https://"} {
		if strings.Contains(css, text) {
			t.Fatalf("docsify css must not depend on external font asset %q", text)
		}
	}
}

// TestHandlerServesRootIndex 校验文档首页能正常输出，避免文档入口漂移。
func TestHandlerServesRootIndex(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	recorder := httptest.NewRecorder()

	Handler()(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("http status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body, err := io.ReadAll(recorder.Result().Body)
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	if !strings.Contains(string(body), "项目文档中心") {
		t.Fatal("文档首页内容不符合预期")
	}
}

// TestHandlerSetsCacheHeaders 校验文档内容与框架静态资源使用不同缓存策略。
func TestHandlerSetsCacheHeaders(t *testing.T) {
	tests := []struct {
		name       string // name 表示测试场景名称。
		path       string // path 表示请求路径。
		wantStatus int    // wantStatus 表示期望 HTTP 状态码。
		wantCache  string // wantCache 表示期望缓存控制头。
	}{
		{name: "文档首页", path: "/api/docs", wantStatus: http.StatusOK, wantCache: docsPageCacheHeader},
		{name: "Markdown文档", path: "/api/docs/%E6%96%87%E6%A1%A3%E9%A6%96%E9%A1%B5.md", wantStatus: http.StatusOK, wantCache: docsPageCacheHeader},
		{name: "本地脚本", path: "/api/docs/vendor/docsify/docsify.min.js", wantStatus: http.StatusOK, wantCache: docsAssetCacheHeader},
		{name: "本地样式", path: "/api/docs/vendor/docsify/vue.css", wantStatus: http.StatusOK, wantCache: docsAssetCacheHeader},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			recorder := httptest.NewRecorder()

			Handler()(recorder, req)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("http status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if got := recorder.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Fatalf("Cache-Control = %q, want %q", got, tt.wantCache)
			}
		})
	}
}
