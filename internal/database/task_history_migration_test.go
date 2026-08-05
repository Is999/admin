package database

import (
	"strings"
	"testing"
)

// TestTaskHistoryMigrationsUseBoundedQueryIndexes 验证短期历史表具备幂等键和所有在线查询索引。
func TestTaskHistoryMigrationsUseBoundedQueryIndexes(t *testing.T) {
	workflowSQL := strings.ToLower(migrationSQLByAsset(t, "task_workflow_run.sql"))
	failureSQL := strings.ToLower(migrationSQLByAsset(t, "task_failure.sql"))
	for _, want := range []string{
		"`event_id` char(64) character set ascii collate ascii_bin not null",
		"unique key `uk_app_event` (`app_id`,`event_id`)",
		"key `idx_app_finished` (`app_id`,`finished_at`,`id`)",
		"key `idx_app_workflow` (`app_id`,`workflow_id`,`finished_at`)",
		"key `idx_app_periodic_finished` (`app_id`,`periodic_name`,`finished_at`)",
		"key `idx_app_source_finished` (`app_id`,`source`,`finished_at`)",
		"key `idx_app_queue_finished` (`app_id`,`queue`,`finished_at`)",
		"`snapshot_json` json not null",
	} {
		if !strings.Contains(workflowSQL, want) {
			t.Fatalf("工作流历史迁移缺少生产查询边界: %s", want)
		}
	}
	for _, want := range []string{
		"`event_id` char(64) character set ascii collate ascii_bin not null",
		"unique key `uk_app_event` (`app_id`,`event_id`)",
		"key `idx_app_failed` (`app_id`,`failed_at`,`id`)",
		"key `idx_app_task` (`app_id`,`task_id`,`failed_at`)",
		"key `idx_app_source_failed` (`app_id`,`source`,`failed_at`)",
		"key `idx_app_workflow_failed` (`app_id`,`workflow_id`,`failed_at`)",
		"key `idx_app_type_failed` (`app_id`,`task_type`,`failed_at`)",
	} {
		if !strings.Contains(failureSQL, want) {
			t.Fatalf("失败历史迁移缺少生产查询边界: %s", want)
		}
	}
	for _, forbidden := range []string{"`payload`", "`result`", "partition by"} {
		if strings.Contains(workflowSQL, forbidden) || strings.Contains(failureSQL, forbidden) {
			t.Fatalf("任务历史迁移包含禁止字段或高维护成本分区: %s", forbidden)
		}
	}
}
