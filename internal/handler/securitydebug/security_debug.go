package securitydebug

import (
	"admin/internal/handler/shared"
	"net/http"

	securitydebuglogic "admin/internal/logic/securitydebug"
	"admin/internal/svc"
	"admin/internal/types"
)

// SecurityDebugSignHandler 模拟请求或响应参数签名。
func SecurityDebugSignHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SecurityDebugSignReq](shared.SecurityDebugSign,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecurityDebugSignReq) (shared.LogicObj, *types.BizResult) {
			logicObj := securitydebuglogic.NewSecurityDebugLogic(r, svcCtx)
			return logicObj, logicObj.Sign(req)
		},
	)(sCtx)
}

// SecurityDebugVerifyHandler 模拟请求或响应参数验签。
func SecurityDebugVerifyHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SecurityDebugVerifyReq](shared.SecurityDebugVerify,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecurityDebugVerifyReq) (shared.LogicObj, *types.BizResult) {
			logicObj := securitydebuglogic.NewSecurityDebugLogic(r, svcCtx)
			return logicObj, logicObj.Verify(req)
		},
	)(sCtx)
}

// SecurityDebugEncryptHandler 模拟请求或响应参数加密。
func SecurityDebugEncryptHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SecurityDebugCipherReq](shared.SecurityDebugEncrypt,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecurityDebugCipherReq) (shared.LogicObj, *types.BizResult) {
			logicObj := securitydebuglogic.NewSecurityDebugLogic(r, svcCtx)
			return logicObj, logicObj.Encrypt(req)
		},
	)(sCtx)
}

// SecurityDebugDecryptHandler 模拟请求或响应参数解密。
func SecurityDebugDecryptHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.ActionHandler[types.SecurityDebugCipherReq](shared.SecurityDebugDecrypt,
		func(r *http.Request, svcCtx *svc.ServiceContext, req *types.SecurityDebugCipherReq) (shared.LogicObj, *types.BizResult) {
			logicObj := securitydebuglogic.NewSecurityDebugLogic(r, svcCtx)
			return logicObj, logicObj.Decrypt(req)
		},
	)(sCtx)
}
