package sharding

import "testing"

// TestPlanTableForBucket 验证不同物理分片数使用稳定桶起点表名。
func TestPlanTableForBucket(t *testing.T) {
	tests := []struct {
		name       string // 用例名称
		firstTable string // 起始物理表
		count      int    // 物理分片数
		bucket     int    // 固定逻辑桶
		want       Table  // 期望物理表
	}{
		{name: "user first", firstTable: "user", count: 2, bucket: 511, want: Table{Name: "user", Index: 0, BucketStart: 0, BucketEnd: 511}},
		{name: "user second", firstTable: "user", count: 2, bucket: 512, want: Table{Name: "user_b0512", Index: 1, BucketStart: 512, BucketEnd: 1023}},
		{name: "tag second", firstTable: "user_tag_0", count: 4, bucket: 256, want: Table{Name: "user_tag_b0256", Index: 1, BucketStart: 256, BucketEnd: 511}},
		{name: "tag last", firstTable: "user_tag_0", count: 4, bucket: 1023, want: Table{Name: "user_tag_b0768", Index: 3, BucketStart: 768, BucketEnd: 1023}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := NewPlan(tt.firstTable, tt.count)
			if err != nil {
				t.Fatalf("NewPlan() error = %v", err)
			}
			got, err := plan.TableForBucket(tt.bucket)
			if err != nil {
				t.Fatalf("TableForBucket() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("TableForBucket() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestPlanKeepsExistingStarts 验证物理分片翻倍时已有桶起点表名保持不变。
func TestPlanKeepsExistingStarts(t *testing.T) {
	two, err := NewPlan("user", 2)
	if err != nil {
		t.Fatalf("NewPlan(2) error = %v", err)
	}
	four, err := NewPlan("user", 4)
	if err != nil {
		t.Fatalf("NewPlan(4) error = %v", err)
	}
	oldTable, _ := two.TableForBucket(512)
	nextTable, _ := four.TableForBucket(512)
	if oldTable.Name != nextTable.Name {
		t.Fatalf("扩容后表名漂移 old=%s next=%s", oldTable.Name, nextTable.Name)
	}
}

// TestNewPlanRejectsInvalidInput 验证非法分片数和表名直接失败。
func TestNewPlanRejectsInvalidInput(t *testing.T) {
	for _, test := range []struct {
		table string // 起始物理表
		count int    // 物理分片数
	}{
		{table: "user", count: 3},
		{table: "user", count: 2048},
		{table: "user-tag", count: 2},
		{table: "0_user", count: 2},
	} {
		if _, err := NewPlan(test.table, test.count); err == nil {
			t.Fatalf("NewPlan(%q, %d) 应失败", test.table, test.count)
		}
	}
}
