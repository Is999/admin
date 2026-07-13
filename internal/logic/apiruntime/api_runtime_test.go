package apiruntime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	i18n "admin/common/i18n"
	"admin/internal/config"
	"admin/internal/types"
)

const (
	// testRuntimeSyncUserID 表示 API 内网同步测试使用的前台用户雪花 ID。
	testRuntimeSyncUserID int64 = 778919762005200896
	// testRuntimeSyncUserIDString 表示 API 内网同步响应中的用户 ID 字符串。
	testRuntimeSyncUserIDString = "778919762005200896"
)

// TestSyncUserRuntimeUsesInternalOpsRoute 验证 admin 只调用 API 内网运行态同步接口。
func TestSyncUserRuntimeUsesInternalOpsRoute(t *testing.T) {
	var gotPayload userRuntimeSyncPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/internal/users/"+testRuntimeSyncUserIDString+"/runtime-sync" {
			t.Fatalf("path = %s, want /internal/users/%s/runtime-sync", r.URL.Path, testRuntimeSyncUserIDString)
		}
		if got := r.Header.Get(apiRuntimeOpsTokenHeader); got != "ops-token" {
			t.Fatalf("%s = %q, want ops-token", apiRuntimeOpsTokenHeader, got)
		}
		if got := r.Header.Get("Accept-Language"); got != i18n.LocaleZHCN {
			t.Fatalf("Accept-Language = %q, want %q", got, i18n.LocaleZHCN)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll payload failed: %v", err)
		}
		assertAPIRequestSigned(t, r, "ops-token", body)
		if err := json.Unmarshal(body, &gotPayload); err != nil {
			t.Fatalf("Unmarshal payload failed: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  true,
			"code":    200,
			"message": "ok",
			"data": map[string]any{
				"userId":                  testRuntimeSyncUserIDString,
				"profileCacheInvalidated": true,
				"sessionsInvalidated":     false,
				"message":                 "",
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(config.APIServiceConfig{
		InternalBaseURL: server.URL + "/",
		OpsToken:        "ops-token",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	resp, err := client.SyncUserRuntime(context.Background(), testRuntimeSyncUserID, false, false, 0, "  manual sync  ")
	if err != nil {
		t.Fatalf("SyncUserRuntime() error = %v", err)
	}
	if !gotPayload.Profile || gotPayload.Sessions || gotPayload.Reason != "manual sync" {
		t.Fatalf("payload = %+v, want profile default true and trimmed reason", gotPayload)
	}
	if !resp.Enabled || !resp.Success || resp.UserID != testRuntimeSyncUserID || !resp.ProfileCacheInvalidated || resp.SessionsInvalidated {
		t.Fatalf("response = %+v, want successful profile-only sync", resp)
	}
	if resp.Message != "API 运行态已同步" {
		t.Fatalf("message = %q, want default sync message", resp.Message)
	}
}

// TestSyncUserRuntimeRejectsMismatchedUserID 验证 API 返回用户 ID 不一致时拒绝成功回执。
func TestSyncUserRuntimeRejectsMismatchedUserID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  true,
			"code":    200,
			"message": "ok",
			"data": map[string]any{
				"userId": strconv.FormatInt(testRuntimeSyncUserID+1, 10),
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(config.APIServiceConfig{
		InternalBaseURL: server.URL,
		OpsToken:        "ops-token",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err = client.SyncUserRuntime(context.Background(), testRuntimeSyncUserID, true, false, 0, ""); err == nil {
		t.Fatalf("SyncUserRuntime() error = nil, want mismatched user ID error")
	}
}

// TestSyncUserRuntimeRequiresCommittedAuthVersion 校验会话失效请求不能绕过数据库认证版本栅栏。
func TestSyncUserRuntimeRequiresCommittedAuthVersion(t *testing.T) {
	client, err := NewClient(config.APIServiceConfig{
		InternalBaseURL: "http://127.0.0.1:1",
		OpsToken:        "ops-token",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err = client.SyncUserRuntime(context.Background(), testRuntimeSyncUserID, false, true, 0, "manual"); err == nil {
		t.Fatal("失效会话未提供认证版本时应在发起 HTTP 请求前失败")
	}
}

// TestSyncUserRuntimeCarriesCommittedAuthVersion 校验会话清理请求和回执都绑定 admin 已提交的认证版本。
func TestSyncUserRuntimeCarriesCommittedAuthVersion(t *testing.T) {
	const authVersion = uint64(7)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload userRuntimeSyncPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode payload error = %v", err)
		}
		if !payload.Sessions || payload.AuthVersion != authVersion {
			t.Fatalf("payload = %+v, want sessions with authVersion=%d", payload, authVersion)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": true,
			"code":   200,
			"data": map[string]any{
				"userId":              testRuntimeSyncUserIDString,
				"sessionsInvalidated": true,
				"authVersion":         authVersion,
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(config.APIServiceConfig{InternalBaseURL: server.URL, OpsToken: "ops-token"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	resp, err := client.SyncUserRuntime(context.Background(), testRuntimeSyncUserID, false, true, authVersion, "manual")
	if err != nil {
		t.Fatalf("SyncUserRuntime() error = %v", err)
	}
	if !resp.SessionsInvalidated || resp.AuthVersion != authVersion {
		t.Fatalf("response = %+v, want invalidated authVersion=%d", resp, authVersion)
	}
}

// TestSyncUserRuntimeRejectsIncompleteResult 验证 API 未确认请求动作时不能返回伪成功回执。
func TestSyncUserRuntimeRejectsIncompleteResult(t *testing.T) {
	tests := []struct {
		name        string // name 表示测试场景。
		profile     bool   // profile 表示是否请求清理资料缓存。
		sessions    bool   // sessions 表示是否请求清理登录态。
		authVersion uint64 // authVersion 表示请求使用的认证版本。
	}{
		{name: "profile", profile: true},
		{name: "sessions", sessions: true, authVersion: 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status": true,
					"code":   200,
					"data": map[string]any{
						"userId":      testRuntimeSyncUserIDString,
						"authVersion": tt.authVersion,
					},
				})
			}))
			defer server.Close()

			client, err := NewClient(config.APIServiceConfig{InternalBaseURL: server.URL, OpsToken: "ops-token"})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			if _, err = client.SyncUserRuntime(context.Background(), testRuntimeSyncUserID, tt.profile, tt.sessions, tt.authVersion, "manual"); err == nil {
				t.Fatal("SyncUserRuntime() error = nil, want incomplete result rejection")
			}
		})
	}
}

// TestRequestAPIRejectsRawErrorBodyLeak 验证下游错误正文不会进入可向上返回的错误链。
func TestRequestAPIRejectsRawErrorBodyLeak(t *testing.T) {
	const secretText = "internal-db-password=do-not-leak"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(secretText))
	}))
	defer server.Close()

	client, err := NewClient(config.APIServiceConfig{InternalBaseURL: server.URL, OpsToken: "ops-token"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.ConfigReloadStatus(context.Background())
	if err == nil {
		t.Fatal("ConfigReloadStatus() error = nil, want HTTP failure")
	}
	if strings.Contains(err.Error(), secretText) {
		t.Fatalf("error leaked downstream response body: %v", err)
	}
}

// TestConfigReloadItemsUsesInternalOpsRoute 验证 API 运行态配置项查询会透传筛选 query。
func TestConfigReloadItemsUsesInternalOpsRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/internal/system/config-reload/items" {
			t.Fatalf("path = %s, want /internal/system/config-reload/items", r.URL.Path)
		}
		if got := r.Header.Get(apiRuntimeOpsTokenHeader); got != "ops-token" {
			t.Fatalf("%s = %q, want ops-token", apiRuntimeOpsTokenHeader, got)
		}
		assertAPIRequestSigned(t, r, "ops-token", nil)
		query := r.URL.Query()
		if query.Get("keyword") != "security" || query.Get("page") != "2" || query.Get("pageSize") != "50" || query.Get("sensitiveOnly") != "true" {
			t.Fatalf("query = %s, want keyword/security page/2 pageSize/50 sensitiveOnly/true", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  true,
			"code":    1000,
			"message": "ok",
			"data": map[string]any{
				"keyword":       "security",
				"sensitiveOnly": true,
				"page":          2,
				"pageSize":      50,
				"total":         1,
				"items": []map[string]any{
					{
						"path":      "security.app_key",
						"value":     "app_****_key",
						"valueType": "string",
						"sensitive": true,
					},
				},
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(config.APIServiceConfig{
		InternalBaseURL: server.URL,
		OpsToken:        "ops-token",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	resp, err := client.ConfigReloadItems(context.Background(), &types.TaskConfigItemQueryReq{
		Keyword:       " security ",
		Page:          2,
		PageSize:      50,
		SensitiveOnly: true,
	})
	if err != nil {
		t.Fatalf("ConfigReloadItems() error = %v", err)
	}
	if resp == nil || resp.Keyword != "security" || resp.Total != 1 || len(resp.Items) != 1 || resp.Items[0].Path != "security.app_key" {
		t.Fatalf("response = %+v, want one security config item", resp)
	}
}

// TestRunConfigReloadUsesSignedInternalOpsRoute 验证手动热加载会使用 POST 空 body 签名访问 API 内网接口。
func TestRunConfigReloadUsesSignedInternalOpsRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/internal/system/config-reload/run" {
			t.Fatalf("path = %s, want /internal/system/config-reload/run", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll payload failed: %v", err)
		}
		if len(body) != 0 {
			t.Fatalf("body = %q, want empty", string(body))
		}
		assertAPIRequestSigned(t, r, "ops-token", body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  true,
			"code":    1000,
			"message": "ok",
			"data": map[string]any{
				"enabled": true,
				"running": false,
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(config.APIServiceConfig{
		InternalBaseURL: server.URL,
		OpsToken:        "ops-token",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err = client.RunConfigReload(context.Background()); err != nil {
		t.Fatalf("RunConfigReload() error = %v", err)
	}
}

// TestConfigReloadItemsAcceptsLargeConfigSnapshot 验证较大的 API 配置快照不会被固定 1MiB 上限截断。
func TestConfigReloadItemsAcceptsLargeConfigSnapshot(t *testing.T) {
	largeYAML := strings.Repeat("security:\n  app_key: app_****_key\n", 40_000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/system/config-reload/items" {
			t.Fatalf("path = %s, want /internal/system/config-reload/items", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  true,
			"code":    1000,
			"message": "ok",
			"data": map[string]any{
				"page":         1,
				"pageSize":     20,
				"snapshotYaml": largeYAML,
				"runtimeYaml":  largeYAML,
				"items":        []map[string]any{},
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(config.APIServiceConfig{
		InternalBaseURL: server.URL,
		OpsToken:        "ops-token",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	resp, err := client.ConfigReloadItems(context.Background(), nil)
	if err != nil {
		t.Fatalf("ConfigReloadItems() error = %v", err)
	}
	if resp == nil || resp.SnapshotYAML != largeYAML || resp.RuntimeYAML != largeYAML {
		t.Fatalf("ConfigReloadItems() did not preserve large YAML snapshot")
	}
}

// TestDocsAssetUsesInternalOpsRoute 验证 API 文档代理只调用内网文档资源接口。
func TestDocsAssetUsesInternalOpsRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/internal/docs/接口文档/前台系统/认证接口.md" {
			t.Fatalf("path = %s, want /internal/docs/接口文档/前台系统/认证接口.md", r.URL.Path)
		}
		if got := r.Header.Get(apiRuntimeOpsTokenHeader); got != "ops-token" {
			t.Fatalf("%s = %q, want ops-token", apiRuntimeOpsTokenHeader, got)
		}
		assertAPIRequestSigned(t, r, "ops-token", nil)
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte("# 认证接口"))
	}))
	defer server.Close()

	client, err := NewClient(config.APIServiceConfig{
		InternalBaseURL: server.URL + "/",
		OpsToken:        "ops-token",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	asset, err := client.DocsAsset(context.Background(), "/接口文档/前台系统/认证接口.md")
	if err != nil {
		t.Fatalf("DocsAsset() error = %v", err)
	}
	if asset.StatusCode != http.StatusOK || asset.ContentType != "text/markdown; charset=utf-8" || asset.CacheControl != "no-cache" || string(asset.Body) != "# 认证接口" {
		t.Fatalf("asset = %+v, body=%q", asset, string(asset.Body))
	}
}

// TestDocsAssetHTTPStatusKeepsAPIStatus 验证文档代理错误保留 API 内网状态码。
func TestDocsAssetHTTPStatusKeepsAPIStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	client, err := NewClient(config.APIServiceConfig{InternalBaseURL: server.URL, OpsToken: "ops-token"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.DocsAsset(context.Background(), "/角色文档/后端开发/AI开发规范.md")
	if DocsAssetHTTPStatus(err) != http.StatusNotFound {
		t.Fatalf("DocsAssetHTTPStatus() = %d, want %d, err=%v", DocsAssetHTTPStatus(err), http.StatusNotFound, err)
	}
}

// TestNewClientValidatesRequiredConfig 验证 API 内网客户端必须显式配置地址和运维令牌。
func TestNewClientValidatesRequiredConfig(t *testing.T) {
	if _, err := NewClient(config.APIServiceConfig{}); err == nil {
		t.Fatal("NewClient() should reject empty config")
	}
	if _, err := NewClient(config.APIServiceConfig{InternalBaseURL: "://bad", OpsToken: "token"}); err == nil {
		t.Fatal("NewClient() should reject invalid base url")
	}
}

// assertAPIRequestSigned 验证 admin 发出的 API 内网请求携带 token 与 HMAC 签名。
func assertAPIRequestSigned(t *testing.T, r *http.Request, token string, body []byte) {
	t.Helper()
	if got := r.Header.Get(apiRuntimeOpsTokenHeader); got != token {
		t.Fatalf("%s = %q, want %q", apiRuntimeOpsTokenHeader, got, token)
	}
	timestamp := r.Header.Get(apiRuntimeOpsTimestampHeader)
	if timestamp == "" {
		t.Fatalf("%s is empty", apiRuntimeOpsTimestampHeader)
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		t.Fatalf("%s = %q parse error: %v", apiRuntimeOpsTimestampHeader, timestamp, err)
	}
	if delta := time.Since(time.Unix(seconds, 0)); delta < -time.Minute || delta > time.Minute {
		t.Fatalf("%s = %q outside test window", apiRuntimeOpsTimestampHeader, timestamp)
	}
	bodyHash := apiRequestBodySHA256(body)
	if got := r.Header.Get(apiRuntimeOpsBodySHA256Header); got != bodyHash {
		t.Fatalf("%s = %q, want %q", apiRuntimeOpsBodySHA256Header, got, bodyHash)
	}
	wantSignature := signAPIRequestText(token, r.Method, r.URL.RequestURI(), timestamp, bodyHash)
	if got := r.Header.Get(apiRuntimeOpsSignatureHeader); got != wantSignature {
		t.Fatalf("%s = %q, want %q", apiRuntimeOpsSignatureHeader, got, wantSignature)
	}
}
