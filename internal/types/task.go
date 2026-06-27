//lint:file-ignore SA5008 ignore go-zero optional tag

package types

import (
	"encoding/json"
	"strings"
	"time"

	"admin/internal/task/stats"

	"github.com/Is999/go-utils/errors"
)

// TriggerTaskWorkflowReq 手动触发工作流请求。
// 该接口既支持立即执行，也支持延迟/定时触发。
type TriggerTaskWorkflowReq struct {
	Name             string   `json:"name"`                      // 工作流名称
	Targets          []string `json:"targets,optional"`          // 执行目标列表（如缓存键列表）
	Queue            string   `json:"queue,optional"`            // 指定投递队列，未填时使用工作流默认队列
	ShardTotal       int      `json:"shardTotal,optional"`       // 分片总数，>1 时会把首个可分片节点拆成多个实例
	GrayPercent      int      `json:"grayPercent,optional"`      // 灰度比例，1~100，默认 100
	UniqueKey        string   `json:"uniqueKey,optional"`        // 去重键，相同 key + TTL 内避免重复触发
	UniqueTTLSeconds *int     `json:"uniqueTTLSeconds,optional"` // 去重 TTL（秒）
	Retry            *int     `json:"retry,optional"`            // 覆盖默认重试次数
	TimeoutSeconds   *int     `json:"timeoutSeconds,optional"`   // 覆盖默认超时时间（秒）
	ProcessAt        string   `json:"processAt,optional"`        // 指定触发时间（RFC3339）
	ProcessInSeconds *int     `json:"processInSeconds,optional"` // 相对当前时间延迟执行（秒）
	Deadline         string   `json:"deadline,optional"`         // 任务截止时间（RFC3339）
}

// Validate 校验并补齐手动触发工作流请求。
func (r *TriggerTaskWorkflowReq) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.Errorf("工作流名称不能为空")
	}
	if r.ShardTotal <= 0 {
		r.ShardTotal = 1
	}
	if r.GrayPercent <= 0 {
		r.GrayPercent = 100
	}
	if r.GrayPercent > 100 {
		r.GrayPercent = 100
	}
	for i := range r.Targets {
		r.Targets[i] = strings.TrimSpace(r.Targets[i])
	}
	if r.ProcessAt != "" {
		if _, err := time.Parse(time.RFC3339, r.ProcessAt); err != nil {
			return errors.Errorf("processAt 必须为 RFC3339 时间格式")
		}
	}
	if r.Deadline != "" {
		if _, err := time.Parse(time.RFC3339, r.Deadline); err != nil {
			return errors.Errorf("deadline 必须为 RFC3339 时间格式")
		}
	}
	if r.ProcessInSeconds != nil && *r.ProcessInSeconds < 0 {
		return errors.Errorf("processInSeconds 不能小于 0")
	}
	if r.UniqueTTLSeconds != nil && *r.UniqueTTLSeconds <= 0 {
		return errors.Errorf("uniqueTTLSeconds 必须大于 0")
	}
	if r.TimeoutSeconds != nil && *r.TimeoutSeconds <= 0 {
		return errors.Errorf("timeoutSeconds 必须大于 0")
	}
	if r.Retry != nil && *r.Retry < 0 {
		return errors.Errorf("retry 不能小于 0")
	}
	return nil
}

// GetTaskWorkflowReq 查询工作流状态请求。
type GetTaskWorkflowReq struct {
	WorkflowID string `form:"workflowId,optional" json:"workflowId,optional" path:"workflowId"` // 工作流实例 ID
}

// Validate 校验工作流状态查询请求。
func (r *GetTaskWorkflowReq) Validate() error {
	if strings.TrimSpace(r.WorkflowID) == "" {
		return errors.Errorf("workflowId 不能为空")
	}
	return nil
}

// OperateTaskQueueReq 队列暂停/恢复请求。
type OperateTaskQueueReq struct {
	Queue string `json:"queue,optional" form:"queue,optional" path:"queue"` // 队列名称
}

// Validate 校验队列控制请求。
func (r *OperateTaskQueueReq) Validate() error {
	if strings.TrimSpace(r.Queue) == "" {
		return errors.Errorf("队列名称不能为空")
	}
	return nil
}

// GetTaskInfoReq 表示任务详情查询请求。
type GetTaskInfoReq struct {
	Queue  string `json:"queue,optional" form:"queue"`                          // 队列名称
	TaskID string `json:"taskId,optional" form:"taskId,optional" path:"taskId"` // 任务 ID
}

