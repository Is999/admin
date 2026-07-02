package runtimefile

import "testing"

// TestSectionSpecsValid 确保运行期外部配置段规格完整且唯一。
func TestSectionSpecsValid(t *testing.T) {
	specs := sectionSpecs()
	if len(specs) == 0 {
		t.Fatal("运行期外部配置段规格不能为空")
	}
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if spec.Key == "" {
			t.Fatalf("运行期外部配置段存在空 key: %+v", spec)
		}
		if spec.apply == nil {
			t.Fatalf("运行期外部配置段缺少合并逻辑: %s", spec.Key)
		}
		if _, ok := seen[spec.Key]; ok {
			t.Fatalf("运行期外部配置段重复: %s", spec.Key)
		}
		seen[spec.Key] = struct{}{}
	}
	for _, key := range []string{
		sectionTaskPeriodic,
		sectionArchiveJobs,
		sectionWorkflows,
	} {
		if _, ok := seen[key]; !ok {
			t.Fatalf("运行期外部配置段缺少 key=%s", key)
		}
	}
}
