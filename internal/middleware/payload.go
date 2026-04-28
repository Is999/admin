package middleware

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"admin/internal/security"

	"github.com/Is999/go-utils/errors"
)

// readRequestBody 读取请求体并重新写回，避免中间件读取后影响 handler 解析参数。
func readRequestBody(r *http.Request) ([]byte, error) {
	if r == nil || r.Body == nil {
		return nil, nil
	}
	if r.ContentLength > security.MaxSecurityRequestBodyBytes {
		return nil, errors.Wrapf(security.ErrSecurityPayloadTooLarge, "安全请求体长度超过上限: %d", security.MaxSecurityRequestBodyBytes)
	}
	limited := io.LimitReader(r.Body, security.MaxSecurityRequestBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if len(body) > security.MaxSecurityRequestBodyBytes {
		return nil, errors.Wrapf(security.ErrSecurityPayloadTooLarge, "安全请求体长度超过上限: %d", security.MaxSecurityRequestBodyBytes)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// replaceJSONBody 用新的 JSON 对象覆盖请求体。
func replaceJSONBody(r *http.Request, data map[string]any) error {
	body, err := json.Marshal(data)
	if err != nil {
		return errors.Tag(err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Type", "application/json")
	return nil
}

// requestParams 读取 query、form 和 JSON body 中的首层参数。
func requestParams(r *http.Request) (map[string]any, error) {
	params := make(map[string]any)
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			params[key] = values[0]
		}
	}

	body, err := readRequestBody(r)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return params, nil
	}

	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
			return nil, errors.Tag(err)
		}
		for key, values := range r.PostForm {
			if len(values) > 0 {
				params[key] = values[0]
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		return params, nil
	}

	var bodyMap map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&bodyMap); err == nil {
		for key, value := range bodyMap {
			params[key] = value
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return params, nil
}

// requestJSONMap 读取 JSON 请求体；空 body 返回空 map。
func requestJSONMap(r *http.Request) (map[string]any, error) {
	body, err := readRequestBody(r)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return map[string]any{}, nil
	}
	var bodyMap map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&bodyMap); err != nil {
		return nil, errors.Tag(err)
	}
	return bodyMap, nil
}

// decodeBase64Header 解码 laravel-admin 风格的 base64 请求头。
func decodeBase64Header(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.Errorf("请求头不能为空")
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil {
		return string(decoded), nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(raw); err == nil {
		return string(decoded), nil
	}
	return "", errors.Errorf("请求头base64格式不合法")
}

// decodeCipherParams 解码 X-Cipher 中的字段加密配置。
func decodeCipherParams(raw string) ([]string, error) {
	text, err := decodeBase64Header(raw)
	if err != nil {
		return nil, errors.Tag(err)
	}
	var params []string
	if err := json.Unmarshal([]byte(text), &params); err != nil {
		return nil, errors.Tag(err)
	}
	return params, nil
}
