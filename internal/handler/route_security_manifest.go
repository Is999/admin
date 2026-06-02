package handler

import (
	"strings"

	"admin/internal/handler/shared"
	"admin/internal/middleware"
	"admin/internal/security"
)

// RouteSecurityManifestItem 描述前后端同步安全策略所需的单路由清单项。
type RouteSecurityManifestItem struct {
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

// DefaultRouteSecurityManifest 返回内置路由的安全策略清单，供测试、文档和前端同步复用。
func DefaultRouteSecurityManifest() []RouteSecurityManifestItem {
	contracts := DefaultRouteContracts()
	items := make([]RouteSecurityManifestItem, 0, len(contracts))
	for _, contract := range contracts {
		alias, ok := routeSecurityManifestAlias(contract.Alias)
		if !ok {
			continue
		}
		policy := security.PolicyByRoute(alias)
		items = append(items, RouteSecurityManifestItem{
			Alias:          alias,
			Method:         contract.Method,
			Path:           contract.Path,
			Access:         contract.Access,
			Describe:       contract.Description,
			RequestSign:    cloneSecurityFields(policy.RequestSign),
			RequestCipher:  cloneSecurityFields(policy.RequestCipher),
			ResponseSign:   cloneSecurityFields(policy.ResponseSign),
			ResponseCipher: cloneSecurityFields(policy.ResponseCipher),
		})
	}
	return items
}

// routeSecurityManifestAlias 过滤无业务安全策略的空别名和 ignore 路由。
func routeSecurityManifestAlias(alias string) (string, bool) {
	alias = strings.TrimSpace(alias)
	if alias == "" || alias == string(middleware.Ignore) {
		return "", false
	}
	return alias, true
}

// cloneSecurityFields 复制字段级安全策略，避免调用方修改全局策略切片。
func cloneSecurityFields(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	result := make([]string, len(fields))
	copy(result, fields)
	return result
}
