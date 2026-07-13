package migration

import (
	"testing"
)

// TestGhostferryConfigFromDSNPreservesConnectionIdentity 验证应用 DSN 能安全转换为内嵌复制连接。
func TestGhostferryConfigFromDSNPreservesConnectionIdentity(t *testing.T) {
	databaseName, databaseConfig, err := ghostferryConfigFromDSN(
		"migration_user:secret@tcp([::1]:3307)/business?collation=utf8mb4_bin&charset=utf8mb4&readTimeout=1500ms&writeTimeout=45s",
		TLSOptions{},
	)
	if err != nil {
		t.Fatalf("ghostferryConfigFromDSN() error = %v", err)
	}
	if databaseName != "business" || databaseConfig.Host != "::1" || databaseConfig.Port != 3307 {
		t.Fatalf("unexpected database config: database=%s config=%+v", databaseName, databaseConfig)
	}
	if databaseConfig.User != "migration_user" || databaseConfig.Pass != "secret" || databaseConfig.Collation != "utf8mb4_bin" {
		t.Fatalf("connection identity drifted: %+v", databaseConfig)
	}
	if databaseConfig.Params["charset"] != "utf8mb4" || databaseConfig.ReadTimeout != 2 || databaseConfig.WriteTimeout != 45 {
		t.Fatalf("connection options drifted: %+v", databaseConfig)
	}
}

// TestGhostferryConfigFromDSNRejectsUnsupportedConnections 验证缺库名、Unix socket、无超时和不完整 TLS 参数直接失败。
func TestGhostferryConfigFromDSNRejectsUnsupportedConnections(t *testing.T) {
	tests := []struct {
		name string     // 用例名称
		dsn  string     // 应用写库 DSN
		tls  TLSOptions // TLS 证书参数
	}{
		{name: "missing database", dsn: "user:secret@tcp(127.0.0.1:3306)/?readTimeout=5s&writeTimeout=5s"},
		{name: "unix socket", dsn: "user:secret@unix(/tmp/mysql.sock)/business?readTimeout=5s&writeTimeout=5s"},
		{name: "missing read timeout", dsn: "user:secret@tcp(127.0.0.1:3306)/business?writeTimeout=5s"},
		{name: "missing write timeout", dsn: "user:secret@tcp(127.0.0.1:3306)/business?readTimeout=5s"},
		{name: "missing tls files", dsn: "user:secret@tcp(127.0.0.1:3306)/business?readTimeout=5s&writeTimeout=5s&tls=true"},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			if _, _, err := ghostferryConfigFromDSN(item.dsn, item.tls); err == nil {
				t.Fatal("期望不安全或不支持的连接参数被拒绝")
			}
		})
	}
}

// TestValidateSameDatabaseAllowsDedicatedCredentials 验证迁移账号可与应用账号不同但数据库目标必须相同。
func TestValidateSameDatabaseAllowsDedicatedCredentials(t *testing.T) {
	applicationDSN := "app_user:app_secret@tcp(127.0.0.1:3306)/business"
	migrationDSN := "migration_user:migration_secret@tcp(127.0.0.1:3306)/business"
	if err := ValidateSameDatabase(applicationDSN, migrationDSN); err != nil {
		t.Fatalf("ValidateSameDatabase() error = %v", err)
	}
}

// TestValidateSameDatabaseRejectsDifferentTarget 验证专用迁移账号不能误连其他库。
func TestValidateSameDatabaseRejectsDifferentTarget(t *testing.T) {
	applicationDSN := "app_user:app_secret@tcp(127.0.0.1:3306)/business"
	migrationDSN := "migration_user:migration_secret@tcp(127.0.0.1:3306)/other"
	if err := ValidateSameDatabase(applicationDSN, migrationDSN); err == nil {
		t.Fatal("期望不同数据库目标被拒绝")
	}
}

// TestDatabaseFingerprintIgnoresCredentialsAndBindsTarget 验证迁移凭证只绑定数据库目标，不泄露或绑定短期账号。
func TestDatabaseFingerprintIgnoresCredentialsAndBindsTarget(t *testing.T) {
	first, err := DatabaseFingerprint("app:secret@tcp(127.0.0.1:3306)/business")
	if err != nil {
		t.Fatalf("DatabaseFingerprint(first) error = %v", err)
	}
	second, err := DatabaseFingerprint("migration:other@tcp(127.0.0.1:3306)/business")
	if err != nil {
		t.Fatalf("DatabaseFingerprint(second) error = %v", err)
	}
	other, err := DatabaseFingerprint("migration:other@tcp(127.0.0.1:3306)/other")
	if err != nil {
		t.Fatalf("DatabaseFingerprint(other) error = %v", err)
	}
	if first == "" || first != second || first == other {
		t.Fatalf("unexpected fingerprints first=%q second=%q other=%q", first, second, other)
	}
}
