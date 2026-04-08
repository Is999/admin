package logic

import (
	codes "admin_cron/common/codes"
	i18n "admin_cron/common/i18n"
	"admin_cron/internal/model"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"
	"net/http"
	"time"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

// AdminMessageLogic 负责管理员消息（站内信/通知）的收件箱管理与发送能力。
type AdminMessageLogic struct {
	*BaseLogic // 复用上下文、数据库和日志能力
}

// NewAdminMessageLogic 创建管理员消息业务对象。
func NewAdminMessageLogic(r *http.Request, svcCtx *svc.ServiceContext) *AdminMessageLogic {
	return &AdminMessageLogic{
		BaseLogic: NewBaseLogic(r, svcCtx),
	}
}

// ListInbox 分页查询当前管理员的消息收件箱列表。
func (l *AdminMessageLogic) ListInbox(req *types.AdminMessageQueryReq) *types.BizResult {
	if req == nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamError)
	}
	if err := req.Validate(); err != nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error())
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}
	startTime, endTime, err := req.TimeRange()
	if err != nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error())
	}

	items, total, err := model.ListAdminMessageInbox(
		l.svc.ReadDB(svc.DatabaseMain),
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
	total, err := model.CountAdminMessageUnread(l.svc.ReadDB(svc.DatabaseMain), ctxAdmin.ID)
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
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error())
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}
	items, err := model.ListAdminMessageNotifications(l.svc.ReadDB(svc.DatabaseMain), ctxAdmin.ID, req.Limit)
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
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error())
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}

	var affected int64
	var err error
	if req.All {
		affected, err = model.MarkAdminMessagesReadAll(l.svc.WriteDB(svc.DatabaseMain), ctxAdmin.ID)
	} else {
		affected, err = model.MarkAdminMessagesRead(l.svc.WriteDB(svc.DatabaseMain), ctxAdmin.ID, req.IDs)
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
		Data: map[string]any{
			"affected": affected,
		},
	}
}

// Delete 把当前管理员的指定消息从收件箱删除（软删除）。
func (l *AdminMessageLogic) Delete(req *types.AdminMessageDeleteReq) *types.BizResult {
	if req == nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamError)
	}
	if err := req.Validate(); err != nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error())
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}

	var affected int64
	var err error
	if req.AllRead {
		affected, err = model.DeleteAdminMessagesAllRead(l.svc.WriteDB(svc.DatabaseMain), ctxAdmin.ID)
	} else {
		affected, err = model.DeleteAdminMessages(l.svc.WriteDB(svc.DatabaseMain), ctxAdmin.ID, req.IDs)
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
		Data: map[string]any{
			"affected": affected,
		},
	}
}

// Send 发送消息到指定管理员收件箱；未指定收件人时自动广播到全部启用管理员。
func (l *AdminMessageLogic) Send(req *types.AdminMessageSendReq) *types.BizResult {
	if req == nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamError)
	}
	if err := req.Validate(); err != nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error())
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}

	receiverIDs := req.ReceiverIDs
	if len(receiverIDs) == 0 {
		if err := l.svc.ReadDB(svc.DatabaseMain).Model(&model.Admin{}).Where("status = 1").Pluck("id", &receiverIDs).Error; err != nil {
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

	if err := l.svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
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
		Data: map[string]any{
			"id": msg.ID,
		},
	}
}

// ListSent 分页查询当前管理员的已发送消息列表。
func (l *AdminMessageLogic) ListSent(req *types.AdminMessageSentQueryReq) *types.BizResult {
	if req == nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamError)
	}
	if err := req.Validate(); err != nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error())
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}
	startTime, endTime, err := req.TimeRange()
	if err != nil {
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error())
	}

	items, total, err := model.ListAdminMessageSent(
		l.svc.ReadDB(svc.DatabaseMain),
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
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error())
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}

	if err := l.svc.ReadDB(svc.DatabaseMain).
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

	items, err := model.ListAdminMessageReceivers(l.svc.ReadDB(svc.DatabaseMain), req.ID)
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
		return types.NewBizResult(codes.ParamError).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyParamErrorFormat, err.Error())
	}
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		return types.NewBizResult(codes.Unauthorized).WithError(types.Nil).SetI18nMessage(i18n.MsgKeyNeedLogin)
	}

	var msg model.AdminMessage
	if err := l.svc.ReadDB(svc.DatabaseMain).Where("id = ?", req.ID).Take(&msg).Error; err != nil {
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

	if err := l.svc.ReadDB(svc.DatabaseMain).
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
		return &types.BizResult{
			Code:       codes.Success,
			MessageKey: i18n.MsgKeyUpdateSuccess,
			Data: map[string]any{
				"id":                 msg.ID,
				"handledStatus":      msg.HandledStatus,
				"handledByAdminName": msg.HandledByAdminName,
				"handledAt": func() string {
					if msg.HandledAt != nil {
						return msg.HandledAt.Format(time.DateTime)
					}
					return ""
				}(),
				"alreadyHandled": true,
			},
		}
	}

	updated, err := model.MarkAdminMessageHandled(l.svc.WriteDB(svc.DatabaseMain), msg.ID, ctxAdmin.ID, ctxAdmin.Name)
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
			Data: map[string]any{
				"id":             msg.ID,
				"handledStatus":  1,
				"alreadyHandled": false,
			},
		}
	}

	var latest model.AdminMessage
	if err := l.svc.ReadDB(svc.DatabaseMain).Where("id = ?", msg.ID).Take(&latest).Error; err != nil {
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
		Data: map[string]any{
			"id":                 latest.ID,
			"handledStatus":      latest.HandledStatus,
			"handledByAdminName": latest.HandledByAdminName,
			"handledAt":          handledAt,
			"alreadyHandled":     true,
		},
	}
}
