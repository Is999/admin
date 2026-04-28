package config

import (
	"admin/internal/handler/shared"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Is999/go-utils/errors"

	i18n "admin/common/i18n"
	configlogic "admin/internal/logic/config"
	"admin/internal/svc"
	"admin/internal/types"
	"admin/pkg/transfer"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// ListSysConfigHandler 查询系统常量配置列表。
func ListSysConfigHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SysConfigListReq](shared.MethodListSysConfig,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SysConfigListReq) (shared.LogicObj, *types.BizResult) {
			logicObj := configlogic.NewSysConfigLogic(r, svcCtx)
			return logicObj, logicObj.List(req)
		},
	)(sCtx)
}

// AddSysConfigHandler 新增系统常量配置。
func AddSysConfigHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.MethodAddSysConfig, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		var req types.CreateSysConfigReq
		if err := parseSysConfigJSONRequest(r, &req); err != nil {
			return nil, shared.ParamErrorResult(0, err)
		}
		logicObj := configlogic.NewSysConfigLogic(r, sCtx)
		resp := logicObj.Create(req.ToSaveSysConfigReq())
		resp.WithReq(&req)
		return logicObj, resp
	})
}

// UpdateSysConfigHandler 编辑系统常量配置。
func UpdateSysConfigHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.MethodUpdateSysConfig, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		var req types.SaveSysConfigReq
		if err := parseSysConfigJSONRequest(r, &req); err != nil {
			return nil, shared.ParamErrorResult(0, err)
		}
		logicObj := configlogic.NewSysConfigLogic(r, sCtx)
		resp := logicObj.Update(&req)
		resp.WithReq(&req)
		return logicObj, resp
	})
}

// parseSysConfigJSONRequest 为系统配置接口优先使用标准 JSON 解析，支持标量 value/example。
func parseSysConfigJSONRequest(r *http.Request, req any) error {
	if !strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return httpx.Parse(r, req)
	}
	if err := httpx.ParsePath(r, req); err != nil {
		return errors.Tag(err)
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return errors.Tag(err)
	}
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	if len(strings.TrimSpace(string(body))) == 0 {
		return httpx.ParseJsonBody(r, req)
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(req); err != nil {
		return errors.Tag(err)
	}
	if validator, ok := req.(interface{ Validate() error }); ok {
		return validator.Validate()
	}
	return nil
}

// GetSysConfigCacheHandler 查看系统常量配置缓存。
func GetSysConfigCacheHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.UUIDPathReq](shared.MethodGetSysConfigCache,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.UUIDPathReq) (shared.LogicObj, *types.BizResult) {
			logicObj := configlogic.NewSysConfigLogic(r, svcCtx)
			return logicObj, logicObj.GetCache(req)
		},
	)(sCtx)
}

// RenewSysConfigHandler 刷新系统常量配置缓存。
func RenewSysConfigHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.UUIDPathReq](shared.MethodRenewSysConfig,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.UUIDPathReq) (shared.LogicObj, *types.BizResult) {
			logicObj := configlogic.NewSysConfigLogic(r, svcCtx)
			return logicObj, logicObj.Renew(req)
		},
	)(sCtx)
}

// ExportSysConfigExcelHandler 导出字典配置 Excel 文件。
func ExportSysConfigExcelHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.SysConfigExcelExportReq
		if err := httpx.Parse(r, &req); err != nil {
			shared.WriteBizResponse(w, r, nil, shared.ParamErrorResult(0, err), nil, "")
			return
		}
		logicObj := configlogic.NewSysConfigLogic(r, sCtx)
		filePath, fileName, resp := logicObj.ExportExcel(&req)
		if resp != nil {
			resp.WithReq(&req)
			shared.WriteBizResponse(w, r, logicObj, resp, shared.ActionLogMap(shared.MethodExportSysConfigExcel), shared.MethodExportSysConfigExcel)
			return
		}
		if logMeta := shared.ActionLogMap(shared.MethodExportSysConfigExcel); logMeta != nil {
			logicObj.AddAdminLog(logMeta.Action, logMeta.Route, string(shared.MethodExportSysConfigExcel), logMeta.Describe, &req)
		}
		if err := transfer.ServeDownload(
			w,
			r,
			filePath,
			fileName,
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		); err != nil {
			resp := types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
				"ExportSysConfigExcelHandler 输出字典导出文件[%s]失败", filePath).ToBizResult()
			resp.WithReq(&req)
			shared.WriteBizResponse(w, r, logicObj, resp, shared.ActionLogMap(shared.MethodExportSysConfigExcel), shared.MethodExportSysConfigExcel)
		}
	}
}

// ImportSysConfigExcelHandler 导入字典配置 Excel 文件。
func ImportSysConfigExcelHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SysConfigExcelImportReq](shared.MethodImportSysConfigExcel,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SysConfigExcelImportReq) (shared.LogicObj, *types.BizResult) {
			logicObj := configlogic.NewSysConfigLogic(r, svcCtx)
			return logicObj, logicObj.ImportExcel(req)
		},
	)(sCtx)
}
