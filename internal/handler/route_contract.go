package handler

import "admin/internal/handler/shared"

// RouteAccess 描述路由的入口鉴权类型，供网关、文档和测试统一理解路由边界。
type RouteAccess = shared.RouteAccess

const (
	RouteAccessPublic   = shared.RouteAccessPublic   // 匿名可访问，或由业务自行完成临时凭证校验
	RouteAccessAuth     = shared.RouteAccessAuth     // 后台登录态鉴权
	RouteAccessInternal = shared.RouteAccessInternal // 内网 IP 白名单入口
	RouteAccessDocs     = shared.RouteAccessDocs     // 文档会话 JWT 鉴权
	RouteAccessHealth   = shared.RouteAccessHealth   // 健康检查与指标入口
)

// RouteContract 是 HTTP 路由对外暴露的稳定契约。
type RouteContract struct {
	Module        string      // 路由模块名称
	Method        string      // HTTP Method
	Path          string      // go-zero 路由路径
	Access        RouteAccess // 入口鉴权类型
	Alias         string      // 权限/审计/trace 统一路由别名；空表示不进入业务鉴权链
	Description   string      // 业务说明
	SkipAccessLog bool        // 是否跳过普通访问日志
}

// Key 返回 method+path 组合键，便于和 go-zero 实际注册结果对齐。
func (c RouteContract) Key() string {
	return c.Method + " " + c.Path
}

// DefaultRouteContracts 返回内置路由模块的完整契约清单，顺序与注册顺序保持一致。
func DefaultRouteContracts() []RouteContract {
	specs := DefaultRouteSpecs()
	contracts := make([]RouteContract, 0, len(specs))
	for _, spec := range specs {
		contracts = append(contracts, routeContractFromSpec(spec))
	}
	return contracts
}

// routeContractFromSpec 将真实路由规格投影为对外契约。
func routeContractFromSpec(spec shared.RouteSpec) RouteContract {
	return RouteContract{
		Module:        spec.Module,
		Method:        spec.Method,
		Path:          spec.Path,
		Access:        spec.Access,
		Alias:         string(spec.RouteAlias()),
		Description:   spec.Describe(),
		SkipAccessLog: spec.SkipAccessLog,
	}
}
