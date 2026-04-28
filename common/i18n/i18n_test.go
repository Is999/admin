package i18n

import (
	"testing"

	"admin/common/codes"
)

func TestNormalizeLocale(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", LocaleZHCN},
		{"zh-CN,zh;q=0.9,en;q=0.8", LocaleZHCN},
		{"en-US,en;q=0.9", LocaleENUS},
		{"en", LocaleENUS},
		{"fr-FR", LocaleZHCN},
	}

	for _, c := range cases {
		if got := NormalizeLocale(c.in); got != c.want {
			t.Fatalf("NormalizeLocale(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

func TestMessageByCode(t *testing.T) {
	if got := MessageByCode(codes.Success, LocaleENUS); got != "Success" {
		t.Fatalf("MessageByCode(Success,en)=%q", got)
	}
	if got := MessageByCode(codes.DeleteSuccess, LocaleENUS); got != "Deleted successfully" {
		t.Fatalf("MessageByCode(DeleteSuccess,en)=%q", got)
	}
	if got := MessageByCode(codes.CheckMFABind, LocaleENUS); got != "MFA binding and enablement are required" {
		t.Fatalf("MessageByCode(CheckMFABind,en)=%q", got)
	}
	if got := MessageByCode(codes.CheckPasswordReset, LocaleENUS); got != "Password must be changed before continuing" {
		t.Fatalf("MessageByCode(CheckPasswordReset,en)=%q", got)
	}
}

// TestMessageCatalogParity 校验中英文语言包 key 完整一致，避免新增文案只补一个语种。
func TestMessageCatalogParity(t *testing.T) {
	zhCNMessageCatalog := messageCatalog[LocaleZHCN]
	enUSMessageCatalog := messageCatalog[LocaleENUS]
	for key := range zhCNMessageCatalog {
		if enUSMessageCatalog[key] == "" {
			t.Fatalf("en-US catalog missing key %q", key)
		}
	}
	for key := range enUSMessageCatalog {
		if zhCNMessageCatalog[key] == "" {
			t.Fatalf("zh-CN catalog missing key %q", key)
		}
	}
}

// TestCodeToMessageKeyCatalogCoverage 校验业务码映射到的 key 在所有语言包中都有文案。
func TestCodeToMessageKeyCatalogCoverage(t *testing.T) {
	for code, key := range codeToMessageKey {
		for locale, catalog := range messageCatalog {
			if catalog[key] == "" {
				t.Fatalf("code %d key %q missing locale %s message", code, key, locale)
			}
		}
	}
}
