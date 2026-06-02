package handler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"admin/internal/handler/shared"
	"admin/internal/security"
)

// TestDefaultRouteSecurityManifestMatchesRouteContracts 确保安全清单逐条来自真实路由契约。
func TestDefaultRouteSecurityManifestMatchesRouteContracts(t *testing.T) {
	manifest := DefaultRouteSecurityManifest()
	contractByRoute := make(map[string]RouteContract, len(DefaultRouteContracts()))
	wantCount := 0
	for _, contract := range DefaultRouteContracts() {
		contractByRoute[contract.Key()] = contract
		if _, ok := routeSecurityManifestAlias(contract.Alias); ok {
			wantCount++
		}
	}
	if len(manifest) != wantCount {
		t.Fatalf("manifest count = %d, want %d", len(manifest), wantCount)
	}
	for _, item := range manifest {
		contract, ok := contractByRoute[routeKey(item.Method, item.Path)]
		if !ok {
			t.Fatalf("manifest route missing from contracts: %+v", item)
		}
		if item.Alias != contract.Alias || item.Access != contract.Access || item.Describe != contract.Description {
			t.Fatalf("manifest route mismatch item=%+v contract=%+v", item, contract)
		}
	}
}

// TestDefaultRouteSecurityManifestMatchesPolicies 确保清单字段和后端安全策略一致。
func TestDefaultRouteSecurityManifestMatchesPolicies(t *testing.T) {
	manifestByAlias := make(map[string]RouteSecurityManifestItem, len(DefaultRouteSecurityManifest()))
	for _, item := range DefaultRouteSecurityManifest() {
		manifestByAlias[item.Alias] = item
		policy := security.PolicyByRoute(item.Alias)
		if !reflect.DeepEqual(item.RequestSign, emptyToNil(policy.RequestSign)) ||
			!reflect.DeepEqual(item.RequestCipher, emptyToNil(policy.RequestCipher)) ||
			!reflect.DeepEqual(item.ResponseSign, emptyToNil(policy.ResponseSign)) ||
			!reflect.DeepEqual(item.ResponseCipher, emptyToNil(policy.ResponseCipher)) {
			t.Fatalf("manifest policy mismatch alias=%s item=%+v policy=%+v", item.Alias, item, policy)
		}
	}
	for alias := range security.RouteSecurityPolicies {
		if _, ok := manifestByAlias[string(alias)]; !ok {
			t.Fatalf("security policy alias missing from route manifest: %s", alias)
		}
	}
}

// TestDefaultRouteSecurityManifestReturnsCopies 确保调用方不能通过清单修改全局策略。
func TestDefaultRouteSecurityManifestReturnsCopies(t *testing.T) {
	for _, item := range DefaultRouteSecurityManifest() {
		if len(item.RequestSign) == 0 {
			continue
		}
		original := security.PolicyByRoute(item.Alias).RequestSign[0]
		item.RequestSign[0] = "changed"
		if got := security.PolicyByRoute(item.Alias).RequestSign[0]; got != original {
			t.Fatalf("global security policy changed alias=%s got=%s want=%s", item.Alias, got, original)
		}
		return
	}
	t.Fatal("manifest should contain at least one request sign field")
}

// TestDefaultRouteSecurityManifestMatchesSnapshots 确保文档和前端安全清单未和后端安全策略漂移。
func TestDefaultRouteSecurityManifestMatchesSnapshots(t *testing.T) {
	wantSnapshot := routeSecurityManifestSnapshotValue()
	wantJSON := routeSecurityManifestSnapshotJSON(t, wantSnapshot)
	for _, target := range routeSecurityManifestSnapshotTargets() {
		if os.Getenv("UPDATE_ROUTE_SECURITY_MANIFEST") == "1" {
			if err := os.MkdirAll(filepath.Dir(target.path), 0o755); err != nil {
				t.Fatalf("create route security manifest dir %s: %v", target.path, err)
			}
			if err := os.WriteFile(target.path, []byte(wantJSON), 0o644); err != nil {
				t.Fatalf("write route security manifest snapshot %s: %v", target.path, err)
			}
			continue
		}
		body, err := os.ReadFile(target.path)
		if err != nil {
			t.Fatalf("read route security manifest snapshot %s: %v", target.name, err)
		}
		var gotSnapshot routeSecurityManifestSnapshot
		if err := json.Unmarshal(body, &gotSnapshot); err != nil {
			t.Fatalf("decode route security manifest snapshot %s: %v", target.name, err)
		}
		if !reflect.DeepEqual(gotSnapshot, wantSnapshot) {
			t.Fatalf("route security manifest snapshot drifted: %s", target.name)
		}
	}
}

