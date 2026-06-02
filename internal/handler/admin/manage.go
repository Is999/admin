package admin

import (
	"admin/internal/handler/shared"
	"net/http"

	i18n "admin/common/i18n"
	adminlogic "admin/internal/logic/admin"
	"admin/internal/svc"
	"admin/internal/types"
	"admin/pkg/transfer"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// ListAdminHandler 查询管理员列表。
func ListAdminHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.AdminListReq](shared.MethodListAdmin,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminListReq) (shared.LogicObj, *types.BizResult) {
			logicObj := adminlogic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.List(req)
		},
	)(sCtx)
}

// GetAdminHandler 查询管理员详情。
func GetAdminHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.IDPathReq](shared.MethodGetAdmin,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.IDPathReq) (shared.LogicObj, *types.BizResult) {
			logicObj := adminlogic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.Get(req)
		},
	)(sCtx)
}

// UpdateAdminHandler 编辑管理员。
func UpdateAdminHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.UpdateAdminReq](shared.MethodUpdateAdmin,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.UpdateAdminReq) (shared.LogicObj, *types.BizResult) {
			logicObj := adminlogic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.Update(req)
		},
	)(sCtx)
}

// DeleteAdminHandler 删除管理员。
func DeleteAdminHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.IDPathReq](shared.MethodDeleteAdmin,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.IDPathReq) (shared.LogicObj, *types.BizResult) {
			logicObj := adminlogic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.Delete(req)
		},
	)(sCtx)
}

// UpdateAdminStatusHandler 修改管理员状态。
func UpdateAdminStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.AdminStatusReq](shared.MethodUpdateAdminStatus,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminStatusReq) (shared.LogicObj, *types.BizResult) {
			logicObj := adminlogic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.UpdateStatus(req)
		},
	)(sCtx)
}

// ResetAdminPasswordHandler 重置管理员密码。
func ResetAdminPasswordHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ResetAdminPasswordReq](shared.MethodResetAdminPassword,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ResetAdminPasswordReq) (shared.LogicObj, *types.BizResult) {
			logicObj := adminlogic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.ResetPassword(req)
		},
	)(sCtx)
}

// ResetAdminInitialStateHandler 重置管理员到首次登录前状态。
func ResetAdminInitialStateHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.ResetAdminInitialStateReq](shared.MethodResetAdminInitialState,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ResetAdminInitialStateReq) (shared.LogicObj, *types.BizResult) {
			logicObj := adminlogic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.ResetInitialState(req)
		},
	)(sCtx)
}

// ListAdminRolesHandler 查询管理员已绑定角色。
func ListAdminRolesHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.IDPathReq](shared.MethodListAdminRoles,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.IDPathReq) (shared.LogicObj, *types.BizResult) {
			logicObj := adminlogic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.ListRoles(req)
		},
	)(sCtx)
}

// TriggerAdminExportHandler 提交管理员列表异步导出任务。
func TriggerAdminExportHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.AdminExportReq](shared.MethodTriggerAdminExport,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminExportReq) (shared.LogicObj, *types.BizResult) {
			logicObj := adminlogic.NewAdminExportLogic(r, svcCtx)
			return logicObj, logicObj.Trigger(req)
		},
	)(sCtx)
}

// GetAdminExportStatusHandler 查询管理员列表异步导出进度。
func GetAdminExportStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.AdminExportJobReq](shared.MethodGetAdminExportStatus,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminExportJobReq) (shared.LogicObj, *types.BizResult) {
			logicObj := adminlogic.NewAdminExportLogic(r, svcCtx)
			return logicObj, logicObj.GetStatus(req)
		},
	)(sCtx)
}

// DownloadAdminExportHandler 下载已生成的管理员导出文件。
func DownloadAdminExportHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AdminExportJobReq
		if err := httpx.Parse(r, &req); err != nil {
			shared.WriteBizResponse(w, r, nil, shared.ParamErrorResult(0, err), nil, "")
			return
		}

		logicObj := adminlogic.NewAdminExportLogic(r, sCtx)
		logMeta := shared.ActionLogParamFromMeta(shared.AdminExportDownload)
		status, resp := logicObj.PrepareDownload(&req)
		if resp != nil {
			resp.WithReq(&req)
			shared.WriteBizResponse(w, r, logicObj, resp, logMeta, shared.MethodDownloadAdminExport)
			return
		}

		if logMeta != nil {
			logicObj.AddAdminLog(logMeta.Action, logMeta.Route, string(shared.MethodDownloadAdminExport), logMeta.Describe, &req)
		}

		objectStream, err := logicObj.OpenDownloadObject(status, r.Header.Get("Range"))
		if err != nil {
			resp := types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
				"DownloadAdminExportHandler 打开管理员导出对象失败").ToBizResult()
			resp.WithReq(&req)
			shared.WriteBizResponse(w, r, logicObj, resp, logMeta, shared.MethodDownloadAdminExport)
			return
		}
		defer objectStream.Reader.Close()

		if err := transfer.ServeStream(
			w,
			r,
			objectStream.Reader,
			status.FileName,
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			objectStream.ContentLength,
			"attachment",
			objectStream.AcceptRanges,
			objectStream.ContentRange,
		); err != nil {
			resp := types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
				"DownloadAdminExportHandler 输出管理员导出文件[%s]失败", status.FileName).ToBizResult()
			resp.WithReq(&req)
			shared.WriteBizResponse(w, r, logicObj, resp, logMeta, shared.MethodDownloadAdminExport)
		}
	}
}

// UpdateAdminRolesHandler 替换管理员角色。
func UpdateAdminRolesHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.AdminRoleAssignReq](shared.MethodUpdateAdminRoles,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminRoleAssignReq) (shared.LogicObj, *types.BizResult) {
			logicObj := adminlogic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.ReplaceRoles(req)
		},
	)(sCtx)
}
