package handler

import (
	"net/http"
	"strings"

	"admin_cron/docs"
	"admin_cron/internal/middleware"
	"admin_cron/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RouteModule 描述一个可插拔 HTTP 路由模块。
type RouteModule interface {
	Name() string         // Name 返回路由模块名称
	Register(*RouteScope) // Register 注册路由模块
}

// RouteScope 表示路由模块注册时共享的上下文。
type RouteScope struct {
	Server             *rest.Server                       // HTTP 服务实例
	ServiceContext     *svc.ServiceContext                // 全局服务上下文
	AuthMiddleware     *middleware.AuthMiddleware         // 后台鉴权中间件
	InternalMiddleware *middleware.InternalOnlyMiddleware // 内网鉴权中间件
}

// RouteModuleFunc 允许通过函数快速声明路由模块。
type RouteModuleFunc struct {
	name     string            // 路由模块名称
	register func(*RouteScope) // 路由注册逻辑
}

// NewRouteModuleFunc 创建函数式路由模块。
func NewRouteModuleFunc(name string, register func(*RouteScope)) RouteModule {
	return RouteModuleFunc{name: strings.TrimSpace(name), register: register}
}

// Name 返回路由模块名称。
func (m RouteModuleFunc) Name() string {
	return m.name
}

// Register 执行路由注册逻辑。
func (m RouteModuleFunc) Register(scope *RouteScope) {
	if m.register == nil {
		return
	}
	m.register(scope)
}

// ComposeRouteModules 合并多组路由模块，保持注册顺序。
func ComposeRouteModules(groups ...[]RouteModule) []RouteModule {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	modules := make([]RouteModule, 0, total)
	for _, group := range groups {
		modules = append(modules, group...)
	}
	return modules
}

// BuiltinRouteModules 返回当前进程默认启用的路由模块集合。
// 该方法仅保留给旧测试和兼容入口复用；启动默认清单以 bootstrap.defaultRouteModules 为唯一来源。
func BuiltinRouteModules() []RouteModule {
	return []RouteModule{
		NewHealthRouteModule(),          // 基础健康检查
		NewAuthRouteModule(),            // 认证模块（登录、登出、刷新 token、登录态初始化）
		NewAdminRouteModule(),           // 管理员模块（管理员管理与管理员操作日志）
		NewMessageRouteModule(),         // 消息中心模块（站内信/通知）
		NewTransferRouteModule(),        // 导入导出通用文件传输模块（断点续传上传等）
		NewTaskRouteModule(),            // 任务系统模块（手动触发工作流、队列控制、状态查询）
		NewCollectorRouteModule(),       // 通用收集器模块（批量入队、批量落库、重试与死信管理）
		NewUserTagRouteModule(),         // 用户标签模块（高性能标签计算工作流）
		NewInternalUserTagRouteModule(), // 内网用户标签模块
		NewInternalAuthRouteModule(),    // 内网认证自举模块（首次上线初始化管理员账号）
		NewDocsRouteModule(),            // API 文档模块
	}
}

// NewHealthRouteModule 创建健康检查路由模块。
func NewHealthRouteModule() RouteModule {
	return NewRouteModuleFunc("health", func(scope *RouteScope) {
		registerHealthRoutes(scope.Server, scope.ServiceContext)
	})
}

// NewAuthRouteModule 创建后台认证路由模块。
func NewAuthRouteModule() RouteModule {
	return NewRouteModuleFunc("auth", func(scope *RouteScope) {
		registerAuthRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewAdminRouteModule 创建管理员管理路由模块。
func NewAdminRouteModule() RouteModule {
	return NewRouteModuleFunc("admin", func(scope *RouteScope) {
		registerAdminRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewMessageRouteModule 创建消息中心路由模块。
func NewMessageRouteModule() RouteModule {
	return NewRouteModuleFunc("message", func(scope *RouteScope) {
		registerMessageRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewTransferRouteModule 创建文件传输路由模块。
func NewTransferRouteModule() RouteModule {
	return NewRouteModuleFunc("transfer", func(scope *RouteScope) {
		registerTransferRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewTaskRouteModule 创建任务系统路由模块。
func NewTaskRouteModule() RouteModule {
	return NewRouteModuleFunc("task", func(scope *RouteScope) {
		registerTaskRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewCollectorRouteModule 创建通用收集器管理路由模块。
func NewCollectorRouteModule() RouteModule {
	return NewRouteModuleFunc("collector", func(scope *RouteScope) {
		registerCollectorRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewUserTagRouteModule 创建用户标签业务路由模块。
func NewUserTagRouteModule() RouteModule {
	return NewRouteModuleFunc("user_tag", func(scope *RouteScope) {
		registerUserTagRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewInternalUserTagRouteModule 创建内网用户标签路由模块。
func NewInternalUserTagRouteModule() RouteModule {
	return NewRouteModuleFunc("internal_user_tag", func(scope *RouteScope) {
		registerInternalUserTagRoutes(scope.Server, scope.ServiceContext, scope.InternalMiddleware)
	})
}

// NewInternalAuthRouteModule 创建内网认证自举路由模块。
func NewInternalAuthRouteModule() RouteModule {
	return NewRouteModuleFunc("internal_auth", func(scope *RouteScope) {
		registerInternalAuthRoutes(scope.Server, scope.ServiceContext, scope.InternalMiddleware)
	})
}

// NewDocsRouteModule 创建 API 文档路由模块。
func NewDocsRouteModule() RouteModule {
	return NewRouteModuleFunc("docs", func(scope *RouteScope) {
		registerDocsRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// RegisterHandlers 统一注册全局中间件和各领域路由模块。
// 该方法保留兼容行为：自动追加内置路由；新启动链应使用 bootstrap 统一清单调用 RegisterHandlersWithModules。
// 中间件顺序固定为 outer recover -> trace -> access log -> inner recover：
// 1. outer recover 兜底保护入口中间件自身异常；
// 2. trace 创建上下文和 span；
// 3. access log 使用 defer 在请求结束时统一收口；
// 4. inner recover 最靠近业务 handler，把 panic 转成标准响应后交回上层记录。
func RegisterHandlers(server *rest.Server, serverCtx *svc.ServiceContext, modules ...RouteModule) {
	moduleGroups := [][]RouteModule{BuiltinRouteModules(), modules}
	RegisterHandlersWithModules(server, serverCtx, ComposeRouteModules(moduleGroups...)...)
}

// RegisterHandlersWithModules 按调用方传入的完整模块清单注册全局中间件和 HTTP 路由。
// bootstrap 统一注册中心会调用该入口，避免 handler 包再次隐式追加内置路由造成重复注册。
func RegisterHandlersWithModules(server *rest.Server, serverCtx *svc.ServiceContext, modules ...RouteModule) {
	server.Use(middleware.NewRecoverMiddleware().Handle)
	server.Use(middleware.NewTraceMiddleware().Handle)
	server.Use(middleware.NewAccessLogMiddleware().Handle)
	server.Use(middleware.NewRecoverMiddleware().Handle)

	// 统一在这里构造领域级中间件，避免各路由注册函数重复创建。
	authMw := middleware.NewAuthMiddleware(serverCtx)
	// 内网服务中间件
	internalMw := middleware.NewInternalOnlyMiddleware()
	scope := &RouteScope{
		Server:             server,
		ServiceContext:     serverCtx,
		AuthMiddleware:     authMw,
		InternalMiddleware: internalMw,
	}
	// 路由模块顺序由调用方传入的完整清单决定，便于 bootstrap 统一管理默认模块和外部模块。
	registerRouteModules(scope, modules...)
}

// registerRouteModules 按声明顺序注册路由模块。
func registerRouteModules(scope *RouteScope, modules ...RouteModule) {
	for _, module := range modules {
		if module == nil {
			continue
		}
		module.Register(scope)
	}
}

// RegisterDocsHandler 注册 API 文档服务，文档路由单独走文档鉴权中间件。
func RegisterDocsHandler(server *rest.Server, serverCtx *svc.ServiceContext) {
	registerDocsRoutes(server, serverCtx, middleware.NewAuthMiddleware(serverCtx))
}

// registerDocsRoutes 注册 API 文档路由，供内置路由模块和兼容入口复用。
func registerDocsRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	server.AddRoute(rest.Route{
		Method:  http.MethodPost,
		Path:    "/api/docs/session",
		Handler: authMw.Handle(DocsSessionHandler(), DocsSession.Alias),
	})
	// 文档路由单独挂载 JWT 鉴权，避免与主业务路由的鉴权链互相耦合。
	server.AddRoutes(
		rest.WithMiddleware(
			middleware.DocsJwtMiddleware(serverCtx),
			[]rest.Route{
				{
					Method:  http.MethodGet,
					Path:    "/api/docs", // 文档首页
					Handler: docs.Handler(),
				},
				{
					Method:  http.MethodGet,
					Path:    "/api/docs/:path", // 文档静态资源
					Handler: docs.Handler(),
				},
				{
					Method:  http.MethodGet,
					Path:    "/api/docs/:path/:sub", // 二级文档资源
					Handler: docs.Handler(),
				},
				{
					Method:  http.MethodGet,
					Path:    "/api/docs/:path/:sub/:file", // 三级文档资源
					Handler: docs.Handler(),
				},
			}...,
		),
	)
}
