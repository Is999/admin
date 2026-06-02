-- 用途：修复早期本地草稿把周期任务名误写入归档任务草稿的问题。

UPDATE `runtime_archive_job` AS bad
LEFT JOIN `runtime_archive_job` AS ok
  ON ok.`app_id` = bad.`app_id`
  AND ok.`env` = bad.`env`
  AND ok.`name` = 'admin_log'
SET
  bad.`name` = 'admin_log',
  bad.`table_name` = 'admin_log',
  bad.`database_name` = IF(bad.`database_name` = '', 'main', bad.`database_name`),
  bad.`remark` = 'repair invalid local archive draft seed'
WHERE bad.`app_id` = '1'
  AND bad.`env` = 'dev'
  AND bad.`name` = 'archive-admin-log-hourly'
  AND bad.`table_name` = ''
  AND ok.`id` IS NULL;

UPDATE `runtime_archive_job` AS bad
INNER JOIN `runtime_archive_job` AS ok
  ON ok.`app_id` = bad.`app_id`
  AND ok.`env` = bad.`env`
  AND ok.`name` = 'admin_log'
SET
  bad.`app_id` = CONCAT(bad.`app_id`, ':legacy'),
  bad.`env` = CONCAT(bad.`env`, ':legacy'),
  bad.`enabled` = 0,
  bad.`table_name` = 'admin_log',
  bad.`database_name` = IF(bad.`database_name` = '', 'main', bad.`database_name`),
  bad.`remark` = 'repair invalid local archive draft seed'
WHERE bad.`app_id` = '1'
  AND bad.`env` = 'dev'
  AND bad.`name` = 'archive-admin-log-hourly'
  AND bad.`table_name` = '';
