package adminlog

import (
	"context"
	"testing"

	"admin/internal/types"
)

// TestBuildOrderClauseAllowsOnlySafeIdentifiers 验证动态排序只能使用合法标识符和明确方向。
func TestBuildOrderClauseAllowsOnlySafeIdentifiers(t *testing.T) {
	tests := []struct {
		name    string // name 表示测试场景。
		orderBy string // orderBy 表示请求排序字段。
		order   string // order 表示请求排序方向。
		want    string // want 表示期望排序表达式。
		wantErr bool   // wantErr 表示是否期望拒绝请求。
	}{
		{name: "default", want: "created_at DESC, id DESC"},
		{name: "ascending", orderBy: "user_id", order: "ASC", want: "`user_id` asc"},
		{name: "default direction", orderBy: "created_at", want: "`created_at` desc"},
		{name: "sql expression", orderBy: "created_at desc, id", order: "asc", wantErr: true},
		{name: "invalid direction", orderBy: "id", order: "sideways", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildOrderClause(tt.orderBy, tt.order)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("buildOrderClause(%q, %q) should fail", tt.orderBy, tt.order)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildOrderClause(%q, %q) error = %v", tt.orderBy, tt.order, err)
			}
			if got != tt.want {
				t.Fatalf("buildOrderClause(%q, %q) = %q, want %q", tt.orderBy, tt.order, got, tt.want)
			}
		})
	}
}

// TestQueryDirectValidatesRequiredInputs 验证管理员日志查询不会在参数或数据库缺失时继续执行。
func TestQueryDirectValidatesRequiredInputs(t *testing.T) {
	if _, _, _, err := QueryDirect(context.Background(), nil, nil, nil, nil, false); err == nil {
		t.Fatal("nil request should be rejected")
	}
	if _, _, _, err := QueryDirect(context.Background(), nil, &types.AdminLogQueryReq{}, nil, nil, false); err == nil {
		t.Fatal("nil database should be rejected")
	}
}
