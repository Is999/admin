package logic

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"admin_cron/helper"
	"admin_cron/internal/audit"
	"admin_cron/internal/infra/loggerx"
	"admin_cron/internal/model"
	"admin_cron/internal/requestctx"
	"admin_cron/internal/svc"

	"github.com/Is999/go-utils/errors"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// adminLogWriteTimeout 为审计日志落库预留独立短超时，避免业务请求超时后把日志写入一并取消。
	adminLogWriteTimeout = 2 * time.Second
)

// BaseLogic 是所有业务 logic 的公共基座，统一封装请求上下文、日志、DB、Redis 与审计能力。
type BaseLogic struct {
	logx.Logger                     // 已绑定当前请求上下文的日志记录器
	ctx         context.Context     // 当前 logic 处理链路使用的上下文
	svc         *svc.ServiceContext // 绑定当前上下文后的服务依赖集合
}

// NewBaseLogic 兼容现有以 http.Request 创建 logic 的调用方式。
func NewBaseLogic(r *http.Request, svcCtx *svc.ServiceContext) *BaseLogic {
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	return NewBaseLogicWithContext(ctx, svcCtx)
}

// NewBaseLogicWithContext 为当前请求克隆一份带上下文的 ServiceContext，确保 DB、日志和审计共享同一条链路。
func NewBaseLogicWithContext(ctx context.Context, svcCtx *svc.ServiceContext) *BaseLogic {
	ctx, _ = requestctx.New(ctx)
	ctx = loggerx.BindContext(ctx)

	var scopedSvc *svc.ServiceContext
	if svcCtx != nil {
		// DB 连接在这里提前绑定请求上下文，后续 model 层查询会自动继承 trace 与日志字段。
		// ServiceContext 内部已经包含原子快照字段，因此通过显式构造作用域副本避免直接复制 atomic.Value。
		scopedSvc = svcCtx.ScopedWithContext(ctx)
	}

	return &BaseLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svc:    scopedSvc,
	}
}

// Context 返回当前 logic 绑定的请求上下文。
func (l *BaseLogic) Context() context.Context {
	return l.ctx
}

// Redis 返回共享 Redis 客户端（兼容单机/集群），具体命令执行时仍应显式传入 l.ctx。
func (l *BaseLogic) Redis() redis.UniversalClient {
	if l.svc == nil {
		return nil
	}
	return l.svc.Rds
}

// Audit 返回统一审计记录器，避免业务层直接拼装 admin_log。
func (l *BaseLogic) Audit() *audit.Recorder {
	if l.svc == nil {
		return nil
	}
	return l.svc.Audit
}

// Meta 返回当前请求链路元数据，供少量需要直接读取 trace/user/result 的逻辑复用。
func (l *BaseLogic) Meta() *requestctx.Meta {
	return requestctx.FromContext(l.ctx)
}

// ClientIP 返回当前请求的客户端 IP（优先取 request meta）。
func (l *BaseLogic) ClientIP() string {
	if meta := l.Meta(); meta != nil {
		return meta.ClientIP
	}
	return ""
}

// AccessToken 返回当前请求的访问令牌，常用于透传或审计补充。
func (l *BaseLogic) AccessToken() string {
	if meta := l.Meta(); meta != nil {
		return meta.AccessToken
	}
	return ""
}

// GetCtxAdmin 返回当前请求上下文中的管理员信息；未登录场景下返回空对象而不是 nil。
func (l *BaseLogic) GetCtxAdmin() *helper.CtxAdmin {
	admin := helper.GetCtxAdmin(l.ctx)
	if admin == nil {
		return &helper.CtxAdmin{}
	}
	return admin
}

