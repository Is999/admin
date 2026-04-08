package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Is999/go-utils/errors"

	i18n "admin_cron/common/i18n"
	"admin_cron/internal/logic"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"
	"admin_cron/pkg/transfer"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// ListAdminHandler 查询管理员列表。
func ListAdminHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.AdminListReq](listAdmin,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminListReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.List(req)
		},
	)(sCtx)
}

// GetAdminHandler 查询管理员详情。
func GetAdminHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.IDPathReq](getAdmin,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.IDPathReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.Get(req)
		},
	)(sCtx)
}

// UpdateAdminHandler 编辑管理员。
func UpdateAdminHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.UpdateAdminReq](updateAdmin,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.UpdateAdminReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.Update(req)
		},
	)(sCtx)
}

// DeleteAdminHandler 删除管理员。
func DeleteAdminHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.IDPathReq](deleteAdmin,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.IDPathReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.Delete(req)
		},
	)(sCtx)
}

// UpdateAdminStatusHandler 修改管理员状态。
func UpdateAdminStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.AdminStatusReq](updateAdminStatus,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminStatusReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.UpdateStatus(req)
		},
	)(sCtx)
}

// ResetAdminPasswordHandler 重置管理员密码。
func ResetAdminPasswordHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.ResetAdminPasswordReq](resetAdminPassword,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ResetAdminPasswordReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.ResetPassword(req)
		},
	)(sCtx)
}

// ResetAdminInitialStateHandler 重置管理员到首次登录前状态。
func ResetAdminInitialStateHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.ResetAdminInitialStateReq](resetAdminInitialState,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.ResetAdminInitialStateReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.ResetInitialState(req)
		},
	)(sCtx)
}

// ListAdminRolesHandler 查询管理员已绑定角色。
func ListAdminRolesHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.IDPathReq](listAdminRoles,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.IDPathReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.ListRoles(req)
		},
	)(sCtx)
}

// TriggerAdminExportHandler 提交管理员列表异步导出任务。
func TriggerAdminExportHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.AdminExportReq](triggerAdminExport,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminExportReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminExportLogic(r, svcCtx)
			return logicObj, logicObj.Trigger(req)
		},
	)(sCtx)
}

// GetAdminExportStatusHandler 查询管理员列表异步导出进度。
func GetAdminExportStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.AdminExportJobReq](getAdminExportStatus,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminExportJobReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminExportLogic(r, svcCtx)
			return logicObj, logicObj.GetStatus(req)
		},
	)(sCtx)
}

// DownloadAdminExportHandler 下载已生成的管理员导出文件。
func DownloadAdminExportHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AdminExportJobReq
		if err := httpx.Parse(r, &req); err != nil {
			writeBizResponse(w, r, nil, paramErrorResult(0, err), nil, "")
			return
		}

		logicObj := logic.NewAdminExportLogic(r, sCtx)
		status, resp := logicObj.PrepareDownload(&req)
		if resp != nil {
			resp.WithReq(&req)
			writeBizResponse(w, r, logicObj, resp, actionLogMap(downloadAdminExport), downloadAdminExport)
			return
		}

		if logMeta := actionLogMap(downloadAdminExport); logMeta != nil {
			logicObj.AddAdminLog(logMeta.action, logMeta.route, string(downloadAdminExport), logMeta.describe, &req)
		}

		objectStream, err := logicObj.OpenDownloadObject(status, r.Header.Get("Range"))
		if err != nil {
			resp := types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
				"DownloadAdminExportHandler 打开管理员导出对象失败").ToBizResult()
			resp.WithReq(&req)
			writeBizResponse(w, r, logicObj, resp, actionLogMap(downloadAdminExport), downloadAdminExport)
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
			writeBizResponse(w, r, logicObj, resp, actionLogMap(downloadAdminExport), downloadAdminExport)
		}
	}
}

// UpdateAdminRolesHandler 替换管理员角色。
func UpdateAdminRolesHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.AdminRoleAssignReq](updateAdminRoles,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminRoleAssignReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.ReplaceRoles(req)
		},
	)(sCtx)
}

// AddAdminRoleHandler 添加管理员角色。
func AddAdminRoleHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.AdminRoleAssignReq](addAdminRole,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminRoleAssignReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.AddRole(req)
		},
	)(sCtx)
}

// DeleteAdminRoleHandler 解除管理员角色。
func DeleteAdminRoleHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.AdminRoleDeleteReq](deleteAdminRole,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.AdminRoleDeleteReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminManageLogic(r, svcCtx)
			return logicObj, logicObj.DeleteRole(req)
		},
	)(sCtx)
}

