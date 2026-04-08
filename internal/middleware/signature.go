package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	codes "admin_cron/common/codes"
	keys "admin_cron/common/rediskeys"
	"admin_cron/helper"
	"admin_cron/internal/infra/loggerx"
	"admin_cron/internal/logic"
	"admin_cron/internal/requestctx"
	"admin_cron/internal/security"
	"admin_cron/internal/svc"

	"github.com/Is999/go-utils"
	"github.com/zeromicro/go-zero/core/logx"
)

// SignatureMiddleware 参考 laravel-admin SignatureData，对敏感请求验签并对敏感响应回签。
type SignatureMiddleware struct {
	svc *svc.ServiceContext // 签名中间件依赖的配置、缓存和秘钥读取服务
}

// NewSignatureMiddleware 创建签名中间件实例。
func NewSignatureMiddleware(svcCtx *svc.ServiceContext) *SignatureMiddleware {
	return &SignatureMiddleware{svc: svcCtx}
}

// Handle 按路由别名执行请求验签和响应回签。
func (m *SignatureMiddleware) Handle(next http.HandlerFunc, alias RouteAlias) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, _ := requestctx.New(r.Context())
		requestctx.SetRequest(ctx, r.Method, r.URL.Path, utils.ClientIP(r))
		if alias != "" && alias != Ignore {
			requestctx.SetRoute(ctx, string(alias))
		}
		r = r.WithContext(ctx)

		policy := security.PolicyByRoute(string(alias))
		if len(policy.RequestSign) == 0 && len(policy.ResponseSign) == 0 {
			next(w, r)
			return
		}
		appID, err := m.requestAppID(r)
		if err != nil {
			m.fail(w, r, http.StatusOK, codes.ParamError, err)
			return
		}
		routeConfig, err := logic.NewSecretKeyLogic(r.Context(), m.svc).GetRouteConfig(appID)
		if err != nil {
			m.fail(w, r, http.StatusOK, codes.InternalError, err)
			return
		}
		if routeConfig.Status != 1 {
			err := errors.New("当前应用秘钥配置已停用")
			m.fail(w, r, http.StatusOK, codes.AuthFailed, err)
			return
		}
		signEnabled := routeConfig.SignEnabled()
		signatureType := strings.ToUpper(strings.TrimSpace(r.Header.Get("X-Signature")))
		if signatureType == "" {
			signatureType = security.SignatureTypeRSA
		}
		traceID := strings.TrimSpace(r.Header.Get(requestctx.HeaderTraceID))
		if traceID == "" {
			err := errors.New("缺少请求头X-Trace-Id")
			m.fail(w, r, http.StatusOK, codes.AuthFailed, err)
			return
		}
		if signEnabled && len(policy.RequestSign) > 0 {
			if err := m.verifyRequest(r, policy, appID, traceID, signatureType); err != nil {
				m.fail(w, r, http.StatusOK, codes.AuthFailed, err)
				return
			}
		}

		recorder := newBodyRecorder()
		next(recorder, r)
		if signEnabled {
			recorder.Header().Set("X-Signature", signatureType)
		} else {
			recorder.Header().Del("X-Signature")
		}
		if signEnabled && strings.TrimSpace(traceID) != "" {
			// 响应回签沿用请求头 X-Trace-Id 参与签名，前端验签必须拿到同一值。
			recorder.Header().Set(requestctx.HeaderTraceID, strings.TrimSpace(traceID))
		}

		if signEnabled && len(policy.ResponseSign) > 0 && recorder.status < http.StatusBadRequest && recorder.body.Len() > 0 {
			resolvedVersion, err := m.signResponse(recorder, policy, appID, traceID, signatureType, r)
			if err != nil {
				m.fail(w, r, http.StatusOK, codes.InternalError, err)
				return
			}
			if strings.TrimSpace(resolvedVersion) != "" {
				recorder.Header().Set(secretKeyVersionHeader, strings.TrimSpace(resolvedVersion))
			}
		}
		flushRecordedResponse(w, recorder)
	}
}

// logRequestHeaders 在签名失败时输出完整请求头和错误链路，便于核对真实入站头与浏览器发出的差异。
func (m *SignatureMiddleware) logRequestHeaders(r *http.Request, appID string, signatureType string, err error) {
	fields := append(loggerx.FieldsFromContext(r.Context()),
		logx.Field("app_id", strings.TrimSpace(appID)),
		logx.Field("signature_type", strings.ToUpper(strings.TrimSpace(signatureType))),
		logx.Field("request_headers", cloneRequestHeaders(r.Header)),
		logx.Field("trace_id_header", r.Header.Values(requestctx.HeaderTraceID)),
	)
	loggerx.Errorw(r.Context(), "签名 请求头", err, fields...)
}

// verifyRequest 校验请求参数中的 sign 字段。
func (m *SignatureMiddleware) verifyRequest(r *http.Request, policy security.RouteSecurityPolicy, appID string, traceID string, signatureType string) error {
	params, err := requestParams(r)
	if err != nil {
		return errors.Tag(err)
	}
	signValue, ok := params["sign"]
	if !ok || security.SignValueString(signValue) == "" {
		return errors.New("缺少签名参数sign")
	}
	signStr := security.BuildSignString(params, policy.RequestSign, traceID, appID)
	signer, resolvedVersion, err := m.signer(r, appID, signatureType, true)
	if err != nil {
		return errors.Tag(err)
	}
	recordResolvedSecretKeyVersion(r, resolvedVersion)
	ok, err = signer.Verify(signStr, security.SignValueString(signValue))
	if err != nil {
		return errors.Tag(err)
	}
	if !ok {
		return errors.New("签名错误")
	}
	if err := m.markRequestVerified(r, appID, traceID); err != nil {
		return errors.Tag(err)
	}
	return nil
}

