package types

import "testing"

// TestListCollectorFailuresReqValidateNormalizesFilters 验证 Collector 失败事件列表过滤参数会在入口归一化。
func TestListCollectorFailuresReqValidateNormalizesFilters(t *testing.T) {
	state := 3
	req := &ListCollectorFailuresReq{
		GetPageReq: GetPageReq{Page: 0, PageSize: 1000},
		BizType:    " user_tag ",
		State:      &state,
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if req.BizType != "user_tag" {
		t.Fatalf("过滤参数归一化失败: %+v", req)
	}
	if req.Page != defaultPageNumber || req.PageSize != maxPageSize {
		t.Fatalf("分页归一化失败: page=%d pageSize=%d", req.Page, req.PageSize)
	}
}

// TestListCollectorFailuresReqValidateRejectsInvalidFilter 验证 Collector 失败事件列表只接受明确状态。
func TestListCollectorFailuresReqValidateRejectsInvalidFilter(t *testing.T) {
	state := 9
	req := &ListCollectorFailuresReq{State: &state}
	if err := req.Validate(); err == nil {
		t.Fatalf("Validate() expected invalid state error")
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

// TestRetryCollectorFailuresReqValidateNormalizesIDs 验证人工重试会去重失败事件 ID 并拒绝无效输入。
func TestRetryCollectorFailuresReqValidateNormalizesIDs(t *testing.T) {
	resetAttempt := false
	req := &RetryCollectorFailuresReq{
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

// TestRetryCollectorFailuresReqValidateRejectsUnsafeInput 验证人工重试拒绝空 ID、非法 ID 和负延迟。
func TestRetryCollectorFailuresReqValidateRejectsUnsafeInput(t *testing.T) {
	cases := []*RetryCollectorFailuresReq{
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
