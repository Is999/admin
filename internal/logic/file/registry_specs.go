package file

// RuntimeRegistrySpec 描述文件业务提供的运行时注册入口。
type RuntimeRegistrySpec struct {
	Name        string // 注册名称，必须在运行时扩展清单中唯一
	File        string // 注册实现所在文件
	Method      string // 注册入口方法
	Description string // 注册项中文说明
}

const (
	// RuntimeRegistryFileUploadPolicy 表示文件上传策略注册入口。
	RuntimeRegistryFileUploadPolicy = "file_upload_policy"
)

// runtimeRegistrySpecs 是文件业务运行时注册入口的清单源。
var runtimeRegistrySpecs = []RuntimeRegistrySpec{
	{
		Name:        RuntimeRegistryFileUploadPolicy,
		File:        "internal/logic/file/file_transfer_policy.go",
		Method:      "file.RegisterFileUploadPolicy",
		Description: "注册文件上传业务策略",
	},
}

// RuntimeRegistrySpecs 返回文件业务运行时注册入口规格快照。
func RuntimeRegistrySpecs() []RuntimeRegistrySpec {
	return append([]RuntimeRegistrySpec(nil), runtimeRegistrySpecs...)
}
