package message

import (
	"net/http"

	"admin/internal/handler/shared"
)

// RouteSpecs 返回管理员消息管理路由规格。
func RouteSpecs() []shared.RouteSpec {
	return []shared.RouteSpec{
		{
			Method:      http.MethodGet,
			Path:        "/api/admin-messages", // 查询管理员消息收件箱。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminMessageList,
			Description: shared.AdminMessageList.Describe,
			Handler:     ListAdminMessageHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/admin-messages/sent", // 查询管理员已发送消息。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminMessageSentList,
			Description: shared.AdminMessageSentList.Describe,
			Handler:     ListAdminMessageSentHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/admin-messages/receiver-options", // 查询消息可用收件人选项。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminMessageReceiverOptions,
			Description: shared.AdminMessageReceiverOptions.Describe,
			Handler:     ListAdminMessageReceiverOptionsHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/admin-messages/:id/receivers", // 查询管理员消息收件人明细。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminMessageReceivers,
			Description: shared.AdminMessageReceivers.Describe,
			Handler:     ListAdminMessageReceiversHandler,
		},
		{
			Method:      http.MethodGet,
			Path:        "/api/admin-messages/unread-count", // 查询管理员未读消息数量。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminMessageUnreadCount,
			Description: shared.AdminMessageUnreadCount.Describe,
			Handler:     GetAdminMessageUnreadCountHandler,
		},
		{
			Method:        http.MethodGet,
			Path:          "/api/admin-messages/notifications", // 查询管理员通知列表。
			Access:        shared.RouteAccessAuth,
			Meta:          shared.AdminMessageNotifications,
			Description:   shared.AdminMessageNotifications.Describe,
			SkipAccessLog: true,
			Handler:       ListAdminMessageNotificationsHandler,
		},
		{
			Method:      http.MethodPatch,
			Path:        "/api/admin-messages/read", // 标记管理员消息已读。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminMessageMarkRead,
			Description: shared.AdminMessageMarkRead.Describe,
			Handler:     MarkAdminMessageReadHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/admin-messages/delete", // 删除管理员消息。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminMessageDelete,
			Description: shared.AdminMessageDelete.Describe,
			Handler:     DeleteAdminMessageHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/admin-messages/send", // 发送管理员消息。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminMessageSend,
			Description: shared.AdminMessageSend.Describe,
			Handler:     SendAdminMessageHandler,
		},
		{
			Method:      http.MethodPost,
			Path:        "/api/admin-messages/handle", // 标记管理员消息已处理。
			Access:      shared.RouteAccessAuth,
			Meta:        shared.AdminMessageHandle,
			Description: shared.AdminMessageHandle.Describe,
			Handler:     HandleAdminMessageHandler,
		},
	}
}
