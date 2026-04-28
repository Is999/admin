package types

import (
	"strings"

	"admin/internal/security"

	"github.com/Is999/go-utils/errors"
)

// SecurityDebugSignReq 表示安全调试台“签名”请求。
type SecurityDebugSignReq struct {
	AppID         string   `json:"appId"`                  // AppID 对应 secret_key.uuid
	RequestID     string   `json:"requestId,optional"`     // 请求唯一标识，为空时后端自动生成
	Timestamp     string   `json:"timestamp,optional"`     // 秒级请求时间戳，为空时后端自动生成
	SignatureType string   `json:"signatureType,optional"` // 签名方式：M/A/R
	SignFields    []string `json:"signFields,optional"`    // 参与签名字段，空时默认全字段
	PayloadText   string   `json:"payloadText"`            // 待签名 JSON 对象文本
}

// Validate 校验签名调试请求。
func (r *SecurityDebugSignReq) Validate() error {
	r.AppID = strings.TrimSpace(r.AppID)
	r.RequestID = strings.TrimSpace(r.RequestID)
	r.Timestamp = strings.TrimSpace(r.Timestamp)
	r.SignatureType = strings.ToUpper(strings.TrimSpace(r.SignatureType))
	r.PayloadText = strings.TrimSpace(r.PayloadText)
	if r.AppID == "" {
		return errors.Errorf("AppID不能为空")
	}
	if r.PayloadText == "" {
		return errors.Errorf("待签名内容不能为空")
	}
	return nil
}

// NormalizedSignatureType 返回归一化后的签名方式。
func (r *SecurityDebugSignReq) NormalizedSignatureType() string {
	if r.SignatureType == "" {
		return security.SignatureTypeRSA
	}
	return r.SignatureType
}

// NormalizedSignFields 返回归一化后的签名字段。
func (r *SecurityDebugSignReq) NormalizedSignFields() []string {
	if len(r.SignFields) == 0 {
		return []string{security.SignFieldAll}
	}
	return r.SignFields
}

// SecurityDebugVerifyReq 表示安全调试台“验签”请求。
type SecurityDebugVerifyReq struct {
	SecurityDebugSignReq
	Sign string `json:"sign"` // 待校验签名值
}

// Validate 校验验签调试请求。
func (r *SecurityDebugVerifyReq) Validate() error {
	if err := r.SecurityDebugSignReq.Validate(); err != nil {
		return errors.Tag(err)
	}
	r.Sign = strings.TrimSpace(r.Sign)
	if r.Sign == "" {
		return errors.Errorf("签名值不能为空")
	}
	return nil
}

// SecurityDebugCipherReq 表示安全调试台“加密/解密”请求。
type SecurityDebugCipherReq struct {
	AppID        string   `json:"appId"`                 // AppID 对应 secret_key.uuid
	CryptoType   string   `json:"cryptoType,optional"`   // 加密方式：A/R
	CipherFields []string `json:"cipherFields,optional"` // 需要加密或解密的字段路径
	PayloadText  string   `json:"payloadText,optional"`  // JSON 对象文本
}

// ValidateEncrypt 校验加密调试请求。
func (r *SecurityDebugCipherReq) ValidateEncrypt() error {
	r.AppID = strings.TrimSpace(r.AppID)
	r.CryptoType = strings.ToUpper(strings.TrimSpace(r.CryptoType))
	r.PayloadText = strings.TrimSpace(r.PayloadText)
	if r.AppID == "" {
		return errors.Errorf("AppID不能为空")
	}
	if r.PayloadText == "" {
		return errors.Errorf("待加密内容不能为空")
	}
	return r.validateCipherFields()
}

// ValidateDecrypt 校验解密调试请求。
func (r *SecurityDebugCipherReq) ValidateDecrypt() error {
	r.AppID = strings.TrimSpace(r.AppID)
	r.CryptoType = strings.ToUpper(strings.TrimSpace(r.CryptoType))
	r.PayloadText = strings.TrimSpace(r.PayloadText)
	if r.AppID == "" {
		return errors.Errorf("AppID不能为空")
	}
	if r.PayloadText == "" {
		return errors.Errorf("待解密内容不能为空")
	}
	return r.validateCipherFields()
}

// NormalizedCryptoType 返回归一化后的加密方式。
func (r *SecurityDebugCipherReq) NormalizedCryptoType() string {
	if r.CryptoType == "" {
		return security.CryptoTypeAES
	}
	return r.CryptoType
}

// NormalizedCipherFields 返回归一化后的字段加密配置。
func (r *SecurityDebugCipherReq) NormalizedCipherFields() []string {
	return r.CipherFields
}

// validateCipherFields 校验安全调试台只能使用字段级加解密。
func (r *SecurityDebugCipherReq) validateCipherFields() error {
	fields := r.NormalizedCipherFields()
	if len(fields) == 0 {
		return errors.Errorf("加密字段不能为空")
	}
	for _, field := range fields {
		if strings.EqualFold(strings.TrimSpace(field), security.CipherWholeBody) {
			return errors.Errorf("不允许整包加密")
		}
	}
	return nil
}

// SecurityDebugSignResp 表示签名调试结果。
type SecurityDebugSignResp struct {
	AppID         string         `json:"appId"`         // 实际参与签名的 AppID
	RequestID     string         `json:"requestId"`     // 实际参与签名的请求标识
	TraceID       string         `json:"traceId"`       // 与 requestId 等值，便于前端按 X-Trace-Id 直接回填
	Timestamp     string         `json:"timestamp"`     // 实际参与签名的秒级请求时间戳
	SignatureType string         `json:"signatureType"` // 实际使用的签名方式
	SignFields    []string       `json:"signFields"`    // 实际参与签名的字段列表
	Payload       map[string]any `json:"payload"`       // 归一化后的待签名对象
	PayloadText   string         `json:"payloadText"`   // 归一化后的待签名 JSON 文本
	SignText      string         `json:"signText"`      // 最终参与签名的字符串
	Sign          string         `json:"sign"`          // 生成的签名值
}

// SecurityDebugVerifyResp 表示验签调试结果。
type SecurityDebugVerifyResp struct {
	SecurityDebugSignResp
	Verified bool `json:"verified"` // 是否验签成功
}

// SecurityDebugCipherResp 表示加密或解密调试结果。
type SecurityDebugCipherResp struct {
	AppID             string         `json:"appId"`                       // 实际使用的 AppID
	CryptoType        string         `json:"cryptoType"`                  // 实际使用的加密方式
	CipherFields      []string       `json:"cipherFields"`                // 实际使用的字段配置
	CipherHeader      string         `json:"cipherHeader"`                // 可直接用于 X-Cipher 的头值
	Payload           map[string]any `json:"payload,omitempty"`           // 输入对象
	PayloadText       string         `json:"payloadText"`                 // 输入文本
	ResultPayload     map[string]any `json:"resultPayload,omitempty"`     // 输出对象
	ResultPayloadText string         `json:"resultPayloadText,omitempty"` // 输出对象 JSON 文本
}