// ListRoleHandler 查询角色列表。
func ListRoleHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.RoleListReq](listRole,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RoleListReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminRoleLogic(r, svcCtx)
			return logicObj, logicObj.List(req)
		},
	)(sCtx)
}

// TreeRoleHandler 查询角色树。
func TreeRoleHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return actionLogHandler(treeRole, func(r *http.Request) (LogicObj, *types.BizResult) {
		logicObj := logic.NewAdminRoleLogic(r, sCtx)
		return logicObj, logicObj.TreeList().WithReq(map[string]any{"action": "tree_role"})
	})
}

// TreeRoleOptionsHandler 查询角色树下拉数据，仅要求登录态合法，不额外校验角色管理权限。
func TreeRoleOptionsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return respHandler(func(r *http.Request) (LogicObj, *types.BizResult) {
		logicObj := logic.NewAdminRoleLogic(r, sCtx)
		return logicObj, logicObj.TreeList().WithReq(map[string]any{"action": "tree_role_options"})
	})
}

// AddRoleHandler 新增角色。
func AddRoleHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.CreateRoleReq](addRole,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CreateRoleReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminRoleLogic(r, svcCtx)
			return logicObj, logicObj.Create(req.ToSaveRoleReq())
		},
	)(sCtx)
}

// UpdateRoleHandler 编辑角色。
func UpdateRoleHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.SaveRoleReq](updateRole,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SaveRoleReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminRoleLogic(r, svcCtx)
			return logicObj, logicObj.Update(req)
		},
	)(sCtx)
}

// DeleteRoleHandler 删除角色。
func DeleteRoleHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.IDPathReq](deleteRole,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.IDPathReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminRoleLogic(r, svcCtx)
			return logicObj, logicObj.Delete(req)
		},
	)(sCtx)
}

// UpdateRoleStatusHandler 修改角色状态。
func UpdateRoleStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.RoleStatusReq](updateRoleStatus,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RoleStatusReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminRoleLogic(r, svcCtx)
			return logicObj, logicObj.UpdateStatus(req)
		},
	)(sCtx)
}

// GetRolePermissionHandler 查询角色权限树。
func GetRolePermissionHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.RolePermissionReq](getRolePermission,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RolePermissionReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminRoleLogic(r, svcCtx)
			return logicObj, logicObj.PermissionTree(req)
		},
	)(sCtx)
}

// UpdateRolePermissionHandler 编辑角色权限。
func UpdateRolePermissionHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.RolePermissionSaveReq](updateRolePermission,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.RolePermissionSaveReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminRoleLogic(r, svcCtx)
			return logicObj, logicObj.SavePermissions(req)
		},
	)(sCtx)
}

// ListPermissionHandler 查询权限列表。
func ListPermissionHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.PermissionListReq](listPermission,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.PermissionListReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminPermissionLogic(r, svcCtx)
			return logicObj, logicObj.List(req)
		},
	)(sCtx)
}

// TreePermissionHandler 查询权限树。
func TreePermissionHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return actionLogHandler(treePermission, func(r *http.Request) (LogicObj, *types.BizResult) {
		logicObj := logic.NewAdminPermissionLogic(r, sCtx)
		return logicObj, logicObj.TreeList().WithReq(map[string]any{"action": "tree_permission"})
	})
}

// MaxPermissionUUIDHandler 查询下一个权限 UUID。
func MaxPermissionUUIDHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return actionLogHandler(maxPermissionUUID, func(r *http.Request) (LogicObj, *types.BizResult) {
		logicObj := logic.NewAdminPermissionLogic(r, sCtx)
		return logicObj, logicObj.MaxUUID().WithReq(map[string]any{"action": "max_permission_uuid"})
	})
}

// AddPermissionHandler 新增权限。
func AddPermissionHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.CreatePermissionReq](addPermission,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CreatePermissionReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminPermissionLogic(r, svcCtx)
			return logicObj, logicObj.Create(req.ToSavePermissionReq())
		},
	)(sCtx)
}

// UpdatePermissionHandler 编辑权限。
func UpdatePermissionHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.SavePermissionReq](updatePermission,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SavePermissionReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminPermissionLogic(r, svcCtx)
			return logicObj, logicObj.Update(req)
		},
	)(sCtx)
}

// DeletePermissionHandler 删除权限。
func DeletePermissionHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.IDPathReq](deletePermission,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.IDPathReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminPermissionLogic(r, svcCtx)
			return logicObj, logicObj.Delete(req)
		},
	)(sCtx)
}

