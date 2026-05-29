package runtimeconfig

import (
	"net/http"

	"admin/internal/handler/shared"
	runtimeconfiglogic "admin/internal/logic/runtimeconfig"
	"admin/internal/svc"
	"admin/internal/types"
)

// GetOverviewHandler 查询运行配置概览。
func GetOverviewHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.MethodRuntimeConfigOverview, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := runtimeconfiglogic.NewRuntimeConfigLogic(r, sCtx)
		return logicObj, logicObj.Overview().WithReq(map[string]any{"action": "runtime_config_overview"})
	})
}

// ListPeriodicTasksHandler 查询周期任务草稿。
func ListPeriodicTasksHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RuntimeTaskPeriodicQueryReq](shared.MethodRuntimeConfigList, func(r *http.Request, sCtx *svc.ServiceContext, req *types.RuntimeTaskPeriodicQueryReq) (shared.LogicObj, *types.BizResult) {
		logicObj := runtimeconfiglogic.NewRuntimeConfigLogic(r, sCtx)
		return logicObj, logicObj.ListPeriodicTasks(req)
	})(sCtx)
}

// SavePeriodicTaskHandler 保存周期任务草稿。
func SavePeriodicTaskHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SaveRuntimeTaskPeriodicReq](shared.MethodRuntimeConfigSave, func(r *http.Request, sCtx *svc.ServiceContext, req *types.SaveRuntimeTaskPeriodicReq) (shared.LogicObj, *types.BizResult) {
		logicObj := runtimeconfiglogic.NewRuntimeConfigLogic(r, sCtx)
		return logicObj, logicObj.SavePeriodicTask(req)
	})(sCtx)
}

// DeletePeriodicTaskHandler 删除周期任务草稿。
func DeletePeriodicTaskHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RuntimeConfigIDReq](shared.MethodRuntimeConfigSave, func(r *http.Request, sCtx *svc.ServiceContext, req *types.RuntimeConfigIDReq) (shared.LogicObj, *types.BizResult) {
		logicObj := runtimeconfiglogic.NewRuntimeConfigLogic(r, sCtx)
		return logicObj, logicObj.DeletePeriodicTask(req)
	})(sCtx)
}

// ListArchiveJobsHandler 查询归档任务草稿。
func ListArchiveJobsHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RuntimeArchiveJobQueryReq](shared.MethodRuntimeConfigList, func(r *http.Request, sCtx *svc.ServiceContext, req *types.RuntimeArchiveJobQueryReq) (shared.LogicObj, *types.BizResult) {
		logicObj := runtimeconfiglogic.NewRuntimeConfigLogic(r, sCtx)
		return logicObj, logicObj.ListArchiveJobs(req)
	})(sCtx)
}

// SaveArchiveJobHandler 保存归档任务草稿。
func SaveArchiveJobHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SaveRuntimeArchiveJobReq](shared.MethodRuntimeConfigSave, func(r *http.Request, sCtx *svc.ServiceContext, req *types.SaveRuntimeArchiveJobReq) (shared.LogicObj, *types.BizResult) {
		logicObj := runtimeconfiglogic.NewRuntimeConfigLogic(r, sCtx)
		return logicObj, logicObj.SaveArchiveJob(req)
	})(sCtx)
}

// DeleteArchiveJobHandler 删除归档任务草稿。
func DeleteArchiveJobHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RuntimeConfigIDReq](shared.MethodRuntimeConfigSave, func(r *http.Request, sCtx *svc.ServiceContext, req *types.RuntimeConfigIDReq) (shared.LogicObj, *types.BizResult) {
		logicObj := runtimeconfiglogic.NewRuntimeConfigLogic(r, sCtx)
		return logicObj, logicObj.DeleteArchiveJob(req)
	})(sCtx)
}

// ValidateDraftHandler 预检运行配置草稿。
func ValidateDraftHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionLogHandler(shared.MethodRuntimeConfigValidate, func(r *http.Request) (shared.LogicObj, *types.BizResult) {
		logicObj := runtimeconfiglogic.NewRuntimeConfigLogic(r, sCtx)
		return logicObj, logicObj.ValidateDraft().WithReq(map[string]any{"action": "runtime_config_validate"})
	})
}

// PublishHandler 发布运行配置草稿。
func PublishHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RuntimeConfigPublishReq](shared.MethodRuntimeConfigPublish, func(r *http.Request, sCtx *svc.ServiceContext, req *types.RuntimeConfigPublishReq) (shared.LogicObj, *types.BizResult) {
		logicObj := runtimeconfiglogic.NewRuntimeConfigLogic(r, sCtx)
		return logicObj, logicObj.Publish(req)
	})(sCtx)
}

// ListReleasesHandler 查询运行配置发布历史。
func ListReleasesHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RuntimeConfigReleaseQueryReq](shared.MethodRuntimeConfigList, func(r *http.Request, sCtx *svc.ServiceContext, req *types.RuntimeConfigReleaseQueryReq) (shared.LogicObj, *types.BizResult) {
		logicObj := runtimeconfiglogic.NewRuntimeConfigLogic(r, sCtx)
		return logicObj, logicObj.ListReleases(req)
	})(sCtx)
}

// GetReleaseHandler 查询运行配置发布快照。
func GetReleaseHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RuntimeConfigReleaseIDReq](shared.MethodRuntimeConfigList, func(r *http.Request, sCtx *svc.ServiceContext, req *types.RuntimeConfigReleaseIDReq) (shared.LogicObj, *types.BizResult) {
		logicObj := runtimeconfiglogic.NewRuntimeConfigLogic(r, sCtx)
		return logicObj, logicObj.GetRelease(req)
	})(sCtx)
}

// RollbackHandler 回滚运行配置发布快照。
func RollbackHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RuntimeConfigRollbackReq](shared.MethodRuntimeConfigRollback, func(r *http.Request, sCtx *svc.ServiceContext, req *types.RuntimeConfigRollbackReq) (shared.LogicObj, *types.BizResult) {
		logicObj := runtimeconfiglogic.NewRuntimeConfigLogic(r, sCtx)
		return logicObj, logicObj.Rollback(req)
	})(sCtx)
}

// ImportCurrentHandler 导入当前运行配置并发布。
func ImportCurrentHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.RuntimeConfigImportReq](shared.MethodRuntimeConfigImport, func(r *http.Request, sCtx *svc.ServiceContext, req *types.RuntimeConfigImportReq) (shared.LogicObj, *types.BizResult) {
		logicObj := runtimeconfiglogic.NewRuntimeConfigLogic(r, sCtx)
		return logicObj, logicObj.ImportCurrent(req)
	})(sCtx)
}
