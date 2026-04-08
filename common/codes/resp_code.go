package codes

const (
	// 通用响应码 0 - 99 为通用状态码
	Undefined          = 0 // 未定义
	Success            = 1 // 成功
	Fail               = 2 // 失败
	CheckMFABind       = 5 // 需要先绑定并启用 MFA
	CheckMFACode       = 6 // 需要校验 MFA 设备验证码
	CheckPasswordReset = 7 // 需要先修改登录密码
	CheckMFAAgain      = 8 // MFA 校验已过期，需要重新验证

	// 通用响应码 100 - 199 为信息性状态码
	Continue = 100 // 继续

	// 通用响应码 200 - 299 为成功状态码
	OK = 200 // 请求成功

	// 通用响应码 400 - 499 为客户端错误状态码
	BadRequest   = 400 // 错误请求
	Unauthorized = 401 // 未授权
	Forbidden    = 403 // 禁止访问
	NotFound     = 404 // 未找到

	// 通用响应码 500 - 599 为服务器错误状态码
	ServerError = 500 // 服务器错误
	ServiceBusy = 503 // 服务繁忙
	Timeout     = 504 // 请求超时

	// 业务响应码 1000 - 1999 为通用业务响应码
	ParamError    = 1001 // 参数错误
	AuthFailed    = 1002 // 验证失败
	RateLimit     = 1003 // 请求过多，限流
	InternalError = 1004 // 内部错误
	DBError       = 1005 // 数据库错误

	CreateSuccess = 1100 // 创建成功
	CreateFail    = 1101 // 创建失败
	AddSuccess    = 1102 // 添加成功
	AddFail       = 1103 // 添加失败
	SaveSuccess   = 1104 // 保存成功
	SaveFail      = 1105 // 保存失败
	UpdateSuccess = 1106 // 更新成功
	UpdateFail    = 1107 // 更新失败
	DeleteSuccess = 1108 // 删除成功
	DeleteFail    = 1109 // 删除失败
	FetchSuccess  = 1110 // 获取成功
	FetchFail     = 1111 // 获取失败

	// 你可以根据需要添加更多的响应码，备注自己使用的范围，其它人勿用，避免冲突
	// 100000 - 102000 已被使用，请勿使用
	UserNotFound      = 100000 // 用户不存在
	InvalidPassword   = 100001 // 密码错误
	UserAlreadyExists = 100002 // 用户已存在
	UserDisabled      = 100003 // 账号被禁用
	InvalidCaptcha    = 100004 // 登录验证码错误或已过期
	InvalidMFACode    = 100005 // MFA动态验证码错误
	// 100000 - 102000 已被使用，请勿使用
	// 你可以根据需要添加更多的响应码，备注自己使用的范围，其它人勿用，避免冲突
)

const (
	// CodeCommonBase 表示通用业务码号段起点，范围 10000-19999。
	CodeCommonBase = 10000
	// CodeAuthBase 表示认证权限业务码号段起点，范围 20000-29999。
	CodeAuthBase = 20000
	// CodeAdminBase 表示后台系统业务码号段起点，范围 30000-39999。
	CodeAdminBase = 30000
	// CodeTaskBase 表示任务系统业务码号段起点，范围 40000-40999。
	CodeTaskBase = 40000
	// CodeWorkflowBase 表示工作流业务码号段起点，范围 41000-41999。
	CodeWorkflowBase = 41000
	// CodeCollectorBase 表示 Collector 业务码号段起点，范围 42000-42999。
	CodeCollectorBase = 42000
	// CodeUserTagBase 表示用户标签业务码号段起点，范围 43000-43999。
	CodeUserTagBase = 43000
	// CodeTransferBase 表示文件传输业务码号段起点，范围 44000-44999。
	CodeTransferBase = 44000
	// CodeDependencyBase 表示外部依赖业务码号段起点，范围 50000-50999。
	CodeDependencyBase = 50000
)

const (
	// DependencyUnavailable 表示 ready 检查或核心流程发现外部依赖不可用。
	DependencyUnavailable = CodeDependencyBase + 1
	// MySQLUnavailable 表示 MySQL 连接不可用。
	MySQLUnavailable = CodeDependencyBase + 2
	// RedisUnavailable 表示 Redis 连接不可用。
	RedisUnavailable = CodeDependencyBase + 3
	// ClickHouseUnavailable 表示 ClickHouse 连接不可用。
	ClickHouseUnavailable = CodeDependencyBase + 4
	// KafkaUnavailable 表示 Kafka 生产链路不可用。
	KafkaUnavailable = CodeDependencyBase + 5
	// TaskQueueUnavailable 表示任务队列组件未就绪或不可用。
	TaskQueueUnavailable = CodeDependencyBase + 6
	// CollectorUnavailable 表示 Collector 组件未就绪或不可用。
	CollectorUnavailable = CodeDependencyBase + 7
)

const (
	// UserTagWorkflowLeaseNotFound 表示用户标签工作流互斥租约不存在，可能已经过期或已释放。
	UserTagWorkflowLeaseNotFound = CodeUserTagBase + 1
	// UserTagWorkflowLeaseOwnerMismatch 表示用户标签工作流互斥租约 owner 与请求 workflowID/mode 不一致。
	UserTagWorkflowLeaseOwnerMismatch = CodeUserTagBase + 2
	// UserTagWorkflowLeaseReleaseFailed 表示用户标签工作流互斥租约释放过程发生未知失败。
	UserTagWorkflowLeaseReleaseFailed = CodeUserTagBase + 3
)

var successCodeSet = map[int]struct{}{
	Success:       {},
	OK:            {},
	CreateSuccess: {},
	AddSuccess:    {},
	SaveSuccess:   {},
	UpdateSuccess: {},
	DeleteSuccess: {},
	FetchSuccess:  {},
}

var codeHTTPStatusMap = map[int]int{
	// codeHTTPStatusMap 维护业务码到 HTTP 状态码的统一映射，响应出口和测试用例共用。
	CheckMFABind:          OK,
	CheckMFACode:          OK,
	CheckPasswordReset:    OK,
	CheckMFAAgain:         BadRequest,
	BadRequest:            BadRequest,
	Unauthorized:          Unauthorized,
	Forbidden:             Forbidden,
	NotFound:              NotFound,
	ServerError:           ServerError,
	ServiceBusy:           ServiceBusy,
	Timeout:               Timeout,
	ParamError:            BadRequest,
	AuthFailed:            Unauthorized,
	RateLimit:             429,
	InternalError:         ServerError,
	DBError:               ServerError,
	InvalidPassword:       BadRequest,
	InvalidMFACode:        BadRequest,
	DependencyUnavailable: ServiceBusy,
	MySQLUnavailable:      ServiceBusy,
	RedisUnavailable:      ServiceBusy,
	ClickHouseUnavailable: ServiceBusy,
	KafkaUnavailable:      ServiceBusy,
	TaskQueueUnavailable:  ServiceBusy,
	CollectorUnavailable:  ServiceBusy,

	UserTagWorkflowLeaseNotFound:      NotFound,
	UserTagWorkflowLeaseOwnerMismatch: BadRequest,
	UserTagWorkflowLeaseReleaseFailed: ServerError,
}

// IsSuccess 判断业务码是否代表成功结果。
// 统一收口后，handler/审计/日志都不需要各自维护一套“哪些 code 算成功”的分支判断。
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
