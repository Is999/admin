package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestRunBuildsIndependentShardRules 验证每张 UID 表可使用独立物理分片数。
func TestRunBuildsIndependentShardRules(t *testing.T) {
	var output bytes.Buffer
	err := run(ruleOptions{
		Database:      "app_db",
		StorageUnits:  "ds_0,ds_1",
		UserShards:    2,
		TableShards:   "user_tag=4,user_log=8,balance_change=8",
		KeyStrategies: "user=application,user_tag=proxy:id,user_log=application,balance_change=proxy:log_id",
	}, &output)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	sqlText := output.String()
	for _, want := range []string{
		"USE `app_db`;",
		"SET DEFAULT SINGLE TABLE STORAGE UNIT = `ds_0`;",
		"CREATE SHARDING TABLE RULE user",
		"CREATE SHARDING TABLE RULE user_tag",
		"CREATE SHARDING TABLE RULE user_log",
		"CREATE SHARDING TABLE RULE balance_change",
		"KEY_GENERATE_STRATEGY(COLUMN=`id`,TYPE(NAME=\"SNOWFLAKE\"))",
		"KEY_GENERATE_STRATEGY(COLUMN=`log_id`,TYPE(NAME=\"SNOWFLAKE\"))",
		"SHOW SHARDING KEY GENERATORS;",
		"PROPERTIES(\"sharding-count\"=\"2\")",
		"PROPERTIES(\"sharding-count\"=\"4\")",
		"PROPERTIES(\"sharding-count\"=\"8\")",
		"ALLOW_HINT_DISABLE=false",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("generated DistSQL missing %q:\n%s", want, sqlText)
		}
	}
	if got := strings.Count(sqlText, "PROPERTIES(\"sharding-count\"=\"2\")"); got != 1 {
		t.Fatalf("user shard rule count = %d, want 1\n%s", got, sqlText)
	}
	if strings.Contains(sqlText, "CREATE SHARDING TABLE RULE `") {
		t.Fatalf("ShardingSphere 5.5.3 rejects quoted rule names:\n%s", sqlText)
	}
	if strings.Contains(sqlText, "--") {
		t.Fatalf("ShardingSphere 5.5.3 rejects SQL comments sent with DistSQL:\n%s", sqlText)
	}
	if strings.Contains(sqlText, "CREATE SHARDING TABLE REFERENCE RULE") {
		t.Fatalf("independent shard rules must not create reference rule\n%s", sqlText)
	}
	if got := strings.Count(sqlText, "KEY_GENERATE_STRATEGY"); got != 2 {
		t.Fatalf("generated key strategy count = %d, want 2\n%s", got, sqlText)
	}
}

// TestRunBuildsExplicitReferenceRule 验证同节点布局的表可显式建立 reference 关系。
func TestRunBuildsExplicitReferenceRule(t *testing.T) {
	var output bytes.Buffer
	err := run(ruleOptions{
		Database:        "app_db",
		StorageUnits:    "ds_0,ds_1",
		UserShards:      2,
		TableShards:     "user_tag=2",
		ReferenceTables: "user_tag",
		KeyStrategies:   "user=application,user_tag=proxy:id",
	}, &output)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if want := "CREATE SHARDING TABLE REFERENCE RULE user_reference (`user`,`user_tag`);"; !strings.Contains(output.String(), want) {
		t.Fatalf("generated DistSQL missing %q:\n%s", want, output.String())
	}
}

