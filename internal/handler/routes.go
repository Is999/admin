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
	"admin/internal/handler/shared"
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

// RouteModuleSpec 描述内置 HTTP 路由模块的装配和注册清单信息。
type RouteModuleSpec struct {
	Name        string                    // 模块名称，必须在内置模块中唯一
	File        string                    // 模块注册和路由规格所在文件
	Method      string                    // 模块构造和路由规格入口
	Description string                    // 模块中文说明
	Routes      func() []shared.RouteSpec // 模块内置路由规格
}

// 内置路由模块名称常量，供规格表和显式构造入口复用。
const (
	// routeModuleHealth 表示健康检查路由模块。
	routeModuleHealth = "health"
	// routeModuleAuth 表示后台认证路由模块。
	routeModuleAuth = "auth"
	// routeModuleAdmin 表示管理员管理路由模块。
	routeModuleAdmin = "admin"
	// routeModuleProfile 表示个人中心路由模块。
	routeModuleProfile = "profile"
	// routeModuleRBAC 表示角色权限路由模块。
	routeModuleRBAC = "rbac"
	// routeModuleConfig 表示系统配置路由模块。
	routeModuleConfig = "config"
	// routeModuleCache 表示缓存管理路由模块。
	routeModuleCache = "cache"
	// routeModuleAdminLog 表示管理员审计日志路由模块。
	routeModuleAdminLog = "admin_log"
	// routeModuleSecretKey 表示秘钥管理路由模块。
	routeModuleSecretKey = "secret_key"
	// routeModuleSecurityDebug 表示安全调试路由模块。
	routeModuleSecurityDebug = "security_debug"
	// routeModuleMessage 表示消息中心路由模块。
	routeModuleMessage = "message"
	// routeModuleTransfer 表示文件传输路由模块。
	routeModuleTransfer = "transfer"
	// routeModuleTask 表示任务系统路由模块。
	routeModuleTask = "task"
	// routeModuleCollector 表示 Collector 管理路由模块。
	routeModuleCollector = "collector"
	// routeModuleUserTag 表示用户标签业务路由模块。
	routeModuleUserTag = "user_tag"
	// routeModuleInternalUserTag 表示内网用户标签路由模块。
	routeModuleInternalUserTag = "internal_user_tag"
	// routeModuleInternalAuth 表示内网认证自举路由模块。
	routeModuleInternalAuth = "internal_auth"
	// routeModuleDocs 表示 API 文档路由模块。
	routeModuleDocs = "docs"
)

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
// 该方法由内置模块规格派生，供启动装配、测试和显式装配入口复用。
func BuiltinRouteModules() []RouteModule {
	specs := BuiltinRouteModuleSpecs()
	modules := make([]RouteModule, 0, len(specs))
	for _, spec := range specs {
		if spec.Routes == nil {
			continue
		}
		modules = append(modules, newRouteModule(spec))
	}
	return modules
}