// UpdatePermissionStatusHandler 修改权限状态。
func UpdatePermissionStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.PermissionStatusReq](updatePermissionStatus,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.PermissionStatusReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewAdminPermissionLogic(r, svcCtx)
			return logicObj, logicObj.UpdateStatus(req)
		},
	)(sCtx)
}

// ListSysConfigHandler 查询系统常量配置列表。
func ListSysConfigHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.SysConfigListReq](listSysConfig,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SysConfigListReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSysConfigLogic(r, svcCtx)
			return logicObj, logicObj.List(req)
		},
	)(sCtx)
}

// AddSysConfigHandler 新增系统常量配置。
func AddSysConfigHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return actionLogHandler(addSysConfig, func(r *http.Request) (LogicObj, *types.BizResult) {
		var req types.CreateSysConfigReq
		if err := parseSysConfigJSONRequest(r, &req); err != nil {
			return nil, paramErrorResult(0, err)
		}
		logicObj := logic.NewSysConfigLogic(r, sCtx)
		resp := logicObj.Create(req.ToSaveSysConfigReq())
		resp.WithReq(&req)
		return logicObj, resp
	})
}

// UpdateSysConfigHandler 编辑系统常量配置。
func UpdateSysConfigHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return actionLogHandler(updateSysConfig, func(r *http.Request) (LogicObj, *types.BizResult) {
		var req types.SaveSysConfigReq
		if err := parseSysConfigJSONRequest(r, &req); err != nil {
			return nil, paramErrorResult(0, err)
		}
		logicObj := logic.NewSysConfigLogic(r, sCtx)
		resp := logicObj.Update(&req)
		resp.WithReq(&req)
		return logicObj, resp
	})
}

// parseSysConfigJSONRequest 为系统配置接口优先使用标准 JSON 解析，兼容标量 value/example。
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
	return ActionHandler[types.UUIDPathReq](getSysConfigCache,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.UUIDPathReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSysConfigLogic(r, svcCtx)
			return logicObj, logicObj.GetCache(req)
		},
	)(sCtx)
}

// RenewSysConfigHandler 刷新系统常量配置缓存。
func RenewSysConfigHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.UUIDPathReq](renewSysConfig,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.UUIDPathReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSysConfigLogic(r, svcCtx)
			return logicObj, logicObj.Renew(req)
		},
	)(sCtx)
}

// ExportSysConfigExcelHandler 导出字典配置 Excel 文件。
func ExportSysConfigExcelHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.SysConfigExcelExportReq
		if err := httpx.Parse(r, &req); err != nil {
			writeBizResponse(w, r, nil, paramErrorResult(0, err), nil, "")
			return
		}
		logicObj := logic.NewSysConfigLogic(r, sCtx)
		filePath, fileName, resp := logicObj.ExportExcel(&req)
		if resp != nil {
			resp.WithReq(&req)
			writeBizResponse(w, r, logicObj, resp, actionLogMap(exportSysConfigExcel), exportSysConfigExcel)
			return
		}
		if logMeta := actionLogMap(exportSysConfigExcel); logMeta != nil {
			logicObj.AddAdminLog(logMeta.action, logMeta.route, string(exportSysConfigExcel), logMeta.describe, &req)
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
			writeBizResponse(w, r, logicObj, resp, actionLogMap(exportSysConfigExcel), exportSysConfigExcel)
		}
	}
}

// ImportSysConfigExcelHandler 导入字典配置 Excel 文件。
func ImportSysConfigExcelHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.SysConfigExcelImportReq](importSysConfigExcel,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SysConfigExcelImportReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSysConfigLogic(r, svcCtx)
			return logicObj, logicObj.ImportExcel(req)
		},
	)(sCtx)
}

// ListCacheHandler 查询缓存目标列表。
func ListCacheHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.CacheListReq](listCache,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CacheListReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSystemCacheLogic(r, svcCtx)
			return logicObj, logicObj.List(req)
		},
	)(sCtx)
}

// GetCacheServerInfoHandler 查询 Redis 服务器信息。
func GetCacheServerInfoHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return actionLogHandler(getCacheServerInfo, func(r *http.Request) (LogicObj, *types.BizResult) {
		logicObj := logic.NewSystemCacheLogic(r, sCtx)
		return logicObj, logicObj.ServerInfo().WithReq(map[string]any{"action": "cache_server_info"})
	})
}

// GetCacheKeyInfoHandler 查询 Redis key 信息。
func GetCacheKeyInfoHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.CacheKeyReq](getCacheKeyInfo,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CacheKeyReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSystemCacheLogic(r, svcCtx)
			return logicObj, logicObj.KeyInfo(req)
		},
	)(sCtx)
}

