package admin

import (
	codes "admin/common/codes"
	i18n "admin/common/i18n"
	"admin/internal/audit"
	"admin/internal/jobs/archive"
	corelogic "admin/internal/logic"
	"admin/internal/svc"
	"admin/internal/types"

	"net/http"
	"time"

	"github.com/Is999/go-utils/errors"
)

// AdminLogLogic 负责管理员审计日志的查询与结果转换。
type AdminLogLogic struct {
	*corelogic.BaseLogic // 复用上下文、数据库和日志能力
}

// NewAdminLogLogic 创建管理员日志业务对象。
func NewAdminLogLogic(r *http.Request, svcCtx *svc.ServiceContext) *AdminLogLogic {
	return &AdminLogLogic{
		BaseLogic: corelogic.NewBaseLogic(r, svcCtx),
	}
}

// QueryAdminLog 按筛选条件分页查询管理员操作日志，并转换为前端展示结构。
// 当前查询只读取热表数据，不再拼接归档历史表，保证后台日志查询链路稳定且易排障。
func (l *AdminLogLogic) QueryAdminLog(req *types.AdminLogQueryReq) *types.BizResult {
	// 管理员日志查询统一经由归档服务封装，但当前策略固定只查热表。
	archiveService := archive.NewService(l.Svc, archive.WithControlDatabase(svc.DatabaseMain))
	logs, total, meta, err := archiveService.QueryAdminLogs(l.Ctx, req)
	if err != nil {
		return &types.BizResult{
			Code:       codes.DBError,
			MessageKey: i18n.MsgKeyQueryFail,
			Error:      errors.Wrap(err, "查询管理员审计日志失败"),
		}
	}
	items := make([]types.AdminLogItem, 0, len(logs))
	// 这里把链路字段一并透出给前端，便于后台直接按 trace_id 定位整条请求链路。
	for _, log := range logs {
		items = append(items, types.AdminLogItem{
			ID:       log.ID,
			UserID:   log.UserID,
			UserName: log.UserName,
			Action:   log.Action,
			Route:    log.Route,
			Method:   log.Method,
			Describe: log.Describe,
			// 审计日志查询阶段再次脱敏，覆盖历史已落库但未完全命中脱敏规则的敏感字段。
			Data:         audit.SanitizeText(log.Data, 4096),
			IP:           log.IP,
			Ipaddr:       log.Ipaddr,
			TraceID:      log.TraceID,
			SpanID:       log.SpanID,
			HTTPStatus:   log.HTTPStatus,
			BizCode:      log.BizCode,
			LatencyMS:    log.LatencyMS,
			Success:      log.Success,
			ErrorMessage: log.ErrorMessage,
			CreatedAt:    log.CreatedAt.Format(time.DateTime),
		})
	}
	return &types.BizResult{
		Code:       codes.Success,
		MessageKey: i18n.MsgKeyQuerySuccess,
		Data:       &types.ListResp[types.AdminLogItem]{List: items, Total: total, Meta: meta},
	}
}
