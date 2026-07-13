//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"strings"

	"admin/pkg/excel"

	"github.com/Is999/go-utils/errors"
)

const (
	// UserExportStatusQueued 表示用户导出任务已入队。
	UserExportStatusQueued = excel.StatusQueued
	// UserExportStatusRunning 表示用户导出任务正在生成文件。
	UserExportStatusRunning = excel.StatusRunning
	// UserExportStatusSucceeded 表示用户导出任务已完成。
	UserExportStatusSucceeded = excel.StatusSucceeded
	// UserExportStatusFailed 表示用户导出任务执行失败。
	UserExportStatusFailed = excel.StatusFailed
	// UserExportTaskType 表示前台用户列表导出的后台任务类型。
	UserExportTaskType = "user:export"
	// UserExportCleanupTaskType 表示前台用户导出文件的延迟清理任务类型。
	UserExportCleanupTaskType = "user:export:cleanup"
)

// UserExportReq 表示前台用户列表导出请求；筛选条件与列表页保持一致，但不携带分页参数。
type UserExportReq struct {
	ID       int64  `json:"id,optional" form:"id,optional"`             // 用户 ID 精确筛选
	ShardNo  *int   `json:"shardNo,optional" form:"shardNo,optional"`   // ID 哈希分片筛选：0-1023
	Username string `json:"username,optional" form:"username,optional"` // 用户名前缀筛选
	Email    string `json:"email,optional" form:"email,optional"`       // 完整邮箱精确筛选
	Phone    string `json:"phone,optional" form:"phone,optional"`       // 完整手机号精确筛选
	Status   *int   `json:"status,optional" form:"status,optional"`     // 状态筛选：1 正常，0 禁用
}

// Validate 校验前台用户列表导出请求。
func (r *UserExportReq) Validate() error {
	r.Username = strings.TrimSpace(r.Username)
	r.Email = strings.TrimSpace(r.Email)
	r.Phone = strings.TrimSpace(r.Phone)
	if r.ID < 0 {
		return errors.Errorf("用户 ID 不合法")
	}
	if r.Status != nil && (*r.Status != 0 && *r.Status != 1) {
		return errors.Errorf("用户状态不合法")
	}
	if r.ShardNo != nil && (*r.ShardNo < 0 || *r.ShardNo >= userShardNoMod) {
		return errors.Errorf("用户分片号必须在0-%d之间", userShardNoMod-1)
	}
	return nil
}

// ToUserListReq 把导出请求转换成用户列表筛选请求，便于复用分表边界校验。
func (r *UserExportReq) ToUserListReq() *UserListReq {
	if r == nil {
		return &UserListReq{}
	}
	return &UserListReq{
		ID:       r.ID,
		ShardNo:  r.ShardNo,
		Username: r.Username,
		Email:    r.Email,
		Phone:    r.Phone,
		Status:   r.Status,
		GetPageReq: GetPageReq{
			Page:     1,
			PageSize: 1,
		},
	}
}

// UserExportJobReq 表示前台用户导出任务状态/下载请求。
type UserExportJobReq struct {
	JobID  string `json:"jobId,optional" form:"jobId,optional" path:"jobId"` // 导出任务 ID
	PartNo int    `json:"partNo,optional" form:"partNo,optional"`            // 下载文件编号；0 表示自动选择已就绪文件
}

// Validate 校验前台用户导出任务请求。
func (r *UserExportJobReq) Validate() error {
	r.JobID = strings.TrimSpace(r.JobID)
	if r.JobID == "" {
		return errors.Errorf("jobId 不能为空")
	}
	if r.PartNo < 0 {
		return errors.Errorf("partNo 不能小于 0")
	}
	return nil
}

// UserExportTriggerResp 表示前台用户导出任务提交回执。
type UserExportTriggerResp struct {
	JobID  string `json:"jobId"`  // 导出任务 ID
	TaskID string `json:"taskId"` // 后台队列任务 ID
	Queue  string `json:"queue"`  // 实际投递队列
	Status string `json:"status"` // 当前导出状态
}

