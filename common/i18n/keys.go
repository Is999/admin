package i18n

import "admin/common/codes"

const (
	// MsgKeyUndefined 表示未知业务状态的通用文案 key。
	MsgKeyUndefined = codes.MsgKeyUndefined
	// MsgKeySuccess 表示通用成功响应文案 key。
	MsgKeySuccess = codes.MsgKeySuccess
	// MsgKeyFail 表示通用失败响应文案 key。
	MsgKeyFail = codes.MsgKeyFail
	// MsgKeyCheckMFABind 表示账号需要先绑定并启用 MFA 的标准提示 key。
	MsgKeyCheckMFABind = codes.MsgKeyCheckMFABind
	// MsgKeyCheckMFA 表示账号需要完成 MFA 动态验证码校验的标准提示 key。
	MsgKeyCheckMFA = codes.MsgKeyCheckMFA
	// MsgKeyCheckPasswordReset 表示账号需要先修改登录密码的标准提示 key。
	MsgKeyCheckPasswordReset = codes.MsgKeyCheckPasswordReset
	// MsgKeyOK 表示 HTTP OK 语义的文案 key。
	MsgKeyOK = codes.MsgKeyOK
	// MsgKeyBadRequest 表示请求参数或格式错误的 HTTP 文案 key。
	MsgKeyBadRequest = codes.MsgKeyBadRequest
	// MsgKeyUnauthorized 表示未授权访问的 HTTP 文案 key。
	MsgKeyUnauthorized = codes.MsgKeyUnauthorized
	// MsgKeyForbidden 表示无权限访问的 HTTP 文案 key。
	MsgKeyForbidden = codes.MsgKeyForbidden
	// MsgKeyNotFound 表示资源未找到的 HTTP 文案 key。
	MsgKeyNotFound = codes.MsgKeyNotFound
	// MsgKeyServerError 表示服务端异常的 HTTP 文案 key。
	MsgKeyServerError = codes.MsgKeyServerError
	// MsgKeyServiceBusy 表示服务繁忙或依赖不可用的 HTTP 文案 key。
	MsgKeyServiceBusy = codes.MsgKeyServiceBusy
	// MsgKeyTimeout 表示请求超时的 HTTP 文案 key。
	MsgKeyTimeout = codes.MsgKeyTimeout

	// MsgKeyParamError 表示通用参数错误的业务文案 key。
	MsgKeyParamError = codes.MsgKeyParamError
	// MsgKeyAuthFailed 表示认证或二次校验失败的业务文案 key。
	MsgKeyAuthFailed = codes.MsgKeyAuthFailed
	// MsgKeyRateLimit 表示触发限流保护的业务文案 key。
	MsgKeyRateLimit = codes.MsgKeyRateLimit
	// MsgKeyInternalError 表示内部错误的业务文案 key。
	MsgKeyInternalError = codes.MsgKeyInternalError
	// MsgKeyDBError 表示数据库错误的业务文案 key。
	MsgKeyDBError = codes.MsgKeyDBError

	// MsgKeyCreateSuccess 表示创建成功的业务文案 key。
	MsgKeyCreateSuccess = codes.MsgKeyCreateSuccess
	// MsgKeyCreateFail 表示创建失败的业务文案 key。
	MsgKeyCreateFail = codes.MsgKeyCreateFail
	// MsgKeyAddSuccess 表示新增成功的业务文案 key。
	MsgKeyAddSuccess = codes.MsgKeyAddSuccess
	// MsgKeyAddFail 表示新增失败的业务文案 key。
	MsgKeyAddFail = codes.MsgKeyAddFail
	// MsgKeySaveSuccess 表示保存成功的业务文案 key。
	MsgKeySaveSuccess = codes.MsgKeySaveSuccess
	// MsgKeySaveFail 表示保存失败的业务文案 key。
	MsgKeySaveFail = codes.MsgKeySaveFail
	// MsgKeyUpdateSuccess 表示更新成功的业务文案 key。
	MsgKeyUpdateSuccess = codes.MsgKeyUpdateSuccess
	// MsgKeyUpdateFail 表示更新失败的业务文案 key。
	MsgKeyUpdateFail = codes.MsgKeyUpdateFail
	// MsgKeyDeleteSuccess 表示删除成功的业务文案 key。
	MsgKeyDeleteSuccess = codes.MsgKeyDeleteSuccess
	// MsgKeyDeleteFail 表示删除失败的业务文案 key。
	MsgKeyDeleteFail = codes.MsgKeyDeleteFail
	// MsgKeyFetchSuccess 表示获取成功的业务文案 key。
	MsgKeyFetchSuccess = codes.MsgKeyFetchSuccess
	// MsgKeyFetchFail 表示获取失败的业务文案 key。
	MsgKeyFetchFail = codes.MsgKeyFetchFail

	// MsgKeyUserNotFound 表示后台账号不存在的文案 key。
	MsgKeyUserNotFound = codes.MsgKeyUserNotFound
	// MsgKeyInvalidPassword 表示后台账号密码错误的文案 key。
	MsgKeyInvalidPassword = codes.MsgKeyInvalidPassword
	// MsgKeyUserAlreadyExists 表示后台账号已存在的文案 key。
	MsgKeyUserAlreadyExists = codes.MsgKeyUserAlreadyExists
	// MsgKeyUserDisabled 表示后台账号被禁用的文案 key。
	MsgKeyUserDisabled = codes.MsgKeyUserDisabled
	// MsgKeyInvalidCaptcha 表示登录验证码错误或过期的文案 key。
	MsgKeyInvalidCaptcha = codes.MsgKeyInvalidCaptcha

	// MsgKeyUnauthorizedText 表示需要登录或重新登录的认证文案 key。
	MsgKeyUnauthorizedText = "auth.unauthorized_text"
	// MsgKeyTokenExpired 表示登录 token 已过期的认证文案 key。
	MsgKeyTokenExpired = "auth.token_expired"
	// MsgKeyTokenInvalid 表示登录 token 无效的认证文案 key。
	MsgKeyTokenInvalid = "auth.token_invalid"
	// MsgKeyAdminLoginIPChanged 表示登录 IP 或环境变化的认证文案 key。
	MsgKeyAdminLoginIPChanged = "auth.admin_login_ip_changed"
	// MsgKeyAdminIPNotAllowed 表示登录 IP 不在白名单的认证文案 key。
	MsgKeyAdminIPNotAllowed = "auth.admin_ip_not_allowed"
	// MsgKeyMFARequired 表示 MFA 信息失效后需要重新验证的认证文案 key。
	MsgKeyMFARequired = "auth.mfa_required"
	// MsgKeyMFAExpired 表示 MFA 校验过期后需要重新验证的认证文案 key。
	MsgKeyMFAExpired = codes.MsgKeyMFAExpired
	// MsgKeyMFACodeInvalid 表示 MFA 动态验证码错误的认证文案 key。
	MsgKeyMFACodeInvalid = codes.MsgKeyMFACodeInvalid
	// MsgKeyMFAForceEnabledDisallowDisable 表示强制 MFA 策略下禁止停用的认证文案 key。
	MsgKeyMFAForceEnabledDisallowDisable = "auth.mfa_force_enabled_disallow_disable"
	// MsgKeyPasswordResetRequired 表示账号必须先修改密码的认证文案 key。
	MsgKeyPasswordResetRequired = "auth.password_reset_required"
	// MsgKeyReplayAttack 表示请求签名防重放校验失败的认证文案 key。
	MsgKeyReplayAttack = "auth.replay_attack"

	// MsgKeyParamErrorFormat 表示参数错误动态详情模板 key。
	MsgKeyParamErrorFormat = "fmt.param_error"
	// MsgKeyInternalErrorFormat 表示内部错误动态详情模板 key。
	MsgKeyInternalErrorFormat = "fmt.internal_error"
	// MsgKeyDBErrorFormat 表示数据库错误动态详情模板 key。
	MsgKeyDBErrorFormat = "fmt.db_error"
	// MsgKeyUserExistsFormat 表示账号已存在动态详情模板 key。
	MsgKeyUserExistsFormat = "fmt.user_exists"
	// MsgKeyJSONParseFailFormat 表示 JSON 解析失败动态详情模板 key。
	MsgKeyJSONParseFailFormat = "fmt.json_parse_fail"

	// MsgKeyQuerySuccess 表示查询成功的业务文案 key。
	MsgKeyQuerySuccess = "biz.query_success"
	// MsgKeyQueryFail 表示查询失败的业务文案 key。
	MsgKeyQueryFail = "biz.query_fail"
	// MsgKeyBindSuccess 表示绑定成功的业务文案 key。
	MsgKeyBindSuccess = "biz.bind_success"
	// MsgKeyBindFail 表示绑定失败的业务文案 key。
	MsgKeyBindFail = "biz.bind_fail"
	// MsgKeyUnbindSuccess 表示解绑成功的业务文案 key。
	MsgKeyUnbindSuccess = "biz.unbind_success"
	// MsgKeyUnbindFail 表示解绑失败的业务文案 key。
	MsgKeyUnbindFail = "biz.unbind_fail"
	// MsgKeyStatusChangeOK 表示状态修改成功的业务文案 key。
	MsgKeyStatusChangeOK = "biz.status_change_success"
	// MsgKeyStatusChangeFail 表示状态修改失败的业务文案 key。
	MsgKeyStatusChangeFail = "biz.status_change_fail"
	// MsgKeyStatusUnchanged 表示状态未变化无需更新的业务文案 key。
	MsgKeyStatusUnchanged = "biz.status_unchanged"
	// MsgKeyNoFieldsUpdated 表示没有字段需要更新的业务文案 key。
	MsgKeyNoFieldsUpdated = "biz.no_fields_updated"
	// MsgKeyNeedLogin 表示当前会话未登录或已失效的认证文案 key。
	MsgKeyNeedLogin = "auth.need_login"
	// MsgKeyTemplateNotFound 表示缓存或业务模板不存在的文案 key。
	MsgKeyTemplateNotFound = "biz.template_not_found"
	// MsgKeyConfigNotFound 表示系统配置不存在的文案 key。
	MsgKeyConfigNotFound = "biz.config_not_found"
	// MsgKeyConfigVersionConflict 表示系统配置已被并发修改的文案 key。
	MsgKeyConfigVersionConflict = "config.version_conflict"
	// MsgKeyCacheKeyNotFound 表示缓存 key 不存在的文案 key。
	MsgKeyCacheKeyNotFound = "cache.key_not_found"
	// MsgKeyLogoutSuccess 表示登出成功的认证文案 key。
	MsgKeyLogoutSuccess = "auth.logout_success"
	// MsgKeyRoleFetchFail 表示获取后台用户角色失败的文案 key。
	MsgKeyRoleFetchFail = "admin.role_fetch_fail"
	// MsgKeyRolePermFetchFail 表示获取角色权限失败的文案 key。
	MsgKeyRolePermFetchFail = "admin.role_perm_fetch_fail"
	// MsgKeyRoleAlreadyExists 表示角色名称已存在的文案 key。
	MsgKeyRoleAlreadyExists = codes.MsgKeyRoleAlreadyExists
	// MsgKeyRoleExistsFormat 表示角色名称已存在动态详情模板 key。
	MsgKeyRoleExistsFormat = "fmt.role_exists"
	// MsgKeyPermissionAlreadyExists 表示权限标识已存在的文案 key。
	MsgKeyPermissionAlreadyExists = codes.MsgKeyPermissionAlreadyExists
	// MsgKeyPermissionExistsFormat 表示权限标识已存在动态详情模板 key。
	MsgKeyPermissionExistsFormat = "fmt.permission_exists"
	// MsgKeyPermCodeFetchFail 表示获取权限码失败的文案 key。
	MsgKeyPermCodeFetchFail = "admin.perm_code_fetch_fail"
	// MsgKeyAdminInfoFetchFail 表示获取管理员资料失败的文案 key。
	MsgKeyAdminInfoFetchFail = "admin.info_fetch_fail"
	// MsgKeyTokenGenerateFail 表示生成后台 token 失败的文案 key。
	MsgKeyTokenGenerateFail = "auth.token_generate_fail"
	// MsgKeyTokenCacheFail 表示更新缓存 token 失败的文案 key。
	MsgKeyTokenCacheFail = "auth.token_cache_fail"
	// MsgKeyCacheInfoFail 表示缓存资料失败的文案 key。
	MsgKeyCacheInfoFail = "cache.info_fail"
	// MsgKeyAccountPwdInvalid 表示后台账号或密码错误的文案 key。
	MsgKeyAccountPwdInvalid = "auth.account_pwd_invalid"
	// MsgKeyAccountFetchFail 表示获取后台账号失败的文案 key。
	MsgKeyAccountFetchFail = "account.fetch_fail"
	// MsgKeyTaskDisabled 表示任务系统未启用的文案 key。
	MsgKeyTaskDisabled = "task.disabled"
	// MsgKeyTaskEnqueueSuccess 表示任务投递成功的文案 key。
	MsgKeyTaskEnqueueSuccess = "task.enqueue_success"
	// MsgKeyTaskEnqueueFail 表示任务投递失败的文案 key。
	MsgKeyTaskEnqueueFail = "task.enqueue_fail"
	// MsgKeyTaskRunSuccess 表示任务切换为立即执行成功的文案 key。
	MsgKeyTaskRunSuccess = "task.run_success"
	// MsgKeyTaskRunFail 表示任务切换为立即执行失败的文案 key。
	MsgKeyTaskRunFail = "task.run_fail"
	// MsgKeyTaskDeleteSuccess 表示删除任务成功的文案 key。
	MsgKeyTaskDeleteSuccess = "task.delete_success"
	// MsgKeyTaskDeleteFail 表示删除任务失败的文案 key。
	MsgKeyTaskDeleteFail = "task.delete_fail"
	// MsgKeyTaskTriggerSuccess 表示工作流触发成功的文案 key。
	MsgKeyTaskTriggerSuccess = "task.trigger_success"
	// MsgKeyTaskTriggerFail 表示工作流触发失败的文案 key。
	MsgKeyTaskTriggerFail = "task.trigger_fail"
	// MsgKeyTaskQueryFail 表示任务状态查询失败的文案 key。
	MsgKeyTaskQueryFail = "task.query_fail"
	// MsgKeyTaskTypeNotFound 表示任务类型不存在的文案 key。
	MsgKeyTaskTypeNotFound = "task.type_not_found"
	// MsgKeyTaskNotFound 表示任务不存在的文案 key。
	MsgKeyTaskNotFound = "task.not_found"
	// MsgKeyTaskWorkflowNotFound 表示工作流不存在的文案 key。
	MsgKeyTaskWorkflowNotFound = "task.workflow_not_found"
	// MsgKeyTaskQueueNotFound 表示任务队列不存在的文案 key。
	MsgKeyTaskQueueNotFound = "task.queue_not_found"
	// MsgKeyTaskPauseSuccess 表示暂停任务队列成功的文案 key。
	MsgKeyTaskPauseSuccess = "task.pause_success"
	// MsgKeyTaskPauseFail 表示暂停任务队列失败的文案 key。
	MsgKeyTaskPauseFail = "task.pause_fail"
	// MsgKeyTaskResumeSuccess 表示恢复任务队列成功的文案 key。
	MsgKeyTaskResumeSuccess = "task.resume_success"
	// MsgKeyTaskResumeFail 表示恢复任务队列失败的文案 key。
	MsgKeyTaskResumeFail = "task.resume_fail"
	// MsgKeyTaskDuplicate 表示任务重复触发的文案 key。
	MsgKeyTaskDuplicate = "task.duplicate"
	// MsgKeyTaskRegistryTypeRegisteredDesc 表示任务注册表默认任务类型说明 key。
	MsgKeyTaskRegistryTypeRegisteredDesc = "task.registry.type.registered.desc"
	// MsgKeyTaskRegistryTypeDefaultHint 表示任务注册表默认任务类型使用提示 key。
	MsgKeyTaskRegistryTypeDefaultHint = "task.registry.type.default.hint"
	// MsgKeyTaskRegistryTypeWorkflowTriggerDesc 表示工作流触发入口任务说明 key。
	MsgKeyTaskRegistryTypeWorkflowTriggerDesc = "task.registry.type.workflow_trigger.desc"
	// MsgKeyTaskRegistryTypeWorkflowTriggerHint 表示工作流触发入口任务使用提示 key。
	MsgKeyTaskRegistryTypeWorkflowTriggerHint = "task.registry.type.workflow_trigger.hint"
	// MsgKeyTaskRegistryTypeWorkflowNoopDesc 表示工作流空节点任务说明 key。
	MsgKeyTaskRegistryTypeWorkflowNoopDesc = "task.registry.type.workflow_noop.desc"
	// MsgKeyTaskRegistryTypeWorkflowNoopHint 表示工作流空节点任务使用提示 key。
	MsgKeyTaskRegistryTypeWorkflowNoopHint = "task.registry.type.workflow_noop.hint"
	// MsgKeyTaskRegistryTypeCacheRefreshRequestDesc 表示缓存刷新请求任务说明 key。
	MsgKeyTaskRegistryTypeCacheRefreshRequestDesc = "task.registry.type.cache_refresh_request.desc"
	// MsgKeyTaskRegistryTypeCacheRefreshRequestHint 表示缓存刷新请求任务使用提示 key。
	MsgKeyTaskRegistryTypeCacheRefreshRequestHint = "task.registry.type.cache_refresh_request.hint"
	// MsgKeyTaskRegistryTypeCacheRefreshBatchDesc 表示缓存刷新批量任务说明 key。
	MsgKeyTaskRegistryTypeCacheRefreshBatchDesc = "task.registry.type.cache_refresh_batch.desc"
	// MsgKeyTaskRegistryTypeCacheRefreshBatchHint 表示缓存刷新批量任务使用提示 key。
	MsgKeyTaskRegistryTypeCacheRefreshBatchHint = "task.registry.type.cache_refresh_batch.hint"
	// MsgKeyTaskRegistryTypeArchiveExecuteDesc 表示归档执行任务说明 key。
	MsgKeyTaskRegistryTypeArchiveExecuteDesc = "task.registry.type.archive_execute.desc"
	// MsgKeyTaskRegistryTypeArchiveExecuteHint 表示归档执行任务使用提示 key。
	MsgKeyTaskRegistryTypeArchiveExecuteHint = "task.registry.type.archive_execute.hint"
	// MsgKeyTaskRegistryTypeDailySummaryDesc 表示任务运行日报任务说明 key。
	MsgKeyTaskRegistryTypeDailySummaryDesc = "task.registry.type.daily_summary.desc"
	// MsgKeyTaskRegistryTypeDailySummaryHint 表示任务运行日报任务使用提示 key。
	MsgKeyTaskRegistryTypeDailySummaryHint = "task.registry.type.daily_summary.hint"
	// MsgKeyTaskRegistryTypeAdminExportDesc 表示管理员导出任务说明 key。
	MsgKeyTaskRegistryTypeAdminExportDesc = "task.registry.type.admin_export.desc"
	// MsgKeyTaskRegistryTypeAdminExportHint 表示管理员导出任务使用提示 key。
	MsgKeyTaskRegistryTypeAdminExportHint = "task.registry.type.admin_export.hint"
	// MsgKeyTaskRegistryTypeUserExportDesc 表示前台用户导出任务说明 key。
	MsgKeyTaskRegistryTypeUserExportDesc = "task.registry.type.user_export.desc"
	// MsgKeyTaskRegistryTypeUserExportHint 表示前台用户导出任务使用提示 key。
	MsgKeyTaskRegistryTypeUserExportHint = "task.registry.type.user_export.hint"
	// MsgKeyTaskRegistryTypeUserTagDesc 表示用户标签任务说明 key。
	MsgKeyTaskRegistryTypeUserTagDesc = "task.registry.type.user_tag.desc"
	// MsgKeyTaskRegistryTypeUserTagHint 表示用户标签任务使用提示 key。
	MsgKeyTaskRegistryTypeUserTagHint = "task.registry.type.user_tag.hint"
	// MsgKeyTaskRegistryWorkflowDefaultHint 表示任务注册表默认工作流使用提示 key。
	MsgKeyTaskRegistryWorkflowDefaultHint = "task.registry.workflow.default.hint"
	// MsgKeyTaskRegistryWorkflowCacheRefreshDesc 表示缓存刷新工作流说明 key。
	MsgKeyTaskRegistryWorkflowCacheRefreshDesc = "task.registry.workflow.cache_refresh.desc"
	// MsgKeyTaskRegistryWorkflowCacheRefreshHint 表示缓存刷新工作流使用提示 key。
	MsgKeyTaskRegistryWorkflowCacheRefreshHint = "task.registry.workflow.cache_refresh.hint"
	// MsgKeyTaskRegistryWorkflowArchiveRunDesc 表示归档工作流说明 key。
	MsgKeyTaskRegistryWorkflowArchiveRunDesc = "task.registry.workflow.archive_run.desc"
	// MsgKeyTaskRegistryWorkflowArchiveRunHint 表示归档工作流使用提示 key。
	MsgKeyTaskRegistryWorkflowArchiveRunHint = "task.registry.workflow.archive_run.hint"
	// MsgKeyTaskRegistryWorkflowDailySummaryDesc 表示任务运行日报工作流说明 key。
	MsgKeyTaskRegistryWorkflowDailySummaryDesc = "task.registry.workflow.daily_summary.desc"
	// MsgKeyTaskRegistryWorkflowDailySummaryHint 表示任务运行日报工作流使用提示 key。
	MsgKeyTaskRegistryWorkflowDailySummaryHint = "task.registry.workflow.daily_summary.hint"
	// MsgKeyTaskRegistryWorkflowUserTagFullDesc 表示用户标签全量工作流说明 key。
	MsgKeyTaskRegistryWorkflowUserTagFullDesc = "task.registry.workflow.user_tag_full.desc"
	// MsgKeyTaskRegistryWorkflowUserTagFullHint 表示用户标签全量工作流使用提示 key。
	MsgKeyTaskRegistryWorkflowUserTagFullHint = "task.registry.workflow.user_tag_full.hint"
	// MsgKeyTaskRegistryWorkflowUserTagDeltaDesc 表示用户标签增量工作流说明 key。
	MsgKeyTaskRegistryWorkflowUserTagDeltaDesc = "task.registry.workflow.user_tag_delta.desc"
	// MsgKeyTaskRegistryWorkflowUserTagDeltaHint 表示用户标签增量工作流使用提示 key。
	MsgKeyTaskRegistryWorkflowUserTagDeltaHint = "task.registry.workflow.user_tag_delta.hint"
	// MsgKeyTaskRegistryWorkflowUserTagTargetedDesc 表示用户标签指定用户补算工作流说明 key。
	MsgKeyTaskRegistryWorkflowUserTagTargetedDesc = "task.registry.workflow.user_tag_targeted.desc"
	// MsgKeyTaskRegistryWorkflowUserTagTargetedHint 表示用户标签指定用户补算工作流使用提示 key。
	MsgKeyTaskRegistryWorkflowUserTagTargetedHint = "task.registry.workflow.user_tag_targeted.hint"
	// MsgKeyTaskRegistryWorkflowUserTagRecalculateDesc 表示用户标签重算工作流说明 key。
	MsgKeyTaskRegistryWorkflowUserTagRecalculateDesc = "task.registry.workflow.user_tag_recalculate.desc"
	// MsgKeyTaskRegistryWorkflowUserTagRecalculateHint 表示用户标签重算工作流使用提示 key。
	MsgKeyTaskRegistryWorkflowUserTagRecalculateHint = "task.registry.workflow.user_tag_recalculate.hint"
	// MsgKeyTaskRegistryWorkflowUserTagRuntimeCleanupDesc 表示用户标签运行期清理工作流说明 key。
	MsgKeyTaskRegistryWorkflowUserTagRuntimeCleanupDesc = "task.registry.workflow.user_tag_runtime_cleanup.desc"
	// MsgKeyTaskRegistryWorkflowUserTagRuntimeCleanupHint 表示用户标签运行期清理工作流使用提示 key。
	MsgKeyTaskRegistryWorkflowUserTagRuntimeCleanupHint = "task.registry.workflow.user_tag_runtime_cleanup.hint"
	// MsgKeyTaskRegistryWorkflowUserTagOutboxRetryDesc 表示用户标签 outbox 重派工作流说明 key。
	MsgKeyTaskRegistryWorkflowUserTagOutboxRetryDesc = "task.registry.workflow.user_tag_outbox_retry.desc"
	// MsgKeyTaskRegistryWorkflowUserTagOutboxRetryHint 表示用户标签 outbox 重派工作流使用提示 key。
	MsgKeyTaskRegistryWorkflowUserTagOutboxRetryHint = "task.registry.workflow.user_tag_outbox_retry.hint"
	// MsgKeyTaskReportRetentionWarning 表示任务运行日报 completed 保留期不足提示 key。
	MsgKeyTaskReportRetentionWarning = "task.report.retention_warning"
	// MsgKeyHotReloadFailed 表示配置热加载失败状态说明 key。
	MsgKeyHotReloadFailed = "hot_reload.failed"
	// MsgKeyHotReloadFingerprintInitFailed 表示初始化配置指纹失败状态说明 key。
	MsgKeyHotReloadFingerprintInitFailed = "hot_reload.fingerprint_init_failed"
	// MsgKeyHotReloadFingerprintReadFailed 表示读取配置指纹失败状态说明 key。
	MsgKeyHotReloadFingerprintReadFailed = "hot_reload.fingerprint_read_failed"
	// MsgKeyHotReloadFileStatusReadFailed 表示读取配置文件状态失败说明 key。
	MsgKeyHotReloadFileStatusReadFailed = "hot_reload.file_status_read_failed"
	// MsgKeyHotReloadNotBound 表示配置热加载未绑定文件说明 key。
	MsgKeyHotReloadNotBound = "hot_reload.not_bound"
	// MsgKeyHotReloadCancelled 表示配置热加载被取消说明 key。
	MsgKeyHotReloadCancelled = "hot_reload.cancelled"
	// MsgKeyHotReloadRuntimeConfigLoadFailed 表示加载运行配置失败说明 key。
	MsgKeyHotReloadRuntimeConfigLoadFailed = "hot_reload.runtime_config_load_failed"
	// MsgKeyHotReloadManualFailed 表示手动触发热加载失败说明 key。
	MsgKeyHotReloadManualFailed = "hot_reload.manual_failed"
	// MsgKeyHotReloadSuccess 表示配置热加载成功说明 key。
	MsgKeyHotReloadSuccess = "hot_reload.success"
	// MsgKeyHotReloadSuccessRestart 表示热加载成功但需重启说明 key。
	MsgKeyHotReloadSuccessRestart = "hot_reload.success_restart"
	// MsgKeyHotReloadUnchanged 表示配置无变化说明 key。
	MsgKeyHotReloadUnchanged = "hot_reload.unchanged"
	// MsgKeyHotReloadWatcherNotStarted 表示热加载 watcher 未启动说明 key。
	MsgKeyHotReloadWatcherNotStarted = "hot_reload.watcher_not_started"
	// MsgKeyHotReloadWatcherRunning 表示热加载 watcher 运行中说明 key。
	MsgKeyHotReloadWatcherRunning = "hot_reload.watcher_running"
	// MsgKeyHotReloadWatcherClosed 表示热加载 watcher 已关闭说明 key。
	MsgKeyHotReloadWatcherClosed = "hot_reload.watcher_closed"
	// MsgKeyHotReloadWatcherStopped 表示热加载 watcher 已停止说明 key。
	MsgKeyHotReloadWatcherStopped = "hot_reload.watcher_stopped"
	// MsgKeySchedulerDisabled 表示周期调度器未启用说明 key。
	MsgKeySchedulerDisabled = "scheduler.disabled"
	// MsgKeySchedulerTaskDisabled 表示任务系统关闭导致调度器未启动说明 key。
	MsgKeySchedulerTaskDisabled = "scheduler.task_disabled"
	// MsgKeySchedulerNotStarted 表示周期调度器尚未启动说明 key。
	MsgKeySchedulerNotStarted = "scheduler.not_started"
	// MsgKeySchedulerNoPeriodicTask 表示未配置有效周期任务说明 key。
	MsgKeySchedulerNoPeriodicTask = "scheduler.no_periodic_task"
	// MsgKeySchedulerAlreadyRunning 表示调度器已在运行说明 key。
	MsgKeySchedulerAlreadyRunning = "scheduler.already_running"
	// MsgKeySchedulerElectionStarted 表示周期调度器开始竞争 leader 说明 key。
	MsgKeySchedulerElectionStarted = "scheduler.election_started"
	// MsgKeySchedulerStopped 表示周期调度器已停止说明 key。
	MsgKeySchedulerStopped = "scheduler.stopped"
	// MsgKeySchedulerLeaderAcquired 表示周期调度器已获取 leader 说明 key。
	MsgKeySchedulerLeaderAcquired = "scheduler.leader_acquired"
	// MsgKeySchedulerLeaderReleased 表示周期调度器释放 leader 后等待重试说明 key。
	MsgKeySchedulerLeaderReleased = "scheduler.leader_released"
	// MsgKeySchedulerSyncSuccess 表示周期任务配置同步成功说明 key。
	MsgKeySchedulerSyncSuccess = "scheduler.sync_success"
	// MsgKeySchedulerSyncFailed 表示周期任务配置同步失败说明 key。
	MsgKeySchedulerSyncFailed = "scheduler.sync_failed"
	// MsgKeySchedulerHeartbeatOK 表示周期调度器 leader 心跳正常说明 key。
	MsgKeySchedulerHeartbeatOK = "scheduler.heartbeat_ok"
	// MsgKeySchedulerEnqueueFailed 表示周期任务入队失败说明 key。
	MsgKeySchedulerEnqueueFailed = "scheduler.enqueue_failed"
	// MsgKeySchedulerBacklogExceeded 表示周期任务队列积压超过阈值说明 key。
	MsgKeySchedulerBacklogExceeded = "scheduler.backlog_exceeded"
	// MsgKeyAPIRuntimeNotConfigured 表示 API 内网运行态未配置说明 key。
	MsgKeyAPIRuntimeNotConfigured = "api_runtime.not_configured"
	// MsgKeyAPIRuntimeStatusFetched 表示 API 热加载状态已获取说明 key。
	MsgKeyAPIRuntimeStatusFetched = "api_runtime.status_fetched"
	// MsgKeyAPIRuntimeItemsFetched 表示 API 运行态配置项已获取说明 key。
	MsgKeyAPIRuntimeItemsFetched = "api_runtime.items_fetched"
	// MsgKeyAPIRuntimeReloadTriggered 表示 API 热加载已触发说明 key。
	MsgKeyAPIRuntimeReloadTriggered = "api_runtime.reload_triggered"
	// MsgKeyAPIRuntimeSyncSuccess 表示 API 运行态同步成功说明 key。
	MsgKeyAPIRuntimeSyncSuccess = "api_runtime.sync_success"
	// MsgKeyAPIRuntimeUserCreateNoCache 表示新增用户无需同步 API 运行态说明 key。
	MsgKeyAPIRuntimeUserCreateNoCache = "api_runtime.user_create_no_cache"
	// MsgKeyAPIRuntimeProfileUnchanged 表示资料未变更无需同步 API 运行态说明 key。
	MsgKeyAPIRuntimeProfileUnchanged = "api_runtime.profile_unchanged"
	// MsgKeyAPIRuntimeStatusUnchanged 表示状态未变更无需同步 API 运行态说明 key。
	MsgKeyAPIRuntimeStatusUnchanged = "api_runtime.status_unchanged"
	// MsgKeyAPIRuntimeProfileSyncWarning 表示资料已更新但 API 缓存同步失败说明 key。
	MsgKeyAPIRuntimeProfileSyncWarning = "api_runtime.profile_sync_warning"
	// MsgKeyAPIRuntimeStatusSyncWarning 表示状态已更新但 API 缓存同步失败说明 key。
	MsgKeyAPIRuntimeStatusSyncWarning = "api_runtime.status_sync_warning"
	// MsgKeyCollectorRunPartialFailed 表示 Collector 失败账本重试仍有失败事件说明 key。
	MsgKeyCollectorRunPartialFailed = "collector.run_partial_failed"
	// MsgKeyCollectorRunSuccess 表示 Collector 执行完成说明 key。
	MsgKeyCollectorRunSuccess = "collector.run_success"
	// MsgKeyAdminExportFileExpired 表示导出文件失效说明 key。
	MsgKeyAdminExportFileExpired = "admin_export.file_expired"
	// MsgKeyUserExportFileExpired 表示前台用户导出文件失效说明 key。
	MsgKeyUserExportFileExpired = "user_export.file_expired"
	// MsgKeyUserTagRecalculateStarted 表示用户标签重算任务已启动说明 key。
	MsgKeyUserTagRecalculateStarted = "user_tag.recalculate_started"
	// MsgKeyDependencyUnavailable 表示核心依赖不可用的文案 key。
	MsgKeyDependencyUnavailable = codes.MsgKeyDependencyUnavailable
	// MsgKeyMySQLUnavailable 表示 MySQL 不可用的文案 key。
	MsgKeyMySQLUnavailable = codes.MsgKeyMySQLUnavailable
	// MsgKeyRedisUnavailable 表示 Redis 不可用的文案 key。
	MsgKeyRedisUnavailable = codes.MsgKeyRedisUnavailable
	// MsgKeyClickHouseUnavailable 表示 ClickHouse 不可用的文案 key。
	MsgKeyClickHouseUnavailable = codes.MsgKeyClickHouseUnavailable
	// MsgKeyKafkaUnavailable 表示 Kafka 不可用的文案 key。
	MsgKeyKafkaUnavailable = codes.MsgKeyKafkaUnavailable
	// MsgKeyTaskQueueUnavailable 表示任务队列不可用的文案 key。
	MsgKeyTaskQueueUnavailable = codes.MsgKeyTaskQueueUnavailable
	// MsgKeyCollectorUnavailable 表示 Collector 不可用的文案 key。
	MsgKeyCollectorUnavailable = codes.MsgKeyCollectorUnavailable
	// MsgKeyUserTagLeaseReleaseSuccess 表示用户标签工作流互斥租约释放成功的文案 key。
	MsgKeyUserTagLeaseReleaseSuccess = "user_tag.lease_release_success"
	// MsgKeyUserTagLeaseReleaseFail 表示用户标签工作流互斥租约释放失败的文案 key。
	MsgKeyUserTagLeaseReleaseFail = codes.MsgKeyUserTagLeaseReleaseFail
	// MsgKeyUserTagLeaseNotFound 表示用户标签工作流互斥租约不存在的文案 key。
	MsgKeyUserTagLeaseNotFound = codes.MsgKeyUserTagLeaseNotFound
	// MsgKeyUserTagLeaseOwnerMismatch 表示用户标签工作流互斥租约 owner 不匹配的文案 key。
	MsgKeyUserTagLeaseOwnerMismatch = codes.MsgKeyUserTagLeaseOwnerMismatch
)
