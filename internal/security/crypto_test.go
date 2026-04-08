package security

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

// TestAESCipherEncryptDecrypt 校验 AES-CBC 加解密与 AES 签名验签流程。
func TestAESCipherEncryptDecrypt(t *testing.T) {
	cipherObj, err := NewAESCipher("12345678901234567890123456789012", "1234567890123456")
	if err != nil {
		t.Fatalf("new aes cipher failed: %v", err)
	}
	encrypted, err := cipherObj.Encrypt("hello")
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	decrypted, err := cipherObj.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if decrypted != "hello" {
		t.Fatalf("expected hello, got %q", decrypted)
	}
	sign, err := cipherObj.Sign("a=1&key=x")
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	ok, err := cipherObj.Verify("a=1&key=x", sign)
	if err != nil || !ok {
		t.Fatalf("verify failed ok=%v err=%v", ok, err)
	}
}

// TestRSASignerAndCipher 校验 RSA 签名验签与分段加解密流程。
func TestRSASignerAndCipher(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key failed: %v", err)
	}
	publicKey := &privateKey.PublicKey
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	publicPEM, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("marshal rsa public key failed: %v", err)
	}
	publicPEMText := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicPEM})

	signer, err := NewRSASigner(string(privatePEM), string(publicPEMText))
	if err != nil {
		t.Fatalf("new rsa signer failed: %v", err)
	}
	sign, err := signer.Sign("payload")
	if err != nil {
		t.Fatalf("rsa sign failed: %v", err)
	}
	ok, err := signer.Verify("payload", sign)
	if err != nil || !ok {
		t.Fatalf("rsa verify failed ok=%v err=%v", ok, err)
	}

	cipherObj, err := NewRSACipher(string(privatePEM), string(publicPEMText))
	if err != nil {
		t.Fatalf("new rsa cipher failed: %v", err)
	}
	encrypted, err := cipherObj.Encrypt("hello rsa")
	if err != nil {
		t.Fatalf("rsa encrypt failed: %v", err)
	}
	decrypted, err := cipherObj.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("rsa decrypt failed: %v", err)
	}
	if decrypted != "hello rsa" {
		t.Fatalf("expected hello rsa, got %q", decrypted)
	}
}

// TestBuildSignString 校验签名字符串字段排序、空值跳过和 key 拼接规则。
func TestBuildSignString(t *testing.T) {
	got := BuildSignString(map[string]any{
		"b": "2",
		"a": "1",
		"c": "",
	}, []string{"b", "c", "a"}, "req", "app")
	want := "a=1&b=2&key=068250e02e610c9a60a1b9aa5a2ed370"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

// TestBuildSignStringStableObjectOrder 校验对象值内部 key 顺序不会影响最终签名串。
func TestBuildSignStringStableObjectOrder(t *testing.T) {
	left := BuildSignString(map[string]any{
		"payload": map[string]any{
			"z": "last",
			"a": "first",
			"nested": map[string]any{
				"b": 2,
				"a": 1,
			},
		},
	}, []string{"payload"}, "req", "app")
	right := BuildSignString(map[string]any{
		"payload": map[string]any{
			"a": "first",
			"nested": map[string]any{
				"a": 1,
				"b": 2,
			},
			"z": "last",
		},
	}, []string{"payload"}, "req", "app")
	if left != right {
		t.Fatalf("expected stable sign text, left=%q right=%q", left, right)
	}
}
