package types

import "testing"

// TestListCollectorTasksReqValidateNormalizesFilters 验证 Collector 列表过滤参数会在入口归一化。
func TestListCollectorTasksReqValidateNormalizesFilters(t *testing.T) {
	state := 3
	req := &ListCollectorTasksReq{
		GetPageReq: GetPageReq{Page: 0, PageSize: 1000},
		BizType:    " user_tag ",
		Transport:  " KAFKA ",
		State:      &state,
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if req.BizType != "user_tag" || req.Transport != "kafka" {
		t.Fatalf("过滤参数归一化失败: %+v", req)
	}
	if req.Page != defaultPageNumber || req.PageSize != maxPageSize {
		t.Fatalf("分页归一化失败: page=%d pageSize=%d", req.Page, req.PageSize)
	}
}

// TestListCollectorTasksReqValidateRejectsInvalidFilter 验证 Collector 列表过滤只接受明确状态和来源。
func TestListCollectorTasksReqValidateRejectsInvalidFilter(t *testing.T) {
	state := 9
	req := &ListCollectorTasksReq{State: &state}
	if err := req.Validate(); err == nil {
		t.Fatalf("Validate() expected invalid state error")
	}

	req = &ListCollectorTasksReq{Transport: "http"}
	if err := req.Validate(); err == nil {
		t.Fatalf("Validate() expected invalid transport error")
	}
}

// TestRunCollectorReqValidateCapsLimit 验证人工执行入口会限制单轮处理规模。
func TestRunCollectorReqValidateCapsLimit(t *testing.T) {
	if err := (&RunCollectorReq{Limit: -1}).Validate(); err == nil {
		t.Fatalf("Validate() expected negative limit error")
	}
	if err := (&RunCollectorReq{Limit: collectorRunMaxLimit + 1}).Validate(); err == nil {
		t.Fatalf("Validate() expected over limit error")
	}
	if err := (&RunCollectorReq{Limit: collectorRunMaxLimit}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

// TestRetryCollectorTasksReqValidateNormalizesIDs 验证人工重试会去重任务 ID 并拒绝无效输入。
func TestRetryCollectorTasksReqValidateNormalizesIDs(t *testing.T) {
	resetAttempt := false
	req := &RetryCollectorTasksReq{
		IDs:          []int64{3, 3, 5},
		DelaySeconds: 60,
		ResetAttempt: &resetAttempt,
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(req.IDs) != 2 || req.IDs[0] != 3 || req.IDs[1] != 5 {
		t.Fatalf("IDs 去重结果不符合预期: %+v", req.IDs)
	}
	if req.ResetAttempt == nil || *req.ResetAttempt {
		t.Fatalf("ResetAttempt 应保留显式 false")
	}
}

// TestRetryCollectorTasksReqValidateRejectsUnsafeInput 验证人工重试拒绝空 ID、非法 ID 和负延迟。
func TestRetryCollectorTasksReqValidateRejectsUnsafeInput(t *testing.T) {
	cases := []*RetryCollectorTasksReq{
		{},
		{IDs: []int64{0}},
		{IDs: []int64{1}, DelaySeconds: -1},
	}
	for _, req := range cases {
		if err := req.Validate(); err == nil {
			t.Fatalf("Validate() expected error for %+v", req)
		}
	}
}
