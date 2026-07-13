//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"
)

// AdminLogQueryReq 管理员日志查询请求，支持按 trace_id、用户和动作维度筛选。
type AdminLogQueryReq struct {
	TraceID     string `json:"traceID,optional" form:"traceID,optional"`     // Trace ID（可选）
	UserID      *int   `json:"userID,optional" form:"userID,optional"`       // 用户 ID（可选）
	UserName    string `json:"username,optional" form:"username,optional"`   // 用户名（可选）
	Action      string `json:"action,optional" form:"action,optional"`       // 动作（可选）
	StartTime   string `json:"startTime,optional" form:"startTime,optional"` // 起始时间（格式：YYYY-MM-DD HH:MM:SS）
	EndTime     string `json:"endTime,optional" form:"endTime,optional"`     // 结束时间（格式：YYYY-MM-DD HH:MM:SS）
	GetOrderReq        // 排序参数
	GetPageReq         // 分页参数
}

// Validate 校验管理员日志查询参数。
func (r *AdminLogQueryReq) Validate() error {
	r.TraceID = strings.TrimSpace(r.TraceID)
	r.UserName = strings.TrimSpace(r.UserName)
	r.Action = strings.TrimSpace(r.Action)
	r.StartTime = strings.TrimSpace(r.StartTime)
	r.EndTime = strings.TrimSpace(r.EndTime)
	if _, _, err := r.TimeRange(); err != nil {
		return errors.Tag(err)
	}
	if err := r.GetOrderReq.Validate(); err != nil {
		return errors.Tag(err)
	}
	return r.GetPageReq.Validate()
}

// TimeRange 解析管理员日志查询时间范围。
func (r *AdminLogQueryReq) TimeRange() (*time.Time, *time.Time, error) {
	startTime, err := parseOptionalDateTime(r.StartTime)
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	endTime, err := parseOptionalDateTime(r.EndTime)
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	if startTime != nil && endTime != nil && startTime.After(*endTime) {
		return nil, nil, errors.Errorf("结束时间不能早于开始时间")
	}
	return startTime, endTime, nil
}

// parseOptionalDateTime 解析可选的日期时间字符串。
// 为空时返回 nil，便于上层统一按“未传该筛选条件”处理。
func parseOptionalDateTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsedAt, err := time.ParseInLocation(time.DateTime, value, time.Local)
	if err != nil {
		return nil, errors.Errorf("时间格式错误，必须为 YYYY-MM-DD HH:MM:SS")
	}
	return &parsedAt, nil
}

// AdminLogItem 管理员日志项，新增链路和结果字段后，前端可以直接展示请求是否成功及关联 trace。
type AdminLogItem struct {
	ID           int    `json:"id"`           // 日志 ID
	UserID       int    `json:"userID"`       // 操作管理员 ID
	UserName     string `json:"username"`     // 管理员用户名
	Action       string `json:"action"`       // 操作类型/动作
	Route        string `json:"route"`        // 请求路由
	Method       string `json:"method"`       // 执行方法
	Describe     string `json:"describe"`     // 操作描述
	Data         string `json:"data"`         // 请求/响应数据（JSON）
	IP           string `json:"ip"`           // 客户端 IP
	Ipaddr       string `json:"ipaddr"`       // IP 归属地
	TraceID      string `json:"traceId"`      // 链路追踪 ID
	SpanID       string `json:"spanId"`       // 链路跨度 ID
	HTTPStatus   int    `json:"httpStatus"`   // HTTP 状态码
	BizCode      int    `json:"bizCode"`      // 业务状态码
	LatencyMS    int64  `json:"latencyMs"`    // 请求耗时
	Success      bool   `json:"success"`      // 是否成功
	ErrorMessage string `json:"errorMessage"` // 错误信息
	CreatedAt    string `json:"createdAt"`    // 创建时间（格式：YYYY-MM-DD HH:MM:SS）
}
