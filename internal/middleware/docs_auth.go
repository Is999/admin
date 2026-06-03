package middleware

import (
	"net/http"
	"net/url"
	pathpkg "path"
	"strings"

	"github.com/Is999/go-utils/errors"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	"admin/helper"
	"admin/internal/infra/loggerx"
	securitylogic "admin/internal/logic/security"
	"admin/internal/requestctx"
	"admin/internal/routealias"
	"admin/internal/svc"

	"github.com/Is999/go-utils"
	"github.com/zeromicro/go-zero/rest"
)

// DocsJwtMiddleware 为文档站提供轻量鉴权；开发和测试环境跳过校验，生产环境要求携带有效 JWT。
func DocsJwtMiddleware(svcCtx *svc.ServiceContext) rest.Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ctx, _ := requestctx.New(r.Context())
			requestctx.SetRequest(ctx, r.Method, r.URL.Path, utils.ClientIP(r))
			docsAlias := docsRouteAliasForPath(r.URL.Path)
			requestctx.SetRoute(ctx, string(docsAlias))
			r = r.WithContext(ctx)
			mode := ""
			if svcCtx != nil {
				mode = svcCtx.CurrentConfig().Mode
			}
			if mode == "dev" || mode == "test" {
				// 开发和测试环境下跳过认证
				next(w, r)
				return
			}

			identity, err := verifyAdminTokenFromDocsRequest(ctx, svcCtx, r, true)
			if err != nil {
				message := i18n.MsgKeyTokenInvalid
				if errors.Is(err, errMissingBearerToken) {
					message = i18n.MsgKeyUnauthorizedText
				}
				if errors.Is(err, errTokenExpired) {
					message = i18n.MsgKeyTokenExpired
				}
				helper.NewJsonResp(ctx, w).
					SetHttpStatus(http.StatusUnauthorized).
					SetCode(codes.Unauthorized).
					Fail(message)
				return
			}

			ip := utils.ClientIP(r)
			if err = securitylogic.NewSecurityLogic(ctx, svcCtx).CheckAdminAccess(identity.UserID, string(docsAlias), ip, identity.LoginIP); err != nil {
				switch {
				case errors.Is(err, securitylogic.ErrAdminPermissionDenied):
					helper.NewJsonResp(ctx, w).
						SetHttpStatus(http.StatusForbidden).
						SetCode(codes.Forbidden).
						Fail(i18n.MsgKeyForbidden)
				case errors.Is(err, securitylogic.ErrAdminDisabled):
					helper.NewJsonResp(ctx, w).
						SetHttpStatus(http.StatusUnauthorized).
						SetCode(codes.Unauthorized).
						Fail(i18n.MsgKeyUserDisabled)
				case errors.Is(err, securitylogic.ErrAdminIPChanged):
					helper.NewJsonResp(ctx, w).
						SetHttpStatus(http.StatusUnauthorized).
						SetCode(codes.Unauthorized).
						SetError(err).
						Fail(i18n.MsgKeyAdminLoginIPChanged)
				case errors.Is(err, securitylogic.ErrAdminIPNotAllowed):
					helper.NewJsonResp(ctx, w).
						SetHttpStatus(http.StatusUnauthorized).
						SetCode(codes.Unauthorized).
						SetError(err).
						Fail(i18n.MsgKeyAdminIPNotAllowed)
				case errors.Is(err, securitylogic.ErrAdminMFARequired):
					helper.NewJsonResp(ctx, w).
						SetHttpStatus(http.StatusOK).
						SetCode(codes.CheckMFACode).
						SetError(err).
						Fail(i18n.MsgKeyCheckMFA)
				default:
					helper.NewJsonResp(ctx, w).
						SetHttpStatus(http.StatusUnauthorized).
						SetCode(codes.Unauthorized).
						Fail(i18n.MsgKeyTokenInvalid)
				}
				return
			}

			requestctx.SetAccessToken(ctx, identity.Token)
			requestctx.SetUser(ctx, identity.UserID, identity.UserName, utils.ClientIP(r))
			r = r.WithContext(loggerx.BindContext(ctx))
			next(w, r)
		}
	}
}

// docsRouteAliasForPath 按文档站请求路径返回目录级权限别名；未命中具体目录时使用入口权限。
func docsRouteAliasForPath(requestPath string) routealias.Alias {
	docsPath := normalizedDocsPath(requestPath)
	if docsPath == "" {
		return routealias.DocsIndex
	}
	for _, rule := range docsPermissionRules {
		if docsPathMatchesRule(docsPath, rule.Prefix) {
			return rule.Alias
		}
	}
	return routealias.DocsIndex
}

// normalizedDocsPath 返回 /api/docs 下的相对路径，空值表示文档首页或站点公共资源。
func normalizedDocsPath(requestPath string) string {
	if text, err := url.PathUnescape(strings.TrimSpace(requestPath)); err == nil {
		requestPath = text
	}
	cleanPath := pathpkg.Clean("/" + strings.TrimLeft(strings.TrimSpace(requestPath), "/"))
	if cleanPath == "/api/docs" || cleanPath == "/api/docs/" {
		return ""
	}
	if !strings.HasPrefix(cleanPath, "/api/docs/") {
		return ""
	}
	return strings.TrimPrefix(cleanPath, "/api/docs/")
}

// docsPathMatchesRule 判断文档相对路径是否命中目录规则，目录本身和目录下文件都算命中。
func docsPathMatchesRule(docsPath string, prefix string) bool {
	docsPath = strings.Trim(strings.TrimSpace(docsPath), "/")
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	return docsPath == prefix || strings.HasPrefix(docsPath, prefix+"/")
}

// docsPermissionRule 描述文档目录前缀与权限别名的关系。
type docsPermissionRule struct {
	Prefix string           // /api/docs 下的相对目录前缀，使用文档站真实目录名
	Alias  routealias.Alias // 命中目录后用于后台权限表校验的稳定别名
}

// docsPermissionRules 维护会随文档目录变动调整的访问规则；更具体的二级目录放在前面。
var docsPermissionRules = []docsPermissionRule{
	{Prefix: "角色文档/运维", Alias: routealias.DocsRoleOps},
	{Prefix: "角色文档/后端开发", Alias: routealias.DocsRoleBackend},
	{Prefix: "角色文档/前端与测试", Alias: routealias.DocsRoleFrontend},
	{Prefix: "功能模块/任务系统", Alias: routealias.DocsFeatureTask},
	{Prefix: "功能模块/用户标签", Alias: routealias.DocsFeatureUserTag},
	{Prefix: "api/接口文档/前台系统", Alias: routealias.DocsAPIServiceFront},
	{Prefix: "api/接口文档", Alias: routealias.DocsAPIServiceIndex},
	{Prefix: "api", Alias: routealias.DocsAPIServiceIndex},
	{Prefix: "接口文档/后台系统", Alias: routealias.DocsAPIAdmin},
	{Prefix: "接口文档/任务系统", Alias: routealias.DocsAPITask},
	{Prefix: "接口文档/用户标签", Alias: routealias.DocsUserTag},
	{Prefix: "接口文档", Alias: routealias.DocsAPIIndex},
}
