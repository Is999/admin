package collectorx

// RuntimeRegistrySpec 描述 Collector 提供的运行时注册入口。
type RuntimeRegistrySpec struct {
	Name        string // 注册名称，必须在运行时扩展清单中唯一
	File        string // 注册实现所在文件
	Method      string // 注册入口方法
	Description string // 注册项中文说明
}

const (
	// RuntimeRegistryCollectorProcessor 表示 Collector Processor 注册入口。
	RuntimeRegistryCollectorProcessor = "collector_processor"
)

// runtimeRegistrySpecs 是 Collector 运行时注册入口的清单源。
var runtimeRegistrySpecs = []RuntimeRegistrySpec{
	{
		Name:        RuntimeRegistryCollectorProcessor,
		File:        "internal/infra/collectorx/types.go + internal/infra/collectorx/manager.go",
		Method:      "collectorx.Manager.RegisterProcessor / RegisterProcessorFunc",
		Description: "按 bizType 注册批量消费 Processor",
	},
}

// RuntimeRegistrySpecs 返回 Collector 运行时注册入口规格快照。
func RuntimeRegistrySpecs() []RuntimeRegistrySpec {
	return append([]RuntimeRegistrySpec(nil), runtimeRegistrySpecs...)
}
