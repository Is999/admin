package transfer

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"
)

// ServeDownload 使用标准 Range/If-Modified-Since 语义输出附件，支持浏览器断点续传下载。
func ServeDownload(w http.ResponseWriter, r *http.Request, filePath string, fileName string, contentType string) error {
	return serveFile(w, r, filePath, fileName, contentType, "attachment")
}

// ServeInline 使用标准 Range/If-Modified-Since 语义输出可直接访问的文件。
func ServeInline(w http.ResponseWriter, r *http.Request, filePath string, fileName string, contentType string) error {
	return serveFile(w, r, filePath, fileName, contentType, "inline")
}

// ServeStream 输出对象流，适用于 S3 等非本地文件系统场景。
func ServeStream(w http.ResponseWriter, r *http.Request, reader io.Reader, fileName string, contentType string, contentLength int64, disposition string, acceptRanges bool, contentRange string) error {
	if reader == nil {
		return os.ErrNotExist
	}
	if strings.TrimSpace(fileName) == "" {
		fileName = "file"
	}
	contentType = safeContentType(contentType, fileName)
	disposition = strings.TrimSpace(disposition)
	disposition = safeDisposition(disposition)
	fileName = safeDownloadFileName(fileName)

	// 对于本地文件等可 seek 的读取器，优先复用标准 ServeContent 语义，保留 Range/缓存协商能力。
	if readSeeker, ok := reader.(io.ReadSeeker); ok {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", buildContentDisposition(disposition, fileName))
		w.Header().Set("Accept-Ranges", "bytes")
		http.ServeContent(w, r, fileName, time.Time{}, readSeeker)
		return nil
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", buildContentDisposition(disposition, fileName))
	if acceptRanges {
		w.Header().Set("Accept-Ranges", "bytes")
	}
	if contentLength > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	}
	if contentRange = safeHeaderValue(contentRange); contentRange != "" {
		w.Header().Set("Content-Range", contentRange)
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	_, err := io.Copy(w, reader)
	return errors.Tag(err)
}

// serveFile 以受控响应头输出本地文件，支持浏览器 Range 下载。
func serveFile(w http.ResponseWriter, r *http.Request, filePath string, fileName string, contentType string, disposition string) error {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return os.ErrNotExist
	}
	file, err := os.Open(filePath)
	if err != nil {
		return errors.Tag(err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return errors.Tag(err)
	}
	if strings.TrimSpace(fileName) == "" {
		fileName = filepath.Base(filePath)
	}
	contentType = safeContentType(contentType, fileName)
	disposition = strings.TrimSpace(disposition)
	disposition = safeDisposition(disposition)
	fileName = safeDownloadFileName(fileName)

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", buildContentDisposition(disposition, fileName))
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, fileName, fileInfo.ModTime(), file)
	return nil
}

// safeDisposition 只允许标准 inline/attachment，避免响应头注入。
func safeDisposition(disposition string) string {
	switch strings.ToLower(strings.TrimSpace(disposition)) {
	case "inline":
		return "inline"
	default:
		return "attachment"
	}
}

// safeDownloadFileName 清洗下载文件名，避免 CRLF、引号和路径片段污染响应头。
func safeDownloadFileName(fileName string) string {
	fileName = strings.ReplaceAll(strings.TrimSpace(fileName), `\`, "/")
	fileName = filepath.Base(fileName)
	if fileName == "" || fileName == "." || fileName == "/" {
		return "file"
	}
	replacer := strings.NewReplacer("\r", "", "\n", "", "\x00", "", `"`, "_", ";", "_")
	fileName = strings.TrimSpace(replacer.Replace(fileName))
	if fileName == "" {
		return "file"
	}
	return fileName
}

// buildContentDisposition 构造兼容中文文件名的 Content-Disposition 响应头。
func buildContentDisposition(disposition string, fileName string) string {
	fileName = safeDownloadFileName(fileName)
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, safeDisposition(disposition), asciiDownloadFileName(fileName), url.PathEscape(fileName))
}

// safeContentType 校验 Content-Type 响应头，非法或空值回退到文件扩展名推导结果。
func safeContentType(contentType string, fileName string) string {
	contentType = safeHeaderValue(contentType)
	if contentType != "" {
		if mediaType, _, err := mime.ParseMediaType(contentType); err == nil && strings.TrimSpace(mediaType) != "" {
			return contentType
		}
	}
	contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(fileName)))
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}
	return contentType
}

// safeHeaderValue 清洗响应头值中的控制字符，避免 CRLF 注入。
func safeHeaderValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return ""
	}
	return value
}

// asciiDownloadFileName 生成 ASCII fallback 文件名；真实中文名通过 filename* 输出。
func asciiDownloadFileName(fileName string) string {
	fileName = safeDownloadFileName(fileName)
	ext := filepath.Ext(fileName)
	base := strings.TrimSuffix(fileName, ext)
	builder := strings.Builder{}
	for _, ch := range base {
		if ch >= 33 && ch <= 126 && ch != '\\' && ch != '"' && ch != ';' {
			builder.WriteRune(ch)
		}
	}
	if builder.Len() == 0 {
		builder.WriteString("file")
	}
	if ext != "" && isASCIISafeExt(ext) {
		builder.WriteString(ext)
	}
	return builder.String()
}

// isASCIISafeExt 判断扩展名是否适合放入 ASCII filename 参数。
func isASCIISafeExt(ext string) bool {
	if ext == "" || strings.ContainsAny(ext, "\r\n\x00/\\\";") {
		return false
	}
	for _, ch := range ext {
		if ch < 33 || ch > 126 {
			return false
		}
	}
	return true
}
