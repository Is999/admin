package codes

// 默认响应文案 key，按业务码契约分段维护，i18n 包据此加载语言包。
const (
	// MsgKeyUndefined 表示未知业务状态的通用文案 key。
	MsgKeyUndefined = "common.undefined"
	// MsgKeySuccess 表示通用成功响应文案 key。
	MsgKeySuccess = "common.success"
	// MsgKeyFail 表示通用失败响应文案 key。
	MsgKeyFail = "common.fail"
	// MsgKeyCheckMFABind 表示账号需要先绑定并启用 MFA 的标准提示 key。
	MsgKeyCheckMFABind = "common.check_mfa_bind"
	// MsgKeyCheckMFA 表示账号需要完成 MFA 动态验证码校验的标准提示 key。
	MsgKeyCheckMFA = "common.check_mfa"
	// MsgKeyCheckPasswordReset 表示账号需要先修改登录密码的标准提示 key。
	MsgKeyCheckPasswordReset = "common.check_password_reset"

	// MsgKeyOK 表示 HTTP OK 语义的文案 key。
	MsgKeyOK = "http.ok"
	// MsgKeyBadRequest 表示请求参数或格式错误的 HTTP 文案 key。
	MsgKeyBadRequest = "http.bad_request"
	// MsgKeyUnauthorized 表示未授权访问的 HTTP 文案 key。
	MsgKeyUnauthorized = "http.unauthorized"
	// MsgKeyForbidden 表示无权限访问的 HTTP 文案 key。
	MsgKeyForbidden = "http.forbidden"
	// MsgKeyNotFound 表示资源未找到的 HTTP 文案 key。
	MsgKeyNotFound = "http.not_found"
	// MsgKeyServerError 表示服务端异常的 HTTP 文案 key。
	MsgKeyServerError = "http.server_error"
	// MsgKeyServiceBusy 表示服务繁忙或依赖不可用的 HTTP 文案 key。
	MsgKeyServiceBusy = "http.service_busy"
	// MsgKeyTimeout 表示请求超时的 HTTP 文案 key。
	MsgKeyTimeout = "http.timeout"

	// MsgKeyParamError 表示通用参数错误的业务文案 key。
	MsgKeyParamError = "biz.param_error"
	// MsgKeyAuthFailed 表示认证或二次校验失败的业务文案 key。
	MsgKeyAuthFailed = "biz.auth_failed"
	// MsgKeyRateLimit 表示触发限流保护的业务文案 key。
	MsgKeyRateLimit = "biz.rate_limit"
	// MsgKeyInternalError 表示内部错误的业务文案 key。
	MsgKeyInternalError = "biz.internal_error"
	// MsgKeyDBError 表示数据库错误的业务文案 key。
	MsgKeyDBError = "biz.db_error"
	// MsgKeyMFAExpired 表示 MFA 校验过期后需要重新验证的认证文案 key。
	MsgKeyMFAExpired = "auth.mfa_expired"

	// MsgKeyCreateSuccess 表示创建成功的业务文案 key。
	MsgKeyCreateSuccess = "biz.create_success"
	// MsgKeyCreateFail 表示创建失败的业务文案 key。
	MsgKeyCreateFail = "biz.create_fail"
	// MsgKeyAddSuccess 表示新增成功的业务文案 key。
	MsgKeyAddSuccess = "biz.add_success"
	// MsgKeyAddFail 表示新增失败的业务文案 key。
	MsgKeyAddFail = "biz.add_fail"
	// MsgKeySaveSuccess 表示保存成功的业务文案 key。
	MsgKeySaveSuccess = "biz.save_success"
	// MsgKeySaveFail 表示保存失败的业务文案 key。
	MsgKeySaveFail = "biz.save_fail"
	// MsgKeyUpdateSuccess 表示更新成功的业务文案 key。
	MsgKeyUpdateSuccess = "biz.update_success"
	// MsgKeyUpdateFail 表示更新失败的业务文案 key。
	MsgKeyUpdateFail = "biz.update_fail"
	// MsgKeyDeleteSuccess 表示删除成功的业务文案 key。
	MsgKeyDeleteSuccess = "biz.delete_success"
	// MsgKeyDeleteFail 表示删除失败的业务文案 key。
	MsgKeyDeleteFail = "biz.delete_fail"
	// MsgKeyFetchSuccess 表示获取成功的业务文案 key。
	MsgKeyFetchSuccess = "biz.fetch_success"
	// MsgKeyFetchFail 表示获取失败的业务文案 key。
	MsgKeyFetchFail = "biz.fetch_fail"

	// MsgKeyUserNotFound 表示后台账号不存在的文案 key。
	MsgKeyUserNotFound = "user.not_found"
	// MsgKeyInvalidPassword 表示后台账号密码错误的文案 key。
	MsgKeyInvalidPassword = "user.invalid_password"
	// MsgKeyUserAlreadyExists 表示后台账号已存在的文案 key。
	MsgKeyUserAlreadyExists = "user.already_exists"
	// MsgKeyUserDisabled 表示后台账号被禁用的文案 key。
	MsgKeyUserDisabled = "user.disabled"
	// MsgKeyInvalidCaptcha 表示登录验证码错误或过期的文案 key。
	MsgKeyInvalidCaptcha = "auth.invalid_captcha"
	// MsgKeyMFACodeInvalid 表示 MFA 动态验证码错误的认证文案 key。
	MsgKeyMFACodeInvalid = "auth.mfa_code_invalid"
	// MsgKeyRoleAlreadyExists 表示角色名称已存在的文案 key。
	MsgKeyRoleAlreadyExists = "admin.role_already_exists"
	// MsgKeyPermissionAlreadyExists 表示权限标识已存在的文案 key。
	MsgKeyPermissionAlreadyExists = "admin.permission_already_exists"

	// MsgKeyDependencyUnavailable 表示核心依赖不可用的文案 key。
	MsgKeyDependencyUnavailable = "dependency.unavailable"
	// MsgKeyMySQLUnavailable 表示 MySQL 不可用的文案 key。
	MsgKeyMySQLUnavailable = "dependency.mysql_unavailable"
	// MsgKeyRedisUnavailable 表示 Redis 不可用的文案 key。
	MsgKeyRedisUnavailable = "dependency.redis_unavailable"
	// MsgKeyClickHouseUnavailable 表示 ClickHouse 不可用的文案 key。
	MsgKeyClickHouseUnavailable = "dependency.clickhouse_unavailable"
	// MsgKeyKafkaUnavailable 表示 Kafka 不可用的文案 key。
	MsgKeyKafkaUnavailable = "dependency.kafka_unavailable"
	// MsgKeyTaskQueueUnavailable 表示任务队列不可用的文案 key。
	MsgKeyTaskQueueUnavailable = "dependency.task_queue_unavailable"
	// MsgKeyCollectorUnavailable 表示 Collector 不可用的文案 key。
	MsgKeyCollectorUnavailable = "dependency.collector_unavailable"

	// MsgKeyUserTagLeaseNotFound 表示用户标签工作流互斥租约不存在的文案 key。
	MsgKeyUserTagLeaseNotFound = "user_tag.lease_not_found"
	// MsgKeyUserTagLeaseOwnerMismatch 表示用户标签工作流互斥租约 owner 不匹配的文案 key。
	MsgKeyUserTagLeaseOwnerMismatch = "user_tag.lease_owner_mismatch"
	// MsgKeyUserTagLeaseReleaseFail 表示释放用户标签工作流互斥租约失败的文案 key。
	MsgKeyUserTagLeaseReleaseFail = "user_tag.lease_release_fail"
)