// builtinRouteModuleSpecs 是内置 HTTP 路由模块的单一装配规格。
var builtinRouteModuleSpecs = []RouteModuleSpec{
	{
		Name:        routeModuleHealth,
		File:        "internal/handler/routes.go + internal/handler/health/routes.go",
		Method:      "handler.NewHealthRouteModule / health.RouteSpecs",
		Description: "注册健康检查路由",
		Routes:      healthhandler.RouteSpecs,
	},
	{
		Name:        routeModuleAuth,
		File:        "internal/handler/routes.go + internal/handler/auth/routes.go",
		Method:      "handler.NewAuthRouteModule / auth.RouteSpecs",
		Description: "注册后台认证路由",
		Routes:      authhandler.RouteSpecs,
	},
	{
		Name:        routeModuleAdmin,
		File:        "internal/handler/routes.go + internal/handler/admin/routes.go",
		Method:      "handler.NewAdminRouteModule / admin.RouteSpecs",
		Description: "注册管理员管理路由",
		Routes:      adminhandler.RouteSpecs,
	},
	{
		Name:        routeModuleProfile,
		File:        "internal/handler/routes.go + internal/handler/profile/routes.go",
		Method:      "handler.NewProfileRouteModule / profile.RouteSpecs",
		Description: "注册个人中心和账号安全路由",
		Routes:      profilehandler.RouteSpecs,
	},
	{
		Name:        routeModuleRBAC,
		File:        "internal/handler/routes.go + internal/handler/rbac/routes.go",
		Method:      "handler.NewRBACRouteModule / rbac.RouteSpecs",
		Description: "注册角色权限路由",
		Routes:      rbachandler.RouteSpecs,
	},
	{
		Name:        routeModuleConfig,
		File:        "internal/handler/routes.go + internal/handler/config/routes.go",
		Method:      "handler.NewConfigRouteModule / config.RouteSpecs",
		Description: "注册系统配置路由",
		Routes:      confighandler.RouteSpecs,
	},
	{
		Name:        routeModuleCache,
		File:        "internal/handler/routes.go + internal/handler/cachemanage/routes.go",
		Method:      "handler.NewCacheRouteModule / cachemanage.RouteSpecs",
		Description: "注册缓存管理路由",
		Routes:      cachehandler.RouteSpecs,
	},
	{
		Name:        routeModuleAdminLog,
		File:        "internal/handler/routes.go + internal/handler/admin/routes.go",
		Method:      "handler.NewAdminLogRouteModule / admin.LogRouteSpecs",
		Description: "注册管理员审计日志路由",
		Routes:      adminhandler.LogRouteSpecs,
	},
	{
		Name:        routeModuleSecretKey,
		File:        "internal/handler/routes.go + internal/handler/secretkey/routes.go",
		Method:      "handler.NewSecretKeyRouteModule / secretkey.RouteSpecs",
		Description: "注册秘钥管理路由",
		Routes:      secretkeyhandler.RouteSpecs,
	},
	{
		Name:        routeModuleSecurityDebug,
		File:        "internal/handler/routes.go + internal/handler/securitydebug/routes.go",
		Method:      "handler.NewSecurityDebugRouteModule / securitydebug.RouteSpecs",
		Description: "注册安全调试路由",
		Routes:      securitydebughandler.RouteSpecs,
	},
	{
		Name:        routeModuleMessage,
		File:        "internal/handler/routes.go + internal/handler/message/routes.go",
		Method:      "handler.NewMessageRouteModule / message.RouteSpecs",
		Description: "注册消息中心路由",
		Routes:      messagehandler.RouteSpecs,
	},
	{
		Name:        routeModuleTransfer,
		File:        "internal/handler/routes.go + internal/handler/transfer/routes.go",
		Method:      "handler.NewTransferRouteModule / transfer.RouteSpecs",
		Description: "注册文件传输路由",
		Routes:      transferhandler.RouteSpecs,
	},
	{
		Name:        routeModuleTask,
		File:        "internal/handler/routes.go + internal/handler/task/routes.go",
		Method:      "handler.NewTaskRouteModule / task.RouteSpecs",
		Description: "注册任务系统路由",
		Routes:      taskhandler.RouteSpecs,
	},
	{
		Name:        routeModuleCollector,
		File:        "internal/handler/routes.go + internal/handler/collector/routes.go",
		Method:      "handler.NewCollectorRouteModule / collector.RouteSpecs",
		Description: "注册 Collector 管理路由",
		Routes:      collectorhandler.RouteSpecs,
	},
	{
		Name:        routeModuleUserTag,
		File:        "internal/handler/routes.go + internal/handler/usertag/routes.go",
		Method:      "handler.NewUserTagRouteModule / usertag.RouteSpecs",
		Description: "注册用户标签业务路由",
		Routes:      usertaghandler.RouteSpecs,
	},
	{
		Name:        routeModuleInternalUserTag,
		File:        "internal/handler/routes.go + internal/handler/usertag/routes.go",
		Method:      "handler.NewInternalUserTagRouteModule / usertag.InternalRouteSpecs",
		Description: "注册内网用户标签路由",
		Routes:      usertaghandler.InternalRouteSpecs,
	},
	{
		Name:        routeModuleInternalAuth,
		File:        "internal/handler/routes.go + internal/handler/auth/routes.go",
		Method:      "handler.NewInternalAuthRouteModule / auth.InternalRouteSpecs",
		Description: "注册内网认证自举路由",
		Routes:      authhandler.InternalRouteSpecs,
	},
	{
		Name:        routeModuleDocs,
		File:        "internal/handler/routes.go + internal/handler/docs/routes.go",
		Method:      "handler.NewDocsRouteModule / docs.RouteSpecs",
		Description: "注册 API 文档路由",
		Routes:      docshandler.RouteSpecs,
	},
}

// BuiltinRouteModuleSpecs 返回内置 HTTP 路由模块规格，供启动装配和注册清单复用。
func BuiltinRouteModuleSpecs() []RouteModuleSpec {
	out := make([]RouteModuleSpec, len(builtinRouteModuleSpecs))
	copy(out, builtinRouteModuleSpecs)
	return out
}