// Validate 校验任务详情查询请求。
func (r *GetTaskInfoReq) Validate() error {
	if strings.TrimSpace(r.Queue) == "" {
		return errors.Errorf("队列名称不能为空")
	}
	if strings.TrimSpace(r.TaskID) == "" {
		return errors.Errorf("taskId 不能为空")
	}
	return nil
}

// ListTaskItemsReq 表示任务列表查询请求。
type ListTaskItemsReq struct {
	Queue      string `json:"queue,optional" form:"queue"`                    // 队列名称
	State      string `json:"state,optional" form:"state"`                    // 任务状态：pending/active/scheduled/retry/archived/completed/aggregating
	Group      string `json:"group,optional" form:"group,optional"`           // 聚合组名称，仅 aggregating 状态使用
	TaskID     string `json:"taskId,optional" form:"taskId,optional"`         // 任务 ID 关键字；填写后按任务 ID 片段筛选
	WorkflowID string `json:"workflowId,optional" form:"workflowId,optional"` // 工作流实例 ID；填写后仅返回该链路下的任务
	TaskName   string `json:"taskName,optional" form:"taskName,optional"`     // 任务名称关键字；支持按周期任务名或展示名包含匹配
	StartTime  string `json:"startTime,optional" form:"startTime,optional"`   // 任务活动时间开始，RFC3339；scheduled 按 nextProcessAt 过滤
	EndTime    string `json:"endTime,optional" form:"endTime,optional"`       // 任务活动时间结束，RFC3339；scheduled 按 nextProcessAt 过滤
	Page       int    `json:"page,optional" form:"page,optional"`             // 页码，从 1 开始
	PageSize   int    `json:"pageSize,optional" form:"pageSize,optional"`     // 每页条数
}

// Validate 校验任务列表查询请求。
func (r *ListTaskItemsReq) Validate() error {
	if strings.TrimSpace(r.Queue) == "" {
		return errors.Errorf("队列名称不能为空")
	}
	state := strings.TrimSpace(r.State)
	switch state {
	case "pending", "active", "scheduled", "retry", "archived", "completed", "aggregating":
	default:
		return errors.Errorf("state 非法")
	}
	if state == "aggregating" && strings.TrimSpace(r.Group) == "" {
		return errors.Errorf("aggregating 状态必须提供 group")
	}
	if len([]rune(strings.TrimSpace(r.TaskID))) > 128 {
		return errors.Errorf("taskId 不能超过 128 个字符")
	}
	if len([]rune(strings.TrimSpace(r.TaskName))) > 128 {
		return errors.Errorf("taskName 不能超过 128 个字符")
	}
	if err := validateTaskListTimeRange(r.StartTime, r.EndTime); err != nil {
		return errors.Tag(err)
	}
	r.Page, r.PageSize = normalizePage(r.Page, r.PageSize, 20)
	return nil
}

// ListTaskItemsOverviewReq 表示任务列表总览查询请求。
// 该接口用于管理后台“任务列表”页首屏和模糊排障场景，支持按需精确查询，也支持受控聚合查询。
type ListTaskItemsOverviewReq struct {
	Queue              string `json:"queue,optional" form:"queue,optional"`                  // 队列名称；为空时按全部可见队列聚合
	State              string `json:"state,optional" form:"state,optional"`                  // 指定任务状态；为空时聚合常用状态并按任务时间倒序返回
	Group              string `json:"group,optional" form:"group,optional"`                  // 聚合组名称，仅 aggregating 使用
	TaskID             string `json:"taskId,optional" form:"taskId,optional"`                // 任务 ID 关键字；填写后按任务 ID 片段筛选
	WorkflowID         string `json:"workflowId,optional" form:"workflowId,optional"`        // 工作流实例 ID；填写后仅返回该链路下的任务
	TaskName           string `json:"taskName,optional" form:"taskName,optional"`            // 任务名称关键字；支持按周期任务名或展示名包含匹配
	StartTime          string `json:"startTime,optional" form:"startTime,optional"`          // 任务活动时间开始，RFC3339；scheduled 按 nextProcessAt 过滤
	EndTime            string `json:"endTime,optional" form:"endTime,optional"`              // 任务活动时间结束，RFC3339；scheduled 按 nextProcessAt 过滤
	Page               int    `json:"page,optional" form:"page,optional"`                    // 页码，从 1 开始
	PageSize           int    `json:"pageSize,optional" form:"pageSize,optional"`            // 每页条数
	IncludeAggregating bool   `json:"includeAggregating,optional" form:"includeAggregating"` // 状态为空时，是否把 aggregating 纳入聚合范围
}

