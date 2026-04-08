package consts

// 定义上下文中使用的键
type contextKey string

const (
	// CtxUserIDKey 用于在 context 中标识用户 ID 的 key
	CtxUserIDKey contextKey = "user_id"
	// CtxUserNameKey 用于在 context 中标识用户名的 key
	CtxUserNameKey contextKey = "user_name"
	// CtxUserIPKey 用于在 context 中标识用户 IP 的 key
	CtxUserIPKey contextKey = "user_ip"
	// TraceIDKey 用于在 context 和日志中标识 trace id 的 key
	TraceIDKey contextKey = "trace_id"
)
