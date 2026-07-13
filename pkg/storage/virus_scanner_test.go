package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"admin/internal/config"
)

// TestClamAVVirusScannerFailClosed 验证干净、命中和扫描异常三种结果不会混淆。
func TestClamAVVirusScannerFailClosed(t *testing.T) {
	tests := []struct {
		name        string // 测试场景名称
		exitCode    int    // 模拟扫描命令退出码
		output      string // 模拟扫描命令输出
		wantMessage string // 期望错误信息片段
	}{
		{name: "clean", exitCode: 0},
		{name: "infected", exitCode: 1, wantMessage: "未通过病毒扫描"},
		{name: "scanner error", exitCode: 2, output: "daemon unavailable", wantMessage: "daemon unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scanner, err := NewVirusScanner(config.FileStorageVirusScannerConfig{
				Name:    config.VirusScannerClamAV,
				Command: writeVirusScannerCommand(t, test.exitCode, test.output),
			})
			if err != nil {
				t.Fatalf("创建 ClamAV 扫描器失败: %v", err)
			}
			err = scanner.ScanFile(context.Background(), "/tmp/upload.bin")
			if test.wantMessage == "" && err != nil {
				t.Fatalf("干净文件扫描失败: %v", err)
			}
			if test.wantMessage != "" && (err == nil || !strings.Contains(err.Error(), test.wantMessage)) {
				t.Fatalf("扫描结果未按 fail-closed 分类 err=%v want=%q", err, test.wantMessage)
			}
		})
	}
}

// TestNewVirusScannerRejectsUnknownName 验证未知扫描器不会静默回退到 noop。
func TestNewVirusScannerRejectsUnknownName(t *testing.T) {
	if _, err := NewVirusScanner(config.FileStorageVirusScannerConfig{Name: "custom"}); err == nil {
		t.Fatal("期望未知病毒扫描器返回错误，实际为 nil")
	}
}

// writeVirusScannerCommand 创建返回固定退出码的 clamdscan 测试替身。
func writeVirusScannerCommand(t *testing.T, exitCode int, output string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clamdscan")
	body := fmt.Sprintf("#!/bin/sh\nprintf '%%s' %q >&2\nexit %d\n", output, exitCode)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("创建 clamdscan 测试替身失败: %v", err)
	}
	return path
}