// Validate 校验任务总览查询请求。
func (r *ListTaskItemsOverviewReq) Validate() error {
	state := strings.TrimSpace(r.State)
	switch state {
	case "", "pending", "active", "scheduled", "retry", "archived", "completed", "aggregating":
	default:
		return errors.Errorf("state 非法")
	}
	if state == "aggregating" && strings.TrimSpace(r.Group) == "" {
		return errors.Errorf("aggregating 状态必须提供 group")
	}
	if len([]rune(strings.TrimSpace(r.TaskID))) > 128 {
		return errors.Errorf("taskId 不能超过 128 个字符")
	}
	if len([]rune(strings.TrimSpace(r.TaskName))) > 128 {
		return errors.Errorf("taskName 不能超过 128 个字符")
	}
	if err := validateTaskListTimeRange(r.StartTime, r.EndTime); err != nil {
		return errors.Tag(err)
	}
	r.Page, r.PageSize = normalizePage(r.Page, r.PageSize, 20)
	return nil
}

// validateTaskListTimeRange 校验任务列表时间范围，避免后端扫描时才发现非法输入。
func validateTaskListTimeRange(start, end string) error {
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	var startTime time.Time
	var endTime time.Time
	if start != "" {
		parsed, err := time.Parse(time.RFC3339, start)
		if err != nil {
			return errors.Errorf("startTime 必须为 RFC3339 时间")
		}
		startTime = parsed
	}
	if end != "" {
		parsed, err := time.Parse(time.RFC3339, end)
		if err != nil {
			return errors.Errorf("endTime 必须为 RFC3339 时间")
		}
		endTime = parsed
	}
	if !startTime.IsZero() && !endTime.IsZero() && startTime.After(endTime) {
		return errors.Errorf("startTime 不能晚于 endTime")
	}
	return nil
}

// OperateTaskReq 表示单任务立即执行或删除请求。
type OperateTaskReq struct {
	Queue  string `json:"queue,optional" form:"queue,optional"`                 // 队列名称
	TaskID string `json:"taskId,optional" form:"taskId,optional" path:"taskId"` // 任务 ID
}

// Validate 校验单任务操作请求。
func (r *OperateTaskReq) Validate() error {
	if strings.TrimSpace(r.Queue) == "" {
		return errors.Errorf("队列名称不能为空")
	}
	if strings.TrimSpace(r.TaskID) == "" {
		return errors.Errorf("taskId 不能为空")
	}
	return nil
}

// EnqueueTaskReq 表示后台通用任务投递请求。
// 该接口面向已注册的任务类型，适合运维补投、手工补偿和轻量级管理操作。
type EnqueueTaskReq struct {
	TaskType         string          `json:"taskType"`                  // 已注册的任务类型
	Payload          json.RawMessage `json:"payload"`                   // 任务负载 JSON
	Queue            string          `json:"queue,optional"`            // 指定队列
	Group            string          `json:"group,optional"`            // 聚合分组名称
	Retry            *int            `json:"retry,optional"`            // 覆盖最大重试次数
	TimeoutSeconds   *int            `json:"timeoutSeconds,optional"`   // 超时时间（秒）
	ProcessAt        string          `json:"processAt,optional"`        // 绝对执行时间（RFC3339）
	ProcessInSeconds *int            `json:"processInSeconds,optional"` // 相对延迟执行时间（秒）
	Deadline         string          `json:"deadline,optional"`         // 截止时间（RFC3339）
	UniqueTTLSeconds *int            `json:"uniqueTTLSeconds,optional"` // 去重窗口（秒）
}

// Validate 校验通用任务投递请求。
func (r *EnqueueTaskReq) Validate() error {
	if strings.TrimSpace(r.TaskType) == "" {
		return errors.Errorf("taskType 不能为空")
	}
	if len(r.Payload) == 0 {
		return errors.Errorf("payload 不能为空")
	}
	if !json.Valid(r.Payload) {
		return errors.Errorf("payload 必须为合法 JSON")
	}
	if r.ProcessAt != "" {
		if _, err := time.Parse(time.RFC3339, r.ProcessAt); err != nil {
			return errors.Errorf("processAt 必须为 RFC3339 时间格式")
		}
	}
	if r.Deadline != "" {
		if _, err := time.Parse(time.RFC3339, r.Deadline); err != nil {
			return errors.Errorf("deadline 必须为 RFC3339 时间格式")
		}
	}
	if r.ProcessInSeconds != nil && *r.ProcessInSeconds < 0 {
		return errors.Errorf("processInSeconds 不能小于 0")
	}
	if r.ProcessAt != "" && r.ProcessInSeconds != nil && *r.ProcessInSeconds > 0 {
		return errors.Errorf("processAt 和 processInSeconds 不能同时设置")
	}
	if r.UniqueTTLSeconds != nil && *r.UniqueTTLSeconds <= 0 {
		return errors.Errorf("uniqueTTLSeconds 必须大于 0")
	}
	if r.TimeoutSeconds != nil && *r.TimeoutSeconds <= 0 {
		return errors.Errorf("timeoutSeconds 必须大于 0")
	}
	if r.Retry != nil && *r.Retry < 0 {
		return errors.Errorf("retry 不能小于 0")
	}
	return nil
}

