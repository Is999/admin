package handler

import (
	"net/http"

	"admin_cron/internal/middleware"
	"admin_cron/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// registerMessageRoutes 注册管理员消息管理相关接口。
func registerMessageRoutes(server *rest.Server, serverCtx *svc.ServiceContext, authMw *middleware.AuthMiddleware) {
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/admin-messages", // 查询管理员消息收件箱
			Handler: authMw.Handle(ListAdminMessageHandler(serverCtx), AdminMessageList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admin-messages/sent", // 查询管理员已发送消息
			Handler: authMw.Handle(ListAdminMessageSentHandler(serverCtx), AdminMessageSentList.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admin-messages/:id/receivers", // 查询消息收件人已读明细
			Handler: authMw.Handle(ListAdminMessageReceiversHandler(serverCtx), AdminMessageReceivers.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admin-messages/unread-count", // 查询未读消息数量
			Handler: authMw.Handle(GetAdminMessageUnreadCountHandler(serverCtx), AdminMessageUnreadCount.Alias),
		},
		{
			Method:  http.MethodGet,
			Path:    "/api/admin-messages/notifications", // 查询顶部铃铛通知列表
			Handler: authMw.Handle(ListAdminMessageNotificationsHandler(serverCtx), AdminMessageNotifications.Alias),
		},
		{
			Method:  http.MethodPatch,
			Path:    "/api/admin-messages/read", // 标记消息已读
			Handler: authMw.Handle(MarkAdminMessageReadHandler(serverCtx), AdminMessageMarkRead.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admin-messages/delete", // 删除消息（软删除）
			Handler: authMw.Handle(DeleteAdminMessageHandler(serverCtx), AdminMessageDelete.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admin-messages/send", // 发送消息
			Handler: authMw.Handle(SendAdminMessageHandler(serverCtx), AdminMessageSend.Alias),
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/admin-messages/handle", // 标记消息已处理
			Handler: authMw.Handle(HandleAdminMessageHandler(serverCtx), AdminMessageHandle.Alias),
		},
	})
}
