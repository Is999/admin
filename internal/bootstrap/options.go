package bootstrap

import (
	"admin/internal/handler"
	"admin/internal/task/runtime"
)

// defaultDisplayName 定义启动日志默认展示名，避免不同入口各自硬编码。
const defaultDisplayName = "admin"

// Options 描述应用启动时可注入的扩展项。
type Options struct {
	DisplayName          string                // 启动日志中展示的服务名
	UseDefaultComponents bool                  // 是否启用内置启动组件集合
	Components           []Component           // 额外注入的启动组件
	RouteModules         []handler.RouteModule // 额外注入的 HTTP 路由模块
	TaskPlugins          []taskruntime.Plugin  // 额外注入的任务运行时插件
}

// Option 定义启动选项的注入方式。
type Option func(*Options)

// WithTaskPlugins 为启动链追加外部任务插件。
func WithTaskPlugins(plugins ...taskruntime.Plugin) Option {
	return func(opts *Options) {
		if opts == nil || len(plugins) == 0 {
			return
		}
		opts.TaskPlugins = append(opts.TaskPlugins, plugins...)
	}
}

// WithComponents 为启动链追加外部启动组件。
func WithComponents(components ...Component) Option {
	return func(opts *Options) {
		if opts == nil || len(components) == 0 {
			return
		}
		opts.Components = append(opts.Components, components...)
	}
}

// WithDefaultComponents 控制是否装配项目内置启动组件集合。
func WithDefaultComponents(enabled bool) Option {
	return func(opts *Options) {
		if opts == nil {
			return
		}
		opts.UseDefaultComponents = enabled
	}
}

// WithRouteModules 为 HTTP 服务追加外部路由模块。
func WithRouteModules(modules ...handler.RouteModule) Option {
	return func(opts *Options) {
		if opts == nil || len(modules) == 0 {
			return
		}
		opts.RouteModules = append(opts.RouteModules, modules...)
	}
}

// WithDisplayName 自定义启动日志中的应用展示名。
func WithDisplayName(name string) Option {
	return func(opts *Options) {
		if opts == nil || name == "" {
			return
		}
		opts.DisplayName = name
	}
}

// resolveOptions 把可选参数收敛成最终启动配置，并补齐默认值。
func resolveOptions(options []Option) Options {
	resolved := Options{
		DisplayName:          defaultDisplayName,
		UseDefaultComponents: true,
	}
	for _, option := range options {
		if option != nil {
			option(&resolved)
		}
	}
	if resolved.DisplayName == "" {
		resolved.DisplayName = defaultDisplayName
	}
	return resolved
}