// TaskConfigItemQueryReq 表示配置热加载运行态配置项查询请求。
// keyword 只匹配配置路径、类型和已脱敏展示值，避免通过搜索反推出原始敏感值。
type TaskConfigItemQueryReq struct {
	Keyword       string `json:"keyword,optional" form:"keyword,optional"`             // 配置路径或展示值关键字
	SensitiveOnly bool   `json:"sensitiveOnly,optional" form:"sensitiveOnly,optional"` // 是否只返回已脱敏配置项
	Page          int    `json:"page,optional" form:"page,optional"`                   // 页码，从 1 开始
	PageSize      int    `json:"pageSize,optional" form:"pageSize,optional"`           // 每页条数，最大 100
}

// Validate 校验并归一化配置项查询请求。
func (r *TaskConfigItemQueryReq) Validate() error {
	if r == nil {
		return errors.Errorf("配置项查询请求不能为空")
	}
	r.Keyword = strings.TrimSpace(r.Keyword)
	if len([]rune(r.Keyword)) > 128 {
		return errors.Errorf("keyword 不能超过 128 个字符")
	}
	r.Page, r.PageSize = normalizePage(r.Page, r.PageSize, 20)
	return nil
}

// TaskWorkflowTriggerResp 手动触发工作流后的回执。
type TaskWorkflowTriggerResp struct {
	TaskID       string `json:"taskId"`              // 触发任务 ID（对应 workflow:trigger 任务）
	WorkflowID   string `json:"workflowId"`          // 预先分配的工作流实例 ID
	WorkflowName string `json:"workflowName"`        // 工作流名称
	Queue        string `json:"queue"`               // 投递队列
	ProcessAt    string `json:"processAt,omitempty"` // 计划执行时间
}

// TaskEnqueueResp 表示通用任务投递后的回执。
type TaskEnqueueResp struct {
	TaskID    string `json:"taskId"`              // Asynq 分配的任务 ID
	TaskType  string `json:"taskType"`            // 任务类型
	Queue     string `json:"queue"`               // 实际投递队列
	ProcessAt string `json:"processAt,omitempty"` // 计划执行时间
}

// TaskItem 表示队列中单个任务的状态快照。
type TaskItem struct {
	ID             string              `json:"id"`                       // 任务 ID
	Queue          string              `json:"queue"`                    // 所属队列
	TaskType       string              `json:"taskType"`                 // 任务类型
	TaskName       string              `json:"taskName,omitempty"`       // 任务名称/脚本名称
	WorkflowID     string              `json:"workflowId,omitempty"`     // 关联工作流实例 ID
	State          string              `json:"state"`                    // 当前状态
	Group          string              `json:"group,omitempty"`          // 聚合分组
	MaxRetry       int                 `json:"maxRetry"`                 // 最大重试次数
	Retried        int                 `json:"retried"`                  // 已重试次数
	LastErr        string              `json:"lastErr,omitempty"`        // 最后一次错误
	TimeoutSec     int64               `json:"timeoutSec"`               // 超时时间（秒）
	StartedAt      string              `json:"startedAt,omitempty"`      // 开始执行时间
	DurationMS     int64               `json:"durationMs,omitempty"`     // 执行耗时（毫秒）；执行中任务表示已运行时长
	Deadline       string              `json:"deadline,omitempty"`       // 截止时间
	NextProcessAt  string              `json:"nextProcessAt,omitempty"`  // 下次计划执行时间
	CompletedAt    string              `json:"completedAt,omitempty"`    // 完成时间
	LastFailedAt   string              `json:"lastFailedAt,omitempty"`   // 最近失败时间
	IsOrphaned     bool                `json:"isOrphaned"`               // Active 任务是否已孤儿化
	Headers        map[string]string   `json:"headers,omitempty"`        // 任务头信息
	Payload        json.RawMessage     `json:"payload"`                  // 原始任务负载
	Result         json.RawMessage     `json:"result,omitempty"`         // 任务结果
	ExecutionTrace *taskstats.Snapshot `json:"executionTrace,omitempty"` // 本次任务处理量统计摘要
}

