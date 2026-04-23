package keys

const (
	// AdminInfo 表示管理员信息缓存键模板。
	// Redis 类型：Hash。
	// `%d` 位置填充管理员 ID。
	AdminInfo = "admin:info:%d"

	// RoleStatus 表示角色状态缓存键。
	// Redis 类型：Hash。
	RoleStatus = "role_status"

	// RolePermission 表示角色权限缓存键模板。
	// Redis 类型：Set。
	// `%d` 位置填充角色 ID。
	RolePermission = "role_permission:%d"

	// RoleTree 表示角色树缓存键。
	// Redis 类型：String（JSON 文本）。
	RoleTree = "role_tree"

	// AdminRoleIDs 表示管理员启用角色 ID 集合缓存键模板。
	// Redis 类型：Set。
	// `%d` 位置填充管理员 ID。
	AdminRoleIDs = "admin_role_ids:%d"

	// AdminPermissionIDs 表示管理员聚合权限 ID 集合缓存键模板。
	// Redis 类型：Set。
	// `%d` 位置填充管理员 ID。
	AdminPermissionIDs = "admin_permission_ids:%d"

	// AdminPermissionUUIDs 表示管理员最终权限码集合缓存键模板。
	// Redis 类型：Set。
	// `%d` 位置填充管理员 ID。
	AdminPermissionUUIDs = "admin_permission_uuids:%d"

	// AdminProfile 表示管理员公开资料缓存键模板。
	// Redis 类型：String（JSON 文本）。
	// `%d` 位置填充管理员 ID。
	AdminProfile = "admin_profile:%d"

	// AdminRolesDetail 表示管理员角色名称列表缓存键模板。
	// Redis 类型：String（JSON 文本）。
	// `%d` 位置填充管理员 ID。
	AdminRolesDetail = "admin_roles_detail:%d"

	// PermissionModule 表示权限模块缓存键。
	// Redis 类型：Hash。
	PermissionModule = "permission_module"

	// PermissionUUID 表示权限 UUID 缓存键。
	// Redis 类型：Hash。
	PermissionUUID = "permission_uuid"

	// PermissionTree 表示权限树缓存键。
	// Redis 类型：String（JSON 文本）。
	PermissionTree = "permission_tree"

	// RoutePermissionIDs 表示路由别名候选权限 ID 集合缓存键模板。
	// Redis 类型：Set。
	// `%s` 位置填充路由别名。
	RoutePermissionIDs = "route_permission_ids:%s"

	// RoutePermissionAliasIndex 表示已写入路由权限候选缓存的路由别名索引。
	// Redis 类型：Set。
	// 成员为 routeAlias，用于权限定义变更时精确删除 `route_permission_ids:{routeAlias}`，避免前缀 SCAN。
	RoutePermissionAliasIndex = "route_permission_ids:index"

	// SysConfigUUID 表示系统配置缓存键模板。
	// Redis 类型：Hash。
	// `%s` 位置填充系统配置 uuid。
	SysConfigUUID = "config_uuid:%s"

	// SecretKeyRoute 表示秘钥版本路由缓存键模板。
	// Redis 类型：Hash。
	// `%s` 位置填充 secret_key.uuid。
	SecretKeyRoute = "secret_key_route:%s"

	// SecretKeyAESVersion 表示版本化 AES 秘钥配置缓存键模板。
	// Redis 类型：Hash。
	// 第一个 `%s` 位置填充 secret_key.uuid，第二个 `%s` 位置填充 key_version。
	SecretKeyAESVersion = "secret_key_aes:%s:%s"

	// SecretKeyRSAVersion 表示版本化 RSA 秘钥配置缓存键模板。
	// Redis 类型：Hash。
	// 第一个 `%s` 位置填充 secret_key.uuid，第二个 `%s` 位置填充 key_version。
	SecretKeyRSAVersion = "secret_key_rsa:%s:%s"

	// SecretKeyAESVersionPrefix 表示指定 AppID 下 AES 版本缓存 key 前缀。
	// Redis 类型：Hash 前缀。
	// `%s` 位置填充 secret_key.uuid，用于按索引成员做精确归属校验。
	SecretKeyAESVersionPrefix = "secret_key_aes:%s:"

	// SecretKeyRSAVersionPrefix 表示指定 AppID 下 RSA 版本缓存 key 前缀。
	// Redis 类型：Hash 前缀。
	// `%s` 位置填充 secret_key.uuid，用于按索引成员做精确归属校验。
	SecretKeyRSAVersionPrefix = "secret_key_rsa:%s:"

	// SecretKeyVersionIndex 表示指定 AppID 下版本材料缓存 key 的精确索引。
	// Redis 类型：Set。
	// `%s` 位置填充 secret_key.uuid；成员为 `secret_key_aes:{uuid}:{keyVersion}` 与 `secret_key_rsa:{uuid}:{keyVersion}` 真实 key。
	SecretKeyVersionIndex = "secret_key_version:index:%s"

	// LoginCheckMFAFlag 表示管理员登录 MFA 校验标记缓存键模板。
	// Redis 类型：String（Unix 时间戳）。
	// `%d` 位置填充管理员 ID。
	LoginCheckMFAFlag = "login_check_mfa_flag:%d"

	// AdminLogoutToken 表示管理员登出令牌标记缓存键模板。
	// Redis 类型：String。
	// `%d` 位置填充管理员 ID，用于防止登录态缓存 miss 时把已登出的旧 token 回源恢复。
	AdminLogoutToken = "admin:logout_token:%d"

	// AdminMFATwoStepTicket 表示管理员二次校验票据缓存键模板。
	// Redis 类型：String。
	// 第一个 `%d` 位置填充管理员 ID，第二个 `%s` 位置填充票据 key。
	AdminMFATwoStepTicket = "admin:mfa:two_step:%d:%s"

	// AdminMFATwoStepTicketPrefix 表示管理员二次校验票据缓存 key 前缀。
	// Redis 类型：String 前缀。
	// `%d` 位置填充管理员 ID，用于按索引成员做精确归属校验。
	AdminMFATwoStepTicketPrefix = "admin:mfa:two_step:%d:"

	// AdminMFATwoStepIndex 表示管理员二次校验票据 key 的精确索引。
	// Redis 类型：Set。
	// `%d` 位置填充管理员 ID；成员为 `admin:mfa:two_step:{adminID}:{ticketKey}` 真实 key。
	AdminMFATwoStepIndex = "admin:mfa:two_step:index:%d"

	// SysConfigExcelExportLock 表示字典配置导出条件互斥锁 key 模板。
	// Redis 类型：String（由 redsync 管理）。
	// `%s` 位置填充导出条件指纹，避免同条件并发重复生成 Excel。
	SysConfigExcelExportLock = "sys_config:excel:export:%s"

	// SysConfigExcelImportLock 表示字典配置导入用户互斥锁 key 模板。
	// Redis 类型：String（由 redsync 管理）。
	// `%d` 位置填充管理员 ID，避免同一管理员并发导入覆盖变更。
	SysConfigExcelImportLock = "sys_config:excel:import:%d"

	// CacheRebuildLock 表示缓存回源重建互斥锁 key 模板。
	// Redis 类型：String（由 redsync 管理）。
	// `%s` 位置填充真实缓存 key 的业务段，实际 Redis key 通过 AppScopedKey 追加 app_id 前缀。
	CacheRebuildLock = "cache:rebuild:lock:%s"

	// AdminExportJob 表示管理员列表导出任务状态缓存键模板。
	// Redis 类型：String（JSON 文本）。
	// `%s` 位置填充导出任务 jobId。
	AdminExportJob = "admin:export:job:%s"

	// AdminExportRequestIndex 表示管理员导出条件到任务 ID 的复用索引。
	// Redis 类型：String。
	// `%s` 位置填充导出条件指纹。
	AdminExportRequestIndex = "admin:export:request:%s"

	// AdminExportRequestLock 表示管理员导出条件互斥锁 key 模板。
	// Redis 类型：String（由 redsync 管理）。
	// `%s` 位置填充导出条件指纹。
	AdminExportRequestLock = "admin:export:request:lock:%s"

	// FileTransferUploadSession 表示断点续传上传会话缓存键模板。
	// Redis 类型：String（JSON 文本）。
	// `%s` 位置填充 uploadId。
	FileTransferUploadSession = "file_transfer:upload:session:%s"

	// FileTransferUploadChunks 表示断点续传上传分片完成集合键模板。
	// Redis 类型：Set。
	// `%s` 位置填充 uploadId。
	FileTransferUploadChunks = "file_transfer:upload:chunks:%s"

	// FileTransferUploadFingerprint 表示断点续传上传文件指纹到 uploadId 的复用索引。
	// Redis 类型：String。
	// `%s` 位置填充文件指纹。
	FileTransferUploadFingerprint = "file_transfer:upload:fingerprint:%s"

	// FileTransferUploadObjectIndex 表示统一存储对象 key 到 uploadId 的反查索引。
	// Redis 类型：String。
	// `%s` 位置填充对象 key 指纹。
	FileTransferUploadObjectIndex = "file_transfer:upload:object:%s"

	// TaskQueueNamespacePrefix 表示任务队列名称的站点命名空间前缀。
	// Redis 类型：队列名/分组名前缀。
	// `%s` 位置填充 app_id，用于共享 task redis 时隔离不同站点。
	TaskQueueNamespacePrefix = "app:%s:"

	// TaskQueueNamespacedName 表示任务队列或分组的站点命名空间名称模板。
	// Redis 类型：队列名/分组名。
	// 第一个 `%s` 位置填充 app_id，第二个 `%s` 位置填充逻辑队列或分组名。
	TaskQueueNamespacedName = "app:%s:%s"

	// TaskQueueDefaultRedisPrefix 表示未配置 app_id 时的任务系统 Redis key 前缀。
	// Redis 类型：Key 前缀。
	TaskQueueDefaultRedisPrefix = "task"

	// TaskQueueAppRedisPrefix 表示配置 app_id 后的任务系统 Redis key 前缀模板。
	// Redis 类型：Key 前缀。
	// `%s` 位置填充 app_id。
	TaskQueueAppRedisPrefix = "task:app:%s"

	// TaskQueueSchedulerLeaderDefault 表示调度器默认 leader 租约 key。
	// Redis 类型：String（由 redsync 管理）。
	TaskQueueSchedulerLeaderDefault = "task:scheduler:leader"

	// UserTagWorkflowUniqueSegment 表示用户标签工作流默认去重键片段模板。
	// Redis 类型：作为 task workflow unique key 的业务片段。
	// 第一个 `%s` 位置填充 mode，第二个 `%x` 位置填充规范化参数摘要。
	UserTagWorkflowUniqueSegment = "user_tag:%s:%x"

	// SignatureReplayRequest 表示请求签名防重放缓存键模板。
	// Redis 类型：String。
	// `%s` 位置填充 RequestID，实际 Redis key 通过 AppScopedKey 追加 app_id 前缀。
	SignatureReplayRequest = "signature:request:%s"

	// LoginCaptcha 表示登录图形验证码缓存键模板。
	// Redis 类型：String。
	// `%s` 位置填充验证码 key。
	LoginCaptcha = "login:captcha:%s"

	// UserTagWorkflowLeaseKeyPrefix 表示用户标签写工作流全局互斥租约 key 前缀。
	// Redis 类型：String。
	// 实际 Redis key 通过 AppScopedKey 追加 app_id 前缀，值为 `workflowID|mode`，释放时必须按完整 owner 精确比较。
	UserTagWorkflowLeaseKeyPrefix = "user_tag:workflow:write_lock"

	// UserTagWorkflowSyncDoneKeyPrefix 表示用户标签 sync_kafka 分片完成屏障 key 前缀。
	// Redis 类型：Set。
	// 实际 Redis key 通过 AppScopedKey 追加 app_id 前缀，后缀拼接 workflow_id。
	UserTagWorkflowSyncDoneKeyPrefix = "user_tag:workflow:sync_done"

	// UserTagRuntimeCleanupLock 表示用户标签运行期辅助表清理互斥锁。
	// Redis 类型：String（由 redsync 管理）。
	// 用途：避免周期调度和人工补跑同时清理 runtime_uid、outbox 等高基数辅助表。
	UserTagRuntimeCleanupLock = "user_tag:runtime:cleanup:lock"

	// UserTagKafkaOutboxRetryScanLock 表示用户标签 Kafka outbox 异常扫描互斥锁。
	// Redis 类型：String（由 redsync 管理）。
	// 用途：限制异常 outbox 扫描只有一个任务推进，降低 Kafka 故障期对 MySQL 的重复领取压力。
	UserTagKafkaOutboxRetryScanLock = "user_tag:kafka_outbox:retry_scan:lock"

	// ArchiveJobPlanLock 表示归档任务区间规划互斥锁 key 模板。
	// Redis 类型：String（由 redsync 管理）。
	// `%s` 位置填充归档 job 名，避免多个 worker 重复规划同一任务区间。
	ArchiveJobPlanLock = "archive:job:%s:plan"

	// ArchiveJobWatermarkLock 表示归档任务水位推进互斥锁 key 模板。
	// Redis 类型：String（由 redsync 管理）。
	// `%s` 位置填充归档 job 名，确保水位只按连续完成区间串行推进。
	ArchiveJobWatermarkLock = "archive:job:%s:watermark"

	// ArchiveJobCleanupLock 表示归档历史表清理互斥锁 key 模板。
	// Redis 类型：String（由 redsync 管理）。
	// `%s` 位置填充归档 job 名，避免并发删除同一批历史表。
	ArchiveJobCleanupLock = "archive:job:%s:cleanup"

	// EmptyValueMarker 表示空值缓存占位符。
	// Redis 类型：String 常量值。
	// 用途：避免缓存穿透时重复回源。
	EmptyValueMarker = "__empty__"
)