// TestRunRejectsMismatchedReferenceTable 验证不同物理分片数的表不能建立 reference 关系。
func TestRunRejectsMismatchedReferenceTable(t *testing.T) {
	err := run(ruleOptions{
		Database:        "app_db",
		StorageUnits:    "ds_0,ds_1",
		UserShards:      2,
		TableShards:     "user_tag=4",
		ReferenceTables: "user_tag",
		KeyStrategies:   "user=application,user_tag=proxy:id",
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "user_tag 的分片数 4 与 user 的 2 不一致") {
		t.Fatalf("run() error = %v, want mismatched reference rejection", err)
	}
}

// TestRunRejectsUnknownReferenceTable 验证 reference 表必须先定义物理分片规则。
func TestRunRejectsUnknownReferenceTable(t *testing.T) {
	err := run(ruleOptions{
		Database:        "app_db",
		StorageUnits:    "ds_0",
		UserShards:      1,
		TableShards:     "user_tag=1",
		ReferenceTables: "user_profile",
		KeyStrategies:   "user=application,user_tag=proxy:id",
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "reference 表 user_profile 未定义物理分片数") {
		t.Fatalf("run() error = %v, want unknown reference rejection", err)
	}
}

// TestRunRejectsMissingUserTagRule 验证 user_tag 必须显式声明自己的物理分片数。
func TestRunRejectsMissingUserTagRule(t *testing.T) {
	err := run(ruleOptions{
		Database:      "app_db",
		StorageUnits:  "ds_0",
		UserShards:    1,
		TableShards:   "user_profile=1",
		KeyStrategies: "user=application,user_profile=application",
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "必须包含 user_tag") {
		t.Fatalf("run() error = %v, want missing user_tag rejection", err)
	}
}

// TestRunRejectsUnsafeIdentifier 验证命令不会把未校验标识符写入 DistSQL。
func TestRunRejectsUnsafeIdentifier(t *testing.T) {
	err := run(ruleOptions{
		Database:     "app_db;DROP DATABASE app_db",
		StorageUnits: "ds_0",
		UserShards:   2,
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "非法标识符") {
		t.Fatalf("run() error = %v, want unsafe identifier rejection", err)
	}
}

// TestValidateShardCountRejectsNonPowerOfTwo 验证物理分片数量只能按 2 的幂增长。
func TestValidateShardCountRejectsNonPowerOfTwo(t *testing.T) {
	for _, count := range []int{0, 3, 1025} {
		if err := validateShardCount(count); err == nil {
			t.Fatalf("validateShardCount(%d) should fail", count)
		}
	}
}

// TestRunRejectsUnevenStorageUnits 验证非 2 的幂存储单元不会生成不均衡实际节点。
func TestRunRejectsUnevenStorageUnits(t *testing.T) {
	err := run(ruleOptions{
		Database:      "app_db",
		StorageUnits:  "ds_0,ds_1,ds_2",
		UserShards:    2,
		TableShards:   "user_tag=4,user_log=4",
		KeyStrategies: "user=application,user_tag=proxy:id,user_log=application",
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "存储单元数量错误") {
		t.Fatalf("run() error = %v, want uneven storage rejection", err)
	}
}

// TestRunRejectsUnusedStorageUnits 验证没有任何表覆盖全部存储单元时直接拒绝生成规则。
func TestRunRejectsUnusedStorageUnits(t *testing.T) {
	err := run(ruleOptions{
		Database:      "app_db",
		StorageUnits:  "ds_0,ds_1,ds_2,ds_3",
		UserShards:    2,
		TableShards:   "user_tag=2",
		KeyStrategies: "user=application,user_tag=proxy:id",
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "无法覆盖 4 个存储单元") {
		t.Fatalf("run() error = %v, want unused storage rejection", err)
	}
}

// TestRunAllowsPowerOfTwoStorageLayout 验证不同表可按容量覆盖不同数量的节点。
func TestRunAllowsPowerOfTwoStorageLayout(t *testing.T) {
	var output bytes.Buffer
	err := run(ruleOptions{
		Database:      "app_db",
		StorageUnits:  "ds_0,ds_1,ds_2,ds_3",
		UserShards:    2,
		TableShards:   "user_tag=4,user_log=8",
		KeyStrategies: "user=application,user_tag=proxy:id,user_log=application",
	}, &output)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if got := strings.Count(output.String(), "STORAGE_UNITS(`ds_0`,`ds_1`,`ds_2`,`ds_3`)"); got != 3 {
		t.Fatalf("storage rule count = %d, want 3\n%s", got, output.String())
	}
}

// TestRunRejectsMissingKeyStrategy 验证新增分片表不能隐式依赖物理自增主键。
func TestRunRejectsMissingKeyStrategy(t *testing.T) {
	err := run(ruleOptions{
		Database:      "app_db",
		StorageUnits:  "ds_0",
		UserShards:    2,
		TableShards:   "user_tag=4,user_log=4",
		KeyStrategies: "user=application,user_tag=proxy:id",
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "user_log 缺少全局主键策略") {
		t.Fatalf("run() error = %v, want missing global key strategy rejection", err)
	}
}

// TestRunRejectsUnsafeCoreKeyStrategy 验证 user 和 user_tag 的既有主键契约不能被规则参数改写。
func TestRunRejectsUnsafeCoreKeyStrategy(t *testing.T) {
	for _, strategy := range []string{
		"user=proxy:id,user_tag=proxy:id",
		"user=application,user_tag=application",
	} {
		err := run(ruleOptions{
			Database:      "app_db",
			StorageUnits:  "ds_0",
			UserShards:    2,
			TableShards:   "user_tag=4",
			KeyStrategies: strategy,
		}, &bytes.Buffer{})
		if err == nil {
			t.Fatalf("keyStrategies=%q run() should reject core key strategy drift", strategy)
		}
	}
}

// TestRunRejectsUnknownKeyStrategyTable 验证主键策略中的表名拼写错误不会被静默忽略。
func TestRunRejectsUnknownKeyStrategyTable(t *testing.T) {
	err := run(ruleOptions{
		Database:      "app_db",
		StorageUnits:  "ds_0",
		UserShards:    2,
		TableShards:   "user_tag=4",
		KeyStrategies: "user=application,user_tag=proxy:id,user_tga=proxy:id",
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "未定义分片表 user_tga") {
		t.Fatalf("run() error = %v, want unknown strategy table rejection", err)
	}
}
