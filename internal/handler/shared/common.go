package shared

import (
	"net/http"

	"github.com/Is999/go-utils/errors"

	codes "admin/common/codes"
	"admin/helper"
	"admin/internal/infra/loggerx"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// LogicObj 抽象所有 handler 依赖的最小 logic 能力，便于统一响应与审计封装。
type LogicObj interface {
	// Errorf 按项目统一日志风格输出错误日志。
	Errorf(string, ...any)

	// AddAdminLog 记录管理员操作审计日志。
	AddAdminLog(action model.AdminLogAction, route, method, describe string, data any)

	// GetCtxAdmin 返回当前请求上下文中的管理员信息。
	GetCtxAdmin() *helper.CtxAdmin
}

// HandlerFunc 约定 handler 内部统一返回 logic 对象和业务响应，便于公共响应与审计逻辑复用。
type HandlerFunc func(r *http.Request) (LogicObj, *types.BizResult)

// RespHandlerFunc 处理普通接口响应，不附带管理员审计日志。
func RespHandlerFunc(fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logicObj, resp := fn(r)
		WriteBizResponse(w, r, logicObj, resp, nil)
	}
}

// ActionLogHandler 在统一响应之外补充管理员审计日志，避免每个 handler 重复写审计模板代码。
// meta 是路由声明中的统一元数据，审计动作、别名和说明都从同一份 RouteMeta 派生。
func ActionLogHandler(meta RouteMeta, fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logicObj, resp := fn(r)
		WriteBizResponse(w, r, logicObj, resp, ActionLogParamFromMeta(meta))
	}
}

// ActionExec 定义带审计日志的标准 handler 执行函数。
// 调用方只需要关心“如何构造 logic 并执行业务”，公共层统一处理参数解析、响应写出和审计补充。
type ActionExec[Req any] func(*http.Request, *svc.ServiceContext, *Req) (LogicObj, *types.BizResult)

// RespExec 定义不带审计日志的标准 handler 执行函数。
type RespExec[Req any] func(*http.Request, *svc.ServiceContext, *Req) (LogicObj, *types.BizResult)

// ActionHandler 泛型封装，简化标准 CRUD handler 的模板代码。
func ActionHandler[Req any](
	meta RouteMeta,
	exec ActionExec[Req],
) func(*svc.ServiceContext) http.HandlerFunc {
	return func(sCtx *svc.ServiceContext) http.HandlerFunc {
		return ActionLogHandler(meta, func(r *http.Request) (LogicObj, *types.BizResult) {
			var req Req
			if err := httpx.Parse(r, &req); err != nil {
				return nil, ParamErrorResult(err)
			}
			logicObj, resp := exec(r, sCtx, &req)
			if resp == nil {
				return logicObj, types.NewBizResult(codes.ServerError).WithError(errors.New("业务响应为空"))
			}
			resp.WithReq(&req)
			return logicObj, resp
		})
	}
}

// RespHandler 泛型封装，简化普通接口（无审计日志）的 handler 模板代码。
func RespHandler[Req any](
	exec RespExec[Req],
) func(*svc.ServiceContext) http.HandlerFunc {
	return func(sCtx *svc.ServiceContext) http.HandlerFunc {
		return RespHandlerFunc(func(r *http.Request) (LogicObj, *types.BizResult) {
			var req Req
			if err := httpx.Parse(r, &req); err != nil {
				return nil, ParamErrorResult(err)
			}
			logicObj, resp := exec(r, sCtx, &req)
			if resp == nil {
				return logicObj, types.NewBizResult(codes.ServerError).WithError(errors.New("业务响应为空"))
			}
			resp.WithReq(&req)
			return logicObj, resp
		})
	}
}

// ParamErrorResult 统一封装参数解析失败响应，强制走国际化模板，避免各 handler 重复拼接文案。
func ParamErrorResult(err error) *types.BizResult {
	return types.ParamErrorResult(err)
}

// WriteBizResponse 把标准业务响应写出，并按需补充审计日志，保证成功/失败处理路径一致。
func WriteBizResponse(w http.ResponseWriter, r *http.Request, logicObj LogicObj, resp *types.BizResult, logMeta *ActionLogParam) {
	// 兜住空响应，避免 handler 意外返回 nil 时 panic。
	if resp == nil {
		resp = types.NewBizResult(codes.ServerError).WithError(errors.New("业务响应为空"))
	}

	message := resp.ResolveMessage(r.Context())
	if resp.IsFailure() {
		if logicObj != nil && resp.Error != nil && !errors.Is(resp.Error, types.Nil) {
			admin := logicObj.GetCtxAdmin()
			if admin != nil && admin.Name != "" {
				logicObj.Errorf("%s %s", admin.Name, loggerx.ErrorChain(resp.Error))
			} else {
				logicObj.Errorf("%s", loggerx.ErrorChain(resp.Error))
			}
		}
		jsonResp := helper.NewJSONResp(r.Context(), w).SetCode(resp.Code)
		if resp.Error != nil && !errors.Is(resp.Error, types.Nil) {
			jsonResp = jsonResp.SetError(resp.Error)
		}
		jsonResp.Fail(message)
	} else {
		helper.NewJSONResp(r.Context(), w).SetCode(resp.Code).SetMessage(message).Success(resp.Data)
	}

	// 统一在响应写出后补充审计日志，成功和失败都保留，便于链路回溯。
	if logicObj != nil && logMeta != nil {
		logicObj.AddAdminLog(logMeta.Action, logMeta.Route, logMeta.Method, logMeta.Describe, resp.Req)
	}
}

// ActionLogParam 表示记录操作日志所需的参数。
type ActionLogParam struct {
	Action   model.AdminLogAction // 操作行为枚举
	Route    string               // 路由别名
	Method   string               // 审计方法标识，与路由别名保持一致，避免额外维护方法映射
	Describe string               // 中文业务说明
}

// ActionLogParamFromMeta 从路由元数据生成审计日志参数，统一 RouteMeta 到审计字段的转换规则。
func ActionLogParamFromMeta(meta RouteMeta) *ActionLogParam {
	if meta.Action == "" {
		return nil
	}
	return &ActionLogParam{
		Action:   meta.Action,
		Route:    string(meta.Alias),
		Method:   string(meta.Alias),
		Describe: meta.Describe,
	}
}

// ActionReq 返回仅包含 action 字段的最小请求上下文，用于无请求体接口补充审计或响应链路信息。
func ActionReq(action string) map[string]any {
	return map[string]any{"action": action}
}
