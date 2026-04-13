package bootstrap

import (
	"sort"

	"admin_cron/internal/handler"
	"admin_cron/internal/taskruntime"

	utils "github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"
)

const (
	// registrationKindComponent 表示应用启动组件注册项。
	registrationKindComponent = "component"
	// registrationKindRoute 表示 HTTP 路由模块注册项。
	registrationKindRoute = "route"
	// registrationKindTaskPlugin 表示任务运行时插件注册项。
	registrationKindTaskPlugin = "task_plugin"
	// registrationKindRuntimeRegistry 表示包级运行时扩展注册表。
	registrationKindRuntimeRegistry = "runtime_registry"

	// componentNameCollector 表示通用收集器启动组件名称。
	componentNameCollector = "collector"
	// componentNameTaskRuntime 表示任务运行时启动组件名称。
	componentNameTaskRuntime = "task_runtime"
	// componentNameHTTPServer 表示 HTTP 服务启动组件名称。
	componentNameHTTPServer = "http_server"
)

// RegistrationManifestItem 描述一个默认注册项，供文档、测试和启动装配核对。
type RegistrationManifestItem struct {
	Kind        string // 注册类型，如 component / route / task_plugin / runtime_registry
	Name        string // 注册名称，必须在同类型内保持唯一
	File        string // 注册实现所在文件
	Method      string // 注册入口方法或构造方法
	Description string // 注册项中文说明
}

