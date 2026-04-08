package logic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	codes "admin_cron/common/codes"
	i18n "admin_cron/common/i18n"
	"admin_cron/internal/security"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

	"github.com/Is999/go-utils/errors"
)

// SecurityDebugLogic 承载后台安全调试台的签名、验签、加密、解密模拟能力。
type SecurityDebugLogic struct {
	*BaseLogic // 复用上下文、日志、审计和秘钥读取能力
}

// NewSecurityDebugLogic 创建安全调试台逻辑对象。
func NewSecurityDebugLogic(r *http.Request, svcCtx *svc.ServiceContext) *SecurityDebugLogic {
	return &SecurityDebugLogic{
		BaseLogic: NewBaseLogic(r, svcCtx),
	}
}

// Sign 模拟前后端对请求或响应参数进行签名，返回待签名串和签名值方便观察。
func (l *SecurityDebugLogic) Sign(req *types.SecurityDebugSignReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(errors.Errorf("签名请求不能为空"))
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	payload, payloadText, err := parseJSONObjectText(req.PayloadText)
	if err != nil {
		return types.ParamErrorResult(err)
	}
	traceID := normalizedDebugTraceID(req.RequestID)
	signFields := normalizeSecurityFields(req.NormalizedSignFields())
	signer, err := l.buildSigner(req.AppID, req.NormalizedSignatureType(), false)
	if err != nil {
		return types.ServerError(i18n.MsgKeyQueryFail, err, "SecurityDebugLogic.Sign 初始化签名器失败").ToBizResult()
	}
	signText := security.BuildSignString(payload, signFields, traceID, req.AppID)
	signValue, err := signer.Sign(signText)
	if err != nil {
		return types.ServerError(i18n.MsgKeyQueryFail, err, "SecurityDebugLogic.Sign 生成签名失败").ToBizResult()
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.SecurityDebugSignResp{
			AppID:         req.AppID,
			RequestID:     traceID,
			TraceID:       traceID,
			SignatureType: req.NormalizedSignatureType(),
			SignFields:    signFields,
			Payload:       payload,
			PayloadText:   payloadText,
			SignText:      signText,
			Sign:          signValue,
		})
}

// Verify 模拟前后端对请求或响应参数进行验签，返回待验签串与验签结果。
func (l *SecurityDebugLogic) Verify(req *types.SecurityDebugVerifyReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(errors.Errorf("验签请求不能为空"))
	}
	if err := req.Validate(); err != nil {
		return types.ParamErrorResult(err)
	}
	payload, payloadText, err := parseJSONObjectText(req.PayloadText)
	if err != nil {
		return types.ParamErrorResult(err)
	}
	traceID := normalizedDebugTraceID(req.RequestID)
	signFields := normalizeSecurityFields(req.NormalizedSignFields())
	signer, err := l.buildSigner(req.AppID, req.NormalizedSignatureType(), true)
	if err != nil {
		return types.ServerError(i18n.MsgKeyQueryFail, err, "SecurityDebugLogic.Verify 初始化验签器失败").ToBizResult()
	}
	signText := security.BuildSignString(payload, signFields, traceID, req.AppID)
	verified, err := signer.Verify(signText, req.Sign)
	if err != nil {
		return types.ServerError(i18n.MsgKeyQueryFail, err, "SecurityDebugLogic.Verify 执行验签失败").ToBizResult()
	}
	return types.NewBizResult(codes.Success).
		SetI18nMessage(i18n.MsgKeyQuerySuccess).
		WithData(types.SecurityDebugVerifyResp{
			SecurityDebugSignResp: types.SecurityDebugSignResp{
				AppID:         req.AppID,
				RequestID:     traceID,
				TraceID:       traceID,
				SignatureType: req.NormalizedSignatureType(),
				SignFields:    signFields,
				Payload:       payload,
				PayloadText:   payloadText,
				SignText:      signText,
				Sign:          req.Sign,
			},
			Verified: verified,
		})
}

