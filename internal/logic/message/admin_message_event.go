package message

import (
	corelogic "admin/internal/logic"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"admin/internal/infra/loggerx"
	"admin/internal/model"
	"admin/internal/svc"

	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

const (
	// adminMessageEventTimeout 为系统事件类消息写入预留短超时，避免对主业务链路造成明显阻塞。
	adminMessageEventTimeout = 800 * time.Millisecond
)

// EmitAdminLoginMessage 在管理员登录成功后，给“超级管理员”与“登录本人”投递一条登录提醒消息。
// 该方法属于系统自动事件，不走管理员操作审计链路。
func EmitAdminLoginMessage(ctx context.Context, svcCtx *svc.ServiceContext, loginAdminID int, loginAdminName, loginIP string) {
	if svcCtx == nil || svcCtx.WriteDB(svc.DatabaseMain) == nil {
		return
	}
	if loginAdminID <= 0 || loginAdminName == "" {
		return
	}

	eventCtx := context.Background()
	if ctx != nil {
		eventCtx = context.WithoutCancel(ctx)
	}
	eventCtx, cancel := context.WithTimeout(eventCtx, adminMessageEventTimeout)
	defer cancel()

	base := corelogic.NewBaseLogicWithContext(eventCtx, svcCtx)
	if base == nil {
		return
	}
	writeDB := base.Svc.WriteDB(svc.DatabaseMain)
	receiverIDs, err := listSuperAdminIDs(writeDB)
	if err != nil {
		loggerx.Errorw(eventCtx, "站内消息 超管查询失败", err,
			logx.Field("event", "管理员登录"),
			logx.Field("admin_id", loginAdminID),
		)
		return
	}
	if len(receiverIDs) == 0 {
		receiverIDs = []int{loginAdminID}
	}
	if !containsInt(receiverIDs, loginAdminID) {
		receiverIDs = append(receiverIDs, loginAdminID)
	}

	dataBytes, _ := json.Marshal(map[string]any{
		"ip":       loginIP,
		"username": loginAdminName,
	})
	now := time.Now()
	msg := &model.AdminMessage{
		Type:            "admin_login",
		Level:           int(model.AdminMessageLevelInfo),
		Title:           "管理员登录提醒",
		Content:         fmt.Sprintf("管理员[%s]于 %s 登录后台，登录IP：%s", loginAdminName, now.Format(time.DateTime), loginIP),
		Data:            string(dataBytes),
		Link:            "",
		SenderAdminID:   loginAdminID,
		SenderAdminName: loginAdminName,
		CreatedAt:       now,
	}

	if err := base.Svc.WriteDB(svc.DatabaseMain).Transaction(func(tx *gorm.DB) error {
		return model.CreateAdminMessageWithReceivers(tx, msg, receiverIDs)
	}); err != nil {
		tagErr := errors.Tag(err)
		loggerx.Errorw(eventCtx, "站内消息 投递失败", tagErr,
			logx.Field("event", "管理员登录"),
			logx.Field("admin_id", loginAdminID),
			logx.Field("receiver_count", len(receiverIDs)),
		)
	}
}

// listSuperAdminIDs 查询全部启用的超级管理员账号 ID。
func listSuperAdminIDs(db *gorm.DB) ([]int, error) {
	if db == nil {
		return nil, errors.Tag(gorm.ErrInvalidDB)
	}
	var ids []int
	err := db.Table(model.TableNameAdmin+" AS a").
		Joins("JOIN "+model.TableNameAdminRoleRel+" AS r ON r.user_id = a.id").
		Where("a.status = 1").
		Where("r.role_id = ?", 1).
		Distinct("a.id").
		Pluck("a.id", &ids).Error
	return ids, errors.Tag(err)
}

// containsInt 判断切片中是否包含指定值。
func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