// DefaultRegistrationManifest 返回项目默认注册清单。
// 该清单只描述内置注册项，不包含调用方通过 Options 注入的外部组件。
func DefaultRegistrationManifest() []RegistrationManifestItem {
	return []RegistrationManifestItem{
		{
			Kind:        registrationKindComponent,
			Name:        componentNameCollector,
			File:        "internal/bootstrap/component_builtin.go",
			Method:      "newCollectorComponent",
			Description: "注册通用收集器启动组件",
		},
		{
			Kind:        registrationKindComponent,
			Name:        componentNameTaskRuntime,
			File:        "internal/bootstrap/component_builtin.go",
			Method:      "newTaskRuntimeComponent",
			Description: "注册任务队列管理器和任务插件运行时",
		},
		{
			Kind:        registrationKindComponent,
			Name:        componentNameHTTPServer,
			File:        "internal/bootstrap/component_builtin.go",
			Method:      "newHTTPServerComponent",
			Description: "注册 HTTP 服务和路由模块",
		},
		{
			Kind:        registrationKindRoute,
			Name:        "health",
			File:        "internal/handler/routes.go + internal/handler/routes_health.go",
			Method:      "handler.NewHealthRouteModule / registerHealthRoutes",
			Description: "注册健康检查路由",
		},
		{
			Kind:        registrationKindRoute,
			Name:        "auth",
			File:        "internal/handler/routes.go + internal/handler/routes_auth.go",
			Method:      "handler.NewAuthRouteModule / registerAuthRoutes",
			Description: "注册后台认证路由",
		},
		{
			Kind:        registrationKindRoute,
			Name:        "admin",
			File:        "internal/handler/routes.go + internal/handler/routes_admin.go",
			Method:      "handler.NewAdminRouteModule / registerAdminRoutes",
			Description: "注册管理员和审计日志路由",
		},
		{
			Kind:        registrationKindRoute,
			Name:        "message",
			File:        "internal/handler/routes.go + internal/handler/routes_message.go",
			Method:      "handler.NewMessageRouteModule / registerMessageRoutes",
			Description: "注册消息中心路由",
		},
		{
			Kind:        registrationKindRoute,
			Name:        "transfer",
			File:        "internal/handler/routes.go + internal/handler/routes_transfer.go",
			Method:      "handler.NewTransferRouteModule / registerTransferRoutes",
			Description: "注册文件传输路由",
		},
		{
			Kind:        registrationKindRoute,
			Name:        "task",
			File:        "internal/handler/routes.go + internal/handler/routes_task.go",
			Method:      "handler.NewTaskRouteModule / registerTaskRoutes",
			Description: "注册任务系统路由",
		},
		{
			Kind:        registrationKindRoute,
			Name:        "collector",
			File:        "internal/handler/routes.go + internal/handler/routes_collector.go",
			Method:      "handler.NewCollectorRouteModule / registerCollectorRoutes",
			Description: "注册 collector 管理路由",
		},
		{
			Kind:        registrationKindRoute,
			Name:        "user_tag",
			File:        "internal/handler/routes.go + internal/handler/routes_user_tag.go",
			Method:      "handler.NewUserTagRouteModule / registerUserTagRoutes",
			Description: "注册用户标签业务路由",
		},
		{
			Kind:        registrationKindRoute,
			Name:        "internal_user_tag",
			File:        "internal/handler/routes.go + internal/handler/routes_user_tag.go",
			Method:      "handler.NewInternalUserTagRouteModule / registerInternalUserTagRoutes",
			Description: "注册内网用户标签路由",
		},
		{
			Kind:        registrationKindRoute,
			Name:        "internal_auth",
			File:        "internal/handler/routes.go + internal/handler/routes_auth.go",
			Method:      "handler.NewInternalAuthRouteModule / registerInternalAuthRoutes",
			Description: "注册内网认证自举路由",
		},
		{
			Kind:        registrationKindRoute,
			Name:        "docs",
			File:        "internal/handler/routes.go",
			Method:      "handler.NewDocsRouteModule / registerDocsRoutes",
			Description: "注册 API 文档路由",
		},
		{
			Kind:        registrationKindTaskPlugin,
			Name:        "core",
			File:        "internal/taskruntime/core_plugin.go",
			Method:      "taskruntime.NewCorePlugin",
			Description: "注册任务核心 handler、聚合器和缓存刷新工作流",
		},
		{
			Kind:        registrationKindTaskPlugin,
			Name:        "archive",
			File:        "internal/jobs/archive/task/plugin.go + internal/taskruntime/archive_plugin.go",
			Method:      "taskruntime.NewArchivePlugin / archivetask.Setup",
			Description: "注册通用归档任务 handler 和工作流",
		},
		{
			Kind:        registrationKindTaskPlugin,
			Name:        "admin_export",
			File:        "internal/taskruntime/admin_export_plugin.go",
			Method:      "taskruntime.NewAdminExportPlugin",
			Description: "注册管理员列表异步导出 handler",
		},
		{
			Kind:        registrationKindTaskPlugin,
			Name:        "user_tag",
			File:        "internal/jobs/usertag/task/plugin.go + internal/taskruntime/user_tag_plugin.go",
			Method:      "taskruntime.NewUserTagPlugin / usertagtask.Setup",
			Description: "注册用户标签任务 handler、周期维护任务和工作流",
		},
		{
			Kind:        registrationKindRuntimeRegistry,
			Name:        "object_storage",
			File:        "pkg/storage/storage.go",
			Method:      "storage.RegisterObjectStorage",
			Description: "注册 local / s3 对象存储实现",
		},
		{
			Kind:        registrationKindRuntimeRegistry,
			Name:        "virus_scanner",
			File:        "pkg/storage/storage.go",
			Method:      "storage.RegisterVirusScanner",
			Description: "注册上传完成后的病毒扫描器",
		},
		{
			Kind:        registrationKindRuntimeRegistry,
			Name:        "file_upload_policy",
			File:        "internal/logic/file_transfer_policy.go",
			Method:      "logic.RegisterFileUploadPolicy",
			Description: "注册文件上传业务策略",
		},
		{
			Kind:        registrationKindRuntimeRegistry,
			Name:        "collector_processor",
			File:        "internal/collector/types.go + internal/collector/manager.go",
			Method:      "collector.Manager.RegisterProcessor / RegisterProcessorFunc",
			Description: "按 bizType 注册批量消费 Processor",
		},
	}
}

