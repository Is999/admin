package storage

// RuntimeRegistrySpec 描述 storage 包提供的运行时注册入口。
type RuntimeRegistrySpec struct {
	Name        string // 注册名称，必须在运行时扩展清单中唯一
	File        string // 注册实现所在文件
	Method      string // 注册入口方法
	Description string // 注册项中文说明
}

const (
	// RuntimeRegistryObjectStorage 表示对象存储实现注册入口。
	RuntimeRegistryObjectStorage = "object_storage"
	// RuntimeRegistryVirusScanner 表示病毒扫描器注册入口。
	RuntimeRegistryVirusScanner = "virus_scanner"
)

// runtimeRegistrySpecs 是 storage 包运行时注册入口的清单源。
var runtimeRegistrySpecs = []RuntimeRegistrySpec{
	{
		Name:        RuntimeRegistryObjectStorage,
		File:        "pkg/storage/storage.go",
		Method:      "storage.RegisterObjectStorage",
		Description: "注册 local / s3 对象存储实现",
	},
	{
		Name:        RuntimeRegistryVirusScanner,
		File:        "pkg/storage/storage.go",
		Method:      "storage.RegisterVirusScanner",
		Description: "注册上传完成后的病毒扫描器",
	},
}

// RuntimeRegistrySpecs 返回 storage 包运行时注册入口规格快照。
func RuntimeRegistrySpecs() []RuntimeRegistrySpec {
	return append([]RuntimeRegistrySpec(nil), runtimeRegistrySpecs...)
}
