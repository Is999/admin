package main

import (
	"context"
	"flag"
	"path/filepath"
	"testing"
)

// TestResolveExplicitRunModeAbsent 验证未显式传 `-mode` 时会回退到配置文件 `run_mode`。
func TestResolveExplicitRunModeAbsent(t *testing.T) {
	flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
	mode := flagSet.Int("mode", 0, "run mode")
	if err := flagSet.Parse([]string{}); err != nil {
		t.Fatalf("parse flags failed: %v", err)
	}
	if got := resolveExplicitRunMode(flagSet, mode); got != nil {
		t.Fatalf("expected nil explicit mode when cli flag is absent, got %v", *got)
	}
}

// TestResolveExplicitRunModePresent 验证显式传 `-mode` 时会优先使用命令行值。
func TestResolveExplicitRunModePresent(t *testing.T) {
	flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
	mode := flagSet.Int("mode", 0, "run mode")
	if err := flagSet.Parse([]string{"-mode", "2"}); err != nil {
		t.Fatalf("parse flags failed: %v", err)
	}
	got := resolveExplicitRunMode(flagSet, mode)
	if got == nil {
		t.Fatal("expected explicit cli mode to be returned")
	}
	if *got != 2 {
		t.Fatalf("expected explicit cli mode 2, got %d", *got)
	}
	*mode = 4
	if *got != 2 {
		t.Fatalf("expected returned cli mode to be a stable snapshot, got %d", *got)
	}
}

// TestResolveExplicitRunModeZeroPresent 验证明示 `-mode=0` 会被识别出来，后续由配置兜底逻辑处理。
func TestResolveExplicitRunModeZeroPresent(t *testing.T) {
	flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
	mode := flagSet.Int("mode", 0, "run mode")
	if err := flagSet.Parse([]string{"-mode", "0"}); err != nil {
		t.Fatalf("parse flags failed: %v", err)
	}
	got := resolveExplicitRunMode(flagSet, mode)
	if got == nil {
		t.Fatal("expected explicit zero cli mode to be returned")
	}
	if *got != 0 {
		t.Fatalf("expected explicit cli mode 0, got %d", *got)
	}
}

// TestRunAppReturnsNonZeroOnWireError 验证装配失败会返回非零退出码，便于托管平台感知失败并重启。
func TestRunAppReturnsNonZeroOnWireError(t *testing.T) {
	missingFile := filepath.Join(t.TempDir(), "missing.yaml")
	if code := runApp(context.Background(), missingFile, nil); code == 0 {
		t.Fatal("期望缺失配置文件时返回非零退出码")
	}
}
