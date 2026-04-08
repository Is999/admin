package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	codes "admin_cron/common/codes"
	"admin_cron/helper"
	"admin_cron/internal/logic"
	"admin_cron/internal/requestctx"
	"admin_cron/internal/security"
	"admin_cron/internal/svc"
	"github.com/Is999/go-utils/errors"
)

const (
	// cipherWholeBody 表示 X-Cipher=cipher 时对整个请求或响应内容加解密。
	cipherWholeBody = security.CipherWholeBody
	// cipherJSONPrefix 表示字段值加解密前需要做 JSON 编解码。
	cipherJSONPrefix = security.CipherJSONPrefix
	// secretKeyVersionHeader 表示当前请求显式指定或服务端最终命中的秘钥版本头。
	secretKeyVersionHeader = "X-Key-Version"
	// secretKeyGrayKeyHeader 表示当前请求用于灰度选路的稳定分桶键。
	secretKeyGrayKeyHeader = "X-Gray-Key"
)

// CryptoMiddleware 参考 laravel-admin CryptoData，对请求敏感字段解密并对响应敏感字段加密。
type CryptoMiddleware struct {
	svc *svc.ServiceContext // 加密中间件依赖的配置、缓存和秘钥读取服务
}

// NewCryptoMiddleware 创建加密中间件实例。
func NewCryptoMiddleware(svcCtx *svc.ServiceContext) *CryptoMiddleware {
	return &CryptoMiddleware{svc: svcCtx}
}

// Handle 根据 X-Cipher/X-Crypto 请求头执行加解密，未携带 X-Cipher 时完全兼容普通请求。
func (m *CryptoMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// policy 表示当前路由别名命中的请求/响应加密策略。
		policy := security.PolicyByRoute(requestRouteAlias(r))
		// requestCipher 表示本次请求实际声明的解密字段或整包配置。
		requestCipher := strings.TrimSpace(r.Header.Get("X-Cipher"))
		// cryptoType 表示当前请求声明的加密算法，默认回落为 AES。
		cryptoType := strings.ToUpper(strings.TrimSpace(r.Header.Get("X-Crypto")))
		if cryptoType == "" {
			cryptoType = security.CryptoTypeAES
		}

		// appID 表示当前请求绑定的秘钥应用标识，请求解密和响应加密都会复用它。
		appID := ""
		cryptoEnabled := true
		var err error
		if requestCipher != "" || len(policy.ResponseCipher) > 0 {
			appID, err = m.requestAppID(r)
			if err != nil {
				m.fail(w, r, codes.ParamError, err)
				return
			}
			routeConfig, err := logic.NewSecretKeyLogic(r.Context(), m.svc).GetRouteConfig(appID)
			if err != nil {
				m.fail(w, r, codes.InternalError, err)
				return
			}
			if routeConfig.Status != 1 {
				err := errors.New("当前应用秘钥配置已停用")
				m.fail(w, r, codes.AuthFailed, err)
				return
			}
			cryptoEnabled = routeConfig.CryptoEnabled()
		}
		if !cryptoEnabled {
			if requestCipher != "" {
				err := errors.New("当前应用已关闭加密解密链路")
				m.fail(w, r, codes.AuthFailed, err)
				return
			}
			r.Header.Del("X-Cipher")
			r.Header.Del("X-Crypto")
		}
		if requestCipher != "" {
			cryptor, resolvedVersion, err := m.cryptor(r, appID, cryptoType, false)
			if err != nil {
				m.fail(w, r, codes.InternalError, err)
				return
			}
			recordResolvedSecretKeyVersion(r, resolvedVersion)
			if err := m.decryptRequest(r, requestCipher, cryptor); err != nil {
				m.fail(w, r, codes.AuthFailed, err)
				return
			}
		}

		// recorder 先拦截业务响应，待需要签名或加密时再统一改写后刷出。
		recorder := newBodyRecorder()
		next(recorder, r)

		// responseCipher 表示当前响应需要按哪些字段或整包方式执行加密。
		responseCipher := strings.TrimSpace(recorder.Header().Get("X-Cipher"))
		if cryptoEnabled && responseCipher == "" && len(policy.ResponseCipher) > 0 && recorder.status < http.StatusBadRequest {
			responseCipher = security.EncodeCipherParams(policy.ResponseCipher)
			if responseCipher != "" {
				recorder.Header().Set("X-Cipher", responseCipher)
			}
		}
		if !cryptoEnabled {
			recorder.Header().Del("X-Cipher")
			recorder.Header().Del("X-Crypto")
		}
		if cryptoEnabled && (requestCipher != "" || responseCipher != "") {
			recorder.Header().Set("X-Crypto", cryptoType)
		}
		if cryptoEnabled && responseCipher != "" && recorder.body.Len() > 0 {
			if appID == "" {
				appID, err = m.requestAppID(r)
				if err != nil {
					m.fail(w, r, codes.ParamError, err)
					return
				}
			}
			cryptor, resolvedVersion, err := m.cryptor(r, appID, cryptoType, true)
			if err != nil {
				m.fail(w, r, codes.InternalError, err)
				return
			}
			recordResolvedSecretKeyVersion(r, resolvedVersion)
			if resolvedVersion != "" {
				recorder.Header().Set(secretKeyVersionHeader, resolvedVersion)
			}
			if err := m.encryptResponse(recorder, responseCipher, cryptor); err != nil {
				m.fail(w, r, codes.InternalError, err)
				return
			}
		}
		flushRecordedResponse(w, recorder)
	}
}

