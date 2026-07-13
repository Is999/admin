package runtimeconfig

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"admin/internal/audit"
	"admin/internal/model"
	"admin/internal/requestctx"
	"admin/internal/svc"

	"github.com/zeromicro/go-zero/rest/pathvar"
)

// auditEnqueueSpy 记录测试期间收到的管理员审计投递次数。
type auditEnqueueSpy struct {
	calls int // 审计投递次数
}

// EnqueueAdminLog 实现审计 Collector 投递接口。
func (s *auditEnqueueSpy) EnqueueAdminLog(context.Context, string, model.AdminLog) error {
	s.calls++
	return nil
}

// TestGetArchiveJobProgressHandlerSkipsAudit 验证高频进度查询不会持续写管理员审计日志。
func TestGetArchiveJobProgressHandlerSkipsAudit(t *testing.T) {
	spy := &auditEnqueueSpy{}
	recorder := audit.NewRecorder(1024)
	recorder.SetEnqueuer(spy)
	svcCtx := &svc.ServiceContext{Audit: recorder}
	ctx, _ := requestctx.New(context.Background())
	requestctx.SetUser(ctx, 7, "admin", "127.0.0.1")
	req := httptest.NewRequest(http.MethodGet, "/api/runtime-config/archive-jobs/7/progress", nil).WithContext(ctx)
	req = pathvar.WithVars(req, map[string]string{"id": "7"})

	GetArchiveJobProgressHandler(svcCtx).ServeHTTP(httptest.NewRecorder(), req)

	if spy.calls != 0 {
		t.Fatalf("归档进度轮询不应写管理员审计日志，实际投递次数=%d", spy.calls)
	}
}