// TaskListResp 表示任务列表响应。
type TaskListResp struct {
	Queue      string     `json:"queue"`                // 查询队列
	State      string     `json:"state"`                // 查询状态
	Group      string     `json:"group,omitempty"`      // 聚合分组
	TaskID     string     `json:"taskId,omitempty"`     // 任务 ID 筛选条件
	WorkflowID string     `json:"workflowId,omitempty"` // 工作流实例 ID 筛选条件
	TaskName   string     `json:"taskName,omitempty"`   // 任务名称关键字筛选条件
	StartTime  string     `json:"startTime,omitempty"`  // 时间范围开始
	EndTime    string     `json:"endTime,omitempty"`    // 时间范围结束
	Page       int        `json:"page"`                 // 当前页码
	PageSize   int        `json:"pageSize"`             // 当前页大小
	Total      int64      `json:"total"`                // 该状态任务总数
	Tasks      []TaskItem `json:"tasks"`                // 任务列表
}

// TaskListOverviewResp 表示任务总览查询响应。
type TaskListOverviewResp struct {
	Queue          string           `json:"queue,omitempty"`          // 当前筛选队列；为空表示聚合全部可见队列
	State          string           `json:"state,omitempty"`          // 前端主动指定的状态；为空表示本次为多状态聚合
	EffectiveState string           `json:"effectiveState,omitempty"` // 指定状态时本次真正命中的任务状态
	Group          string           `json:"group,omitempty"`          // 聚合分组
	TaskID         string           `json:"taskId,omitempty"`         // 任务 ID 筛选条件
	WorkflowID     string           `json:"workflowId,omitempty"`     // 工作流实例 ID 筛选条件
	TaskName       string           `json:"taskName,omitempty"`       // 任务名称关键字筛选条件
	StartTime      string           `json:"startTime,omitempty"`      // 时间范围开始
	EndTime        string           `json:"endTime,omitempty"`        // 时间范围结束
	Page           int              `json:"page"`                     // 当前页码
	PageSize       int              `json:"pageSize"`                 // 当前页大小
	Total          int64            `json:"total"`                    // 当前结果总数
	AggregateMode  bool             `json:"aggregateMode"`            // 是否为多队列聚合视图
	Queues         []string         `json:"queues,omitempty"`         // 本次实际参与查询的队列列表
	StateTotals    map[string]int64 `json:"stateTotals"`              // 各任务状态的队列级总数；用于前端展示可切换状态入口
	Tasks          []TaskItem       `json:"tasks"`                    // 当前页任务列表
}

// TaskTypeRegistryItem 表示一个已注册的任务类型。
type TaskTypeRegistryItem struct {
	TaskType          string `json:"taskType"`                 // 任务类型标识
	Description       string `json:"description,omitempty"`    // 任务类型中文说明
	UsageHint         string `json:"usageHint,omitempty"`      // 使用提示
	PayloadExample    string `json:"payloadExample,omitempty"` // 推荐负载 JSON 示例
	ManualRecommended bool   `json:"manualRecommended"`        // 是否推荐人工手动投递
}

// TaskTypeRegistryResp 表示任务类型注册清单响应。
type TaskTypeRegistryResp struct {
	Items []TaskTypeRegistryItem `json:"items"` // 已注册任务类型列表
}

// WorkflowRegistryItem 表示一个已注册的工作流定义。
type WorkflowRegistryItem struct {
	Name           string `json:"name"`                     // 工作流名称
	Description    string `json:"description"`              // 工作流说明
	DefaultQueue   string `json:"defaultQueue"`             // 默认执行队列
	NodeCount      int    `json:"nodeCount"`                // 节点数量
	UsageHint      string `json:"usageHint,omitempty"`      // 使用提示
	TargetsExample string `json:"targetsExample,omitempty"` // 执行目标填写示例
}

// WorkflowRegistryResp 表示工作流注册清单响应。
type WorkflowRegistryResp struct {
	Items []WorkflowRegistryItem `json:"items"` // 已注册工作流列表
}