// requestRouteAlias 从请求上下文读取统一路由别名。
func requestRouteAlias(r *http.Request) string {
	if r == nil {
		return ""
	}
	if meta := requestctx.FromContext(r.Context()); meta != nil {
		return strings.TrimSpace(meta.Route)
	}
	return ""
}

// decryptRequest 解密请求体或请求体首层字段。
func (m *CryptoMiddleware) decryptRequest(r *http.Request, cipherHeader string, cryptor security.Cryptor) error {
	if strings.EqualFold(cipherHeader, cipherWholeBody) {
		params, err := requestParams(r)
		if err != nil {
			return errors.Tag(err)
		}
		cipherText := security.SignValueString(params["ciphertext"])
		if cipherText == "" {
			body, err := readRequestBody(r)
			if err != nil {
				return errors.Tag(err)
			}
			cipherText = strings.TrimSpace(string(body))
		}
		if cipherText == "" {
			return nil
		}
		plain, err := cryptor.Decrypt(cipherText)
		if err != nil {
			return errors.Wrap(err, "请求解密失败")
		}
		var bodyMap map[string]any
		if err := json.Unmarshal([]byte(plain), &bodyMap); err != nil {
			return errors.Wrap(err, "解密后的请求体不是JSON对象")
		}
		return replaceJSONBody(r, bodyMap)
	}

	cipherParams, err := decodeCipherParams(cipherHeader)
	if err != nil {
		return errors.Tag(err)
	}
	bodyMap, err := requestJSONMap(r)
	if err != nil {
		return errors.Tag(err)
	}
	for _, param := range cipherParams {
		// isJSON 表示该字段原始值需要先做 JSON 编解码再加解密。
		isJSON := strings.HasPrefix(param, cipherJSONPrefix)
		// field 表示当前要解密的请求首层字段名。
		field := strings.TrimPrefix(param, cipherJSONPrefix)
		value, ok := bodyMap[field]
		if !ok || security.SignValueString(value) == "" {
			continue
		}
		plain, err := cryptor.Decrypt(security.SignValueString(value))
		if err != nil {
			return errors.Wrapf(err, "请求字段[%s]解密失败", field)
		}
		if isJSON {
			var jsonValue any
			if plain != "" {
				if err := json.Unmarshal([]byte(plain), &jsonValue); err != nil {
					return errors.Wrapf(err, "请求字段[%s] JSON解码失败", field)
				}
			}
			bodyMap[field] = jsonValue
		} else {
			bodyMap[field] = plain
		}
	}
	return replaceJSONBody(r, bodyMap)
}

