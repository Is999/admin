package jobs

import (
	"admin/common/i18n"
	"admin/internal/jobs/archive"
	"admin/internal/jobs/taskreport"
	usertagtask "admin/internal/jobs/usertag/task"
	taskqueue "admin/internal/task/queue"
	"admin/internal/types"
)

// taskTypeDisplaySpecs 返回业务任务类型的说明、示例 payload 和手工推荐标记。
func taskTypeDisplaySpecs() []taskqueue.TaskTypeDisplaySpec {
	return []taskqueue.TaskTypeDisplaySpec{
		{
			TaskType:       archive.TaskTypeExecute,
			DescriptionKey: i18n.MsgKeyTaskRegistryTypeArchiveExecuteDesc,
			UsageHintKey:   i18n.MsgKeyTaskRegistryTypeArchiveExecuteHint,
			PayloadExample: "{\n  \"targets\": [\"admin_log\"]\n}",
		},
		{
			TaskType:          taskreport.TaskTypeDailySummary,
			DescriptionKey:    i18n.MsgKeyTaskRegistryTypeDailySummaryDesc,
			UsageHintKey:      i18n.MsgKeyTaskRegistryTypeDailySummaryHint,
			PayloadExample:    "{\n  \"windowStart\": \"2026-06-30T10:00:00+08:00\",\n  \"windowEnd\": \"2026-07-01T10:00:00+08:00\"\n}",
			ManualRecommended: true,
		},
		{
			TaskType:          types.AdminExportTaskType,
			DescriptionKey:    i18n.MsgKeyTaskRegistryTypeAdminExportDesc,
			UsageHintKey:      i18n.MsgKeyTaskRegistryTypeAdminExportHint,
			PayloadExample:    "{\n  \"jobId\": \"job-demo-001\",\n  \"operatorId\": 1,\n  \"operatorName\": \"admin\",\n  \"request\": {\n    \"username\": \"demo\"\n  }\n}",
			ManualRecommended: true,
		},
		{
			TaskTypePrefix: "user_tag:",
			DescriptionKey: i18n.MsgKeyTaskRegistryTypeUserTagDesc,
			UsageHintKey:   i18n.MsgKeyTaskRegistryTypeUserTagHint,
			PayloadExample: "{\n  \"mode\": \"targeted\",\n  \"targets\": [\"uid=10001\"],\n  \"uids\": [10001],\n  \"tagTypes\": [2, 3]\n}",
		},
	}
}

// workflowDisplaySpecs 返回业务工作流的操作提示和 targets 示例。
func workflowDisplaySpecs() []taskqueue.WorkflowDisplaySpec {
	return []taskqueue.WorkflowDisplaySpec{
		{
			Name:           archive.WorkflowNameRun,
			DescriptionKey: i18n.MsgKeyTaskRegistryWorkflowArchiveRunDesc,
			UsageHintKey:   i18n.MsgKeyTaskRegistryWorkflowArchiveRunHint,
			TargetsExample: "admin_log",
		},
		{
			Name:           taskreport.WorkflowNameDailySummary,
			DescriptionKey: i18n.MsgKeyTaskRegistryWorkflowDailySummaryDesc,
			UsageHintKey:   i18n.MsgKeyTaskRegistryWorkflowDailySummaryHint,
		},
		{
			Name:           usertagtask.WorkflowNameUserTagFull,
			DescriptionKey: i18n.MsgKeyTaskRegistryWorkflowUserTagFullDesc,
			UsageHintKey:   i18n.MsgKeyTaskRegistryWorkflowUserTagFullHint,
		},
		{
			Name:           usertagtask.WorkflowNameUserTagDelta,
			DescriptionKey: i18n.MsgKeyTaskRegistryWorkflowUserTagDeltaDesc,
			UsageHintKey:   i18n.MsgKeyTaskRegistryWorkflowUserTagDeltaHint,
		},
		{
			Name:           usertagtask.WorkflowNameUserTagTargeted,
			DescriptionKey: i18n.MsgKeyTaskRegistryWorkflowUserTagTargetedDesc,
			UsageHintKey:   i18n.MsgKeyTaskRegistryWorkflowUserTagTargetedHint,
			TargetsExample: "uid:10001, uid:10002",
		},
		{
			Name:           usertagtask.WorkflowNameUserTagRecalculate,
			DescriptionKey: i18n.MsgKeyTaskRegistryWorkflowUserTagRecalculateDesc,
			UsageHintKey:   i18n.MsgKeyTaskRegistryWorkflowUserTagRecalculateHint,
			TargetsExample: "tagType:2, tagType:3",
		},
		{
			Name:           usertagtask.WorkflowNameUserTagRuntimeCleanup,
			DescriptionKey: i18n.MsgKeyTaskRegistryWorkflowUserTagRuntimeCleanupDesc,
			UsageHintKey:   i18n.MsgKeyTaskRegistryWorkflowUserTagRuntimeCleanupHint,
		},
		{
			Name:           usertagtask.WorkflowNameUserTagEventOutboxRetryScan,
			DescriptionKey: i18n.MsgKeyTaskRegistryWorkflowUserTagOutboxRetryDesc,
			UsageHintKey:   i18n.MsgKeyTaskRegistryWorkflowUserTagOutboxRetryHint,
		},
	}
}
