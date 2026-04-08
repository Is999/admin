package docs

import (
	"io/fs"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"strings"
)

const (
	docsPathPrefix       = "/api/docs"              // 文档站路由前缀
	docsAssetCacheHeader = "public, max-age=604800" // 静态依赖缓存 7 天
	docsPageCacheHeader  = "no-cache"               // 文档内容保持可及时更新
)

// Handler 返回文档站点的 HTTP 处理器。
func Handler() http.HandlerFunc {
	var fileServer http.Handler
	var initErr error
	// 开发环境优先直接读取本地 docs/site，便于中文文件名和文档热更新。
	if _, err := os.Stat("docs/site"); err == nil {
		fileServer = http.FileServer(http.Dir("docs/site"))
	} else {
		// 生产环境没有本地文档目录时，回退到内嵌文档。
		sub, err := fs.Sub(FS, "site")
		if err != nil {
			initErr = err
		} else {
			fileServer = http.FileServer(http.FS(sub))
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if initErr != nil {
			http.Error(w, "文档资源初始化失败", http.StatusInternalServerError)
			return
		}

		// Docsify 会把中文文件名编码到 URL 中，这里先解码再交给文件服务器。
		path := strings.TrimPrefix(r.URL.Path, docsPathPrefix)
		if path == "" {
			path = "/"
		}
		if decodedPath, err := url.PathUnescape(path); err == nil {
			path = decodedPath
		}

		setDocsCacheHeader(w, path)
		req := r.Clone(r.Context())
		req.URL.Path = path
		req.URL.RawPath = ""
		fileServer.ServeHTTP(w, req)
	}
}

// setDocsCacheHeader 按资源类型设置缓存策略，避免每次打开文档重复拉取框架资源。
func setDocsCacheHeader(w http.ResponseWriter, filePath string) {
	if w == nil {
		return
	}
	w.Header().Set("Cache-Control", docsCacheControl(filePath))
}

// docsCacheControl 返回文档站资源缓存头，Markdown 与首页不做强缓存。
func docsCacheControl(filePath string) string {
	switch strings.ToLower(pathpkg.Ext(filePath)) {
	case ".css", ".gif", ".ico", ".jpeg", ".jpg", ".js", ".png", ".svg", ".woff", ".woff2":
		return docsAssetCacheHeader
	default:
		return docsPageCacheHeader
	}
}
