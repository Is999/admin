package audit

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestSerialize 验证对应场景。
func TestSerialize(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		maxBytes int
		contains []string // 期望输出中必须包含的子串
		excludes []string // 期望输出中不能包含的子串
	}{
		{
			name: "简单结构体脱敏",
			input: struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}{
				Username: "admin",
				Password: "supersecret123",
			},
			maxBytes: 1000,
			contains: []string{`"username":"admin"`, `"password":"***"`},
			excludes: []string{"supersecret123"},
		},
		{
			name: "数字和布尔脱敏",
			input: map[string]any{
				"normal": 123,
				"secret": 456,
				"token":  true,
			},
			maxBytes: 1000,
			contains: []string{`"normal":123`, `"secret":"***"`, `"token":"***"`},
			excludes: []string{"456", "true"},
		},
		{
			name: "嵌套对象脱敏",
			input: map[string]any{
				"user": map[string]any{
					"name":  "test",
					"token": "nested_token_value",
				},
				"password": map[string]any{
					"old": "old_pass",
					"new": "new_pass",
				}, // password 整个对象会被脱敏
			},
			maxBytes: 1000,
			contains: []string{`"name":"test"`, `"token":"***"`, `"password":"***"`},
			excludes: []string{"nested_token_value", "old_pass", "new_pass"},
		},
		{
			name: "数组中的对象脱敏",
			input: []map[string]any{
				{"id": 1, "password": "p1"},
				{"id": 2, "password": "p2"},
			},
			maxBytes: 1000,
			contains: []string{`"id":1`, `"id":2`, `"password":"***"`},
			excludes: []string{"p1", "p2"},
		},
		{
			name: "包含转义引号的字符串",
			input: map[string]string{
				"desc":     "this is a \"quote\"",
				"password": "my \"secret\" pass",
			},
			maxBytes: 1000,
			contains: []string{`"desc":"this is a \"quote\""`, `"password":"***"`},
			excludes: []string{"my \\\"secret\\\" pass"},
		},
		{
			name: "AES和RSA引用字段脱敏",
			input: map[string]string{
				"aesKeyRef":              "/etc/admin/keys/app/aes_key",
				"aes_iv_ref":             "/etc/admin/keys/app/aes_iv",
				"rsaPrivateKeyServerRef": "/etc/admin/keys/app/server_private.pem",
				"rsaPublicKeyServerRef":  "/etc/admin/keys/app/server_public.pem",
			},
			maxBytes: 1000,
			contains: []string{
				`"aesKeyRef":"***"`,
				`"aes_iv_ref":"***"`,
				`"rsaPrivateKeyServerRef":"***"`,
				`"rsaPublicKeyServerRef":"***"`,
			},
			excludes: []string{
				"/etc/admin/keys/app/aes_key",
				"/etc/admin/keys/app/aes_iv",
				"server_private.pem",
				"server_public.pem",
			},
		},
		{
			name:     "超长截断",
			input:    map[string]string{"long_field": strings.Repeat("A", 100)},
			maxBytes: 50,
			contains: []string{"...(truncated)"},
		},
		{
			name:     "nil 值",
			input:    nil,
			maxBytes: 100,
			contains: []string{""},
		},
		{
			name:     "纯字符串输入",
			input:    "just a string",
			maxBytes: 100,
			contains: []string{"just a string"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Serialize(tt.input, tt.maxBytes)

			for _, c := range tt.contains {
				if !strings.Contains(got, c) && got != c {
					t.Errorf("Serialize() = %v, want it to contain %v", got, c)
				}
			}

			for _, e := range tt.excludes {
				if strings.Contains(got, e) {
					t.Errorf("Serialize() = %v, want it to exclude %v", got, e)
				}
			}

			// 如果不是截断，验证输出仍是合法的 JSON（除了 nil 和纯字符串）
			if tt.input != nil && fmt.Sprintf("%T", tt.input) != "string" && !strings.HasSuffix(got, "...(truncated)") {
				var dummy any
				if err := json.Unmarshal([]byte(got), &dummy); err != nil {
					t.Errorf("Serialize() output is not valid JSON: %v, output: %s", err, got)
				}
			}
		})
	}
}

// TestSanitizeText 验证历史文本日志回显时也会执行脱敏。
func TestSanitizeText(t *testing.T) {
	input := `{"aesKeyRef":"/etc/admin/keys/app/aes_key","rsaPrivateKeyServerRef":"/etc/admin/keys/app/server_private.pem","remark":"ok"}`
	got := SanitizeText(input, 4096)
	for _, item := range []string{`"aesKeyRef":"***"`, `"rsaPrivateKeyServerRef":"***"`, `"remark":"ok"`} {
		if !strings.Contains(got, item) {
			t.Fatalf("SanitizeText() = %s, want contain %s", got, item)
		}
	}
	for _, item := range []string{"/etc/admin/keys/app/aes_key", "server_private.pem"} {
		if strings.Contains(got, item) {
			t.Fatalf("SanitizeText() = %s, want exclude %s", got, item)
		}
	}
}

// BenchmarkSerialize_JSON_vs_New 性能对比测试
// go test -benchmem -run=^$ -bench ^BenchmarkSerialize$ admin/internal/audit
// BenchmarkSerialize 基准测试对应场景。
func BenchmarkSerialize(b *testing.B) {
	data := map[string]any{
		"user": map[string]any{
			"username": "admin",
			"password": "super_secret_password",
			"profile": map[string]any{
				"age": 30,
				"bio": "A very long bio string to simulate larger payload",
			},
		},
		"permissions": []string{"read", "write", "admin"},
		"token":       "jwt_token_string_here",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Serialize(data, 4096)
	}
}
