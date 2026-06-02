package message

import (
	codes "admin/common/codes"
	i18n "admin/common/i18n"
	corelogic "admin/internal/logic"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"
	"net/http"
	"time"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

// AdminMessageLogic 负责管理员消息（站内信/通知）的收件箱管理与发送能力。
type AdminMessageLogic struct {
	*corelogic.BaseLogic // 复用上下文、数据库和日志能力
}

// NewAdminMessageLogic 创建管理员消息业务对象。
func NewAdminMessageLogic(r *http.Request, svcCtx *svc.ServiceContext) *AdminMessageLogic {
	return &AdminMessageLogic{
		BaseLogic: corelogic.NewBaseLogic(r, svcCtx),
	}
}

// ListInbox 分页查询当前管理员的消息收件箱列表。
func (l *AdminMessageLogic) ListInbox(req *types.AdminMessageQueryReq) *types.BizResult {
	if req == nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamError)
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}
	startTime, endTime, err := req.TimeRange()
	if err != nil {
		return types.ParamErrorResult(err)
	}

	items, total, err := model.ListAdminMessageInbox(
		l.Svc.ReadDB(svc.DatabaseMain),
		ctxAdmin.ID,
		req.Page,
		req.PageSize,
		req.Type,
		req.Level,
		req.ReadStatus,
		req.Keyword,
		startTime,
		endTime,
	)
	if err != nil {
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyQueryFail,
			Error:      errors.Wrap(err, "查询管理员消息收件箱失败"),
		}
	}

	respItems := make([]types.AdminMessageItem, 0, len(items))
	for _, item := range items {
		respItems = append(respItems, types.AdminMessageItem(item))
	}
	return &types.BizResult{
		Code:       codes.Success,
		MessageKey: i18n.MsgKeyQuerySuccess,
		Data:       &types.ListResp[types.AdminMessageItem]{List: respItems, Total: total},
	}
}

// UnreadCount 查询当前管理员未读消息数量。
func (l *AdminMessageLogic) UnreadCount() *types.BizResult {
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}
	total, err := model.CountAdminMessageUnread(l.Svc.ReadDB(svc.DatabaseMain), ctxAdmin.ID)
	if err != nil {
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyQueryFail,
			Error:      errors.Wrap(err, "统计管理员未读消息失败"),
		}
	}
	return &types.BizResult{
		Code:       codes.Success,
		MessageKey: i18n.MsgKeyQuerySuccess,
		Data:       &types.AdminMessageUnreadCountResp{Unread: total},
	}
}

// ListNotifications 查询当前管理员用于顶部铃铛展示的通知列表。
func (l *AdminMessageLogic) ListNotifications(req *types.AdminMessageNotificationReq) *types.BizResult {
	if req == nil {
		req = &types.AdminMessageNotificationReq{}
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}
	items, err := model.ListAdminMessageNotifications(l.Svc.ReadDB(svc.DatabaseMain), ctxAdmin.ID, req.Limit)
	if err != nil {
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyQueryFail,
			Error:      errors.Wrap(err, "查询管理员通知列表失败"),
		}
	}
	respItems := make([]types.AdminMessageItem, 0, len(items))
	for _, item := range items {
		respItems = append(respItems, types.AdminMessageItem(item))
	}
	return &types.BizResult{
		Code:       codes.Success,
		MessageKey: i18n.MsgKeyQuerySuccess,
		Data:       respItems,
	}
}

// MarkRead 把当前管理员的指定消息标记为已读。
func (l *AdminMessageLogic) MarkRead(req *types.AdminMessageMarkReadReq) *types.BizResult {
	if req == nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamError)
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}

	var affected int64
	var err error
	if req.All {
		affected, err = model.MarkAdminMessagesReadAll(l.Svc.WriteDB(svc.DatabaseMain), ctxAdmin.ID)
	} else {
		affected, err = model.MarkAdminMessagesRead(l.Svc.WriteDB(svc.DatabaseMain), ctxAdmin.ID, req.IDs)
	}
	if err != nil {
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyUpdateFail,
			Error:      errors.Wrap(err, "标记管理员消息已读失败"),
		}
	}
	return &types.BizResult{
		Code:       codes.Success,
		MessageKey: i18n.MsgKeyUpdateSuccess,
		Data:       &types.AdminMessageAffectedResp{Affected: affected},
	}
}

