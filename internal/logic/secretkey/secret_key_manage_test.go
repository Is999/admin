package secretkey

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"admin/internal/model"
	"admin/internal/types"
)

// TestMaskSecretKeyValue 验证秘钥列表脱敏规则，避免敏感字段明文暴露。
func TestMaskSecretKeyValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "短文本保留前缀",
			input: "12345678",
			want:  "12****",
		},
		{
			name:  "文件路径仅展示文件名摘要",
			input: "/etc/admin/keys/app/server_private.pem",
			want:  "serv****.pem",
		},
		{
			name:  "短文件名保留前缀",
			input: "/tmp/aes_iv",
			want:  "ae****",
		},
		{
			name:  "空值保持为空",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maskSecretKeyValue(tt.input); got != tt.want {
				t.Fatalf("maskSecretKeyValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSecretKeyModelToItem 验证列表与详情场景的秘钥返回语义不同。
func TestSecretKeyModelToItem(t *testing.T) {
	row := model.SecretKey{
		ID:            1,
		UUID:          "app.demo",
		Title:         "测试秘钥",
		StableVersion: "v1",
		GrayVersion:   "v2",
		GrayPercent:   30,
		Status:        1,
		Remark:        "remark",
		CreatedAt:     time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local),
		UpdatedAt:     time.Date(2026, 1, 2, 3, 4, 6, 0, time.Local),
	}
	selected := model.SecretKeyVersion{
		ID:                     11,
		KeyVersion:             "v1",
		AESKeyRef:              "/etc/admin/keys/app.demo/aes_key",
		AESIVRef:               "/etc/admin/keys/app.demo/aes_iv",
		RSAPublicKeyUserRef:    "/etc/admin/keys/app.demo/user_public.pem",
		RSAPublicKeyServerRef:  "/etc/admin/keys/app.demo/server_public.pem",
		RSAPrivateKeyServerRef: "/etc/admin/keys/app.demo/server_private.pem",
		Status:                 1,
		Remark:                 "stable",
		CreatedAt:              time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local),
		UpdatedAt:              time.Date(2026, 1, 2, 3, 4, 6, 0, time.Local),
	}
	grayVersion := model.SecretKeyVersion{
		ID:                     12,
		KeyVersion:             "v2",
		AESKeyRef:              "/etc/admin/keys/app.demo/aes_key_v2",
		AESIVRef:               "/etc/admin/keys/app.demo/aes_iv_v2",
		RSAPublicKeyUserRef:    "/etc/admin/keys/app.demo/user_public_v2.pem",
		RSAPublicKeyServerRef:  "/etc/admin/keys/app.demo/server_public_v2.pem",
		RSAPrivateKeyServerRef: "/etc/admin/keys/app.demo/server_private_v2.pem",
		Status:                 1,
		Remark:                 "gray",
		CreatedAt:              time.Date(2026, 1, 2, 3, 4, 7, 0, time.Local),
		UpdatedAt:              time.Date(2026, 1, 2, 3, 4, 8, 0, time.Local),
	}
	versions := []model.SecretKeyVersion{selected, grayVersion}

	listItem := secretKeyModelToItem(row, versions, &selected, true)
	if !listItem.SecretMasked {
		t.Fatalf("list item should be marked masked")
	}
	if listItem.AESKeyRef != "ae****" {
		t.Fatalf("masked AESKeyRef mismatch: %s", listItem.AESKeyRef)
	}
	if listItem.RSAPublicKeyUserRef != "user****.pem" {
		t.Fatalf("masked RSAPublicKeyUserRef mismatch: %s", listItem.RSAPublicKeyUserRef)
	}
	if listItem.RSAPrivateKeyServerRef != "serv****.pem" {
		t.Fatalf("masked private key ref mismatch: %q", listItem.RSAPrivateKeyServerRef)
	}
	if listItem.VersionCount != 2 {
		t.Fatalf("list version count mismatch: %d", listItem.VersionCount)
	}

	detailItem := secretKeyModelToItem(row, versions, &selected, false)
	if detailItem.SecretMasked {
		t.Fatalf("detail item should not be marked masked")
	}
	if detailItem.AESKeyRef != selected.AESKeyRef {
		t.Fatalf("detail AESKeyRef mismatch: %s", detailItem.AESKeyRef)
	}
	if detailItem.RSAPrivateKeyServerRef != selected.RSAPrivateKeyServerRef {
		t.Fatalf("detail private key ref mismatch: %s", detailItem.RSAPrivateKeyServerRef)
	}
	if len(detailItem.VersionList) != 2 {
		t.Fatalf("detail version list mismatch: %d", len(detailItem.VersionList))
	}
	if !detailItem.VersionList[0].IsStable {
		t.Fatalf("stable version flag mismatch")
	}
	if !detailItem.VersionList[1].IsGray {
		t.Fatalf("gray version flag mismatch")
	}
}

// TestResolvePEMTextFromFile 验证 RSA PEM 会从绝对路径文件中读取。
func TestResolvePEMTextFromFile(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "server_private.pem")
	want := "-----BEGIN RSA PRIVATE KEY-----\nline1\nline2\n-----END RSA PRIVATE KEY-----"
	if err := os.WriteFile(filePath, []byte(want), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", filePath, err)
	}
	got, err := resolvePEMText(filePath)
	if err != nil {
		t.Fatalf("resolvePEMText(%s) failed: %v", filePath, err)
	}
	if got != want {
		t.Fatalf("resolvePEMText(%s) = %q, want %q", filePath, got, want)
	}
}