// SearchCacheKeyHandler 搜索 Redis key。
func SearchCacheKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.CacheKeyReq](searchCacheKey,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CacheKeyReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSystemCacheLogic(r, svcCtx)
			return logicObj, logicObj.SearchKey(req)
		},
	)(sCtx)
}

// RenewCacheHandler 刷新指定缓存。
func RenewCacheHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.CacheRenewReq](renewCache,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CacheRenewReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSystemCacheLogic(r, svcCtx)
			return logicObj, logicObj.Renew(req)
		},
	)(sCtx)
}

// RenewAllCacheHandler 刷新全部内置缓存。
func RenewAllCacheHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return actionLogHandler(renewAllCache, func(r *http.Request) (LogicObj, *types.BizResult) {
		logicObj := logic.NewSystemCacheLogic(r, sCtx)
		return logicObj, logicObj.RenewAll().WithReq(map[string]any{"action": "renew_all_cache"})
	})
}

// WarmupCacheHandler 按模板预热缓存，解决模板 key 在 Redis 未命中时无法全量刷新的问题。
func WarmupCacheHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.CacheWarmupReq](warmupCache,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CacheWarmupReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSystemCacheLogic(r, svcCtx)
			return logicObj, logicObj.WarmupTemplate(req)
		},
	)(sCtx)
}

// ListSecretKeyHandler 查询秘钥配置列表。
func ListSecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.SecretKeyListReq](listSecretKey,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecretKeyListReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.List(req)
		},
	)(sCtx)
}

// GetSecretKeyHandler 查询单个秘钥详情。
func GetSecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.SecretKeyDetailReq](getSecretKey,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecretKeyDetailReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.Get(req)
		},
	)(sCtx)
}

// AddSecretKeyHandler 新增秘钥配置。
func AddSecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.CreateSecretKeyReq](addSecretKey,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CreateSecretKeyReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.Create(req.ToSaveSecretKeyReq())
		},
	)(sCtx)
}

// UpdateSecretKeyHandler 编辑秘钥配置。
func UpdateSecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.SaveSecretKeyReq](updateSecretKey,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SaveSecretKeyReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.Update(req)
		},
	)(sCtx)
}

// UpdateSecretKeyStatusHandler 修改秘钥启用状态。
func UpdateSecretKeyStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.SecretKeyStatusReq](updateSecretKeyStatus,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecretKeyStatusReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.UpdateStatus(req)
		},
	)(sCtx)
}

// RenewSecretKeyHandler 刷新指定 AppID 的秘钥缓存。
func RenewSecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.SecretKeyRenewReq](renewSecretKey,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecretKeyRenewReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.Renew(req)
		},
	)(sCtx)
}

// ValidateSecretKeyHandler 对待保存的秘钥路径执行静态预检。
func ValidateSecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.SecretKeyValidateReq](validateSecretKey,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecretKeyValidateReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.ValidatePaths(req)
		},
	)(sCtx)
}

// SelfCheckSecretKeyHandler 对已落库的秘钥执行运行态自检。
func SelfCheckSecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.SecretKeySelfCheckReq](selfCheckSecretKey,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecretKeySelfCheckReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.SelfCheck(req)
		},
	)(sCtx)
}

// SecurityDebugSignHandler 模拟请求或响应参数签名。
func SecurityDebugSignHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.SecurityDebugSignReq](securityDebugSign,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecurityDebugSignReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSecurityDebugLogic(r, svcCtx)
			return logicObj, logicObj.Sign(req)
		},
	)(sCtx)
}

// SecurityDebugVerifyHandler 模拟请求或响应参数验签。
func SecurityDebugVerifyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.SecurityDebugVerifyReq](securityDebugVerify,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecurityDebugVerifyReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSecurityDebugLogic(r, svcCtx)
			return logicObj, logicObj.Verify(req)
		},
	)(sCtx)
}

// SecurityDebugEncryptHandler 模拟请求或响应参数加密。
func SecurityDebugEncryptHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.SecurityDebugCipherReq](securityDebugEncrypt,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecurityDebugCipherReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSecurityDebugLogic(r, svcCtx)
			return logicObj, logicObj.Encrypt(req)
		},
	)(sCtx)
}

// SecurityDebugDecryptHandler 模拟请求或响应参数解密。
func SecurityDebugDecryptHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return ActionHandler[types.SecurityDebugCipherReq](securityDebugDecrypt,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecurityDebugCipherReq) (LogicObj, *types.BizResult) {
			logicObj := logic.NewSecurityDebugLogic(r, svcCtx)
			return logicObj, logicObj.Decrypt(req)
		},
	)(sCtx)
}
