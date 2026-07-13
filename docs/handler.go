package docs

import (
	"admin/internal/routealias"
	"io/fs"
	"net/http"
	"os"
	pathpkg "path"
	"strings"
)

const (
	docsAssetCacheHeader = "public, max-age=604800" // 静态依赖缓存 7 天
	docsPageCacheHeader  = "no-cache"               // 文档内容保持可及时更新
)

// docsFileSystem 只允许文档根目录用于首页解析，拒绝子目录列表。
type docsFileSystem struct {
	base http.FileSystem // base 是本地或内嵌文档文件系统。
}

// Open 打开文档文件，并把子目录统一视为不存在。
func (d docsFileSystem) Open(name string) (http.File, error) {
	file, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.IsDir() && pathpkg.Clean("/"+strings.TrimPrefix(name, "/")) != "/" {
		_ = file.Close()
		return nil, fs.ErrNotExist
	}
	return file, nil
}

// Handler 返回文档站点的 HTTP 处理器。
func Handler() http.HandlerFunc {
	var fileServer http.Handler
	var initErr error
	// 开发环境优先直接读取本地 docs/site，便于中文文件名和文档热更新。
	if _, err := os.Stat("docs/site"); err == nil {
		fileServer = http.FileServer(docsFileSystem{base: http.Dir("docs/site")})
	} else {
		// 生产环境没有本地文档目录时，回退到内嵌文档。
		sub, err := fs.Sub(FS, "site")
		if err != nil {
			initErr = err
		} else {
			fileServer = http.FileServer(docsFileSystem{base: http.FS(sub)})
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if initErr != nil {
			http.Error(w, "文档资源初始化失败", http.StatusInternalServerError)
			return
		}

		docsPath, ok := routealias.NormalizeDocsRequestPath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		filePath := "/"
		if docsPath != "" {
			filePath += docsPath
		}

		setDocsCacheHeader(w, filePath)
		req := r.Clone(r.Context())
		req.URL.Path = filePath
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
