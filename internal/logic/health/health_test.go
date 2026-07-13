package health

import (
	"context"
	"testing"
	"time"

	"admin/internal/config"
	"admin/internal/svc"
	"admin/internal/types"
)

// TestRunDependencyChecksRunsConcurrently 确保慢依赖不会串行放大 readiness 延迟。
func TestRunDependencyChecksRunsConcurrently(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	checks := []dependencyCheck{
		func() (types.HealthDependencyStatus, error) {
			started <- "mysql"
			<-release
			return dependencyOK("mysql"), nil
		},
		func() (types.HealthDependencyStatus, error) {
			started <- "redis"
			<-release
			return dependencyOK("redis"), nil
		},
	}
	done := make(chan []types.HealthDependencyStatus, 1)
	go func() {
		statuses, _ := runDependencyChecks(checks)
		done <- statuses
	}()

	for range checks {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("依赖探测未并发启动")
		}
	}
	close(release)
	statuses := <-done
	if len(statuses) != 2 || statuses[0].Name != "mysql" || statuses[1].Name != "redis" {
		t.Fatalf("依赖状态顺序不稳定: %+v", statuses)
	}
}

// TestCheckVirusScanner 验证 readiness 会检查实际扫描器配置。
func TestCheckVirusScanner(t *testing.T) {
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{})
	logic := NewHealthLogic(context.Background(), svcCtx)
	status, err := logic.checkVirusScanner(context.Background())
	if err != nil || status.Status != healthStatusOK {
		t.Fatalf("noop 扫描器就绪检查失败 status=%+v err=%v", status, err)
	}
	svcCtx = svc.NewServiceContext(config.Config{FileStorage: config.FileStorageConfig{
		VirusScanner: config.FileStorageVirusScannerConfig{
			Name:    config.VirusScannerClamAV,
			Command: "/path/not-found/clamdscan",
		},
	}}, svc.Dependencies{})
	logic = NewHealthLogic(context.Background(), svcCtx)
	if _, err = logic.checkVirusScanner(context.Background()); err == nil {
		t.Fatal("期望不可用的 ClamAV 客户端使 readiness 失败")
	}
}