// Delete 把当前管理员的指定消息从收件箱删除（软删除）。
func (l *AdminMessageLogic) Delete(req *types.AdminMessageDeleteReq) *types.BizResult {
	if req == nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamError)
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}

	var affected int64
	var err error
	if req.AllRead {
		affected, err = model.DeleteAdminMessagesAllRead(l.Svc.WriteDB(svc.DatabaseMain), ctxAdmin.ID)
	} else {
		affected, err = model.DeleteAdminMessages(l.Svc.WriteDB(svc.DatabaseMain), ctxAdmin.ID, req.IDs)
	}
	if err != nil {
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyDeleteFail,
			Error:      errors.Wrap(err, "删除管理员消息失败"),
		}
	}
	return &types.BizResult{
		Code:       codes.Success,
		MessageKey: i18n.MsgKeyDeleteSuccess,
		Data:       &types.AdminMessageAffectedResp{Affected: affected},
	}
}

// Send 发送消息到指定管理员收件箱；未指定收件人时自动广播到全部启用管理员。
func (l *AdminMessageLogic) Send(req *types.AdminMessageSendReq) *types.BizResult {
	if req == nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamError)
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}

	receiverIDs := req.ReceiverIDs
	if len(receiverIDs) == 0 {
		if err := l.Svc.ReadDB(svc.DatabaseMain).Model(&model.Admin{}).Where("status = 1").Pluck("id", &receiverIDs).Error; err != nil {
			return &types.BizResult{
				Code:       codes.DBError,
				MessageKey: i18n.MsgKeyQueryFail,
				Error:      errors.Wrap(err, "查询启用管理员列表失败"),
			}
		}
	}
	if len(receiverIDs) == 0 {
		return types.NewBizResult(codes.Fail).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyAddFail)
	}

	now := time.Now()
	msg := &model.AdminMessage{
		Type:            req.Type,
		Level:           req.Level,
		Title:           req.Title,
		Content:         req.Content,
		Data:            req.Data,
		Link:            req.Link,
		SenderAdminID:   ctxAdmin.ID,
		SenderAdminName: ctxAdmin.Name,
		CreatedAt:       now,
	}

	if err := l.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		return model.CreateAdminMessageWithReceivers(tx, msg, receiverIDs)
	}); err != nil {
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyAddFail,
			Error:      errors.Wrap(err, "发送管理员消息失败"),
		}
	}
	return &types.BizResult{
		Code:       codes.Success,
		MessageKey: i18n.MsgKeyAddSuccess,
		Data:       &types.AdminMessageSendResp{ID: msg.ID},
	}
}

// ListSent 分页查询当前管理员的已发送消息列表。
func (l *AdminMessageLogic) ListSent(req *types.AdminMessageSentQueryReq) *types.BizResult {
	if req == nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamError)
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}
	startTime, endTime, err := req.TimeRange()
	if err != nil {
		return types.ParamErrorResult(err)
	}

	items, total, err := model.ListAdminMessageSent(
		l.Svc.ReadDB(svc.DatabaseMain),
		ctxAdmin.ID,
		req.Page,
		req.PageSize,
		req.Type,
		req.Level,
		req.Keyword,
		startTime,
		endTime,
	)
	if err != nil {
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyQueryFail,
			Error:      errors.Wrap(err, "查询管理员已发送消息失败"),
		}
	}

	respItems := make([]types.AdminMessageSentItem, 0, len(items))
	for _, item := range items {
		respItems = append(respItems, types.AdminMessageSentItem(item))
	}
	return &types.BizResult{
		Code:       codes.Success,
		MessageKey: i18n.MsgKeyQuerySuccess,
		Data:       &types.ListResp[types.AdminMessageSentItem]{List: respItems, Total: total},
	}
}