// newRouteModule 从内置模块规格派生真实路由模块。
func newRouteModule(spec RouteModuleSpec) RouteModule {
	return NewRouteModuleFunc(spec.Name, func(scope *RouteScope) {
		if spec.Routes == nil {
			return
		}
		shared.AddRouteSpecs(scope.Server, scope.ServiceContext, scope.AuthMiddleware, scope.InternalMiddleware, routeSpecsWithModule(spec.Name, spec.Routes()))
	})
}

// DefaultRouteSpecs 返回内置 HTTP 路由规格，顺序与内置模块注册顺序保持一致。
func DefaultRouteSpecs() []shared.RouteSpec {
	moduleSpecs := BuiltinRouteModuleSpecs()
	groups := make([][]shared.RouteSpec, 0, len(moduleSpecs))
	total := 0
	for _, moduleSpec := range moduleSpecs {
		if moduleSpec.Routes == nil {
			continue
		}
		group := routeSpecsWithModule(moduleSpec.Name, moduleSpec.Routes())
		total += len(group)
		groups = append(groups, group)
	}
	specs := make([]shared.RouteSpec, 0, total)
	for _, group := range groups {
		specs = append(specs, group...)
	}
	return specs
}

// routeSpecsWithModule 返回写入模块名称后的路由规格快照。
func routeSpecsWithModule(module string, specs []shared.RouteSpec) []shared.RouteSpec {
	out := make([]shared.RouteSpec, len(specs))
	copy(out, specs)
	for index := range out {
		if out[index].Module == "" {
			out[index].Module = module
		}
	}
	return out
}

// newBuiltinRouteModule 按内置模块名称构造路由模块，供显式 NewXxx 入口复用同一份规格。
func newBuiltinRouteModule(name string) RouteModule {
	spec, ok := builtinRouteModuleSpec(name)
	if !ok {
		panic("unknown builtin route module: " + name)
	}
	return newRouteModule(spec)
}

// builtinRouteModuleSpec 根据模块名称读取内置模块规格。
func builtinRouteModuleSpec(name string) (RouteModuleSpec, bool) {
	name = strings.TrimSpace(name)
	for _, spec := range builtinRouteModuleSpecs {
		if spec.Name == name {
			return spec, true
		}
	}
	return RouteModuleSpec{}, false
}

// NewHealthRouteModule 创建健康检查路由模块。
func NewHealthRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleHealth)
}

// NewAuthRouteModule 创建后台认证路由模块。
func NewAuthRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleAuth)
}

// NewAdminRouteModule 创建管理员管理路由模块。
func NewAdminRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleAdmin)
}

// NewProfileRouteModule 创建个人中心和账号安全路由模块。
func NewProfileRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleProfile)
}

// NewRBACRouteModule 创建角色权限路由模块。
func NewRBACRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleRBAC)
}

// NewConfigRouteModule 创建系统配置路由模块。
func NewConfigRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleConfig)
}

// NewCacheRouteModule 创建缓存管理路由模块。
func NewCacheRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleCache)
}

// NewAdminLogRouteModule 创建管理员审计日志路由模块。
func NewAdminLogRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleAdminLog)
}

// NewSecretKeyRouteModule 创建秘钥管理路由模块。
func NewSecretKeyRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleSecretKey)
}

// NewSecurityDebugRouteModule 创建安全调试路由模块。
func NewSecurityDebugRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleSecurityDebug)
}

// NewMessageRouteModule 创建消息中心路由模块。
func NewMessageRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleMessage)
}

// NewTransferRouteModule 创建文件传输路由模块。
func NewTransferRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleTransfer)
}

// NewTaskRouteModule 创建任务系统路由模块。
func NewTaskRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleTask)
}

// NewCollectorRouteModule 创建通用收集器管理路由模块。
func NewCollectorRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleCollector)
}

// NewUserTagRouteModule 创建用户标签业务路由模块。
func NewUserTagRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleUserTag)
}

// NewInternalUserTagRouteModule 创建内网用户标签路由模块。
func NewInternalUserTagRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleInternalUserTag)
}

// NewInternalAuthRouteModule 创建内网认证自举路由模块。
func NewInternalAuthRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleInternalAuth)
}

// NewDocsRouteModule 创建 API 文档路由模块。
func NewDocsRouteModule() RouteModule {
	return newBuiltinRouteModule(routeModuleDocs)
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

	// 统一在这里构造领域级中间件，避免各模块重复创建。
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
