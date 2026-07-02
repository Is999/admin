package builtin

import (
	core "admin/internal/bootstrap/components"
	"admin/internal/bootstrap/register"
)

const (
	// NameCollector 表示通用收集器启动组件名称。
	NameCollector = "collector"
	// NameCDCConsumer 表示 Debezium CDC 消费启动组件名称。
	NameCDCConsumer = "cdc_consumer"
	// NameTaskRuntime 表示任务运行时启动组件名称。
	NameTaskRuntime = "task_runtime"
	// NameHTTPServer 表示 HTTP 服务启动组件名称。
	NameHTTPServer = "http_server"
)

// Spec 描述一个内置启动组件及其清单说明。
type Spec struct {
	register.Spec                       // 注册清单元信息
	Build         func() core.Component // 启动组件构造函数
}

// DefaultSpecs 返回项目内置启动组件规格，顺序即启动装配顺序。
func DefaultSpecs() []Spec {
	return []Spec{
		{
			Spec: register.Spec{
				Name:        NameCollector,
				File:        "internal/bootstrap/components/builtin/collector.go",
				Method:      "newCollector",
				Description: "注册通用收集器启动组件",
			},
			Build: newCollector,
		},
		{
			Spec: register.Spec{
				Name:        NameCDCConsumer,
				File:        "internal/bootstrap/components/builtin/cdc.go",
				Method:      "newCDCConsumer",
				Description: "注册 Debezium CDC 消费组件",
			},
			Build: newCDCConsumer,
		},
		{
			Spec: register.Spec{
				Name:        NameTaskRuntime,
				File:        "internal/bootstrap/components/builtin/task.go",
				Method:      "newTaskRuntime",
				Description: "注册任务队列管理器和任务插件运行时",
			},
			Build: newTaskRuntime,
		},
		{
			Spec: register.Spec{
				Name:        NameHTTPServer,
				File:        "internal/bootstrap/components/builtin/http.go",
				Method:      "newHTTPServer",
				Description: "注册 HTTP 服务和路由模块",
			},
			Build: newHTTPServer,
		},
	}
}

// DefaultComponents 从启动组件规格派生项目内置启动组件集合。
func DefaultComponents() []core.Component {
	specs := DefaultSpecs()
	components := make([]core.Component, 0, len(specs))
	for _, spec := range specs {
		if spec.Build == nil {
			continue
		}
		components = append(components, spec.Build())
	}
	return components
}

// ResolveStartup 合并内置组件与外部组件，保持外部组件在内置组件之后执行。
func ResolveStartup(useDefault bool, extra []core.Component) []core.Component {
	components := make([]core.Component, 0, len(extra)+len(DefaultSpecs()))
	if useDefault {
		components = append(components, DefaultComponents()...)
	}
	components = append(components, extra...)
	return components
}
