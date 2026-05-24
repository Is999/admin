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
	shared.AddRouteSpecs(server, serverCtx, authMw, nil, RouteSpecs())
}

// RouteSpecs 返回管理员消息管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		shared.AuthRoute(http.MethodGet, "/api/admin-messages", shared.AdminMessageList, ListAdminMessageHandler),
		shared.AuthRoute(http.MethodGet, "/api/admin-messages/sent", shared.AdminMessageSentList, ListAdminMessageSentHandler),
		shared.AuthRoute(http.MethodGet, "/api/admin-messages/:id/receivers", shared.AdminMessageReceivers, ListAdminMessageReceiversHandler),
		shared.AuthRoute(http.MethodGet, "/api/admin-messages/unread-count", shared.AdminMessageUnreadCount, GetAdminMessageUnreadCountHandler),
		shared.AuthRoute(http.MethodGet, "/api/admin-messages/notifications", shared.AdminMessageNotifications, ListAdminMessageNotificationsHandler).WithSkipAccessLog(),
		shared.AuthRoute(http.MethodPatch, "/api/admin-messages/read", shared.AdminMessageMarkRead, MarkAdminMessageReadHandler),
		shared.AuthRoute(http.MethodPost, "/api/admin-messages/delete", shared.AdminMessageDelete, DeleteAdminMessageHandler),
		shared.AuthRoute(http.MethodPost, "/api/admin-messages/send", shared.AdminMessageSend, SendAdminMessageHandler),
		shared.AuthRoute(http.MethodPost, "/api/admin-messages/handle", shared.AdminMessageHandle, HandleAdminMessageHandler),
	}
}
