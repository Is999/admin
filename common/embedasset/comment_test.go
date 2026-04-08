package embedasset

import "testing"

// TestStripLeadingLineCommentsRemovesHeaderOnly 验证只剥离文件头连续注释，不影响正文中的注释符。
func TestStripLeadingLineCommentsRemovesHeaderOnly(t *testing.T) {
	// source 表示带文件头注释和正文字符串注释符的 SQL 资产样例。
	source := "-- 代码资产：测试模板。\n-- KEYS/ARGV 说明。\n\nSELECT '-- keep' AS value\n-- tail comment\n"
	// got 保存剥离后的可执行文本。
	got := StripLeadingLineComments(source, "--")
	// want 表示只移除文件头后的期望结果，正文注释和字符串必须原样保留。
	want := "SELECT '-- keep' AS value\n-- tail comment\n"
	if got != want {
		t.Fatalf("StripLeadingLineComments() = %q, want %q", got, want)
	}
}

// TestStripLeadingLineCommentsKeepsPlainText 验证没有文件头注释时不删除普通文本前置空行。
func TestStripLeadingLineCommentsKeepsPlainText(t *testing.T) {
	// source 表示没有文件头注释的资产文本。
	source := "\nSELECT 1"
	// got 保存剥离后的结果。
	got := StripLeadingLineComments(source, "--")
	if got != source {
		t.Fatalf("StripLeadingLineComments() = %q, want original %q", got, source)
	}
}
