package types

import "testing"

// TestDocPermissionListReqValidate 验证文档权限列表筛选和分页参数会被归一化。
func TestDocPermissionListReqValidate(t *testing.T) {
	status := 1
	req := &DocPermissionListReq{
		Site:       " API ",
		Title:      " 认证 ",
		Path:       " 接口文档 ",
		Status:     &status,
		GetPageReq: GetPageReq{Page: 0, PageSize: 1000},
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("DocPermissionListReq.Validate() error = %v", err)
	}
	if req.Site != "api" || req.Title != "认证" || req.Path != "接口文档" {
		t.Fatalf("DocPermissionListReq.Validate() req = %+v", req)
	}
	if req.Page != 1 || req.PageSize != 100 {
		t.Fatalf("DocPermissionListReq.Validate() page = %d, pageSize = %d", req.Page, req.PageSize)
	}
}

// TestDocPermissionRequestsRejectInvalidValues 验证文档权限请求拒绝非法站点、状态和主键。
func TestDocPermissionRequestsRejectInvalidValues(t *testing.T) {
	invalidStatus := 2
	for _, req := range []*DocPermissionListReq{
		{Site: "other"},
		{Status: &invalidStatus},
	} {
		if err := req.Validate(); err == nil {
			t.Fatalf("DocPermissionListReq.Validate() req = %+v, want error", req)
		}
	}
	for _, req := range []*DocPermissionStatusReq{
		{ID: 0, Status: intPointer(1)},
		{ID: 1},
		{ID: 1, Status: intPointer(2)},
	} {
		if err := req.Validate(); err == nil {
			t.Fatalf("DocPermissionStatusReq.Validate() req = %+v, want error", req)
		}
	}
}

// intPointer 返回测试使用的整数指针。
func intPointer(value int) *int {
	return &value
}