// TaskWorkflowNodeItem 表示工作流中单个节点的执行状态。
type TaskWorkflowNodeItem struct {
	Name           string                       `json:"name"`                     // 节点名称
	TaskType       string                       `json:"taskType"`                 // 实际执行的任务类型
	Queue          string                       `json:"queue"`                    // 节点执行队列
	Status         string                       `json:"status"`                   // 节点状态：pending/running/success/failed/skipped
	DependsOn      []string                     `json:"dependsOn,omitempty"`      // 依赖节点列表
	Expected       int                          `json:"expected"`                 // 期望执行实例数（含分片）
	Succeeded      int                          `json:"succeeded"`                // 已成功实例数
	Failed         int                          `json:"failed"`                   // 已失败实例数
	Skipped        int                          `json:"skipped"`                  // 被灰度跳过的实例数
	ErrorMessage   string                       `json:"errorMessage,omitempty"`   // 节点失败信息
	StartedAt      string                       `json:"startedAt,omitempty"`      // 节点开始执行时间
	FinishedAt     string                       `json:"finishedAt,omitempty"`     // 节点完成时间
	DurationMS     int64                        `json:"durationMs,omitempty"`     // 节点执行耗时（毫秒）；运行中节点表示已运行时长
	Progress       *taskstats.Progress          `json:"progress,omitempty"`       // 节点实例执行进度
	ExecutionTrace *taskstats.Snapshot          `json:"executionTrace,omitempty"` // 节点处理量聚合摘要
	ShardTraces    []TaskWorkflowShardTraceItem `json:"shardTraces,omitempty"`    // 节点分片处理量明细
}

// TaskWorkflowShardTraceItem 表示工作流节点中单个分片的处理量快照。
type TaskWorkflowShardTraceItem struct {
	ShardIndex     int                 `json:"shardIndex"`               // 分片下标
	ShardTotal     int                 `json:"shardTotal"`               // 分片总数
	Status         string              `json:"status,omitempty"`         // 分片状态
	Progress       *taskstats.Progress `json:"progress,omitempty"`       // 分片执行进度
	ExecutionTrace *taskstats.Snapshot `json:"executionTrace,omitempty"` // 分片处理量摘要
}

// TaskWorkflowStatusResp 表示工作流实例的整体状态。
type TaskWorkflowStatusResp struct {
	WorkflowID     string                 `json:"workflowId"`               // 工作流实例 ID
	WorkflowName   string                 `json:"workflowName"`             // 工作流名称
	Status         string                 `json:"status"`                   // 工作流状态：pending/running/success/failed
	Source         string                 `json:"source"`                   // 触发来源：api/periodic/internal
	Queue          string                 `json:"queue"`                    // 默认执行队列
	Targets        []string               `json:"targets,omitempty"`        // 执行目标列表
	ShardTotal     int                    `json:"shardTotal"`               // 分片总数
	GrayPercent    int                    `json:"grayPercent"`              // 灰度比例
	ErrorMessage   string                 `json:"errorMessage,omitempty"`   // 工作流失败原因
	CreatedAt      string                 `json:"createdAt"`                // 创建时间
	UpdatedAt      string                 `json:"updatedAt"`                // 最近更新时间
	FinishedAt     string                 `json:"finishedAt,omitempty"`     // 完成时间
	DurationMS     int64                  `json:"durationMs,omitempty"`     // 工作流总耗时（毫秒）；执行中工作流表示已运行时长
	Progress       *taskstats.Progress    `json:"progress,omitempty"`       // 工作流执行进度
	ExecutionTrace *taskstats.Snapshot    `json:"executionTrace,omitempty"` // 工作流处理量聚合摘要
	Nodes          []TaskWorkflowNodeItem `json:"nodes"`                    // 节点明细
}

// TaskQueueItem 表示单个队列的当前快照。
type TaskQueueItem struct {
	Name        string `json:"name"`        // 队列名称
	Paused      bool   `json:"paused"`      // 是否已暂停
	Size        int    `json:"size"`        // 队列总任务数
	Pending     int    `json:"pending"`     // 待执行任务数
	Active      int    `json:"active"`      // 执行中任务数
	Scheduled   int    `json:"scheduled"`   // 定时任务数
	Retry       int    `json:"retry"`       // 重试队列任务数
	Archived    int    `json:"archived"`    // 归档任务数
	Completed   int    `json:"completed"`   // 已完成保留任务数
	Aggregating int    `json:"aggregating"` // 聚合中的任务数
	Processed   int    `json:"processed"`   // 当日已处理任务数
	Failed      int    `json:"failed"`      // 当日失败任务数
	LatencyMS   int64  `json:"latencyMs"`   // 队列延迟（毫秒）
	MemoryUsage int64  `json:"memoryUsage"` // 预估内存占用（字节）
}

