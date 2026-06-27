package types

import (
	"net/http/httptest"
	"testing"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// TestListTaskItemsOverviewReqValidate_AllowEmptyQueue 确保任务总览查询允许不传队列，由后端走可见队列聚合。
func TestListTaskItemsOverviewReqValidate_AllowEmptyQueue(t *testing.T) {
	req := &ListTaskItemsOverviewReq{
		Page:               1,
		PageSize:           20,
		IncludeAggregating: false,
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("期望空队列总览查询校验通过，实际错误: %v", err)
	}
}

// TestListTaskItemsOverviewReqValidate_AggregatingNeedGroup 确保 aggregating 查询仍然要求提供分组。
func TestListTaskItemsOverviewReqValidate_AggregatingNeedGroup(t *testing.T) {
	req := &ListTaskItemsOverviewReq{
		State: "aggregating",
	}

	if err := req.Validate(); err == nil {
		t.Fatal("期望 aggregating 查询在缺少 group 时返回错误")
	}
}

// TestListTaskItemsOverviewReqParse_AllowEmptyQueue 确保 GET 参数解析阶段允许 queue 缺省。
func TestListTaskItemsOverviewReqParse_AllowEmptyQueue(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/tasks/overview?includeAggregating=false&page=1&pageSize=20", nil)

	var bindReq ListTaskItemsOverviewReq
	if err := httpx.Parse(req, &bindReq); err != nil {
		t.Fatalf("期望 httpx.Parse 允许空 queue，实际错误: %v", err)
	}
	if bindReq.Queue != "" {
		t.Fatalf("期望未传 queue 时保持空字符串，实际为: %q", bindReq.Queue)
	}
}

// TestListTaskItemsOverviewReqParse_TaskID 确保任务 ID 筛选参数可通过 GET 查询串绑定。
func TestListTaskItemsOverviewReqParse_TaskID(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/tasks/overview?taskId=task-fragment&includeAggregating=false&page=1&pageSize=20", nil)

	var bindReq ListTaskItemsOverviewReq
	if err := httpx.Parse(req, &bindReq); err != nil {
		t.Fatalf("期望 httpx.Parse 支持 taskId 筛选，实际错误: %v", err)
	}
	if bindReq.TaskID != "task-fragment" {
		t.Fatalf("期望 taskId 正确绑定，实际为: %q", bindReq.TaskID)
	}
}
