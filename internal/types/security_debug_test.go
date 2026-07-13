package types

import (
	"testing"

	"admin/internal/security"
)

// TestSecurityDebugSignReqValidateTrimsSignMeta 验证安全调试台只使用标准签名元数据。
func TestSecurityDebugSignReqValidateTrimsSignMeta(t *testing.T) {
	req := &SecurityDebugSignReq{
		AppID:       "demo-app",
		RequestID:   " request-001 ",
		Timestamp:   " 1700000000 ",
		PayloadText: `{"username":"admin"}`,
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if req.RequestID != "request-001" {
		t.Fatalf("RequestID = %q, want request-001", req.RequestID)
	}
	if req.Timestamp != "1700000000" {
		t.Fatalf("Timestamp = %q, want 1700000000", req.Timestamp)
	}
}

// TestSecurityDebugSignReqValidateSignatureTypes 验证安全调试只接受 AES 和 RSA 签名方式。
func TestSecurityDebugSignReqValidateSignatureTypes(t *testing.T) {
	for _, signatureType := range []string{"", " a ", "r"} {
		req := &SecurityDebugSignReq{
			AppID:         "demo-app",
			SignatureType: signatureType,
			PayloadText:   `{}`,
		}
		if err := req.Validate(); err != nil {
			t.Fatalf("Validate() signatureType=%q error = %v", signatureType, err)
		}
	}

	for _, signatureType := range []string{"M", "MD5", "unknown"} {
		req := &SecurityDebugSignReq{
			AppID:         "demo-app",
			SignatureType: signatureType,
			PayloadText:   `{}`,
		}
		if err := req.Validate(); err == nil {
			t.Fatalf("Validate() signatureType=%q expected rejection", signatureType)
		}
	}
}

// TestSecurityDebugCipherReqValidateNormalizesFields 验证加解密字段会去空、去重并归一化。
func TestSecurityDebugCipherReqValidateNormalizesFields(t *testing.T) {
	req := &SecurityDebugCipherReq{
		AppID:        " demo-app ",
		CryptoType:   " a ",
		CipherFields: []string{" username ", "", "username", "phone"},
		PayloadText:  ` {"username":"admin"} `,
	}

	if err := req.ValidateEncrypt(); err != nil {
		t.Fatalf("ValidateEncrypt() error = %v", err)
	}
	if req.AppID != "demo-app" || req.CryptoType != "A" {
		t.Fatalf("基础字段归一化失败: %+v", req)
	}
	if len(req.CipherFields) != 2 || req.CipherFields[0] != "username" || req.CipherFields[1] != "phone" {
		t.Fatalf("CipherFields 归一化失败: %+v", req.CipherFields)
	}
}

// TestSecurityDebugCipherReqValidateRejectsUnsafeFields 验证加解密调试只允许字段级配置。
func TestSecurityDebugCipherReqValidateRejectsUnsafeFields(t *testing.T) {
	cases := []*SecurityDebugCipherReq{
		{AppID: "demo-app", CipherFields: []string{" "}, PayloadText: "{}"},
		{AppID: "demo-app", CipherFields: []string{security.CipherWholeBody}, PayloadText: "{}"},
	}
	for _, req := range cases {
		if err := req.ValidateDecrypt(); err == nil {
			t.Fatalf("ValidateDecrypt() expected error for %+v", req)
		}
	}
}
