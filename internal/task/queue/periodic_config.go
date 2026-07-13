package taskqueue

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"admin/helper"
	"admin/internal/config"
	"admin/internal/infra/loggerx"
	tasklimits "admin/internal/task/limits"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
	"github.com/zeromicro/go-zero/core/logx"
)

// periodicConfigs 把系统配置和插件注册的周期任务转换成 Asynq 调度配置。
func (m *Manager) periodicConfigs() ([]*asynq.PeriodicTaskConfig, error) {
	items := m.allPeriodicTasks()
	configs := make([]*asynq.PeriodicTaskConfig, 0, len(items))
	for idx, item := range items {
		item, invalidErr := m.validatePeriodicTaskConfig(item)
		if invalidErr != nil {
			m.notifyPeriodicTaskConfigInvalid(idx, item, invalidErr.Error())
			continue
		}
		payload := WorkflowTriggerPayload{
			WorkflowName:     item.Workflow,
			Queue:            item.Queue,
			Targets:          item.Targets,
			ShardTotal:       max(item.ShardTotal, 1),
			GrayPercent:      item.GrayPercent,
			Source:           WorkflowSourcePeriodic,
			PeriodicName:     periodicTaskDisplayName(item),
			TimeoutSeconds:   item.TimeoutSeconds,
			UniqueKey:        item.UniqueKey,
			UniqueTTLSeconds: item.UniqueTTLSeconds,
		}
		var retryOverride *int
		if item.Retry > 0 {
			retryOverride = new(item.Retry)
			payload.Retry = retryOverride
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, errors.Tag(err)
		}
		task := asynq.NewTaskWithHeaders(TypeWorkflowTrigger, body, map[string]string{
			headerTaskName:     periodicTaskDisplayName(item),
			headerTaskSource:   WorkflowSourcePeriodic,
			HeaderPeriodicName: periodicTaskDisplayName(item),
			headerWorkflowName: item.Workflow,
		})
		opts := []asynq.Option{
			asynq.Queue(m.namespacedQueueName(helper.FirstNonEmptyString(item.Queue, m.defaultWorkflowQueue()))),
			asynq.MaxRetry(workflowTaskRetryBudget(m.defaultRetry(retryOverride))),
			asynq.Retention(taskCompletedRetention),
		}
		if item.TimeoutSeconds > 0 {
			opts = append(opts, asynq.Timeout(time.Duration(item.TimeoutSeconds)*time.Second))
		}
		if item.Deadline != "" {
			if deadline, parseErr := time.Parse(time.RFC3339, item.Deadline); parseErr == nil {
				opts = append(opts, asynq.Deadline(deadline))
			}
		}
		if item.UniqueKey != "" && item.UniqueTTLSeconds > 0 {
			opts = append(opts, asynq.Unique(time.Duration(item.UniqueTTLSeconds)*time.Second))
		}
		configs = append(configs, &asynq.PeriodicTaskConfig{
			Cronspec: item.Cron,
			Task:     task,
			Opts:     opts,
		})
	}
	return configs, nil
}

// ValidatePeriodicTaskDefinitions 检查周期任务引用的工作流是否已在当前进程注册。
// 滚动发布时只记录未知工作流，调度同步阶段继续跳过无效配置。
func (m *Manager) ValidatePeriodicTaskDefinitions() error {
	if m == nil || !m.IsEnabled() {
		return nil
	}
	items := m.allPeriodicTasks()
	for idx, item := range items {
		normalized, invalidErr := m.validatePeriodicTaskConfig(item)
		if invalidErr == nil {
			continue
		}
		m.notifyPeriodicTaskConfigInvalid(idx, normalized, invalidErr.Error())
	}
	return nil
}

// validatePeriodicTaskConfig 校验并归一化单个周期任务配置，异常时仅跳过当前任务。
func (m *Manager) validatePeriodicTaskConfig(item config.TaskPeriodicConfig) (config.TaskPeriodicConfig, error) {
	cronspec, err := periodicTaskCronspec(item)
	if err != nil {
		return item, errors.Tag(err)
	}
	if err = validatePeriodicCronspec(cronspec); err != nil {
		return item, errors.Tag(err)
	}
	item.Cron = cronspec
	item.Name = strings.TrimSpace(item.Name)
	item.Workflow = strings.TrimSpace(item.Workflow)
	if item.Workflow == "" {
		return item, errors.Errorf("周期任务 workflow 不能为空")
	}
	definition, err := m.workflowDefinition(item.Workflow)
	if err != nil {
		return item, errors.Tag(err)
	}
	item.Queue = strings.TrimSpace(item.Queue)
	item.Targets = normalizeStrings(item.Targets)
	item.UniqueKey = strings.TrimSpace(item.UniqueKey)
	item.Deadline = strings.TrimSpace(item.Deadline)
	if item.ShardTotal < 0 {
		return item, errors.Errorf("周期任务 shard_total 不能小于 0")
	}
	if item.GrayPercent < 0 || item.GrayPercent > 100 {
		return item, errors.Errorf("周期任务 gray_percent 必须在 0 到 100 之间")
	}
	if item.Retry < 0 {
		return item, errors.Errorf("周期任务 retry 不能小于 0")
	}
	if item.Retry > tasklimits.MaxRetry {
		return item, errors.Errorf("周期任务 retry 不能超过 %d", tasklimits.MaxRetry)
	}
	if item.TimeoutSeconds < 0 {
		return item, errors.Errorf("周期任务 timeout_seconds 不能小于 0")
	}
	if item.TimeoutSeconds > tasklimits.MaxTimeoutSeconds {
		return item, errors.Errorf("周期任务 timeout_seconds 不能超过 %d", tasklimits.MaxTimeoutSeconds)
	}
	if item.UniqueTTLSeconds < 0 {
		return item, errors.Errorf("周期任务 unique_ttl_seconds 不能小于 0")
	}
	if definition.PeriodicUniqueBySchedule {
		if item.UniqueKey == "" {
			return item, errors.Errorf("按计划时刻去重的周期工作流必须配置 unique_key")
		}
		if item.UniqueTTLSeconds == 0 {
			return item, errors.Errorf("按计划时刻去重的周期工作流必须配置大于 0 的 unique_ttl_seconds")
		}
	}
	if item.Deadline != "" {
		if _, err = time.Parse(time.RFC3339, item.Deadline); err != nil {
			return item, errors.Wrap(err, "解析周期任务 deadline 失败")
		}
	}
	if item.UniqueKey != "" && item.UniqueTTLSeconds == 0 {
		// 周期任务配置了 unique_key 时默认复用任务系统全局去重 TTL，避免遗漏 TTL 后静默失去防重效果。
		item.UniqueTTLSeconds = int(m.uniqueTTL(nil).Seconds())
	}
	return item, nil
}