// TestResolvePEMTextRejectInlinePEM 验证当前项目不再允许直接录入 PEM 文本。
func TestResolvePEMTextRejectInlinePEM(t *testing.T) {
	if _, err := resolvePEMText("-----BEGIN RSA PRIVATE KEY-----\nabc"); err == nil {
		t.Fatal("resolvePEMText should reject inline pem text")
	}
}

// TestRunSecretKeyRSASignVerifySelfCheckUsesServerKeyPair 验证运行态验签自检使用的是服务端公私钥，而不是误用用户公钥。
func TestRunSecretKeyRSASignVerifySelfCheckUsesServerKeyPair(t *testing.T) {
	serverPrivatePEM, serverPublicPEM := generateTestRSAPEMPair(t)
	userPrivatePEM, userPublicPEM := generateTestRSAPEMPair(t)
	if serverPrivatePEM == userPrivatePEM || serverPublicPEM == userPublicPEM {
		t.Fatal("test RSA pairs should be different")
	}
	passed, err := runSecretKeyRSASignVerifySelfCheck(serverPrivatePEM, serverPublicPEM)
	if err != nil {
		t.Fatalf("runSecretKeyRSASignVerifySelfCheck() error = %v", err)
	}
	if !passed {
		t.Fatal("runSecretKeyRSASignVerifySelfCheck() should pass with server key pair")
	}
}

// TestRunSecretKeyRSARequestDecryptSelfCheckUsesDerivedServerPublic 验证解密自检使用服务端密钥。
func TestRunSecretKeyRSARequestDecryptSelfCheckUsesDerivedServerPublic(t *testing.T) {
	serverPrivatePEM, _ := generateTestRSAPEMPair(t)
	passed, err := runSecretKeyRSARequestDecryptSelfCheck(serverPrivatePEM)
	if err != nil {
		t.Fatalf("runSecretKeyRSARequestDecryptSelfCheck() error = %v", err)
	}
	if !passed {
		t.Fatal("runSecretKeyRSARequestDecryptSelfCheck() should pass with derived server public key")
	}
}

// TestValidateSecretKeyEnabledValuesAllowsDerivedServerPublic 验证服务端公钥路径可为空并由私钥派生。
func TestValidateSecretKeyEnabledValuesAllowsDerivedServerPublic(t *testing.T) {
	tempDir := t.TempDir()
	serverPrivatePEM, _ := generateTestRSAPEMPair(t)
	_, userPublicPEM := generateTestRSAPEMPair(t)
	serverPrivatePath := filepath.Join(tempDir, "server_private.pem")
	userPublicPath := filepath.Join(tempDir, "user_public.pem")
	if err := os.WriteFile(serverPrivatePath, []byte(serverPrivatePEM), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", serverPrivatePath, err)
	}
	if err := os.WriteFile(userPublicPath, []byte(userPublicPEM), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", userPublicPath, err)
	}

	err := validateSecretKeyEnabledValues(&types.SaveSecretKeyReq{
		SignStatus:             1,
		VersionStatus:          1,
		RSAPublicKeyUserRef:    userPublicPath,
		RSAPrivateKeyServerRef: serverPrivatePath,
	})
	if err != nil {
		t.Fatalf("validateSecretKeyEnabledValues() error = %v", err)
	}
}

// generateTestRSAPEMPair 生成测试用 RSA 公私钥 PEM，避免重复散落构造逻辑。
func generateTestRSAPEMPair(t *testing.T) (string, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})
	publicASN1, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("x509.MarshalPKIXPublicKey() error = %v", err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicASN1,
	})
	return string(privatePEM), string(publicPEM)
}
