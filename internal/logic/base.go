package logic

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	keys "admin/common/rediskeys"
	"admin/common/runtimecfg"
	"admin/helper"
	"admin/internal/audit"
	"admin/internal/infra/loggerx"
	"admin/internal/model"
	"admin/internal/requestctx"
	"admin/internal/svc"

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
	Ctx         context.Context     // 当前 logic 处理链路使用的上下文
	Svc         *svc.ServiceContext // 绑定当前上下文后的服务依赖集合
}

// NewBaseLogic 用于 HTTP 请求入口创建 logic。
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
		Ctx:    ctx,
		Svc:    scopedSvc,
	}
}

// Redis 返回共享 Redis 客户端（支持单机/集群），具体命令执行时仍应显式传入 l.Ctx。
func (l *BaseLogic) Redis() redis.UniversalClient {
	if l.Svc == nil {
		return nil
	}
	return l.Svc.Rds
}

// AppID 返回当前 Redis 缓存命名空间使用的 app_id。
func (l *BaseLogic) AppID() string {
	if l == nil || l.Svc == nil {
		return ""
	}
	return strings.TrimSpace(l.Svc.CurrentConfig().AppID)
}

// AppRedisKey 给直接 Redis 缓存和锁追加当前 app_id 命名空间。
func (l *BaseLogic) AppRedisKey(key string) string {
	if l == nil {
		return ""
	}
	appID := l.AppID()
	if appID == "" || appID != runtimecfg.AppID() {
		return ""
	}
	return keys.WithPrefix(key)
}

// AppRedisKeys 批量追加当前 app_id 命名空间并过滤空 key。
func (l *BaseLogic) AppRedisKeys(cacheKeys ...string) []string {
	result := make([]string, 0, len(cacheKeys))
	for _, key := range cacheKeys {
		key = l.AppRedisKey(key)
		if key == "" {
			continue
		}
		result = append(result, key)
	}
	return helper.UniqueNonEmptyStrings(result)
}

// Audit 返回统一审计记录器，避免业务层直接拼装 admin_log。
func (l *BaseLogic) Audit() *audit.Recorder {
	if l.Svc == nil {
		return nil
	}
	return l.Svc.Audit
}

// Meta 返回当前请求链路元数据，供少量需要直接读取 trace/user/result 的逻辑复用。
func (l *BaseLogic) Meta() *requestctx.Meta {
	return requestctx.FromContext(l.Ctx)
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
	admin := helper.GetCtxAdmin(l.Ctx)
	if admin == nil {
		return &helper.CtxAdmin{}
	}
	return admin
}