// TaskServerItem 表示当前在线 worker 节点快照。
type TaskServerItem struct {
	ID             string         `json:"id"`                  // Worker 实例 ID
	Host           string         `json:"host"`                // Worker 所在主机
	PID            int            `json:"pid"`                 // Worker 进程 ID
	Status         string         `json:"status"`              // Worker 状态
	Concurrency    int            `json:"concurrency"`         // 并发度
	StrictPriority bool           `json:"strictPriority"`      // 是否启用严格优先级
	Queues         map[string]int `json:"queues,omitempty"`    // 队列权重配置
	StartedAt      string         `json:"startedAt,omitempty"` // 启动时间
}

// TaskQueueListResp 表示任务队列与在线 worker 概览。
type TaskQueueListResp struct {
	Queues    []TaskQueueItem    `json:"queues"`              // 队列快照列表
	Servers   []TaskServerItem   `json:"servers,omitempty"`   // 在线 worker 节点列表
	Scheduler *TaskSchedulerItem `json:"scheduler,omitempty"` // 周期调度器运行状态
}

// TaskConfigReloadStatusResp 表示 config.yaml 热加载运行状态。
// 该状态描述“运行期配置快照”是否已更新，不表示数据库、Redis、Kafka 等基础设施连接已在线重建。
type TaskConfigReloadStatusResp struct {
	Enabled                bool   `json:"enabled"`                 // 是否启用配置热加载
	Watching               bool   `json:"watching"`                // 当前是否正在后台监听配置文件
	ConfigFile             string `json:"configFile"`              // 当前监听的配置文件路径
	CheckIntervalSeconds   int    `json:"checkIntervalSeconds"`    // 当前轮询间隔，单位秒
	ConfigVersion          string `json:"configVersion"`           // 当前生效配置版本指纹
	ConfigSummary          string `json:"configSummary"`           // 当前关键配置摘要
	RestartRequired        bool   `json:"restartRequired"`         // 本次热加载后是否仍需重启进程才能完全生效
	RestartReason          string `json:"restartReason"`           // 需要重启才能完全生效的原因摘要
	LastStatus             string `json:"lastStatus"`              // 最近一次处理结果：idle/success/failed
	LastMessage            string `json:"lastMessage"`             // 最近一次处理结果说明
	LastTriggerSource      string `json:"lastTriggerSource"`       // 最近一次触发来源：watcher/manual_api/startup 等
	LastFailureCategory    string `json:"lastFailureCategory"`     // 最近一次失败分类：fingerprint/load/not_bound 等
	LastCheckedAt          string `json:"lastCheckedAt,omitempty"` // 最近一次检查配置文件时间
	LastReloadAt           string `json:"lastReloadAt,omitempty"`  // 最近一次触发配置重载时间
	LastSuccessAt          string `json:"lastSuccessAt,omitempty"` // 最近一次成功加载时间
	LastFailureAt          string `json:"lastFailureAt,omitempty"` // 最近一次失败时间
	ReloadCount            int64  `json:"reloadCount"`             // 累计成功加载次数
	SuppressedFailureCount int64  `json:"suppressedFailureCount"`  // 限频压制的重复失败日志次数
}

// TaskConfigItem 表示一条已脱敏的运行态配置项。
type TaskConfigItem struct {
	Path      string `json:"path"`      // 扁平化配置路径，如 mysql.write_data_source
	Value     string `json:"value"`     // 展示值；敏感项只保留首尾少量字符
	ValueType string `json:"valueType"` // 值类型：string/number/bool/list/object/null
	Sensitive bool   `json:"sensitive"` // 是否按敏感数据处理过
}

// TaskConfigSectionStat 表示顶层配置分组统计。
type TaskConfigSectionStat struct {
	Name           string `json:"name"`           // 顶层配置名称
	Total          int    `json:"total"`          // 分组内配置项数量
	SensitiveTotal int    `json:"sensitiveTotal"` // 分组内敏感配置项数量
}

// TaskConfigSourceMeta 表示运行态配置快照来源。
type TaskConfigSourceMeta struct {
	Source            string `json:"source"`            // 快照来源，固定为 runtime_snapshot
	ConfigFile        string `json:"configFile"`        // 当前监听的配置文件
	RuntimeFile       string `json:"runtimeFile"`       // 运行期外部配置文件
	ConfigVersion     string `json:"configVersion"`     // 当前生效配置版本指纹
	LastStatus        string `json:"lastStatus"`        // 最近一次热加载状态
	LastTriggerSource string `json:"lastTriggerSource"` // 最近一次触发来源
	LastReloadAt      string `json:"lastReloadAt"`      // 最近一次触发重载时间
	LastSuccessAt     string `json:"lastSuccessAt"`     // 最近一次成功加载时间
	RestartRequired   bool   `json:"restartRequired"`   // 是否仍需重启进程才能完全生效
	RestartReason     string `json:"restartReason"`     // 需要重启才能完全生效的原因摘要
}

