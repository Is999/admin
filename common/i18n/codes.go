package i18n

import "admin/common/codes"

// codeToMessageKey 维护业务响应码到多语言 key 的映射。
var codeToMessageKey = map[int]string{
	codes.Undefined:          MsgKeyUndefined,          // 未定义使用该多语言 key。
	codes.Success:            MsgKeySuccess,            // 成功使用该多语言 key。
	codes.Fail:               MsgKeyFail,               // 失败使用该多语言 key。
	codes.CheckMFABind:       MsgKeyCheckMFABind,       // 需要先绑定并启用 MFA使用该多语言 key。
	codes.CheckMFACode:       MsgKeyCheckMFA,           // 需要校验 MFA 设备验证码使用该多语言 key。
	codes.CheckPasswordReset: MsgKeyCheckPasswordReset, // 需要先修改登录密码使用该多语言 key。

	codes.Continue:     MsgKeyContinue,     // 继续使用该多语言 key。
	codes.OK:           MsgKeyOK,           // 请求成功使用该多语言 key。
	codes.BadRequest:   MsgKeyBadRequest,   // 错误请求使用该多语言 key。
	codes.Unauthorized: MsgKeyUnauthorized, // 未授权使用该多语言 key。
	codes.Forbidden:    MsgKeyForbidden,    // 禁止访问使用该多语言 key。
	codes.NotFound:     MsgKeyNotFound,     // 未找到使用该多语言 key。
	codes.ServerError:  MsgKeyServerError,  // 服务器错误使用该多语言 key。
	codes.ServiceBusy:  MsgKeyServiceBusy,  // 服务繁忙使用该多语言 key。
	codes.Timeout:      MsgKeyTimeout,      // 请求超时使用该多语言 key。

	codes.ParamError:    MsgKeyParamError,    // 参数错误使用该多语言 key。
	codes.AuthFailed:    MsgKeyAuthFailed,    // 验证失败使用该多语言 key。
	codes.RateLimit:     MsgKeyRateLimit,     // 请求过多，限流使用该多语言 key。
	codes.InternalError: MsgKeyInternalError, // 内部错误使用该多语言 key。
	codes.DBError:       MsgKeyDBError,       // 数据库错误使用该多语言 key。
	codes.CheckMFAAgain: MsgKeyMFAExpired,    // MFA 校验已过期，需要重新验证使用该多语言 key。

	codes.CreateSuccess: MsgKeyCreateSuccess, // 创建成功使用该多语言 key。
	codes.CreateFail:    MsgKeyCreateFail,    // 创建失败使用该多语言 key。
	codes.AddSuccess:    MsgKeyAddSuccess,    // 添加成功使用该多语言 key。
	codes.AddFail:       MsgKeyAddFail,       // 添加失败使用该多语言 key。
	codes.SaveSuccess:   MsgKeySaveSuccess,   // 保存成功使用该多语言 key。
	codes.SaveFail:      MsgKeySaveFail,      // 保存失败使用该多语言 key。
	codes.UpdateSuccess: MsgKeyUpdateSuccess, // 更新成功使用该多语言 key。
	codes.UpdateFail:    MsgKeyUpdateFail,    // 更新失败使用该多语言 key。
	codes.DeleteSuccess: MsgKeyDeleteSuccess, // 删除成功使用该多语言 key。
	codes.DeleteFail:    MsgKeyDeleteFail,    // 删除失败使用该多语言 key。
	codes.FetchSuccess:  MsgKeyFetchSuccess,  // 获取成功使用该多语言 key。
	codes.FetchFail:     MsgKeyFetchFail,     // 获取失败使用该多语言 key。

	codes.UserNotFound:                 MsgKeyUserNotFound,            // 用户不存在使用该多语言 key。
	codes.InvalidPassword:              MsgKeyInvalidPassword,         // 密码错误使用该多语言 key。
	codes.UserAlreadyExists:            MsgKeyUserAlreadyExists,       // 用户已存在使用该多语言 key。
	codes.UserDisabled:                 MsgKeyUserDisabled,            // 账号被禁用使用该多语言 key。
	codes.InvalidCaptcha:               MsgKeyInvalidCaptcha,          // 登录验证码错误或已过期使用该多语言 key。
	codes.InvalidMFACode:               MsgKeyMFACodeInvalid,          // MFA动态验证码错误使用该多语言 key。
	codes.AdminRoleAlreadyExists:       MsgKeyRoleAlreadyExists,       // 后台角色名称已存在使用该多语言 key。
	codes.AdminPermissionAlreadyExists: MsgKeyPermissionAlreadyExists, // 后台权限标识已存在使用该多语言 key。

	codes.DependencyUnavailable: MsgKeyDependencyUnavailable, // ready 检查或核心流程发现外部依赖不可用使用该多语言 key。
	codes.MySQLUnavailable:      MsgKeyMySQLUnavailable,      // MySQL 连接不可用使用该多语言 key。
	codes.RedisUnavailable:      MsgKeyRedisUnavailable,      // Redis 连接不可用使用该多语言 key。
	codes.ClickHouseUnavailable: MsgKeyClickHouseUnavailable, // ClickHouse 连接不可用使用该多语言 key。
	codes.KafkaUnavailable:      MsgKeyKafkaUnavailable,      // Kafka 生产链路不可用使用该多语言 key。
	codes.TaskQueueUnavailable:  MsgKeyTaskQueueUnavailable,  // 任务队列组件未就绪或不可用使用该多语言 key。
	codes.CollectorUnavailable:  MsgKeyCollectorUnavailable,  // Collector 组件未就绪或不可用使用该多语言 key。

	codes.UserTagWorkflowLeaseNotFound:      MsgKeyUserTagLeaseNotFound,      // 用户标签工作流互斥租约不存在，可能已经过期或已释放使用该多语言 key。
	codes.UserTagWorkflowLeaseOwnerMismatch: MsgKeyUserTagLeaseOwnerMismatch, // 用户标签工作流互斥租约 owner 与请求 workflowID/mode 不一致使用该多语言 key。
	codes.UserTagWorkflowLeaseReleaseFailed: MsgKeyUserTagLeaseReleaseFail,   // 用户标签工作流互斥租约释放过程发生未知失败使用该多语言 key。
}
