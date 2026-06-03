package secretkey

import (
	"admin/internal/handler/shared"
	"net/http"

	secretkeylogic "admin/internal/logic/secretkey"
	"admin/internal/svc"
	"admin/internal/types"
)

// ListSecretKeyHandler 查询秘钥配置列表。
func ListSecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SecretKeyListReq](shared.SecretKeyList,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecretKeyListReq) (shared.LogicObj, *types.BizResult) {
			logicObj := secretkeylogic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.List(req)
		},
	)(sCtx)
}

// GetSecretKeyHandler 查询单个秘钥详情。
func GetSecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SecretKeyDetailReq](shared.SecretKeyGet,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecretKeyDetailReq) (shared.LogicObj, *types.BizResult) {
			logicObj := secretkeylogic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.Get(req)
		},
	)(sCtx)
}

// AddSecretKeyHandler 新增秘钥配置。
func AddSecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.CreateSecretKeyReq](shared.SecretKeyAdd,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.CreateSecretKeyReq) (shared.LogicObj, *types.BizResult) {
			logicObj := secretkeylogic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.Create(req.ToSaveSecretKeyReq())
		},
	)(sCtx)
}

// UpdateSecretKeyHandler 编辑秘钥配置。
func UpdateSecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SaveSecretKeyReq](shared.SecretKeyUpdate,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SaveSecretKeyReq) (shared.LogicObj, *types.BizResult) {
			logicObj := secretkeylogic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.Update(req)
		},
	)(sCtx)
}

// UpdateSecretKeyStatusHandler 修改秘钥启用状态。
func UpdateSecretKeyStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SecretKeyStatusReq](shared.SecretKeyStatus,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecretKeyStatusReq) (shared.LogicObj, *types.BizResult) {
			logicObj := secretkeylogic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.UpdateStatus(req)
		},
	)(sCtx)
}

// RenewSecretKeyHandler 刷新指定 AppID 的秘钥缓存。
func RenewSecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SecretKeyRenewReq](shared.SecretKeyRenew,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecretKeyRenewReq) (shared.LogicObj, *types.BizResult) {
			logicObj := secretkeylogic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.Renew(req)
		},
	)(sCtx)
}

// ValidateSecretKeyHandler 对待保存的秘钥路径执行静态预检。
func ValidateSecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SecretKeyValidateReq](shared.SecretKeyValidate,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecretKeyValidateReq) (shared.LogicObj, *types.BizResult) {
			logicObj := secretkeylogic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.ValidatePaths(req)
		},
	)(sCtx)
}

// SelfCheckSecretKeyHandler 对已落库的秘钥执行运行态自检。
func SelfCheckSecretKeyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SecretKeySelfCheckReq](shared.SecretKeySelfCheck,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecretKeySelfCheckReq) (shared.LogicObj, *types.BizResult) {
			logicObj := secretkeylogic.NewSecretKeyManageLogic(r, svcCtx)
			return logicObj, logicObj.SelfCheck(req)
		},
	)(sCtx)
}
