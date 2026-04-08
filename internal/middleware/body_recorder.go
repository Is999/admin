package middleware

import (
	"bytes"
	"net/http"
)

// bodyRecorder 捕获下游响应头、状态码和响应体，供签名/加密中间件二次处理。
type bodyRecorder struct {
	header http.Header  // header 保存下游写入的响应头
	body   bytes.Buffer // body 保存下游写入的响应体
	status int          // status 保存下游写入的 HTTP 状态码
}

// newBodyRecorder 创建响应捕获器。
func newBodyRecorder() *bodyRecorder {
	return &bodyRecorder{
		header: make(http.Header),
		status: http.StatusOK,
	}
}

// Header 返回可写响应头集合。
func (r *bodyRecorder) Header() http.Header {
	return r.header
}

// WriteHeader 记录 HTTP 状态码。
func (r *bodyRecorder) WriteHeader(status int) {
	r.status = status
}

// Write 记录响应体内容。
func (r *bodyRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(data)
}

// flushRecordedResponse 把捕获到的响应写回真实 ResponseWriter。
func flushRecordedResponse(w http.ResponseWriter, recorder *bodyRecorder) {
	copyHeader(w.Header(), recorder.Header())
	status := recorder.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(recorder.body.Bytes())
}

// copyHeader 复制响应头，避免签名/加密中间件丢失下游设置的 Header。
func copyHeader(dst http.Header, src http.Header) {
	for key, values := range src {
		dst.Del(key)
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