const (
	// statusTooManyRequests 表示触发限流保护时建议返回的 HTTP 状态码。
	statusTooManyRequests = 429
)

// CodeContract 描述业务码的默认响应契约。
type CodeContract struct {
	Code       int    // Code 是唯一业务码。
	HTTPStatus int    // HTTPStatus 是该业务码建议返回的 HTTP 状态码。
	Success    bool   // Success 表示统一响应是否按成功结果处理。
	MessageKey string // MessageKey 是默认多语言文案 key。
}

// codeSpec 是内部响应码契约源，元素按通用、认证、后台系统、依赖和用户标签分段维护。
type codeSpec struct {
	code       int    // code 是业务响应码。
	httpStatus int    // httpStatus 是默认 HTTP 状态码。
	success    bool   // success 表示该业务码是否按成功响应处理。
	messageKey string // messageKey 是默认多语言文案 key。
}

// defaultCodeSpecs 是业务码默认契约源，派生成功码集合、HTTP 状态和默认文案 key。
var defaultCodeSpecs = []codeSpec{
	{code: Undefined, httpStatus: ServerError, messageKey: MsgKeyUndefined},                                        // 未定义业务码按服务端异常和未知状态文案处理。
	{code: Success, httpStatus: OK, success: true, messageKey: MsgKeySuccess},                                      // 通用成功码按成功响应处理。
	{code: Fail, httpStatus: ServerError, messageKey: MsgKeyFail},                                                  // 通用失败码按服务端异常处理。
	{code: CheckMFABind, httpStatus: OK, messageKey: MsgKeyCheckMFABind},                                           // 需要先绑定并启用 MFA 时返回业务成功链路。
	{code: CheckMFACode, httpStatus: OK, messageKey: MsgKeyCheckMFA},                                               // 需要校验 MFA 设备验证码时返回业务成功链路。
	{code: CheckPasswordReset, httpStatus: OK, messageKey: MsgKeyCheckPasswordReset},                               // 需要先修改登录密码时返回业务成功链路。
	{code: CheckMFAAgain, httpStatus: BadRequest, messageKey: MsgKeyMFAExpired},                                    // MFA 校验过期按客户端参数错误处理。
	{code: OK, httpStatus: OK, success: true, messageKey: MsgKeyOK},                                                // HTTP OK 语义按成功响应处理。
	{code: BadRequest, httpStatus: BadRequest, messageKey: MsgKeyBadRequest},                                       // 错误请求返回 HTTP 400。
	{code: Unauthorized, httpStatus: Unauthorized, messageKey: MsgKeyUnauthorized},                                 // 未授权返回 HTTP 401。
	{code: Forbidden, httpStatus: Forbidden, messageKey: MsgKeyForbidden},                                          // 禁止访问返回 HTTP 403。
	{code: NotFound, httpStatus: NotFound, messageKey: MsgKeyNotFound},                                             // 资源不存在返回 HTTP 404。
	{code: ServerError, httpStatus: ServerError, messageKey: MsgKeyServerError},                                    // 服务端异常返回 HTTP 500。
	{code: ServiceBusy, httpStatus: ServiceBusy, messageKey: MsgKeyServiceBusy},                                    // 服务繁忙返回 HTTP 503。
	{code: Timeout, httpStatus: Timeout, messageKey: MsgKeyTimeout},                                                // 请求超时返回 HTTP 504。
	{code: ParamError, httpStatus: BadRequest, messageKey: MsgKeyParamError},                                       // 参数错误返回 HTTP 400。
	{code: AuthFailed, httpStatus: Unauthorized, messageKey: MsgKeyAuthFailed},                                     // 验证失败返回 HTTP 401。
	{code: RateLimit, httpStatus: statusTooManyRequests, messageKey: MsgKeyRateLimit},                              // 请求过多返回 HTTP 429。
	{code: InternalError, httpStatus: ServerError, messageKey: MsgKeyInternalError},                                // 内部错误返回 HTTP 500。
	{code: DBError, httpStatus: ServerError, messageKey: MsgKeyDBError},                                            // 数据库错误返回 HTTP 500。
	{code: CreateSuccess, httpStatus: OK, success: true, messageKey: MsgKeyCreateSuccess},                          // 创建成功按成功响应处理。
	{code: CreateFail, httpStatus: ServerError, messageKey: MsgKeyCreateFail},                                      // 创建失败返回 HTTP 500。
	{code: AddSuccess, httpStatus: OK, success: true, messageKey: MsgKeyAddSuccess},                                // 添加成功按成功响应处理。
	{code: AddFail, httpStatus: ServerError, messageKey: MsgKeyAddFail},                                            // 添加失败返回 HTTP 500。
	{code: SaveSuccess, httpStatus: OK, success: true, messageKey: MsgKeySaveSuccess},                              // 保存成功按成功响应处理。
	{code: SaveFail, httpStatus: ServerError, messageKey: MsgKeySaveFail},                                          // 保存失败返回 HTTP 500。
	{code: UpdateSuccess, httpStatus: OK, success: true, messageKey: MsgKeyUpdateSuccess},                          // 更新成功按成功响应处理。
	{code: UpdateFail, httpStatus: ServerError, messageKey: MsgKeyUpdateFail},                                      // 更新失败返回 HTTP 500。
	{code: DeleteSuccess, httpStatus: OK, success: true, messageKey: MsgKeyDeleteSuccess},                          // 删除成功按成功响应处理。
	{code: DeleteFail, httpStatus: ServerError, messageKey: MsgKeyDeleteFail},                                      // 删除失败返回 HTTP 500。
	{code: FetchSuccess, httpStatus: OK, success: true, messageKey: MsgKeyFetchSuccess},                            // 获取成功按成功响应处理。
	{code: FetchFail, httpStatus: ServerError, messageKey: MsgKeyFetchFail},                                        // 获取失败返回 HTTP 500。
	{code: UserNotFound, httpStatus: NotFound, messageKey: MsgKeyUserNotFound},                                     // 用户不存在返回 HTTP 404。
	{code: InvalidPassword, httpStatus: BadRequest, messageKey: MsgKeyInvalidPassword},                             // 密码错误返回 HTTP 400。
	{code: UserAlreadyExists, httpStatus: BadRequest, messageKey: MsgKeyUserAlreadyExists},                         // 用户已存在返回 HTTP 400。
	{code: UserDisabled, httpStatus: Unauthorized, messageKey: MsgKeyUserDisabled},                                 // 账号禁用返回 HTTP 401。
	{code: InvalidCaptcha, httpStatus: BadRequest, messageKey: MsgKeyInvalidCaptcha},                               // 验证码错误返回 HTTP 400。
	{code: InvalidMFACode, httpStatus: BadRequest, messageKey: MsgKeyMFACodeInvalid},                               // MFA 动态验证码错误返回 HTTP 400。
	{code: AdminRoleAlreadyExists, httpStatus: BadRequest, messageKey: MsgKeyRoleAlreadyExists},                    // 后台角色名称已存在返回 HTTP 400。
	{code: AdminPermissionAlreadyExists, httpStatus: BadRequest, messageKey: MsgKeyPermissionAlreadyExists},        // 后台权限标识已存在返回 HTTP 400。
	{code: DependencyUnavailable, httpStatus: ServiceBusy, messageKey: MsgKeyDependencyUnavailable},                // 核心依赖不可用返回 HTTP 503。
	{code: MySQLUnavailable, httpStatus: ServiceBusy, messageKey: MsgKeyMySQLUnavailable},                          // MySQL 不可用返回 HTTP 503。
	{code: RedisUnavailable, httpStatus: ServiceBusy, messageKey: MsgKeyRedisUnavailable},                          // Redis 不可用返回 HTTP 503。
	{code: ClickHouseUnavailable, httpStatus: ServiceBusy, messageKey: MsgKeyClickHouseUnavailable},                // ClickHouse 不可用返回 HTTP 503。
	{code: KafkaUnavailable, httpStatus: ServiceBusy, messageKey: MsgKeyKafkaUnavailable},                          // Kafka 不可用返回 HTTP 503。
	{code: TaskQueueUnavailable, httpStatus: ServiceBusy, messageKey: MsgKeyTaskQueueUnavailable},                  // 任务队列不可用返回 HTTP 503。
	{code: CollectorUnavailable, httpStatus: ServiceBusy, messageKey: MsgKeyCollectorUnavailable},                  // Collector 不可用返回 HTTP 503。
	{code: UserTagWorkflowLeaseNotFound, httpStatus: NotFound, messageKey: MsgKeyUserTagLeaseNotFound},             // 用户标签工作流互斥租约不存在返回 HTTP 404。
	{code: UserTagWorkflowLeaseOwnerMismatch, httpStatus: BadRequest, messageKey: MsgKeyUserTagLeaseOwnerMismatch}, // 用户标签工作流互斥租约 owner 不匹配返回 HTTP 400。
	{code: UserTagWorkflowLeaseReleaseFailed, httpStatus: ServerError, messageKey: MsgKeyUserTagLeaseReleaseFail},  // 用户标签工作流互斥租约释放失败返回 HTTP 500。
}

