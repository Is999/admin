package security

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Is999/go-utils/errors"
)

// TestValidateSecurityFieldCountRejectsTooManyFields 验证对应场景符合预期。
func TestValidateSecurityFieldCountRejectsTooManyFields(t *testing.T) {
	fields := make([]string, 0, MaxSecurityFieldCount+1)
	for i := 0; i < MaxSecurityFieldCount+1; i++ {
		fields = append(fields, fmt.Sprintf("f%d", i))
	}
	if err := ValidateSecurityFieldCount(fields, "请求签名"); err == nil {
		t.Fatal("ValidateSecurityFieldCount() should reject too many fields")
	}
}

// TestValidateSecurityScalarValueRejectsComplexValue 验证对应场景符合预期。
func TestValidateSecurityScalarValueRejectsComplexValue(t *testing.T) {
	value := map[string]any{"name": "demo"}
	if err := ValidateSecurityScalarValue("请求签名", "profile", value); err == nil {
		t.Fatal("ValidateSecurityScalarValue() should reject complex value")
	}
}

// TestValidateSecurityTextValueRejectsOversizeValue 验证对应场景符合预期。
func TestValidateSecurityTextValueRejectsOversizeValue(t *testing.T) {
	value := strings.Repeat("x", MaxSecurityFieldBytes+1)
	if err := ValidateSecurityTextValue("请求加密", "password", value, MaxSecurityFieldBytes); err == nil {
		t.Fatal("ValidateSecurityTextValue() should reject oversize value")
	}
}

// TestValidateSecurityJSONValueRejectsOversizeValue 验证对应场景符合预期。
func TestValidateSecurityJSONValueRejectsOversizeValue(t *testing.T) {
	value := map[string]any{"text": strings.Repeat("x", MaxSecurityJSONFieldBytes)}
	if _, err := ValidateSecurityJSONValue("响应加密", "profile", value); err == nil {
		t.Fatal("ValidateSecurityJSONValue() should reject oversize JSON value")
	}
}

// TestValidateSecurityLimitErrorsUseSentinel 验证对应场景符合预期。
func TestValidateSecurityLimitErrorsUseSentinel(t *testing.T) {
	err := ValidateSecurityScalarValue("响应签名", "profile", map[string]any{"name": "demo"})
	if !errors.Is(err, ErrSecurityPayloadTooLarge) {
		t.Fatalf("ValidateSecurityScalarValue() error = %v, want ErrSecurityPayloadTooLarge", err)
	}
}