// ListReceivers 查询当前管理员发送的指定消息的收件人已读明细。
func (l *AdminMessageLogic) ListReceivers(req *types.AdminMessageIDPathReq) *types.BizResult {
	if req == nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamError)
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}

	if err := l.Svc.ReadDB(svc.DatabaseMain).
		Model(&model.AdminMessage{}).
		Select("id").
		Where("id = ?", req.ID).
		Where("sender_admin_id = ?", ctxAdmin.ID).
		Take(&model.AdminMessage{}).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NewBizResult(codes.Forbidden).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyForbidden)
		}
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyQueryFail,
			Error:      errors.Wrap(err, "校验消息归属失败"),
		}
	}

	items, err := model.ListAdminMessageReceivers(l.Svc.ReadDB(svc.DatabaseMain), req.ID)
	if err != nil {
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyQueryFail,
			Error:      errors.Wrap(err, "查询消息收件人明细失败"),
		}
	}
	respItems := make([]types.AdminMessageReceiverItem, 0, len(items))
	for _, item := range items {
		respItems = append(respItems, types.AdminMessageReceiverItem(item))
	}
	return &types.BizResult{
		Code:       codes.Success,
		MessageKey: i18n.MsgKeyQuerySuccess,
		Data:       respItems,
	}
}

// isProcessableAdminMessageType 判断消息类型是否允许被管理员手动标记为已处理。
func isProcessableAdminMessageType(msgType string) bool {
	switch types.AdminMessageType(msgType) {
	case types.AdminMessageTypeApprovalNotice,
		types.AdminMessageTypeSecurityAlert,
		types.AdminMessageTypeTaskException:
		return true
	default:
		return false
	}
}

// Handle 把当前管理员收到的指定消息标记为“已处理”。
func (l *AdminMessageLogic) Handle(req *types.AdminMessageHandleReq) *types.BizResult {
	if req == nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamError)
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}

	var msg model.AdminMessage
	if err := l.Svc.ReadDB(svc.DatabaseMain).Where("id = ?", req.ID).Take(&msg).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NewBizResult(codes.NotFound).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNotFound)
		}
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyQueryFail,
			Error:      errors.Wrap(err, "查询消息失败"),
		}
	}
	if !isProcessableAdminMessageType(msg.Type) {
		return types.NewBizResult(codes.Fail).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyFail)
	}

	if err := l.Svc.ReadDB(svc.DatabaseMain).
		Model(&model.AdminMessageReceiver{}).
		Where("message_id = ?", req.ID).
		Where("receiver_admin_id = ?", ctxAdmin.ID).
		Where("delete_status = 0").
		Select("id").
		Take(&model.AdminMessageReceiver{}).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NewBizResult(codes.Forbidden).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyForbidden)
		}
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyQueryFail,
			Error:      errors.Wrap(err, "校验收件关系失败"),
		}
	}

	if msg.HandledStatus == 1 {
		handledAt := ""
		if msg.HandledAt != nil {
			handledAt = msg.HandledAt.Format(time.DateTime)
		}
		return &types.BizResult{
			Code:       codes.Success,
			MessageKey: i18n.MsgKeyUpdateSuccess,
			Data: &types.AdminMessageHandleResp{
				ID:                 msg.ID,
				HandledStatus:      msg.HandledStatus,
				HandledByAdminName: msg.HandledByAdminName,
				HandledAt:          handledAt,
				AlreadyHandled:     true,
			},
		}
	}

	updated, err := model.MarkAdminMessageHandled(l.Svc.WriteDB(svc.DatabaseMain), msg.ID, ctxAdmin.ID, ctxAdmin.Name)
	if err != nil {
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyUpdateFail,
			Error:      errors.Wrap(err, "标记消息已处理失败"),
		}
	}
	if updated {
		return &types.BizResult{
			Code:       codes.Success,
			MessageKey: i18n.MsgKeyUpdateSuccess,
			Data: &types.AdminMessageHandleResp{
				ID:             msg.ID,
				HandledStatus:  1,
				AlreadyHandled: false,
			},
		}
	}

	var latest model.AdminMessage
	if err := l.Svc.ReadDB(svc.DatabaseMain).Where("id = ?", msg.ID).Take(&latest).Error; err != nil {
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyQueryFail,
			Error:      errors.Wrap(err, "查询最新处理状态失败"),
		}
	}
	handledAt := ""
	if latest.HandledAt != nil {
		handledAt = latest.HandledAt.Format(time.DateTime)
	}
	return &types.BizResult{
		Code:       codes.Success,
		MessageKey: i18n.MsgKeyUpdateSuccess,
		Data: &types.AdminMessageHandleResp{
			ID:                 latest.ID,
			HandledStatus:      latest.HandledStatus,
			HandledByAdminName: latest.HandledByAdminName,
			HandledAt:          handledAt,
			AlreadyHandled:     true,
		},
	}
}
