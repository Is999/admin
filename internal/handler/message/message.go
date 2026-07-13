package message

import (
	"admin/internal/handler/shared"
	messagelogic "admin/internal/logic/message"
	"admin/internal/svc"
	"admin/internal/types"
	"net/http"
)

// ListAdminMessageHandler 处理管理员消息收件箱分页查询请求。
func ListAdminMessageHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.AdminMessageQueryReq](shared.AdminMessageList, func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageQueryReq) (shared.LogicObj, *types.BizResult) {
		logicObj := messagelogic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.ListInbox(req)
	})(sCtx)
}

// GetAdminMessageUnreadCountHandler 查询当前管理员未读消息数量。
func GetAdminMessageUnreadCountHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.RespHandler[struct{}](func(r *http.Request, svcCtx *svc.ServiceContext, _ *struct{}) (shared.LogicObj, *types.BizResult) {
		logicObj := messagelogic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.UnreadCount()
	})(sCtx)
}

// ListAdminMessageNotificationsHandler 查询顶部铃铛通知列表。
func ListAdminMessageNotificationsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.RespHandler[types.AdminMessageNotificationReq](func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageNotificationReq) (shared.LogicObj, *types.BizResult) {
		logicObj := messagelogic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.ListNotifications(req)
	})(sCtx)
}

// MarkAdminMessageReadHandler 标记管理员消息为已读（支持批量与全部）。
func MarkAdminMessageReadHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.AdminMessageMarkReadReq](shared.AdminMessageMarkRead, func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageMarkReadReq) (shared.LogicObj, *types.BizResult) {
		logicObj := messagelogic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.MarkRead(req)
	})(sCtx)
}

// DeleteAdminMessageHandler 删除管理员消息（软删除，支持批量与清空全部已读）。
func DeleteAdminMessageHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.AdminMessageDeleteReq](shared.AdminMessageDelete, func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageDeleteReq) (shared.LogicObj, *types.BizResult) {
		logicObj := messagelogic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.Delete(req)
	})(sCtx)
}

// SendAdminMessageHandler 发送管理员消息到收件箱（站内信/通知）。
func SendAdminMessageHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.AdminMessageSendReq](shared.AdminMessageSend, func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageSendReq) (shared.LogicObj, *types.BizResult) {
		logicObj := messagelogic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.Send(req)
	})(sCtx)
}

// ListAdminMessageSentHandler 处理管理员已发送消息分页查询请求。
func ListAdminMessageSentHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.AdminMessageSentQueryReq](shared.AdminMessageSentList, func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageSentQueryReq) (shared.LogicObj, *types.BizResult) {
		logicObj := messagelogic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.ListSent(req)
	})(sCtx)
}

// ListAdminMessageReceiverOptionsHandler 查询发送消息时可选的启用管理员。
func ListAdminMessageReceiverOptionsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.RespHandler[types.AdminMessageReceiverOptionQueryReq](func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageReceiverOptionQueryReq) (shared.LogicObj, *types.BizResult) {
		logicObj := messagelogic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.ListReceiverOptions(req)
	})(sCtx)
}

// ListAdminMessageReceiversHandler 查询指定消息的收件人已读明细（仅发送人可见）。
func ListAdminMessageReceiversHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.AdminMessageIDPathReq](shared.AdminMessageReceivers, func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageIDPathReq) (shared.LogicObj, *types.BizResult) {
		logicObj := messagelogic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.ListReceivers(req)
	})(sCtx)
}

// HandleAdminMessageHandler 标记指定消息为已处理。
func HandleAdminMessageHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.AdminMessageHandleReq](shared.AdminMessageHandle, func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageHandleReq) (shared.LogicObj, *types.BizResult) {
		logicObj := messagelogic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.Handle(req)
	})(sCtx)
}
