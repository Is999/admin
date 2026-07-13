package builtin

import (
	"crypto/tls"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestInternalServerTLSConfigRequiresClientCertificate 验证内网 TLS 强制校验客户端证书。
func TestInternalServerTLSConfigRequiresClientCertificate(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	t.Cleanup(server.Close)
	caFile := filepath.Join(t.TempDir(), "client-ca.crt")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	tlsConfig, err := internalServerTLSConfig(caFile)
	if err != nil {
		t.Fatalf("internalServerTLSConfig() error = %v", err)
	}
	if tlsConfig.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("ClientAuth=%v，期望=%v", tlsConfig.ClientAuth, tls.RequireAndVerifyClientCert)
	}
	if tlsConfig.MinVersion < tls.VersionTLS12 {
		t.Fatalf("MinVersion=%d，不能低于 TLS 1.2", tlsConfig.MinVersion)
	}
}
