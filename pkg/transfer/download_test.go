package transfer

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBuildContentDispositionSanitizesFileName 确保下载文件名不会污染响应头。
func TestBuildContentDispositionSanitizesFileName(t *testing.T) {
	header := buildContentDisposition("bad\r\nvalue", "../报表\";x=.xlsx")
	if strings.ContainsAny(header, "\r\n") {
		t.Fatalf("Content-Disposition 不应包含换行: %q", header)
	}
	if !strings.HasPrefix(header, "attachment;") {
		t.Fatalf("非法 disposition 应回退为 attachment: %q", header)
	}
	if !strings.Contains(header, "filename*=") {
		t.Fatalf("期望包含 RFC5987 文件名: %q", header)
	}
}

// TestServeStreamSanitizesHeaders 确保流式下载响应头使用清洗后的文件名。
func TestServeStreamSanitizesHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/download", nil)
	if err := ServeStream(recorder, request, strings.NewReader("hello"), "bad\r\nname.txt", "text/plain\r\nx: y", 5, "inline", false, "bytes 0-4/5\r\nx"); err != nil {
		t.Fatalf("输出流失败: %v", err)
	}
	disposition := recorder.Header().Get("Content-Disposition")
	if strings.ContainsAny(disposition, "\r\n") {
		t.Fatalf("响应头不应包含换行: %q", disposition)
	}
	if !strings.HasPrefix(disposition, "inline;") {
		t.Fatalf("期望 inline 响应头: %q", disposition)
	}
	if recorder.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("非法 Content-Type 应回退到扩展名推导，实际为 %q", recorder.Header().Get("Content-Type"))
	}
	if recorder.Header().Get("Content-Range") != "" {
		t.Fatalf("非法 Content-Range 应被丢弃，实际为 %q", recorder.Header().Get("Content-Range"))
	}
}
