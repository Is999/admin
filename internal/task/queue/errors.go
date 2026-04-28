package taskqueue

import "github.com/Is999/go-utils/errors"

var (
	// ErrTaskQueueDisabled 表示当前服务未启用任务系统。
	ErrTaskQueueDisabled = errors.New("任务队列未启用")
	// ErrTaskTypeNotFound 表示通用任务投递时指定的任务类型未注册。
	ErrTaskTypeNotFound = errors.New("任务类型未注册")
	// ErrWorkflowNotFound 表示指定工作流定义不存在。
	ErrWorkflowNotFound = errors.New("工作流定义不存在")
	// ErrWorkflowAlreadyExists 表示同一个工作流实例已存在。
	ErrWorkflowAlreadyExists = errors.New("工作流已存在")
)
