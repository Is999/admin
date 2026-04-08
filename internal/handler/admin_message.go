package handler

import (
	"admin_cron/internal/logic"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"
	"net/http"
)

// ListAdminMessageHandler 处理管理员消息收件箱分页查询请求。
func ListAdminMessageHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.AdminMessageQueryReq](listAdminMessage, func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageQueryReq) (LogicObj, *types.BizResult) {
		logicObj := logic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.ListInbox(req)
	})(sCtx)
}

// GetAdminMessageUnreadCountHandler 查询当前管理员未读消息数量。
func GetAdminMessageUnreadCountHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return RespHandler[struct{}](func(r *http.Request, svcCtx *svc.ServiceContext, _ *struct{}) (LogicObj, *types.BizResult) {
		logicObj := logic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.UnreadCount()
	})(sCtx)
}

// ListAdminMessageNotificationsHandler 查询顶部铃铛通知列表。
func ListAdminMessageNotificationsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return RespHandler[types.AdminMessageNotificationReq](func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageNotificationReq) (LogicObj, *types.BizResult) {
		logicObj := logic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.ListNotifications(req)
	})(sCtx)
}

// MarkAdminMessageReadHandler 标记管理员消息为已读（支持批量与全部）。
func MarkAdminMessageReadHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.AdminMessageMarkReadReq](markAdminMessageRead, func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageMarkReadReq) (LogicObj, *types.BizResult) {
		logicObj := logic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.MarkRead(req)
	})(sCtx)
}

// DeleteAdminMessageHandler 删除管理员消息（软删除，支持批量与清空全部已读）。
func DeleteAdminMessageHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.AdminMessageDeleteReq](deleteAdminMessage, func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageDeleteReq) (LogicObj, *types.BizResult) {
		logicObj := logic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.Delete(req)
	})(sCtx)
}

// SendAdminMessageHandler 发送管理员消息到收件箱（站内信/通知）。
func SendAdminMessageHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.AdminMessageSendReq](sendAdminMessage, func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageSendReq) (LogicObj, *types.BizResult) {
		logicObj := logic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.Send(req)
	})(sCtx)
}

// ListAdminMessageSentHandler 处理管理员已发送消息分页查询请求。
func ListAdminMessageSentHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.AdminMessageSentQueryReq](listAdminMessageSent, func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageSentQueryReq) (LogicObj, *types.BizResult) {
		logicObj := logic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.ListSent(req)
	})(sCtx)
}

// ListAdminMessageReceiversHandler 查询指定消息的收件人已读明细（仅发送人可见）。
func ListAdminMessageReceiversHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.AdminMessageIDPathReq](listAdminMessageReceivers, func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageIDPathReq) (LogicObj, *types.BizResult) {
		logicObj := logic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.ListReceivers(req)
	})(sCtx)
}

// HandleAdminMessageHandler 标记指定消息为已处理。
func HandleAdminMessageHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.AdminMessageHandleReq](handleAdminMessage, func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminMessageHandleReq) (LogicObj, *types.BizResult) {
		logicObj := logic.NewAdminMessageLogic(r, svcCtx)
		return logicObj, logicObj.Handle(req)
	})(sCtx)
}
