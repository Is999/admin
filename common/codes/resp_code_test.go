package codes

import "testing"

// TestDefaultCodeContracts 验证业务码契约唯一且可派生运行时映射。
func TestDefaultCodeContracts(t *testing.T) {
	seen := make(map[int]struct{})
	for _, contract := range DefaultCodeContracts() {
		if _, ok := seen[contract.Code]; ok {
			t.Fatalf("duplicate code contract code=%d", contract.Code)
		}
		seen[contract.Code] = struct{}{}

		if contract.HTTPStatus == 0 {
			t.Fatalf("code=%d missing HTTP status", contract.Code)
		}
		if contract.MessageKey == "" {
			t.Fatalf("code=%d missing message key", contract.Code)
		}
		if got := HTTPStatus(contract.Code); got != contract.HTTPStatus {
			t.Fatalf("HTTPStatus(%d) = %d, want %d", contract.Code, got, contract.HTTPStatus)
		}
		if got := IsSuccess(contract.Code); got != contract.Success {
			t.Fatalf("IsSuccess(%d) = %v, want %v", contract.Code, got, contract.Success)
		}
		if got, ok := MessageKey(contract.Code); !ok || got != contract.MessageKey {
			t.Fatalf("MessageKey(%d) = (%q,%v), want (%q,true)", contract.Code, got, ok, contract.MessageKey)
		}
	}
}

// TestMessageKeyUnknown 验证未知业务码不暴露默认文案 key。
func TestMessageKeyUnknown(t *testing.T) {
	if key, ok := MessageKey(999999); ok {
		t.Fatalf("MessageKey(unknown) = (%q,true), want false", key)
	}
}

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

// TestBusinessHTTPStatus 验证常用业务码返回明确 HTTP 状态，不依赖旧兜底分支。
func TestBusinessHTTPStatus(t *testing.T) {
	cases := map[int]int{
		CreateFail:     ServerError,
		AddFail:        ServerError,
		UserNotFound:   NotFound,
		UserDisabled:   Unauthorized,
		InvalidCaptcha: BadRequest,
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