// UserExportTaskPayload 表示前台用户导出后台任务负载。
type UserExportTaskPayload struct {
	JobID        string        `json:"jobId"`                  // 导出任务 ID
	OperatorID   int           `json:"operatorId"`             // 发起导出的管理员 ID
	OperatorName string        `json:"operatorName,omitempty"` // 发起导出的管理员账号
	Request      UserExportReq `json:"request"`                // 导出筛选条件快照
}

// UserExportCleanupTaskPayload 表示前台用户导出文件延迟清理任务负载。
type UserExportCleanupTaskPayload struct {
	JobID      string   `json:"jobId"`      // 导出任务 ID
	ObjectKeys []string `json:"objectKeys"` // 需要删除的统一存储对象 key
}

// UserExportFileItem 表示前台用户导出的单个文件分片。
type UserExportFileItem struct {
	PartNo        int    `json:"partNo"`        // 文件编号，从 1 开始递增
	FileName      string `json:"fileName"`      // 下载文件名
	RowCount      int64  `json:"rowCount"`      // 当前文件数据行数，不含表头
	ProcessedFrom int64  `json:"processedFrom"` // 当前文件覆盖的全局起始行序号
	ProcessedTo   int64  `json:"processedTo"`   // 当前文件覆盖的全局结束行序号
	FileSize      int64  `json:"fileSize"`      // 文件字节数
	DownloadReady bool   `json:"downloadReady"` // 是否已可下载
	DownloadURL   string `json:"downloadUrl"`   // 当前分片下载地址
	CreatedAt     string `json:"createdAt"`     // 文件生成时间
	FilePath      string `json:"-"`             // 服务器本地文件路径，仅后端下载处理使用
	ObjectKey     string `json:"-"`             // 统一存储对象 key
	StorageType   string `json:"-"`             // 统一存储类型
	ContentType   string `json:"-"`             // 文件 MIME
}

// UserExportStatusResp 表示前台用户导出任务的可轮询状态。
type UserExportStatusResp struct {
	JobID              string               `json:"jobId"`             // 导出任务 ID
	TaskID             string               `json:"taskId"`            // 后台队列任务 ID
	Queue              string               `json:"queue"`             // 队列名称
	Status             string               `json:"status"`            // 导出状态
	Progress           int                  `json:"progress"`          // 进度百分比：0-100
	Processed          int64                `json:"processed"`         // 已处理行数
	Total              int64                `json:"total"`             // 预估总行数
	EstimatedSeconds   int64                `json:"estimatedSeconds"`  // 剩余预估秒数
	FileName           string               `json:"fileName"`          // 导出文件名
	DownloadReady      bool                 `json:"downloadReady"`     // 是否已可下载
	Files              []UserExportFileItem `json:"files"`             // 已生成的导出文件分片，按 partNo 升序排列
	PartCount          int                  `json:"partCount"`         // 已生成文件分片数量
	SplitRows          int64                `json:"splitRows"`         // 单文件拆分阈值
	ErrorMessage       string               `json:"errorMessage"`      // 失败原因
	CreatedAt          string               `json:"createdAt"`         // 创建时间
	StartedAt          string               `json:"startedAt"`         // 开始执行时间
	FinishedAt         string               `json:"finishedAt"`        // 完成时间
	UpdatedAt          string               `json:"updatedAt"`         // 最近更新时间
	FilePath           string               `json:"-"`                 // 服务器本地文件路径，仅后端下载处理使用
	ObjectKey          string               `json:"-"`                 // 统一存储对象 key
	StorageType        string               `json:"-"`                 // 统一存储类型
	ContentType        string               `json:"-"`                 // 文件 MIME
	DownloadURL        string               `json:"downloadUrl"`       // 下载地址
	ProcessAt          string               `json:"processAt"`         // 计划执行时间
	LastCursorID       int64                `json:"-"`                 // 最近处理到的用户 ID，仅任务恢复与调试使用
	LastProcessedAt    string               `json:"lastProcessedAt"`   // 最近一批进度更新时间
	AverageRowsPerSec  int64                `json:"averageRowsPerSec"` // 平均导出速度（行/秒）
	OperatorID         int                  `json:"-"`                 // 发起导出的管理员 ID，仅后端做权限校验使用
	RequestFingerprint string               `json:"-"`                 // 归一化导出条件指纹
	AuthorizedAdminIDs []int                `json:"-"`                 // 允许复用该导出结果的管理员 ID 列表
}
