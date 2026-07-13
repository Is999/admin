package cache

import (
	"context"

	"admin/internal/types"
)

// adminSessionContextKey 隔离管理员会话在请求上下文中的存储键。
type adminSessionContextKey struct{}

// WithAdminSession 把已通过 JWT 与 Redis 校验的管理员会话写入当前请求上下文。
func WithAdminSession(ctx context.Context, session *types.AdminSession) context.Context {
	if ctx == nil || session == nil {
		return ctx
	}
	return context.WithValue(ctx, adminSessionContextKey{}, session)
}

// AdminSessionFromContext 返回当前请求已校验的管理员会话。
func AdminSessionFromContext(ctx context.Context) (*types.AdminSession, bool) {
	if ctx == nil {
		return nil, false
	}
	session, ok := ctx.Value(adminSessionContextKey{}).(*types.AdminSession)
	return session, ok && session != nil
}
