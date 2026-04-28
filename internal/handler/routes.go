package handler

import (
	"strings"

	adminhandler "admin/internal/handler/admin"
	authhandler "admin/internal/handler/auth"
	cachehandler "admin/internal/handler/cachemanage"
	collectorhandler "admin/internal/handler/collector"
	confighandler "admin/internal/handler/config"
	docshandler "admin/internal/handler/docs"
	healthhandler "admin/internal/handler/health"
	messagehandler "admin/internal/handler/message"
	profilehandler "admin/internal/handler/profile"
	rbachandler "admin/internal/handler/rbac"
	secretkeyhandler "admin/internal/handler/secretkey"
	securitydebughandler "admin/internal/handler/securitydebug"
	taskhandler "admin/internal/handler/task"
	transferhandler "admin/internal/handler/transfer"
	usertaghandler "admin/internal/handler/usertag"
	"admin/internal/middleware"
	"admin/internal/svc"

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
// 该方法供测试和显式装配入口复用；启动默认清单以 bootstrap.defaultRouteModules 为唯一来源。
func BuiltinRouteModules() []RouteModule {
	return []RouteModule{
		NewHealthRouteModule(),          // 基础健康检查
		NewAuthRouteModule(),            // 认证模块（登录、登出、刷新 token、登录态初始化）
		NewAdminRouteModule(),           // 管理员管理模块
		NewProfileRouteModule(),         // 个人中心和账号安全模块
		NewRBACRouteModule(),            // 角色权限模块
		NewConfigRouteModule(),          // 系统配置模块
		NewCacheRouteModule(),           // 缓存管理模块
		NewAdminLogRouteModule(),        // 管理员审计日志模块
		NewSecretKeyRouteModule(),       // 秘钥管理模块
		NewSecurityDebugRouteModule(),   // 安全调试模块
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
		healthhandler.RegisterRoutes(scope.Server, scope.ServiceContext)
	})
}

// NewAuthRouteModule 创建后台认证路由模块。
func NewAuthRouteModule() RouteModule {
	return NewRouteModuleFunc("auth", func(scope *RouteScope) {
		authhandler.RegisterRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewAdminRouteModule 创建管理员管理路由模块。
func NewAdminRouteModule() RouteModule {
	return NewRouteModuleFunc("admin", func(scope *RouteScope) {
		adminhandler.RegisterRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewProfileRouteModule 创建个人中心和账号安全路由模块。
func NewProfileRouteModule() RouteModule {
	return NewRouteModuleFunc("profile", func(scope *RouteScope) {
		profilehandler.RegisterManageRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewRBACRouteModule 创建角色权限路由模块。
func NewRBACRouteModule() RouteModule {
	return NewRouteModuleFunc("rbac", func(scope *RouteScope) {
		rbachandler.RegisterRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewConfigRouteModule 创建系统配置路由模块。
func NewConfigRouteModule() RouteModule {
	return NewRouteModuleFunc("config", func(scope *RouteScope) {
		confighandler.RegisterRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewCacheRouteModule 创建缓存管理路由模块。
func NewCacheRouteModule() RouteModule {
	return NewRouteModuleFunc("cache", func(scope *RouteScope) {
		cachehandler.RegisterRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewAdminLogRouteModule 创建管理员审计日志路由模块。
func NewAdminLogRouteModule() RouteModule {
	return NewRouteModuleFunc("admin_log", func(scope *RouteScope) {
		adminhandler.RegisterLogRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewSecretKeyRouteModule 创建秘钥管理路由模块。
func NewSecretKeyRouteModule() RouteModule {
	return NewRouteModuleFunc("secret_key", func(scope *RouteScope) {
		secretkeyhandler.RegisterRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewSecurityDebugRouteModule 创建安全调试路由模块。
func NewSecurityDebugRouteModule() RouteModule {
	return NewRouteModuleFunc("security_debug", func(scope *RouteScope) {
		securitydebughandler.RegisterRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewMessageRouteModule 创建消息中心路由模块。
func NewMessageRouteModule() RouteModule {
	return NewRouteModuleFunc("message", func(scope *RouteScope) {
		messagehandler.RegisterRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewTransferRouteModule 创建文件传输路由模块。
func NewTransferRouteModule() RouteModule {
	return NewRouteModuleFunc("transfer", func(scope *RouteScope) {
		transferhandler.RegisterRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewTaskRouteModule 创建任务系统路由模块。
func NewTaskRouteModule() RouteModule {
	return NewRouteModuleFunc("task", func(scope *RouteScope) {
		taskhandler.RegisterRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewCollectorRouteModule 创建通用收集器管理路由模块。
func NewCollectorRouteModule() RouteModule {
	return NewRouteModuleFunc("collector", func(scope *RouteScope) {
		collectorhandler.RegisterRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewUserTagRouteModule 创建用户标签业务路由模块。
func NewUserTagRouteModule() RouteModule {
	return NewRouteModuleFunc("user_tag", func(scope *RouteScope) {
		usertaghandler.RegisterRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// NewInternalUserTagRouteModule 创建内网用户标签路由模块。
func NewInternalUserTagRouteModule() RouteModule {
	return NewRouteModuleFunc("internal_user_tag", func(scope *RouteScope) {
		usertaghandler.RegisterInternalRoutes(scope.Server, scope.ServiceContext, scope.InternalMiddleware)
	})
}

// NewInternalAuthRouteModule 创建内网认证自举路由模块。
func NewInternalAuthRouteModule() RouteModule {
	return NewRouteModuleFunc("internal_auth", func(scope *RouteScope) {
		authhandler.RegisterInternalRoutes(scope.Server, scope.ServiceContext, scope.InternalMiddleware)
	})
}

// NewDocsRouteModule 创建 API 文档路由模块。
func NewDocsRouteModule() RouteModule {
	return NewRouteModuleFunc("docs", func(scope *RouteScope) {
		docshandler.RegisterRoutes(scope.Server, scope.ServiceContext, scope.AuthMiddleware)
	})
}

// RegisterHandlers 统一注册全局中间件和各领域路由模块。
// 自动组合内置路由和额外模块；启动链使用 bootstrap 统一清单调用 RegisterHandlersWithModules。
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
	docshandler.RegisterRoutes(server, serverCtx, middleware.NewAuthMiddleware(serverCtx))
}