// encryptResponse 加密响应体或响应 data 下的字段；字段路径支持 `user.buildMFAURL` 这类点路径。
func (m *CryptoMiddleware) encryptResponse(recorder *bodyRecorder, cipherHeader string, cryptor security.Cryptor) error {
	if strings.EqualFold(cipherHeader, cipherWholeBody) {
		encrypted, err := cryptor.Encrypt(recorder.body.String())
		if err != nil {
			return errors.Wrap(err, "响应整体加密失败")
		}
		recorder.body.Reset()
		_, _ = recorder.body.WriteString(encrypted)
		recorder.Header().Del("Content-Length")
		return nil
	}

	cipherParams, err := decodeCipherParams(cipherHeader)
	if err != nil {
		return errors.Tag(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(recorder.body.Bytes(), &envelope); err != nil {
		return nil
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok || data == nil {
		return nil
	}
	for _, param := range cipherParams {
		// isJSON 表示当前响应字段写回前需要按 JSON 文本整体加密。
		isJSON := strings.HasPrefix(param, cipherJSONPrefix)
		// fieldPath 表示当前响应 data 下需要加密的点路径字段。
		fieldPath := strings.TrimPrefix(param, cipherJSONPrefix)
		value, ok := nestedCipherValue(data, fieldPath)
		if !ok || security.SignValueString(value) == "" {
			continue
		}
		plain := security.SignValueString(value)
		if isJSON {
			body, err := json.Marshal(value)
			if err != nil {
				return errors.Wrapf(err, "响应字段[%s] JSON编码失败", fieldPath)
			}
			plain = string(body)
		}
		encrypted, err := cryptor.Encrypt(plain)
		if err != nil {
			return errors.Wrapf(err, "响应字段[%s]加密失败", fieldPath)
		}
		if ok := setNestedCipherValue(data, fieldPath, encrypted); !ok {
			return errors.Errorf("响应字段[%s]写回加密结果失败", fieldPath)
		}
	}
	envelope["data"] = data
	body, err := json.Marshal(envelope)
	if err != nil {
		return errors.Tag(err)
	}
	recorder.body.Reset()
	_, _ = recorder.body.Write(body)
	recorder.Header().Del("Content-Length")
	return nil
}

// nestedCipherValue 按点路径读取 map 中的嵌套字段值。
func nestedCipherValue(data map[string]any, fieldPath string) (any, bool) {
	if data == nil {
		return nil, false
	}
	parts := splitCipherFieldPath(fieldPath)
	if len(parts) == 0 {
		return nil, false
	}
	current := any(data)
	for _, part := range parts {
		node, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, exists := node[part]
		if !exists {
			return nil, false
		}
		current = value
	}
	return current, true
}

// setNestedCipherValue 按点路径回写 map 中的嵌套字段值。
func setNestedCipherValue(data map[string]any, fieldPath string, value any) bool {
	if data == nil {
		return false
	}
	parts := splitCipherFieldPath(fieldPath)
	if len(parts) == 0 {
		return false
	}
	current := data
	for index, part := range parts {
		if index == len(parts)-1 {
			current[part] = value
			return true
		}
		next, ok := current[part].(map[string]any)
		if !ok {
			return false
		}
		current = next
	}
	return false
}

// splitCipherFieldPath 把 `user.buildMFAURL` 这类字段路径拆成逐级键名。
func splitCipherFieldPath(fieldPath string) []string {
	fieldPath = strings.TrimSpace(fieldPath)
	if fieldPath == "" {
		return nil
	}
	rawParts := strings.Split(fieldPath, ".")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

// cryptor 根据 X-Crypto 获取加密或解密实现。
func (m *CryptoMiddleware) cryptor(r *http.Request, appID string, cryptoType string, isEncrypt bool) (security.Cryptor, string, error) {
	secretKeyLogic := logic.NewSecretKeyLogic(r.Context(), m.svc)
	versionHint := requestSecretKeyVersionHint(r)
	grayKey := requestSecretKeyGrayKey(r)
	switch cryptoType {
	case security.CryptoTypeAES:
		aesKey, resolvedVersion, err := secretKeyLogic.GetAESKey(appID, versionHint, grayKey)
		if err != nil {
			return nil, "", errors.Tag(err)
		}
		cryptor, err := security.NewAESCipher(aesKey.Key, aesKey.IV)
		return cryptor, resolvedVersion, errors.Tag(err)
	case security.CryptoTypeRSA:
		keyType := logic.RSAServerPrivateKey
		if isEncrypt {
			keyType = logic.RSAUserPublicKey
		}
		pemText, resolvedVersion, err := secretKeyLogic.GetRSAKey(appID, versionHint, grayKey, keyType)
		if err != nil {
			return nil, "", errors.Tag(err)
		}
		if isEncrypt {
			cipherObj, err := security.NewRSACipher("", pemText)
			if err != nil {
				return nil, "", errors.Tag(err)
			}
			return cipherObj, resolvedVersion, nil
		}
		cipherObj, err := security.NewRSACipher(pemText, "")
		if err != nil {
			return nil, "", errors.Tag(err)
		}
		return cipherObj, resolvedVersion, nil
	default:
		return nil, "", errors.New("加密方式不合法")
	}
}

// requestSecretKeyVersionHint 读取请求头中显式指定的秘钥版本。
func requestSecretKeyVersionHint(r *http.Request) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.Header.Get(secretKeyVersionHeader))
}

// requestSecretKeyGrayKey 读取请求头中的灰度分桶键。
func requestSecretKeyGrayKey(r *http.Request) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.Header.Get(secretKeyGrayKeyHeader))
}

// recordResolvedSecretKeyVersion 把最终命中的秘钥版本写回请求头，供后续中间件和响应复用。
func recordResolvedSecretKeyVersion(r *http.Request, resolvedVersion string) {
	if r == nil || strings.TrimSpace(resolvedVersion) == "" {
		return
	}
	r.Header.Set(secretKeyVersionHeader, strings.TrimSpace(resolvedVersion))
}

// requestAppID 从 X-App-Id 请求头解析真实 AppID。
func (m *CryptoMiddleware) requestAppID(r *http.Request) (string, error) {
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

// fail 写出加密中间件失败响应，响应文案直接由业务码解析，错误详情只进入日志链路。
func (m *CryptoMiddleware) fail(w http.ResponseWriter, r *http.Request, code int, err error) {
	helper.NewJsonResp(r.Context(), w).
		SetHttpStatus(http.StatusOK).
		SetCode(code).
		SetError(err).
		Fail("")
}