var (
	// successCodeSet 由 defaultCodeSpecs 派生统一响应可识别为成功的业务码集合。
	successCodeSet = buildSuccessCodeSet(defaultCodeSpecs)
	// codeHTTPStatusMap 由 defaultCodeSpecs 派生业务码到 HTTP 状态码的建议映射。
	codeHTTPStatusMap = buildCodeHTTPStatusMap(defaultCodeSpecs)
	// codeMessageKeyMap 由 defaultCodeSpecs 派生业务码到默认多语言 key 的映射。
	codeMessageKeyMap = buildCodeMessageKeyMap(defaultCodeSpecs)
)

// DefaultCodeContracts 返回业务码默认响应契约快照，调用方不能修改内部源表。
func DefaultCodeContracts() []CodeContract {
	contracts := make([]CodeContract, 0, len(defaultCodeSpecs))
	for _, spec := range defaultCodeSpecs {
		contracts = append(contracts, CodeContract{
			Code:       spec.code,
			HTTPStatus: spec.httpStatus,
			Success:    spec.success,
			MessageKey: spec.messageKey,
		})
	}
	return contracts
}

// MessageKey 返回业务码默认多语言文案 key，未知业务码返回 false。
func MessageKey(code int) (string, bool) {
	key, ok := codeMessageKeyMap[code]
	return key, ok
}