// AddAdminLog 通过统一审计记录器落库，避免业务代码自行开 goroutine 或重复拼装公共字段。
func (l *BaseLogic) AddAdminLog(action model.AdminLogAction, route, method, describe string, data any) {
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		loggerx.Infow(l.Context(), "管理员审计跳过",
			logx.Field("skip_reason", "缺少管理员上下文"),
			logx.Field("action", action),
			logx.Field("route", route),
			logx.Field("handler_method", method),
		)
		return
	}
	if l.Audit() == nil {
		loggerx.Infow(l.Context(), "管理员审计跳过",
			logx.Field("skip_reason", "审计记录器未就绪"),
			logx.Field("action", action),
			logx.Field("route", route),
			logx.Field("handler_method", method),
		)
		return
	}

	// 审计日志在 handler 响应写出后立即落库，早于 access log middleware 的 defer 收口。
	// 因此这里先按请求开始时间刷新一次当前耗时，避免 admin_log.latency_ms 一直使用默认 0。
	requestctx.RefreshLatency(l.Context())

	// 审计日志需要继承当前请求的 trace / user / route 等上下文值，但不应继续受原请求 deadline 影响。
	// 否则主业务接近超时时，日志写库会被同一个 context 一起取消，导致“业务成功但审计缺失”。
	auditCtx := context.Background()
	if l.ctx != nil {
		auditCtx = context.WithoutCancel(l.ctx)
	}
	auditCtx, cancel := context.WithTimeout(auditCtx, adminLogWriteTimeout)
	defer cancel()

	requestctx.SetRoute(auditCtx, route)
	if err := l.Audit().Record(auditCtx, audit.Event{
		Action:   action,
		Route:    route,
		Method:   method,
		Describe: describe,
		Data:     data,
		UserID:   ctxAdmin.ID,
		UserName: ctxAdmin.Name,
		IP: func() string {
			if l.ClientIP() != "" {
				return l.ClientIP()
			}
			return ctxAdmin.IP
		}(),
	}); err != nil {
		logWrappedError(l, err, "AddAdminLog 记录管理员操作日志失败")
	}
}

// RdsGetJsonObj 从 Redis 读取 JSON 字符串并反序列化到目标对象。
func (l *BaseLogic) RdsGetJsonObj(key string, dest any) error {
	val, err := l.svc.Rds.Get(l.ctx, key).Result()
	if err != nil {
		return errors.Tag(err)
	}
	return errors.Tag(json.Unmarshal([]byte(val), dest))
}

// wrapLogicError 统一给逻辑层错误补充业务上下文，同时保留原始错误链。
func wrapLogicError(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	format = strings.TrimSpace(format)
	if format == "" {
		return errors.Tag(err)
	}
	if len(args) > 0 {
		return errors.Wrapf(err, format, args...)
	}
	return errors.Wrap(err, format)
}

// logWrappedError 用于无法继续向上返回的兜底场景，统一补充上下文后按一条错误链打印。
func logWrappedError(logger interface{ Errorf(string, ...any) }, err error, format string, args ...any) {
	if logger == nil || err == nil {
		return
	}
	logger.Errorf("%s", loggerx.ErrorChain(wrapLogicError(err, format, args...)))
}

// RdsSetJSONValue 将值序列化为 JSON 后写入 Redis，并设置过期时间。
func (l *BaseLogic) RdsSetJSONValue(key string, value any, expireSec int) error {
	data, err := json.Marshal(value)
	if err != nil {
		return errors.Tag(err)
	}
	return errors.Tag(l.svc.Rds.Set(l.ctx, key, data, time.Duration(expireSec)*time.Second).Err())
}

// RdsDelKeys 批量删除 Redis 键。
func (l *BaseLogic) RdsDelKeys(keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if err := l.svc.Rds.Del(l.ctx, key).Err(); err != nil {
			return errors.Tag(err)
		}
	}
	return nil
}

// RdsReloadKey 先删除指定 Redis 键，再执行回调重建缓存内容。
func (l *BaseLogic) RdsReloadKey(key string, fn func() error) error {
	if key == "" {
		return nil
	}
	err := l.svc.Rds.Del(l.ctx, key).Err()
	if err != nil {
		return errors.Tag(err)
	}
	return fn()
}

// ReloadCacheAsync 把缓存重建请求投递到统一任务队列。
// 旧版直接起 goroutine 的方式虽然简单，但不具备重试、崩溃恢复和聚合能力；现在统一收口到任务系统。
func (l *BaseLogic) ReloadCacheAsync(operation, key string) {
	if l == nil || l.svc == nil || l.svc.Task == nil || key == "" {
		return
	}
	if err := l.svc.Task.EnqueueCacheRefresh(l.ctx, operation, []string{key}); err != nil {
		if operation == "" {
			operation = "BaseLogic.ReloadCacheAsync"
		}
		logWrappedError(l, err, "%s enqueue cache reload failed, key=%s", operation, key)
	}
}