// signResponse 对标准响应 data 首层字段生成 sign 并写回响应体。
func (m *SignatureMiddleware) signResponse(recorder *bodyRecorder, policy security.RouteSecurityPolicy, appID string, traceID string, signatureType string, r *http.Request) (string, error) {
	var envelope map[string]any
	if err := json.Unmarshal(recorder.body.Bytes(), &envelope); err != nil {
		return "", nil
	}
	status, _ := envelope["status"].(bool)
	if !status {
		return "", nil
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok || data == nil {
		return "", nil
	}
	signStr := security.BuildSignString(data, policy.ResponseSign, traceID, appID)
	signer, resolvedVersion, err := m.signer(r, appID, signatureType, false)
	if err != nil {
		return "", errors.Tag(err)
	}
	signValue, err := signer.Sign(signStr)
	if err != nil {
		return "", errors.Tag(err)
	}
	data["sign"] = signValue
	envelope["data"] = data
	body, err := json.Marshal(envelope)
	if err != nil {
		return "", errors.Tag(err)
	}
	recorder.body.Reset()
	_, _ = recorder.body.Write(body)
	recorder.Header().Set("Content-Length", "")
	return resolvedVersion, nil
}

// signer 根据 X-Signature 获取签名或验签实现。
func (m *SignatureMiddleware) signer(r *http.Request, appID string, signatureType string, isVerify bool) (security.Signer, string, error) {
	secretKeyLogic := logic.NewSecretKeyLogic(r.Context(), m.svc)
	versionHint := requestSecretKeyVersionHint(r)
	grayKey := requestSecretKeyGrayKey(r)
	switch signatureType {
	case security.SignatureTypeMD5:
		return security.MD5Signer{}, "", nil
	case security.SignatureTypeAES:
		aesKey, resolvedVersion, err := secretKeyLogic.GetAESKey(appID, versionHint, grayKey)
		if err != nil {
			return nil, "", errors.Tag(err)
		}
		signer, err := security.NewAESCipher(aesKey.Key, aesKey.IV)
		return signer, resolvedVersion, errors.Tag(err)
	case security.SignatureTypeRSA:
		keyType := logic.RSAServerPrivateKey
		if isVerify {
			keyType = logic.RSAUserPublicKey
		}
		pemText, resolvedVersion, err := secretKeyLogic.GetRSAKey(appID, versionHint, grayKey, keyType)
		if err != nil {
			return nil, "", errors.Tag(err)
		}
		recordResolvedSecretKeyVersion(r, resolvedVersion)
		if isVerify {
			signer, err := security.NewRSASigner("", pemText)
			if err != nil {
				return nil, "", errors.Tag(err)
			}
			return signer, resolvedVersion, nil
		}
		signer, err := security.NewRSASigner(pemText, "")
		if err != nil {
			return nil, "", errors.Tag(err)
		}
		return signer, resolvedVersion, nil
	default:
		return nil, "", errors.New("签名方式不合法")
	}
}

// requestAppID 从 X-App-Id 请求头解析真实 AppID。
func (m *SignatureMiddleware) requestAppID(r *http.Request) (string, error) {
	raw := r.Header.Get("X-App-Id")
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("缺少请求头X-App-Id")
	}
	appID, err := decodeBase64Header(raw)
	if err != nil {
		return "", errors.New("请求头X-App-Id格式错误")
	}
	return appID, nil
}

// fail 写出签名中间件失败响应，响应文案直接由业务码解析，错误详情只进入日志链路。
func (m *SignatureMiddleware) fail(w http.ResponseWriter, r *http.Request, httpStatus int, code int, err error) {
	appID := ""
	if m != nil {
		if currentAppID, currentErr := m.requestAppID(r); currentErr == nil {
			appID = currentAppID
		}
	}
	signatureType := strings.ToUpper(strings.TrimSpace(r.Header.Get("X-Signature")))
	if signatureType == "" {
		signatureType = security.SignatureTypeRSA
	}
	fields := append(loggerx.FieldsFromContext(r.Context()),
		logx.Field("http_status", httpStatus),
		logx.Field("biz_code", code),
	)
	loggerx.Errorw(r.Context(), "签名 处理失败", err, fields...)
	m.logRequestHeaders(r, appID, signatureType, err)
	helper.NewJsonResp(r.Context(), w).
		SetHttpStatus(httpStatus).
		SetCode(code).
		SetError(err).
		Fail("")
}

// cloneRequestHeaders 复制当前请求头，避免后续链路复用底层切片时影响调试日志。
func cloneRequestHeaders(header http.Header) map[string][]string {
	result := make(map[string][]string, len(header))
	for key, values := range header {
		copiedValues := make([]string, 0, len(values))
		copiedValues = append(copiedValues, values...)
		result[key] = copiedValues
	}
	return result
}

// markRequestVerified 使用 Redis 记录已验签请求，避免同一个 trace_id 在有效期内重复提交。
func (m *SignatureMiddleware) markRequestVerified(r *http.Request, appID string, traceID string) error {
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(traceID) == "" {
		return errors.New("签名请求标识不能为空")
	}
	if m.svc == nil || m.svc.Rds == nil {
		return errors.New("签名防重放缓存未初始化")
	}
	key := fmt.Sprintf(keys.SignatureReplayRequest, appID, traceID)
	ok, err := m.svc.Rds.SetNX(r.Context(), key, "1", 5*time.Minute).Result()
	if err != nil {
		return errors.Tag(err)
	}
	if !ok {
		return errors.New("重复请求")
	}
	return nil
}
