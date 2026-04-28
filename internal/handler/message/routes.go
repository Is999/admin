package message

import (
	"admin/internal/handler/shared"
	"net/http"

	"admin/internal/middleware"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterRoutes 注册管理员消息管理相关接口。
func RegisterRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/admin-messages", // 查询管理员消息收件箱
			Handler: authMw.Handle(ListAdminMessageHandler(serverCtx), shared.AdminMessageList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admin-messages/sent", // 查询管理员已发送消息
			Handler: authMw.Handle(ListAdminMessageSentHandler(serverCtx), shared.AdminMessageSentList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admin-messages/:id/receivers", // 查询消息收件人已读明细
			Handler: authMw.Handle(ListAdminMessageReceiversHandler(serverCtx), shared.AdminMessageReceivers.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admin-messages/unread-count", // 查询未读消息数量
			Handler: authMw.Handle(GetAdminMessageUnreadCountHandler(serverCtx), shared.AdminMessageUnreadCount.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admin-messages/notifications", // 查询顶部铃铛通知列表
			Handler: authMw.Handle(ListAdminMessageNotificationsHandler(serverCtx), shared.AdminMessageNotifications.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/admin-messages/read", // 标记消息已读
			Handler: authMw.Handle(MarkAdminMessageReadHandler(serverCtx), shared.AdminMessageMarkRead.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admin-messages/delete", // 删除消息（软删除）
			Handler: authMw.Handle(DeleteAdminMessageHandler(serverCtx), shared.AdminMessageDelete.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admin-messages/send", // 发送消息
			Handler: authMw.Handle(SendAdminMessageHandler(serverCtx), shared.AdminMessageSend.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admin-messages/handle", // 标记消息已处理
			Handler: authMw.Handle(HandleAdminMessageHandler(serverCtx), shared.AdminMessageHandle.Alias),
		},
	})
}
