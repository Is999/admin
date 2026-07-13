package model

import (
	"testing"

	"admin/common/idgen"
)

// TestUserTagPhysicalTableName 验证用户标签独立按固定桶路由物理表。
func TestUserTagPhysicalTableName(t *testing.T) {
	table, err := UserTagPhysicalTableName(700, 4)
	if err != nil {
		t.Fatalf("UserTagPhysicalTableName() error = %v", err)
	}
	if table != "user_tag_b0512" {
		t.Fatalf("UserTagPhysicalTableName() = %q, want user_tag_b0512", table)
	}
}

// TestUserTagModelShardGuards 验证所有 UID 用户标签表只接受统一固定逻辑桶。
func TestUserTagModelShardGuards(t *testing.T) {
	uid := int64(1024)
	fixedBucket := idgen.ShardNo(uid)
	rawModulo := int(uid % idgen.ShardMod)
	if rawModulo == fixedBucket {
		t.Fatal("测试 UID 未产生裸取模与固定桶差异")
	}
	cases := []struct {
		name string          // 模型名称
		run  func(int) error // 执行对应模型的创建校验
	}{
		{name: "result", run: func(shardNo int) error {
			return (&UserTagRecord{UID: uint64(uid), ShardNo: shardNo}).BeforeCreate(nil)
		}},
		{name: "runtime_uid", run: func(shardNo int) error {
			return (&UserTagRuntimeUID{UID: uid, ShardNo: shardNo}).BeforeCreate(nil)
		}},
		{name: "event_outbox", run: func(shardNo int) error {
			return (&UserTagEventOutbox{UID: uid, ShardNo: shardNo}).BeforeCreate(nil)
		}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if err := item.run(fixedBucket); err != nil {
				t.Fatalf("BeforeCreate() error = %v", err)
			}
			if err := item.run(rawModulo); err == nil {
				t.Fatal("BeforeCreate() 应拒绝裸 UID 取模")
			}
		})
	}
}

// TestUserTagRuntimeUIDBatchCreateRunsShardGuard 验证批量写入口不会绕过固定桶校验。
func TestUserTagRuntimeUIDBatchCreateRunsShardGuard(t *testing.T) {
	uid := int64(1024)
	rows := []UserTagRuntimeUID{{
		WorkflowID: "workflow",
		UID:        uid,
		ShardNo:    int(uid % idgen.ShardMod),
	}}
	if err := newUserDryRunDB(t).Table(TableNameUserTagRuntimeUID).CreateInBatches(rows, 1000).Error; err == nil {
		t.Fatal("CreateInBatches() 应拒绝裸 UID 取模")
	}
}