// AddAdminLog 通过统一审计记录器落库，避免业务代码自行开 goroutine 或重复拼装公共字段。
func (l *BaseLogic) AddAdminLog(action model.AdminLogAction, route, method, describe string, data any) {
	ctxAdmin := l.GetCtxAdmin()
	if ctxAdmin.ID == 0 {
		loggerx.Infow(l.Ctx, "管理员审计跳过",
			logx.Field("skip_reason", "缺少管理员上下文"),
			logx.Field("action", action),
			logx.Field("route", route),
			logx.Field("handler_method", method),
		)
		return
	}
	if l.Audit() == nil {
		loggerx.Infow(l.Ctx, "管理员审计跳过",
			logx.Field("skip_reason", "审计记录器未就绪"),
			logx.Field("action", action),
			logx.Field("route", route),
			logx.Field("handler_method", method),
		)
		return
	}

	// 审计日志在 handler 响应写出后立即落库，早于 access log middleware 的 defer 收口。
	// 因此这里先按请求开始时间刷新一次当前耗时，避免 admin_log.latency_ms 一直使用默认 0。
	requestctx.RefreshLatency(l.Ctx)

	// 审计日志需要继承当前请求的 trace / user / route 等上下文值，但不应继续受原请求 deadline 影响。
	// 否则主业务接近超时时，日志写库会被同一个 context 一起取消，导致“业务成功但审计缺失”。
	auditCtx := context.Background()
	if l.Ctx != nil {
		auditCtx = context.WithoutCancel(l.Ctx)
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

// RdsGetJsonObj 从当前 app_id 命名空间读取 JSON 字符串并反序列化到目标对象。
func (l *BaseLogic) RdsGetJsonObj(key string, dest any) error {
	if l == nil || l.Svc == nil || l.Svc.Rds == nil {
		return errors.New("Redis 未初始化")
	}
	key = l.AppRedisKey(key)
	if key == "" {
		return errors.New("Redis key 为空")
	}
	val, err := l.Svc.Rds.Get(l.Ctx, key).Result()
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

// WrapLogicError 给拆分后的领域包复用统一错误上下文包装。
func WrapLogicError(err error, format string, args ...any) error {
	return wrapLogicError(err, format, args...)
}

// logWrappedError 用于无法继续向上返回的兜底场景，统一补充上下文后按一条错误链打印。
func logWrappedError(logger interface{ Errorf(string, ...any) }, err error, format string, args ...any) {
	if logger == nil || err == nil {
		return
	}
	logger.Errorf("%s", loggerx.ErrorChain(wrapLogicError(err, format, args...)))
}

// LogWrappedError 给拆分后的领域包复用兜底错误日志格式。
func LogWrappedError(logger interface{ Errorf(string, ...any) }, err error, format string, args ...any) {
	logWrappedError(logger, err, format, args...)
}

// RdsSetJSONValue 将值序列化为 JSON 后写入当前 app_id 命名空间，并设置过期时间。
func (l *BaseLogic) RdsSetJSONValue(key string, value any, expireSec int) error {
	if l == nil || l.Svc == nil || l.Svc.Rds == nil {
		return errors.New("Redis 未初始化")
	}
	key = l.AppRedisKey(key)
	if key == "" {
		return errors.New("Redis key 为空")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return errors.Tag(err)
	}
	return errors.Tag(l.Svc.Rds.Set(l.Ctx, key, data, time.Duration(expireSec)*time.Second).Err())
}

// RdsDelKeys 批量删除当前 app_id 命名空间下的 Redis 键。
func (l *BaseLogic) RdsDelKeys(keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	if l == nil || l.Svc == nil || l.Svc.Rds == nil {
		return errors.New("Redis 未初始化")
	}
	deleted := false
	for _, key := range keys {
		key = l.AppRedisKey(key)
		if key == "" {
			continue
		}
		deleted = true
		if err := l.Svc.Rds.Del(l.Ctx, key).Err(); err != nil {
			return errors.Tag(err)
		}
	}
	if !deleted {
		return errors.New("Redis key 为空")
	}
	return nil
}

// RdsReloadKey 先删除指定 Redis 键，再执行回调重建缓存内容。
func (l *BaseLogic) RdsReloadKey(key string, fn func() error) error {
	key = l.AppRedisKey(key)
	if key == "" {
		return nil
	}
	err := l.Svc.Rds.Del(l.Ctx, key).Err()
	if err != nil {
		return errors.Tag(err)
	}
	return fn()
}

// ReloadCacheAsync 把缓存重建请求投递到统一任务队列。
// 早期直接起 goroutine 的方式虽然简单，但不具备重试、崩溃恢复和聚合能力；现在统一收口到任务系统。
func (l *BaseLogic) ReloadCacheAsync(operation, key string) {
	if l == nil || l.Svc == nil || l.Svc.Task == nil || key == "" {
		return
	}
	if err := l.Svc.Task.EnqueueCacheRefresh(l.Ctx, operation, []string{key}); err != nil {
		if operation == "" {
			operation = "BaseLogic.ReloadCacheAsync"
		}
		logWrappedError(l, err, "%s enqueue cache reload failed, key=%s", operation, key)
	}
}
