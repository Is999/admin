package security

import (
	"testing"
)

// TestBuildSignString 校验签名字符串字段排序、空值跳过和 key 拼接规则。
func TestBuildSignString(t *testing.T) {
	got := BuildSignString(map[string]any{
		"b": "2",
		"a": "1",
		"c": "",
	}, []string{"b", "c", "a"}, "req", "1700000000", "app")
	want := "v2|app=3:app|trace=3:req|timestamp=10:1700000000|field=1:a1:1|field=1:b1:2"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

// TestBuildSignStringSeparatesDelimiterValues 验证长度前缀协议不会因字段值包含旧分隔符而碰撞。
func TestBuildSignStringSeparatesDelimiterValues(t *testing.T) {
	left := BuildSignString(map[string]any{"a": "1&b=2", "b": "3"}, []string{"a", "b"}, "trace", "1700000000", "app")
	right := BuildSignString(map[string]any{"a": "1", "b": "2&b=3"}, []string{"a", "b"}, "trace", "1700000000", "app")
	if left == right {
		t.Fatalf("不同字段值生成了相同签名串: %q", left)
	}
}

// TestBuildSignStringStableObjectOrder 校验对象值内部 key 顺序不会影响最终签名串。
func TestBuildSignStringStableObjectOrder(t *testing.T) {
	left := BuildSignString(map[string]any{
		"payload": map[string]any{
			"z": "last",
			"a": "first",
			"nested": map[string]any{
				"b": 2,
				"a": 1,
			},
		},
	}, []string{"payload"}, "req", "1700000000", "app")
	right := BuildSignString(map[string]any{
		"payload": map[string]any{
			"a": "first",
			"nested": map[string]any{
				"a": 1,
				"b": 2,
			},
			"z": "last",
		},
	}, []string{"payload"}, "req", "1700000000", "app")
	if left != right {
		t.Fatalf("expected stable sign text, left=%q right=%q", left, right)
	}
}

// TestBuildSignStringResolvesNestedFieldPaths 校验登录响应的嵌套敏感明文真实参与回签。
func TestBuildSignStringResolvesNestedFieldPaths(t *testing.T) {
	data := map[string]any{
		"token": "token-value",
		"user": map[string]any{
			"phone":       "138****0000",
			"buildMFAURL": "otpauth://demo",
		},
	}
	got := BuildSignString(data, []string{"token", "user.phone", "user.buildMFAURL"}, "trace", "1700000000", "app")
	want := "v2|app=3:app|trace=5:trace|timestamp=10:1700000000" +
		"|field=5:token11:token-value" +
		"|field=16:user.buildMFAURL14:otpauth://demo" +
		"|field=10:user.phone11:138****0000"
	if got != want {
		t.Fatalf("BuildSignString() = %q, want %q", got, want)
	}
}
