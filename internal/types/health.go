package types

// HealthDependencyStatus 表示 ready 检查中的单个依赖状态。
type HealthDependencyStatus struct {
	Name    string `json:"name"`              // 依赖名称，如 mysql_admin、redis、task_queue
	Status  string `json:"status"`            // 依赖状态：ok/error/skipped
	Code    int    `json:"code,omitempty"`    // 依赖异常时对应的业务状态码
	Message string `json:"message,omitempty"` // 依赖异常或跳过时的中文说明
}

// HealthStatusResp 表示服务健康检查响应。
type HealthStatusResp struct {
	Status       string                   `json:"status"`       // 服务整体状态：ok/error
	Mode         string                   `json:"mode"`         // 当前进程启动模式：api/worker/scheduler/all
	Node         string                   `json:"node"`         // 当前节点名称或主机名
	Version      string                   `json:"version"`      // 当前配置版本指纹或构建版本
	Dependencies []HealthDependencyStatus `json:"dependencies"` // 依赖状态列表
}
