package admin

import (
	"context"
	"testing"

	"admin/internal/config"
	"admin/internal/infra/ipregion"
	corelogic "admin/internal/logic"
	"admin/internal/model"
	"admin/internal/requestctx"
	"admin/internal/svc"
)

// TestSetLastLoginIPClearsRegionWhenDisabled 验证关闭归属地能力后新 IP 不会继续关联旧地区。
func TestSetLastLoginIPClearsRegionWhenDisabled(t *testing.T) {
	locator, err := ipregion.New(config.IPRegionConfig{})
	if err != nil {
		t.Fatalf("ipregion.New() error = %v", err)
	}
	t.Cleanup(locator.Close)
	const (
		clientIP     = "2001:4860:4860::8888%very-long-interface-name"
		normalizedIP = "2001:4860:4860::8888"
	)
	ctx, meta := requestctx.New(context.Background())
	requestctx.SetRequest(ctx, "POST", "/api/auth/login", clientIP)
	svcCtx := svc.NewServiceContext(config.Config{}, svc.Dependencies{IPRegion: locator})
	logicObj := &AdminLogic{BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx)}
	admin := &model.Admin{LastLoginIP: "8.8.8.8", LastLoginIPAddr: "旧地区"}

	logicObj.setLastLoginIP(admin, clientIP)

	if admin.LastLoginIP != normalizedIP || admin.LastLoginIPAddr != "" {
		t.Fatalf("最后登录 IP 配对错误: ip=%q region=%q", admin.LastLoginIP, admin.LastLoginIPAddr)
	}
	if !meta.ClientIPRegionResolved || meta.ClientIPRegion != "" {
		t.Fatalf("请求级空归属地未缓存: resolved=%t region=%q", meta.ClientIPRegionResolved, meta.ClientIPRegion)
	}
}
