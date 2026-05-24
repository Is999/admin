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
	// MsgKeyContinue 表示 HTTP Continue 语义的文案 key。
	MsgKeyContinue = codes.MsgKeyContinue
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