// Encrypt 模拟前后端按整包或字段模式对数据执行加密。
func (l *SecurityDebugLogic) Encrypt(req *types.SecurityDebugCipherReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(errors.Errorf("加密请求不能为空"))
	}
	if err := req.ValidateEncrypt(); err != nil {
		return types.ParamErrorResult(err)
	}
	cipherFields := normalizeSecurityFields(req.NormalizedCipherFields())
	cryptor, err := l.buildCryptor(req.AppID, req.NormalizedCryptoType(), true)
	if err != nil {
		return types.ServerError(i18n.MsgKeyQueryFail, err, "SecurityDebugLogic.Encrypt 初始化加密器失败").ToBizResult()
	}
	resp := types.SecurityDebugCipherResp{
		AppID:        req.AppID,
		CryptoType:   req.NormalizedCryptoType(),
		CipherFields: cipherFields,
		CipherHeader: security.EncodeCipherParams(cipherFields),
		WholeBody:    req.IsWholeBodyCipher(),
		PayloadText:  req.PayloadText,
	}
	if req.IsWholeBodyCipher() {
		ciphertext, err := cryptor.Encrypt(req.PayloadText)
		if err != nil {
			return types.ServerError(i18n.MsgKeyQueryFail, err, "SecurityDebugLogic.Encrypt 整包加密失败").ToBizResult()
		}
		resp.Ciphertext = ciphertext
		resp.Plaintext = req.PayloadText
		return types.NewBizResult(codes.Success).SetI18nMessage(i18n.MsgKeyQuerySuccess).WithData(resp)
	}
	payload, payloadText, err := parseJSONObjectText(req.PayloadText)
	if err != nil {
		return types.ParamErrorResult(err)
	}
	result, resultText, err := encryptFields(payload, cipherFields, cryptor)
	if err != nil {
		return types.ServerError(i18n.MsgKeyQueryFail, err, "SecurityDebugLogic.Encrypt 字段加密失败").ToBizResult()
	}
	resp.Payload = payload
	resp.PayloadText = payloadText
	resp.ResultPayload = result
	resp.ResultPayloadText = resultText
	return types.NewBizResult(codes.Success).SetI18nMessage(i18n.MsgKeyQuerySuccess).WithData(resp)
}

// Decrypt 模拟前后端按整包或字段模式对数据执行解密。
func (l *SecurityDebugLogic) Decrypt(req *types.SecurityDebugCipherReq) *types.BizResult {
	if req == nil {
		return types.ParamErrorResult(errors.Errorf("解密请求不能为空"))
	}
	if err := req.ValidateDecrypt(); err != nil {
		return types.ParamErrorResult(err)
	}
	cipherFields := normalizeSecurityFields(req.NormalizedCipherFields())
	cryptor, err := l.buildCryptor(req.AppID, req.NormalizedCryptoType(), false)
	if err != nil {
		return types.ServerError(i18n.MsgKeyQueryFail, err, "SecurityDebugLogic.Decrypt 初始化解密器失败").ToBizResult()
	}
	resp := types.SecurityDebugCipherResp{
		AppID:        req.AppID,
		CryptoType:   req.NormalizedCryptoType(),
		CipherFields: cipherFields,
		CipherHeader: security.EncodeCipherParams(cipherFields),
		WholeBody:    req.IsWholeBodyCipher(),
	}
	if req.IsWholeBodyCipher() {
		plainText, err := cryptor.Decrypt(req.Ciphertext)
		if err != nil {
			return types.ServerError(i18n.MsgKeyQueryFail, err, "SecurityDebugLogic.Decrypt 整包解密失败").ToBizResult()
		}
		resp.Ciphertext = req.Ciphertext
		resp.Plaintext = plainText
		resp.PayloadText = req.Ciphertext
		return types.NewBizResult(codes.Success).SetI18nMessage(i18n.MsgKeyQuerySuccess).WithData(resp)
	}
	payload, payloadText, err := parseJSONObjectText(req.PayloadText)
	if err != nil {
		return types.ParamErrorResult(err)
	}
	result, resultText, err := decryptFields(payload, cipherFields, cryptor)
	if err != nil {
		return types.ServerError(i18n.MsgKeyQueryFail, err, "SecurityDebugLogic.Decrypt 字段解密失败").ToBizResult()
	}
	resp.Payload = payload
	resp.PayloadText = payloadText
	resp.ResultPayload = result
	resp.ResultPayloadText = resultText
	return types.NewBizResult(codes.Success).SetI18nMessage(i18n.MsgKeyQuerySuccess).WithData(resp)
}

// buildSigner 根据 AppID 和签名方式初始化签名器或验签器。
func (l *SecurityDebugLogic) buildSigner(appID string, signatureType string, isVerify bool) (security.Signer, error) {
	switch signatureType {
	case security.SignatureTypeMD5:
		return security.MD5Signer{}, nil
	case security.SignatureTypeAES:
		aesKey, err := NewSecretKeyLogic(l.Context(), l.svc).GetAESKeyByAppID(appID)
		if err != nil {
			return nil, errors.Tag(err)
		}
		return security.NewAESCipher(aesKey.Key, aesKey.IV)
	case security.SignatureTypeRSA:
		keyType := RSAServerPrivateKey
		if isVerify {
			keyType = RSAUserPublicKey
		}
		pemText, err := NewSecretKeyLogic(l.Context(), l.svc).GetRSAKeyByAppID(appID, keyType)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if isVerify {
			return security.NewRSASigner("", pemText)
		}
		return security.NewRSASigner(pemText, "")
	default:
		return nil, errors.Errorf("签名方式不合法: %s", signatureType)
	}
}