// TaskConfigItemQueryResp 表示运行态配置项查询结果。
type TaskConfigItemQueryResp struct {
	Keyword        string                  `json:"keyword,omitempty"` // 当前查询关键字
	SensitiveOnly  bool                    `json:"sensitiveOnly"`     // 是否只返回敏感项
	Page           int                     `json:"page"`              // 当前页码
	PageSize       int                     `json:"pageSize"`          // 当前页大小
	Total          int64                   `json:"total"`             // 命中总数
	TotalItems     int                     `json:"totalItems"`        // 当前快照配置项总数
	SensitiveTotal int                     `json:"sensitiveTotal"`    // 当前快照敏感配置项总数
	Sections       []TaskConfigSectionStat `json:"sections"`          // 顶层配置分组统计
	Source         TaskConfigSourceMeta    `json:"source"`            // 当前运行态快照来源
	SnapshotYAML   string                  `json:"snapshotYaml"`      // 完整脱敏 YAML 快照
	RuntimeYAML    string                  `json:"runtimeYaml"`       // 运行期外部配置的脱敏 YAML 视图
	Items          []TaskConfigItem        `json:"items"`             // 当前页配置项
}

// TaskSchedulerItem 表示周期调度器当前运行状态。
// 该结构用于展示“是否启动 leader 选举、是否已持有 leader、最近同步与入队是否成功”等关键信息。
type TaskSchedulerItem struct {
	Enabled                  bool   `json:"enabled"`                           // 配置中是否启用调度器
	Running                  bool   `json:"running"`                           // 当前进程是否已启动 leader 选举循环
	HasLeader                bool   `json:"hasLeader"`                         // 当前进程是否持有 leader
	InstanceID               string `json:"instanceId,omitempty"`              // 当前进程实例 ID
	LeaderLockKey            string `json:"leaderLockKey,omitempty"`           // leader 锁 key
	LeaseTTLSeconds          int    `json:"leaseTtlSeconds"`                   // leader 锁租约时长（秒）
	RenewIntervalSeconds     int    `json:"renewIntervalSeconds"`              // leader 锁续租间隔（秒）
	SyncIntervalSeconds      int    `json:"syncIntervalSeconds"`               // 周期任务配置同步间隔（秒）
	HeartbeatIntervalSeconds int    `json:"heartbeatIntervalSeconds"`          // 调度器心跳间隔（秒）
	PeriodicTaskCount        int    `json:"periodicTaskCount"`                 // 当前有效周期任务数量
	LastStatus               string `json:"lastStatus,omitempty"`              // 最近一次调度器总体状态
	LastMessage              string `json:"lastMessage,omitempty"`             // 最近一次调度器总体状态说明
	LastStartedAt            string `json:"lastStartedAt,omitempty"`           // 最近一次启动 leader 选举时间
	LastHeartbeatAt          string `json:"lastHeartbeatAt,omitempty"`         // 最近一次心跳上报时间
	LastAcquireAt            string `json:"lastAcquireAt,omitempty"`           // 最近一次获取 leader 时间
	LastReleaseAt            string `json:"lastReleaseAt,omitempty"`           // 最近一次释放或丢失 leader 时间
	LastSyncAt               string `json:"lastSyncAt,omitempty"`              // 最近一次同步周期配置时间
	LastSyncStatus           string `json:"lastSyncStatus,omitempty"`          // 最近一次同步结果：success/failed
	LastSyncMessage          string `json:"lastSyncMessage,omitempty"`         // 最近一次同步结果说明
	LastEnqueueAt            string `json:"lastEnqueueAt,omitempty"`           // 最近一次周期任务入队时间
	LastEnqueueTaskName      string `json:"lastEnqueueTaskName,omitempty"`     // 最近一次入队的任务名称
	LastEnqueueTaskType      string `json:"lastEnqueueTaskType,omitempty"`     // 最近一次入队的任务类型
	LastEnqueueErrorAt       string `json:"lastEnqueueErrorAt,omitempty"`      // 最近一次入队失败时间
	LastEnqueueErrorMessage  string `json:"lastEnqueueErrorMessage,omitempty"` // 最近一次入队失败原因
}