// TestDefaultRouteMetasCoverRouteSpecs 确保路由规格引用的 Meta 均来自统一登记表。
func TestDefaultRouteMetasCoverRouteSpecs(t *testing.T) {
	metas := shared.DefaultRouteMetas()
	for _, meta := range metas {
		if strings.TrimSpace(string(meta.Alias)) == "" {
			t.Fatalf("route meta missing alias: %+v", meta)
		}
	}
	for _, spec := range DefaultRouteSpecs() {
		if spec.Meta.Alias == "" {
			continue
		}
		if !routeMetaRegistered(metas, spec.Meta) {
			t.Fatalf("route spec meta not registered: %s %s meta=%+v", spec.Method, spec.Path, spec.Meta)
		}
	}
}

// routeMetaRegistered 返回路由测试辅助数据。
func routeMetaRegistered(metas []shared.RouteMeta, want shared.RouteMeta) bool {
	for _, meta := range metas {
		if meta == want {
			return true
		}
	}
	return false
}

// routeSecurityManifestSnapshotTargets 返回路由测试辅助数据。
func routeSecurityManifestSnapshotTargets() []struct {
	name string // name 表示测试场景名称。
	path string // path 表示请求路径。
} {
	return []struct {
		name string // name 表示测试场景名称。
		path string // path 表示请求路径。
	}{
		{
			name: "admin docs",
			path: filepath.Join("..", "..", "docs", "site", "route_security_manifest.json"),
		},
		{
			name: "admin-vue runtime manifest",
			path: filepath.Join("..", "..", "..", "admin-vue", "apps", "web-antd", "src", "utils", "security", "route-security-manifest.json"),
		},
	}
}

// routeSecurityManifestSnapshot 表示测试使用的辅助结构。
type routeSecurityManifestSnapshot struct {
	Version int                                 `json:"version"` // 快照版本
	Routes  []routeSecurityManifestSnapshotItem `json:"routes"`  // 前端同步路由清单
}

// routeSecurityManifestSnapshotItem 表示测试使用的辅助结构。
type routeSecurityManifestSnapshotItem struct {
	Alias          string             `json:"alias"`          // 路由别名
	Method         string             `json:"method"`         // HTTP 方法
	Path           string             `json:"path"`           // HTTP 路径
	Access         shared.RouteAccess `json:"access"`         // 访问边界
	Describe       string             `json:"describe"`       // 中文业务说明
	RequestSign    []string           `json:"requestSign"`    // 请求签名字段
	RequestCipher  []string           `json:"requestCipher"`  // 请求解密字段
	ResponseSign   []string           `json:"responseSign"`   // 响应回签字段
	ResponseCipher []string           `json:"responseCipher"` // 响应加密字段
}

// routeSecurityManifestSnapshotValue 返回路由测试辅助数据。
func routeSecurityManifestSnapshotValue() routeSecurityManifestSnapshot {
	return routeSecurityManifestSnapshot{
		Version: 1,
		Routes:  routeSecurityManifestSnapshotItems(DefaultRouteSecurityManifest()),
	}
}

// routeSecurityManifestSnapshotJSON 返回路由测试辅助数据。
func routeSecurityManifestSnapshotJSON(t *testing.T, snapshot routeSecurityManifestSnapshot) string {
	t.Helper()
	body, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("marshal route security manifest snapshot: %v", err)
	}
	return string(body) + "\n"
}

// routeSecurityManifestSnapshotItems 返回路由测试辅助数据。
func routeSecurityManifestSnapshotItems(items []RouteSecurityManifestItem) []routeSecurityManifestSnapshotItem {
	result := make([]routeSecurityManifestSnapshotItem, 0, len(items))
	for _, item := range items {
		result = append(result, routeSecurityManifestSnapshotItem{
			Alias:          item.Alias,
			Method:         item.Method,
			Path:           item.Path,
			Access:         item.Access,
			Describe:       item.Describe,
			RequestSign:    emptyToSlice(item.RequestSign),
			RequestCipher:  emptyToSlice(item.RequestCipher),
			ResponseSign:   emptyToSlice(item.ResponseSign),
			ResponseCipher: emptyToSlice(item.ResponseCipher),
		})
	}
	return result
}

// routeKey 返回路由测试辅助数据。
func routeKey(method string, path string) string {
	return method + " " + path
}

// emptyToNil 表示测试辅助逻辑。
func emptyToNil(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// emptyToSlice 表示测试辅助逻辑。
func emptyToSlice(fields []string) []string {
	if len(fields) == 0 {
		return []string{}
	}
	return fields
}
