package apiruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"admin/internal/config"
	"admin/internal/types"
)

// TestSyncUserRuntimeUsesInternalOpsRoute 验证 admin 只调用 API 内网运行态同步接口。
func TestSyncUserRuntimeUsesInternalOpsRoute(t *testing.T) {
	var gotPayload userRuntimeSyncPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/internal/users/42/runtime-sync" {
			t.Fatalf("path = %s, want /internal/users/42/runtime-sync", r.URL.Path)
		}
		if got := r.Header.Get(apiRuntimeOpsTokenHeader); got != "ops-token" {
			t.Fatalf("%s = %q, want ops-token", apiRuntimeOpsTokenHeader, got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("Decode payload failed: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  true,
			"code":    200,
			"message": "ok",
			"data": map[string]any{
				"userId":                  42,
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
	resp, err := client.SyncUserRuntime(context.Background(), 42, false, false, "  manual sync  ")
	if err != nil {
		t.Fatalf("SyncUserRuntime() error = %v", err)
	}
	if !gotPayload.Profile || gotPayload.Sessions || gotPayload.Reason != "manual sync" {
		t.Fatalf("payload = %+v, want profile default true and trimmed reason", gotPayload)
	}
	if !resp.Enabled || !resp.Success || resp.UserID != 42 || !resp.ProfileCacheInvalidated || resp.SessionsInvalidated {
		t.Fatalf("response = %+v, want successful profile-only sync", resp)
	}
	if resp.Message != "API 运行态已同步" {
		t.Fatalf("message = %q, want default sync message", resp.Message)
	}
}

// TestConfigReloadItemsUsesInternalOpsRoute 验证 API 运行态配置项查询会透传筛选 query。
func TestConfigReloadItemsUsesInternalOpsRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != apiRuntimeConfigReloadItemsPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, apiRuntimeConfigReloadItemsPath)
		}
		if got := r.Header.Get(apiRuntimeOpsTokenHeader); got != "ops-token" {
			t.Fatalf("%s = %q, want ops-token", apiRuntimeOpsTokenHeader, got)
		}
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

// TestConfigReloadItemsAcceptsLargeConfigSnapshot 验证较大的 API 配置快照不会被固定 1MiB 上限截断。
func TestConfigReloadItemsAcceptsLargeConfigSnapshot(t *testing.T) {
	largeYAML := strings.Repeat("security:\n  app_key: app_****_key\n", 40_000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != apiRuntimeConfigReloadItemsPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, apiRuntimeConfigReloadItemsPath)
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