// ValidateDefaultRegistrationManifest 校验默认注册清单与真实内置注册集合是否一致。
// 该校验会在启动阶段执行，避免“文档清单已改、真实注册没改”或反过来地漂移问题。
func ValidateDefaultRegistrationManifest() error {
	// 读取静态注册清单；该清单是文档、启动装配和测试共同依赖的单一对照源。
	items := DefaultRegistrationManifest()
	// 先校验清单本身的字段完整性和唯一性，避免后续真实注册比对时被空 kind/name 干扰。
	if err := validateManifestItems(items); err != nil {
		return errors.Tag(err)
	}
	// 按注册类型归集清单名称，后续逐类和真实内置注册集合做双向差异检查。
	manifestNames := groupManifestNames(items)

	// 校验启动组件：组件数量少但影响启动生命周期，必须确保清单和 defaultComponents 完全一致。
	actualComponentNames := componentNames(defaultComponents())
	if err := validateNameListUnique(registrationKindComponent, actualComponentNames); err != nil {
		return errors.Tag(err)
	}
	if err := validateManifestKindNames(registrationKindComponent, manifestNames[registrationKindComponent], actualComponentNames); err != nil {
		return errors.Tag(err)
	}

	// 校验 HTTP 路由模块：新增或删除路由模块时必须同步注册清单，避免接口文档和实际路由漂移。
	actualRouteNames := routeModuleNames(defaultRouteModules())
	if err := validateNameListUnique(registrationKindRoute, actualRouteNames); err != nil {
		return errors.Tag(err)
	}
	if err := validateManifestKindNames(registrationKindRoute, manifestNames[registrationKindRoute], actualRouteNames); err != nil {
		return errors.Tag(err)
	}

	// 校验任务插件：任务 handler、workflow 和周期任务依赖插件注册，清单缺失会影响排障和启动核对。
	actualPluginNames := pluginNames(defaultTaskPlugins())
	if err := validateNameListUnique(registrationKindTaskPlugin, actualPluginNames); err != nil {
		return errors.Tag(err)
	}
	if err := validateManifestKindNames(registrationKindTaskPlugin, manifestNames[registrationKindTaskPlugin], actualPluginNames); err != nil {
		return errors.Tag(err)
	}

	// 校验运行时扩展入口：包级注册表虽然不参与启动组件顺序，也必须和清单文档保持一致。
	actualRuntimeNames := defaultRuntimeRegistryNames()
	if err := validateNameListUnique(registrationKindRuntimeRegistry, actualRuntimeNames); err != nil {
		return errors.Tag(err)
	}
	if err := validateManifestKindNames(registrationKindRuntimeRegistry, manifestNames[registrationKindRuntimeRegistry], actualRuntimeNames); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// defaultComponents 返回项目内置启动组件集合。
func defaultComponents() []Component {
	return []Component{
		newCollectorComponent(),   // 通用收集器组件
		newTaskRuntimeComponent(), // 任务运行时组件
		newHTTPServerComponent(),  // HTTP 服务与路由组件
	}
}

// defaultRouteModules 返回项目内置 HTTP 路由模块集合。
func defaultRouteModules() []handler.RouteModule {
	return []handler.RouteModule{
		handler.NewHealthRouteModule(),          // 基础健康检查
		handler.NewAuthRouteModule(),            // 认证模块
		handler.NewAdminRouteModule(),           // 管理员模块
		handler.NewMessageRouteModule(),         // 消息中心模块
		handler.NewTransferRouteModule(),        // 文件传输模块
		handler.NewTaskRouteModule(),            // 任务系统模块
		handler.NewCollectorRouteModule(),       // Collector 管理模块
		handler.NewUserTagRouteModule(),         // 用户标签模块
		handler.NewInternalUserTagRouteModule(), // 内网用户标签模块
		handler.NewInternalAuthRouteModule(),    // 内网认证自举模块
		handler.NewDocsRouteModule(),            // API 文档模块
	}
}

// resolveRouteModules 合并内置路由模块与外部注入模块。
func resolveRouteModules(options Options) []handler.RouteModule {
	return handler.ComposeRouteModules(defaultRouteModules(), options.RouteModules)
}

// defaultTaskPlugins 返回项目内置任务运行时插件集合。
func defaultTaskPlugins() []taskruntime.Plugin {
	return []taskruntime.Plugin{
		taskruntime.NewCorePlugin(),        // 核心插件
		taskruntime.NewArchivePlugin(),     // 通用归档插件
		taskruntime.NewAdminExportPlugin(), // 管理员导出插件
		taskruntime.NewUserTagPlugin(),     // 用户标签插件
	}
}

// resolveTaskPlugins 合并内置任务插件与外部注入插件。
func resolveTaskPlugins(options Options) []taskruntime.Plugin {
	return taskruntime.ComposePlugins(defaultTaskPlugins(), options.TaskPlugins)
}

// defaultRuntimeRegistryNames 返回清单需要覆盖的包级运行时扩展入口。
func defaultRuntimeRegistryNames() []string {
	return []string{
		"object_storage",
		"virus_scanner",
		"file_upload_policy",
		"collector_processor",
	}
}

// validateManifestItems 校验注册清单项本身的结构完整性和唯一性。
func validateManifestItems(items []RegistrationManifestItem) error {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.Kind == "" {
			return errors.Errorf("默认注册清单存在空 kind 项")
		}
		if item.Name == "" {
			return errors.Errorf("默认注册清单[%s]存在空 name 项", item.Kind)
		}
		if item.File == "" {
			return errors.Errorf("默认注册清单[%s:%s]存在空 file 项", item.Kind, item.Name)
		}
		if item.Method == "" {
			return errors.Errorf("默认注册清单[%s:%s]存在空 method 项", item.Kind, item.Name)
		}
		if item.Description == "" {
			return errors.Errorf("默认注册清单[%s:%s]存在空 description 项", item.Kind, item.Name)
		}
		key := item.Kind + ":" + item.Name
		if _, ok := seen[key]; ok {
			return errors.Errorf("默认注册清单存在重复项 %s", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// groupManifestNames 按 kind 归集注册名称，供后续和真实内置注册集合比对。
func groupManifestNames(items []RegistrationManifestItem) map[string][]string {
	grouped := make(map[string][]string, 4)
	for _, item := range items {
		grouped[item.Kind] = append(grouped[item.Kind], item.Name)
	}
	return grouped
}

// validateManifestKindNames 校验某一类注册清单名称与真实内置注册名称完全一致。
func validateManifestKindNames(kind string, manifestNames, actualNames []string) error {
	missing := utils.Diff(actualNames, manifestNames)
	extra := utils.Diff(manifestNames, actualNames)
	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return errors.Errorf("默认注册清单[%s]与真实内置注册不一致 missing=%v extra=%v", kind, missing, extra)
}

// validateNameListUnique 校验真实注册列表内部名称唯一，避免同一模块被重复装配。
func validateNameListUnique(kind string, names []string) error {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" {
			return errors.Errorf("默认注册清单[%s]存在空真实注册名称", kind)
		}
		if _, ok := seen[name]; ok {
			return errors.Errorf("默认注册清单[%s]存在重复真实注册名称: %s", kind, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// componentNames 提取组件名称列表，供注册清单校验复用。
func componentNames(items []Component) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		names = append(names, item.Name())
	}
	return names
}

// routeModuleNames 提取路由模块名称列表，供注册清单校验复用。
func routeModuleNames(items []handler.RouteModule) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		names = append(names, item.Name())
	}
	return names
}

// pluginNames 提取任务插件名称列表，供注册清单校验复用。
func pluginNames(items []taskruntime.Plugin) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		names = append(names, item.Name())
	}
	return names
}
