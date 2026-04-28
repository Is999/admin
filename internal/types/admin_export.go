//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"admin/pkg/excel"
	"strings"

	"github.com/Is999/go-utils/errors"
)

const (
	// AdminExportStatusQueued 表示导出任务已入队，等待后台 worker 执行。
	AdminExportStatusQueued = excel.StatusQueued
	// AdminExportStatusRunning 表示导出任务正在生成文件。
	AdminExportStatusRunning = excel.StatusRunning
	// AdminExportStatusSucceeded 表示导出任务已完成，可下载结果文件。
	AdminExportStatusSucceeded = excel.StatusSucceeded
	// AdminExportStatusFailed 表示导出任务执行失败。
	AdminExportStatusFailed = excel.StatusFailed
	// AdminExportTaskType 表示管理员列表导出的后台任务类型。
	AdminExportTaskType = "admin:export"
)

// AdminExportReq 表示管理员列表导出请求；筛选条件与列表页保持一致，但不携带分页参数。
type AdminExportReq struct {
	Username string `json:"username,optional" form:"username,optional"` // 登录用户名筛选
	RealName string `json:"realName,optional" form:"realName,optional"` // 真实姓名筛选
	Status   *int   `json:"status,optional" form:"status,optional"`     // 账号状态筛选：1 正常，0 禁用
	RoleID   *int   `json:"roleID,optional" form:"roleID,optional"`     // 角色 ID 筛选
}

// Validate 校验管理员列表导出请求。
func (r *AdminExportReq) Validate() error {
	r.Username = strings.TrimSpace(r.Username)
	r.RealName = strings.TrimSpace(r.RealName)
	if r.Status != nil && (*r.Status != 0 && *r.Status != 1) {
		return errors.Errorf("账号状态不合法")
	}
	return nil
}

// ToAdminListReq 把导出请求转换成管理员列表筛选请求，便于复用现有查询逻辑。
func (r *AdminExportReq) ToAdminListReq() *AdminListReq {
	if r == nil {
		return &AdminListReq{}
	}
	return &AdminListReq{
		Username: r.Username,
		RealName: r.RealName,
		Status:   r.Status,
		RoleID:   r.RoleID,
	}
}

// AdminExportJobReq 表示管理员导出任务状态/下载请求。
type AdminExportJobReq struct {
	JobID string `json:"jobId,optional" form:"jobId,optional" path:"jobId"` // 导出任务 ID
}

// Validate 校验管理员导出任务请求。
func (r *AdminExportJobReq) Validate() error {
	r.JobID = strings.TrimSpace(r.JobID)
	if r.JobID == "" {
		return errors.Errorf("jobId 不能为空")
	}
	return nil
}

// AdminExportTriggerResp 表示导出任务提交回执。
type AdminExportTriggerResp struct {
	JobID  string `json:"jobId"`  // 导出任务 ID
	TaskID string `json:"taskId"` // 后台队列任务 ID
	Queue  string `json:"queue"`  // 实际投递队列
	Status string `json:"status"` // 当前导出状态
}

// AdminExportTaskPayload 表示管理员导出后台任务负载。
type AdminExportTaskPayload struct {
	JobID        string         `json:"jobId"`                  // 导出任务 ID
	OperatorID   int            `json:"operatorId"`             // 发起导出的管理员 ID
	OperatorName string         `json:"operatorName,omitempty"` // 发起导出的管理员账号
	Request      AdminExportReq `json:"request"`                // 导出筛选条件快照
}

// AdminExportStatusResp 表示管理员导出任务的可轮询状态。
type AdminExportStatusResp struct {
	JobID              string `json:"jobId"`             // 导出任务 ID
	TaskID             string `json:"taskId"`            // 后台队列任务 ID
	Queue              string `json:"queue"`             // 队列名称
	Status             string `json:"status"`            // 导出状态
	Progress           int    `json:"progress"`          // 进度百分比：0-100
	Processed          int64  `json:"processed"`         // 已处理行数
	Total              int64  `json:"total"`             // 预估总行数
	EstimatedSeconds   int64  `json:"estimatedSeconds"`  // 剩余预估秒数
	FileName           string `json:"fileName"`          // 导出文件名
	DownloadReady      bool   `json:"downloadReady"`     // 是否已可下载
	ErrorMessage       string `json:"errorMessage"`      // 失败原因
	CreatedAt          string `json:"createdAt"`         // 创建时间
	StartedAt          string `json:"startedAt"`         // 开始执行时间
	FinishedAt         string `json:"finishedAt"`        // 完成时间
	UpdatedAt          string `json:"updatedAt"`         // 最近更新时间
	FilePath           string `json:"-"`                 // 服务器本地文件路径，仅后端下载处理使用
	ObjectKey          string `json:"-"`                 // 统一存储对象 key
	StorageType        string `json:"-"`                 // 统一存储类型
	ContentType        string `json:"-"`                 // 文件 MIME
	DownloadURL        string `json:"downloadUrl"`       // 下载地址
	ProcessAt          string `json:"processAt"`         // 计划执行时间
	LastCursorID       int    `json:"-"`                 // 最近处理到的管理员 ID，仅任务恢复与调试使用
	LastProcessedAt    string `json:"lastProcessedAt"`   // 最近一批进度更新时间
	AverageRowsPerSec  int64  `json:"averageRowsPerSec"` // 平均导出速度（行/秒）
	OperatorID         int    `json:"-"`                 // 发起导出的管理员 ID，仅后端做权限校验使用
	RequestFingerprint string `json:"-"`                 // 归一化导出条件指纹
	AuthorizedAdminIDs []int  `json:"-"`                 // 允许复用该导出结果的管理员 ID 列表
}
