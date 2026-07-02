package register

import "testing"

// TestValidateNamesUniqueRejectsDuplicate 确保真实注册集合出现重复名称时会被启动校验拦截。
func TestValidateNamesUniqueRejectsDuplicate(t *testing.T) {
	if err := ValidateNamesUnique(KindRoute, []string{"task", "task"}); err == nil {
		t.Fatal("期望重复注册名称返回错误，实际为 nil")
	}
}