// buildCryptor 根据 AppID 和加密方式初始化加解密器。
func (l *SecurityDebugLogic) buildCryptor(appID string, cryptoType string, isEncrypt bool) (security.Cryptor, error) {
	switch cryptoType {
	case security.CryptoTypeAES:
		aesKey, err := NewSecretKeyLogic(l.Context(), l.svc).GetAESKeyByAppID(appID)
		if err != nil {
			return nil, errors.Tag(err)
		}
		return security.NewAESCipher(aesKey.Key, aesKey.IV)
	case security.CryptoTypeRSA:
		keyType := RSAServerPrivateKey
		if isEncrypt {
			keyType = RSAUserPublicKey
		}
		pemText, err := NewSecretKeyLogic(l.Context(), l.svc).GetRSAKeyByAppID(appID, keyType)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if isEncrypt {
			return security.NewRSACipher("", pemText)
		}
		return security.NewRSACipher(pemText, "")
	default:
		return nil, errors.Errorf("加密方式不合法: %s", cryptoType)
	}
}

// parseJSONObjectText 把 JSON 对象文本解析成 map，并返回格式化后的 JSON 文本，方便前端观察。
func parseJSONObjectText(text string) (map[string]any, string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, "", errors.Errorf("JSON对象不能为空")
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, "", errors.Wrap(err, "JSON对象格式不合法")
	}
	formatted, err := marshalJSONText(payload)
	if err != nil {
		return nil, "", errors.Tag(err)
	}
	return payload, formatted, nil
}

// marshalJSONText 把对象编码为格式化 JSON 文本，便于在调试台直接观察。
func marshalJSONText(value any) (string, error) {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", errors.Tag(err)
	}
	return string(body), nil
}

// encryptFields 按字段列表加密首层字段，行为对齐真实中间件字段模式。
func encryptFields(payload map[string]any, fields []string, cryptor security.Cryptor) (map[string]any, string, error) {
	result := cloneMap(payload)
	for _, field := range fields {
		isJSON := strings.HasPrefix(field, security.CipherJSONPrefix)
		name := strings.TrimSpace(strings.TrimPrefix(field, security.CipherJSONPrefix))
		value, ok := result[name]
		if !ok || security.SignValueString(value) == "" {
			continue
		}
		plainText := security.SignValueString(value)
		if isJSON {
			body, err := json.Marshal(value)
			if err != nil {
				return nil, "", errors.Wrapf(err, "字段[%s] JSON编码失败", name)
			}
			plainText = string(body)
		}
		ciphertext, err := cryptor.Encrypt(plainText)
		if err != nil {
			return nil, "", errors.Wrapf(err, "字段[%s]加密失败", name)
		}
		result[name] = ciphertext
	}
	formatted, err := marshalJSONText(result)
	if err != nil {
		return nil, "", errors.Tag(err)
	}
	return result, formatted, nil
}

// decryptFields 按字段列表解密首层字段，行为对齐真实中间件字段模式。
func decryptFields(payload map[string]any, fields []string, cryptor security.Cryptor) (map[string]any, string, error) {
	result := cloneMap(payload)
	for _, field := range fields {
		isJSON := strings.HasPrefix(field, security.CipherJSONPrefix)
		name := strings.TrimSpace(strings.TrimPrefix(field, security.CipherJSONPrefix))
		value, ok := result[name]
		if !ok || security.SignValueString(value) == "" {
			continue
		}
		plainText, err := cryptor.Decrypt(security.SignValueString(value))
		if err != nil {
			return nil, "", errors.Wrapf(err, "字段[%s]解密失败", name)
		}
		if isJSON {
			var decoded any
			decoder := json.NewDecoder(bytes.NewReader([]byte(plainText)))
			decoder.UseNumber()
			if err := decoder.Decode(&decoded); err != nil {
				return nil, "", errors.Wrapf(err, "字段[%s] JSON解码失败", name)
			}
			result[name] = decoded
			continue
		}
		result[name] = plainText
	}
	formatted, err := marshalJSONText(result)
	if err != nil {
		return nil, "", errors.Tag(err)
	}
	return result, formatted, nil
}

// cloneMap 克隆首层 map，避免直接修改原始输入对象。
func cloneMap(src map[string]any) map[string]any {
	result := make(map[string]any, len(src))
	for key, value := range src {
		result[key] = value
	}
	return result
}

// normalizedDebugTraceID 生成调试用 trace_id，保持和真实签名链路一样稳定。
func normalizedDebugTraceID(traceID string) string {
	traceID = strings.TrimSpace(traceID)
	if traceID != "" {
		return traceID
	}
	return fmt.Sprintf("security-debug-%d", time.Now().UnixNano())
}

// normalizeSecurityFields 对签名/加密字段列表做去重与空值过滤。
func normalizeSecurityFields(fields []string) []string {
	result := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		text := strings.TrimSpace(field)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		result = append(result, text)
	}
	return result
}