// notifyPeriodicTaskConfigInvalid 记录周期任务配置异常，保证错误配置可被日志检索和告警系统发现。
func (m *Manager) notifyPeriodicTaskConfigInvalid(index int, item config.TaskPeriodicConfig, reason string) {
	fields := []logx.LogField{
		logx.Field("index", index),
		logx.Field("task_name", strings.TrimSpace(item.Name)),
		logx.Field("cron", strings.TrimSpace(item.Cron)),
		logx.Field("every_seconds", item.EverySeconds),
		logx.Field("workflow", strings.TrimSpace(item.Workflow)),
		logx.Field("task_queue", strings.TrimSpace(item.Queue)),
		logx.Field("unique_key", strings.TrimSpace(item.UniqueKey)),
		logx.Field("failure_reason", strings.TrimSpace(reason)),
	}
	loggerx.ErrorTextw(context.Background(), "周期任务 配置无效", reason, fields...)
	m.runPeriodicConfigInvalidHooks(index, item, reason, fields)
}

// runPeriodicConfigInvalidHooks 推送周期任务配置异常告警；同一异常在限频窗口内只通知一次。
func (m *Manager) runPeriodicConfigInvalidHooks(index int, item config.TaskPeriodicConfig, reason string, fields []logx.LogField) {
	if m == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	now := time.Now()
	key := periodicConfigInvalidAlertKey(index, item, reason)
	m.mu.Lock()
	if len(m.periodicConfigHooks) == 0 {
		m.mu.Unlock()
		return
	}
	if m.periodicConfigAlertedMap == nil {
		m.periodicConfigAlertedMap = make(map[string]periodicConfigAlertState)
	}
	state := m.periodicConfigAlertedMap[key]
	if !state.LastSentAt.IsZero() && now.Sub(state.LastSentAt) < periodicConfigAlertTTL {
		state.SuppressedCount++
		m.periodicConfigAlertedMap[key] = state
		m.mu.Unlock()
		return
	}
	triggerCount := state.SuppressedCount + 1
	m.periodicConfigAlertedMap[key] = periodicConfigAlertState{LastSentAt: now}
	hooks := append([]PeriodicConfigInvalidHook(nil), m.periodicConfigHooks...)
	m.mu.Unlock()

	report := PeriodicConfigInvalidReport{
		Index:        index,
		Task:         item,
		Reason:       reason,
		OccurredAt:   now,
		TriggerCount: triggerCount,
	}
	hookFailed := false
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		if err := hook(context.Background(), report); err != nil {
			hookFailed = true
			loggerx.Errorw(context.Background(), "周期任务配置异常告警钩子执行失败", err, fields...)
		}
	}
	if hookFailed {
		m.mu.Lock()
		current := m.periodicConfigAlertedMap[key]
		if current.LastSentAt.Equal(now) {
			state.SuppressedCount += current.SuppressedCount
			if state.LastSentAt.IsZero() && state.SuppressedCount == 0 {
				delete(m.periodicConfigAlertedMap, key)
			} else {
				m.periodicConfigAlertedMap[key] = state
			}
		}
		m.mu.Unlock()
	}
}

// periodicConfigInvalidAlertKey 生成周期任务异常告警指纹，用于对重复同步日志限频。
func periodicConfigInvalidAlertKey(index int, item config.TaskPeriodicConfig, reason string) string {
	return strings.Join([]string{
		strconv.Itoa(index),
		strings.TrimSpace(item.Name),
		strings.TrimSpace(item.Workflow),
		strings.TrimSpace(item.Cron),
		strconv.Itoa(item.EverySeconds),
		strings.TrimSpace(item.Queue),
		strings.TrimSpace(item.UniqueKey),
		strings.TrimSpace(reason),
	}, "\x00")
}
