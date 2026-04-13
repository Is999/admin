package codes

import "testing"

// TestVerifyFailureHTTPStatus 验证解锁类校验失败不会返回 401，避免前端误判为登录态失效。
func TestVerifyFailureHTTPStatus(t *testing.T) {
	// cases 表示需要按客户端参数错误处理的会话校验失败业务码集合。
	cases := map[int]int{
		InvalidPassword:    BadRequest,
		InvalidMFACode:     BadRequest,
		CheckMFAAgain:      BadRequest,
		CheckMFABind:       OK,
		CheckMFACode:       OK,
		CheckPasswordReset: OK,
	}
	for code, want := range cases {
		if got := HTTPStatus(code); got != want {
			t.Fatalf("HTTPStatus(%d) = %d, want %d", code, got, want)
		}
	}
}

// TestDuplicateBusinessHTTPStatus 验证新增类唯一性冲突不会落成 500。
func TestDuplicateBusinessHTTPStatus(t *testing.T) {
	for _, code := range []int{
		UserAlreadyExists,
		AdminRoleAlreadyExists,
		AdminPermissionAlreadyExists,
	} {
		if got := HTTPStatus(code); got != BadRequest {
			t.Fatalf("HTTPStatus(%d) = %d, want %d", code, got, BadRequest)
		}
	}
}
