package security

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// TestEncodeCipherParams 验证字段级加密头编码会去空、去重并拒绝整包加密。
func TestEncodeCipherParams(t *testing.T) {
	if got := EncodeCipherParams([]string{CipherWholeBody}); got != "" {
		t.Fatalf("EncodeCipherParams whole body = %q, want empty", got)
	}

	got := EncodeCipherParams([]string{"token", " token ", "", "user.phone"})
	body, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	var params []string
	if err := json.Unmarshal(body, &params); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	want := []string{"token", "user.phone"}
	if len(params) != len(want) {
		t.Fatalf("params len = %d, want %d", len(params), len(want))
	}
	for index := range want {
		if params[index] != want[index] {
			t.Fatalf("params[%d] = %q, want %q", index, params[index], want[index])
		}
	}
}