// IsSuccess 判断业务码是否代表成功结果。
// 统一收口后，handler、审计和日志都不需要各自维护一套“哪些 code 算成功”的分支判断。
func IsSuccess(code int) bool {
	_, ok := successCodeSet[code]
	return ok
}

// HTTPStatus 根据业务码返回建议 HTTP 状态码，未知成功码返回 200，未知失败码返回 500。
func HTTPStatus(code int) int {
	if status, ok := codeHTTPStatusMap[code]; ok {
		return status
	}
	if IsSuccess(code) {
		return OK
	}
	return ServerError
}

// buildSuccessCodeSet 从响应码契约派生成功码集合。
func buildSuccessCodeSet(specs []codeSpec) map[int]struct{} {
	result := make(map[int]struct{})
	for _, spec := range specs {
		if spec.success {
			result[spec.code] = struct{}{}
		}
	}
	return result
}

// buildCodeHTTPStatusMap 从响应码契约派生 HTTP 状态码映射。
func buildCodeHTTPStatusMap(specs []codeSpec) map[int]int {
	result := make(map[int]int, len(specs))
	for _, spec := range specs {
		result[spec.code] = spec.httpStatus
	}
	return result
}

// buildCodeMessageKeyMap 从响应码契约派生默认多语言 key 映射。
func buildCodeMessageKeyMap(specs []codeSpec) map[int]string {
	result := make(map[int]string, len(specs))
	for _, spec := range specs {
		if spec.messageKey != "" {
			result[spec.code] = spec.messageKey
		}
	}
	return result
}
