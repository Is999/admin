package helper

import (
	"admin_cron/internal/requestctx"
	"context"
)

// CtxAdmin 存储上下文中的管理员信息。
type CtxAdmin struct {
	ID   int
	Name string
	IP   string
}

// GetCtxAdmin 从请求元数据中提取管理员信息。
// 这样业务层不再依赖散落的 context.WithValue，自定义上下文取值统一收口到 requestctx.Meta。
func GetCtxAdmin(r context.Context) *CtxAdmin {
	if r == nil {
		return nil
	}
	meta := requestctx.FromContext(r)
	if meta == nil || meta.UserID == 0 {
		return nil
	}

	admin := &CtxAdmin{
		ID:   meta.UserID,
		Name: meta.UserName,
		IP:   meta.ClientIP,
	}
	if admin.Name == "" {
		return nil
	}
	return admin
}
