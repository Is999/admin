package logic

import (
	"context"
	"fmt"
	"strings"
	"time"

	"admin_cron/helper"

	utils "github.com/Is999/go-utils"
	"github.com/Is999/go-utils/errors"

	keys "admin_cron/common/rediskeys"
	"admin_cron/internal/model"
	"admin_cron/internal/svc"

	tablecache "github.com/Is999/table-cache"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	// ConfigAdminIPWhitelistDisable 表示是否禁用后台 IP 白名单，true 时跳过白名单校验。
	ConfigAdminIPWhitelistDisable = "adminIpWhitelistDisable"
	// ConfigAdminIPWhitelist 表示后台 IP 白名单配置。
	ConfigAdminIPWhitelist = "adminIpWhitelist"
	// ConfigAdminCheckChangeIP 表示是否校验后台登录 IP 变更。
	ConfigAdminCheckChangeIP = "adminCheckChangeIp"
	// ConfigAdminMFACheckEnable 表示是否强制启用后台 MFA 校验。
	ConfigAdminMFACheckEnable = "adminMFACheckEnable"
	// ConfigAdminMFACheckFrequency 表示后台 MFA 校验频率，单位秒。
	ConfigAdminMFACheckFrequency = "adminMFACheckFrequency"
	// ConfigAdminDisableMFACheckScenario 表示后台禁用 MFA 校验的场景配置。
	ConfigAdminDisableMFACheckScenario = "adminDisableMFACheckScenario"
)

const (
	// adminSuperRoleID 表示超级管理员角色 ID，保持与 laravel-admin Role::getSuperRole 一致。
	adminSuperRoleID = 1
	// routePermissionBypassAlias 必须与 middleware.Ignore 保持一致，用于已登录但不绑定业务权限码的通用接口。
	routePermissionBypassAlias = "ignore"
	// securityOptionalConfigTimeout 表示安全链路读取“可回退”的系统配置时使用的独立短超时。
	// 登录、鉴权等主链路不应因为这类兜底配置读取抖动而整体超时。
	securityOptionalConfigTimeout = 500 * time.Millisecond
)

var (
	// ErrAdminNotFound 表示管理员不存在。
	ErrAdminNotFound = errors.New("管理员不存在")
	// ErrAdminDisabled 表示管理员账号已被禁用。
	ErrAdminDisabled = errors.New("管理员账号已被禁用")
	// ErrAdminIPChanged 表示当前请求 IP 与 token 登录 IP 不一致。
	ErrAdminIPChanged = errors.New("管理员登录IP已变更")
	// ErrAdminIPNotAllowed 表示当前请求 IP 不在后台白名单。
	ErrAdminIPNotAllowed = errors.New("当前IP不在后台白名单")
	// ErrAdminPermissionDenied 表示当前管理员没有路由访问权限。
	ErrAdminPermissionDenied = errors.New("管理员权限不足")
	// ErrAdminPasswordResetRequired 表示当前管理员必须先修改登录密码。
	ErrAdminPasswordResetRequired = errors.New("需要先修改登录密码")
	// ErrAdminMFARequired 表示当前管理员需要先完成 MFA 登录校验。
	ErrAdminMFARequired = errors.New("需要完成MFA验证")
	// ErrAdminMFABindRequired 表示当前管理员必须先完成 MFA 绑定与启用。
	ErrAdminMFABindRequired = errors.New("需要先绑定并启用MFA")
	// ErrAdminMFACodeInvalid 表示当前提交的 MFA 动态验证码不正确。
	ErrAdminMFACodeInvalid = errors.New("MFA动态验证码错误")
	// ErrAdminMFATwoStepExpired 表示当前二次校验票据已过期或无效。
	ErrAdminMFATwoStepExpired = errors.New("MFA校验已过期")
)

// SecurityLogic 承载鉴权链路中的账号状态、IP 白名单、MFA 与权限校验逻辑。
type SecurityLogic struct {
	*BaseLogic // 复用上下文、数据库、Redis 和日志能力
}

// NewSecurityLogic 创建后台安全校验逻辑对象。
func NewSecurityLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SecurityLogic {
	return &SecurityLogic{
		BaseLogic: NewBaseLogicWithContext(ctx, svcCtx),
	}
}

// CheckAdminAccess 按 laravel-admin AdminAuth 的顺序校验管理员状态、IP、MFA 与业务权限。
func (l *SecurityLogic) CheckAdminAccess(userID int, routeAlias string, currentIP string, loginIP string) error {
	routeAlias = strings.TrimSpace(routeAlias)
	if routeAlias == "" {
		return errors.Tag(ErrAdminPermissionDenied)
	}
	admin, err := l.getAdminForAccess(userID)
	if err != nil {
		return errors.Tag(err)
	}
	if admin.Status != 1 {
		return ErrAdminDisabled
	}
	if err := l.checkAdminIP(currentIP, loginIP); err != nil {
		return errors.Tag(err)
	}
	if err := l.checkAdminNeedResetPassword(admin, routeAlias); err != nil {
		return errors.Tag(err)
	}
	if !shouldBypassLoginMFACheck(admin, routeAlias) {
		if err := l.checkAdminMFA(admin); err != nil {
			return errors.Tag(err)
		}
	}
	allowed, err := l.CheckRoutePermission(userID, routeAlias)
	if err != nil {
		return errors.Tag(err)
	}
	if !allowed {
		return ErrAdminPermissionDenied
	}
	return nil
}

// CheckRoutePermission 根据路由别名校验管理员是否拥有对应权限。
func (l *SecurityLogic) CheckRoutePermission(userID int, routeAlias string) (bool, error) {
	if strings.TrimSpace(routeAlias) == routePermissionBypassAlias {
		// 显式挂 middleware.Ignore 的路由只跳过业务权限表；JWT、账号状态、IP 与 MFA 仍在 CheckAdminAccess 前置校验。
		return true, nil
	}
	if permissionAllowlist[routeAlias] {
		return true, nil
	}

	roleIDs, err := l.enabledRoleIDs(userID)
	if err != nil {
		return false, errors.Tag(err)
	}
	if len(roleIDs) == 0 {
		return false, nil
	}
	for _, roleID := range roleIDs {
		if roleID == adminSuperRoleID {
			return true, nil
		}
	}

	permissionIDs, err := l.routePermissionIDs(routeAlias)
	if err != nil {
		return false, errors.Tag(err)
	}
	if len(permissionIDs) == 0 {
		// 未配置到权限表的接口默认拒绝，避免新增敏感接口因漏初始化权限而越权访问。
		return false, nil
	}
	userPermissionIDs, err := l.userPermissionIDsWithCache(userID)
	if err != nil {
		return false, errors.Tag(err)
	}
	permissionSet := make(map[int]struct{}, len(userPermissionIDs))
	for _, permissionID := range userPermissionIDs {
		permissionSet[permissionID] = struct{}{}
	}
	for _, permissionID := range permissionIDs {
		if _, ok := permissionSet[permissionID]; ok {
			return true, nil
		}
	}
	return false, nil
}

// SecurityConfigBool 读取布尔型系统配置，读取失败时使用调用方给出的默认值。
func SecurityConfigBool(ctx context.Context, svcCtx *svc.ServiceContext, uuid string, defaultValue bool) (result bool) {
	result = defaultValue
	defer func() {
		if recover() != nil {
			result = defaultValue
		}
	}()
	configCtx, cancel := optionalSecurityConfigContext(ctx)
	defer cancel()
	value, err := (&SysConfigLogic{BaseLogic: NewBaseLogicWithContext(configCtx, svcCtx)}).GetCachedValue(uuid)
	if err != nil {
		return result
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "1" || strings.EqualFold(v, "true")
	case int:
		return v == 1
	case float64:
		return int(v) == 1
	default:
		return result
	}
}

// SecurityConfigStringSlice 读取字符串数组配置，读取失败时使用调用方给出的默认值。
func SecurityConfigStringSlice(ctx context.Context, svcCtx *svc.ServiceContext, uuid string, defaultValue []string) (result []string) {
	result = defaultValue
	defer func() {
		if recover() != nil {
			result = defaultValue
		}
	}()
	configCtx, cancel := optionalSecurityConfigContext(ctx)
	defer cancel()
	value, err := (&SysConfigLogic{BaseLogic: NewBaseLogicWithContext(configCtx, svcCtx)}).GetCachedValue(uuid)
	if err != nil {
		return result
	}
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				result = append(result, text)
			}
		}
		return result
	case map[string]any:
		result := make([]string, 0, len(v))
		for key := range v {
			if strings.TrimSpace(key) != "" {
				result = append(result, strings.TrimSpace(key))
			}
		}
		return result
	case string:
		if strings.TrimSpace(v) == "" {
			return []string{}
		}
		parts := strings.Split(v, ",")
		result := make([]string, 0, len(parts))
		for _, item := range parts {
			text := strings.TrimSpace(item)
			if text != "" {
				result = append(result, text)
			}
		}
		return result
	default:
		return result
	}
}

// optionalSecurityConfigContext 为安全链路中的“可失败可回退”配置读取创建独立短超时上下文。
// 这里故意不复用请求上下文的 deadline，避免登录成功后仅因配置缓存慢读而整条请求超时。
func optionalSecurityConfigContext(context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), securityOptionalConfigTimeout)
}

// getAdminForAccess 查询管理员基础鉴权信息。
func (l *SecurityLogic) getAdminForAccess(userID int) (*model.Admin, error) {
	var admin model.Admin
	if err := l.svc.WriteDB(svc.DatabaseMain).
		Select("id", "name", "status", "mfa_status", "need_reset_password", "last_login_time", "mfa_secure_key").
		Where("id = ?", userID).
		First(&admin).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAdminNotFound
		}
		return nil, errors.Tag(err)
	}
	return &admin, nil
}

// checkAdminIP 校验 token 登录 IP、当前请求 IP 与白名单配置。
func (l *SecurityLogic) checkAdminIP(currentIP string, loginIP string) error {
	whitelistDisabled := SecurityConfigBool(l.Context(), l.svc, ConfigAdminIPWhitelistDisable, true)
	if whitelistDisabled {
		return nil
	}
	currentIP = strings.TrimSpace(currentIP)
	loginIP = strings.TrimSpace(loginIP)
	ipChanged := loginIP != "" && currentIP != "" && loginIP != currentIP
	if ipChanged && SecurityConfigBool(l.Context(), l.svc, ConfigAdminCheckChangeIP, true) {
		return ErrAdminIPChanged
	}
	if ipChanged {
		whitelist := SecurityConfigStringSlice(l.Context(), l.svc, ConfigAdminIPWhitelist, nil)
		if len(whitelist) > 0 && !utils.IsHas(currentIP, helper.UniqueNonEmptyStrings(whitelist)) {
			return ErrAdminIPNotAllowed
		}
	}
	return nil
}

// ForceLoginMFAEnabled 判断系统是否开启了登录阶段强制 MFA。
func (l *SecurityLogic) ForceLoginMFAEnabled() bool {
	return SecurityConfigBool(l.Context(), l.svc, ConfigAdminMFACheckEnable, false)
}

// NeedLoginMFA 判断当前管理员登录态是否必须完成 MFA 校验。
func (l *SecurityLogic) NeedLoginMFA(admin *model.Admin) bool {
	if admin == nil {
		return false
	}
	// 首次登录或重置密码后的临时密码阶段，优先放行改密流程，MFA 允许后续再补。
	if admin.NeedResetPassword == 1 {
		return false
	}
	if admin.MfaStatus == 1 {
		return true
	}
	return l.ForceLoginMFAEnabled()
}

// NeedBindMFAOnLogin 判断当前管理员在登录阶段是否需要先完成首次绑定并启用 MFA。
// 账号自身已经是启用态时，不提供自助换绑；设备异常由后台受控处理。
func (l *SecurityLogic) NeedBindMFAOnLogin(admin *model.Admin) bool {
	if admin == nil || admin.NeedResetPassword == 1 {
		return false
	}
	return admin.MfaStatus != 1
}

// checkAdminMFA 校验登录阶段是否已经完成 MFA；系统强制启用时，未开启账号也必须先完成绑定与校验。
func (l *SecurityLogic) checkAdminMFA(admin *model.Admin) error {
	if admin == nil {
		return ErrAdminNotFound
	}
	needMFA := l.NeedLoginMFA(admin)
	if !needMFA || l.Redis() == nil {
		return nil
	}
	if l.ForceLoginMFAEnabled() && admin.MfaStatus != 1 {
		return ErrAdminMFABindRequired
	}
	flag, err := l.Redis().Get(l.Context(), fmt.Sprintf(keys.LoginCheckMFAFlag, admin.ID)).Int64()
	if errors.Is(err, redis.Nil) {
		return ErrAdminMFARequired
	}
	if err != nil {
		return errors.Tag(err)
	}
	if !loginMFAFlagMatches(flag, admin.LastLoginTime) {
		return ErrAdminMFARequired
	}
	return nil
}

// enabledRoleIDs 查询用户拥有的启用角色 ID。
func (l *SecurityLogic) enabledRoleIDs(userID int) ([]int, error) {
	return (&AdminRoleLogic{BaseLogic: l.BaseLogic}).enabledRoleIDsByUserWithCache(userID)
}

// routePermissionIDs 查询路由别名对应的启用权限 ID。
func (l *SecurityLogic) routePermissionIDs(routeAlias string) ([]int, error) {
	if l.Redis() == nil {
		var permissionIDs []int
		err := l.svc.WriteDB(svc.DatabaseMain).Model(&model.AdminPermission{}).
			Where("status = 1 AND module = ?", routeAlias).
			Pluck("id", &permissionIDs).Error
		return permissionIDs, errors.Tag(err)
	}
	manager, err := tableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	cacheKey := fmt.Sprintf(keys.RoutePermissionIDs, routeAlias)
	trackRoutePermissionAliasCache(l.BaseLogic, routeAlias)
	var values []string
	result, err := manager.LoadThrough(l.Context(), tableCachePhysicalKey(l.BaseLogic, cacheKey), &values, nil)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if result.State == tablecache.LookupStateEmpty {
		return []int{}, nil
	}
	if len(values) == 0 {
		return []int{}, nil
	}
	return parsePositiveIntStrings(values, "路由候选权限缓存")
}

// userPermissionIDsWithCache 查询管理员聚合权限 ID 集合，供鉴权链路优先走缓存。
func (l *SecurityLogic) userPermissionIDsWithCache(userID int) ([]int, error) {
	if userID <= 0 {
		return []int{}, nil
	}
	if l.Redis() == nil {
		roleIDs, err := l.enabledRoleIDs(userID)
		if err != nil {
			return nil, errors.Tag(err)
		}
		if len(roleIDs) == 0 {
			return []int{}, nil
		}
		var permissionIDs []int
		err = l.svc.WriteDB(svc.DatabaseMain).Model(&model.AdminRolePermissionRel{}).
			Distinct("permission_id").
			Where("role_id IN ?", roleIDs).
			Pluck("permission_id", &permissionIDs).Error
		return permissionIDs, errors.Tag(err)
	}
	manager, err := tableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	cacheKey := fmt.Sprintf(keys.AdminPermissionIDs, userID)
	var values []string
	result, err := manager.LoadThrough(l.Context(), tableCachePhysicalKey(l.BaseLogic, cacheKey), &values, nil)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if result.State == tablecache.LookupStateEmpty {
		return []int{}, nil
	}
	return parsePositiveIntStrings(values, "管理员聚合权限缓存")
}

// userPermissionUUIDsWithCache 查询管理员最终权限码集合，供高频权限初始化链路优先走缓存。
func (l *SecurityLogic) userPermissionUUIDsWithCache(userID int) ([]string, error) {
	if userID <= 0 {
		return []string{}, nil
	}
	if l.Redis() == nil {
		permissionIDs, err := l.userPermissionIDsWithCache(userID)
		if err != nil {
			return nil, errors.Tag(err)
		}
		return (&AdminPermissionLogic{BaseLogic: l.BaseLogic}).permissionUUIDsByIDsWithCache(permissionIDs)
	}
	manager, err := tableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	cacheKey := fmt.Sprintf(keys.AdminPermissionUUIDs, userID)
	values := make([]string, 0)
	result, err := manager.LoadThrough(l.Context(), tableCachePhysicalKey(l.BaseLogic, cacheKey), &values, nil)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if result.State == tablecache.LookupStateEmpty {
		return []string{}, nil
	}
	return helper.UniqueNonEmptyStrings(values), nil
}

// permissionAllowlist 显式列出只依赖登录态和账号状态的个人/会话接口。
// 这些接口不走权限表，但仍会经过 token、账号状态、IP 与 MFA 校验，不属于未配置权限的默认放行。
var permissionAllowlist = map[string]bool{
	"auth.refresh":           true,
	"auth.logout":            true,
	"auth.codes":             true,
	"auth.login_after_info":  true,
	"role.tree.options":      true,
	"user.mine":              true,
	"user.permissions":       true,
	"user.check_secure":      true,
	"user.check_mfa_secure":  true,
	"user.update_password":   true,
	"user.update_mine":       true,
	"user.update_mfa_status": true,
	"user.update_mfa_secret": true,
	// 个人中心刷新 MFA 绑定秘钥属于当前登录账号的自助安全操作，
	// 只依赖登录态、账号状态和后续 MFA 二次校验，不额外绑定后台业务权限码。
	"user.refresh_mfa_secret": true,
	"user.update_avatar":      true,
	// 权限 UUID 预览只生成候选值，不写权限表；新增/编辑权限仍由权限保存接口做权限控制。
	"permission.max_uuid": true,
	// 消息中心属于个人收件箱能力，仅依赖登录态与账号安全校验，不绑定后台权限码。
	"message.list":          true,
	"message.sent_list":     true,
	"message.receivers":     true,
	"message.unread_count":  true,
	"message.notifications": true,
	"message.mark_read":     true,
	"message.delete":        true,
	"message.send":          true,
	"message.handle":        true,
}

// passwordResetAllowlist 显式列出“必须先修改密码”状态下仍允许访问的自助接口。
var passwordResetAllowlist = map[string]bool{
	"auth.refresh":            true,
	"auth.logout":             true,
	"auth.codes":              true,
	"auth.login_after_info":   true,
	"user.mine":               true,
	"user.permissions":        true,
	"user.check_secure":       true,
	"user.check_mfa_secure":   true,
	"user.update_password":    true,
	"user.update_mine":        true,
	"user.update_mfa_status":  true,
	"user.update_mfa_secret":  true,
	"user.refresh_mfa_secret": true,
	"user.update_avatar":      true,
	"message.notifications":   true,
}

// loginMFAAllowlist 显式列出“正在完成登录 MFA 校验”时允许访问的会话接口。
// 这里必须至少放行 MFA 动态码校验接口本身，避免“校验 MFA 的接口自己又被要求先完成 MFA”形成递归拦截。
var loginMFAAllowlist = map[string]bool{
	"auth.refresh":            true,
	"auth.logout":             true,
	"auth.codes":              true,
	"user.check_mfa_secure":   true,
	"user.refresh_mfa_secret": true,
	"user.update_mfa_status":  true,
	"user.mine":               true,
	"message.notifications":   true,
}

// checkAdminNeedResetPassword 校验管理员是否处于必须先修改登录密码状态。
func (l *SecurityLogic) checkAdminNeedResetPassword(admin *model.Admin, routeAlias string) error {
	if admin == nil || admin.NeedResetPassword != 1 {
		return nil
	}
	if passwordResetAllowlist[strings.TrimSpace(routeAlias)] {
		return nil
	}
	return ErrAdminPasswordResetRequired
}

// shouldSkipMFAForPasswordReset 判断必须改密阶段是否允许当前路由先跳过登录 MFA 校验。
func shouldSkipMFAForPasswordReset(admin *model.Admin, routeAlias string) bool {
	if admin == nil || admin.NeedResetPassword != 1 {
		return false
	}
	return passwordResetAllowlist[strings.TrimSpace(routeAlias)]
}

// shouldBypassLoginMFACheck 判断当前路由是否允许跳过“登录态尚未完成 MFA”的前置拦截。
// 该判断只用于让 MFA 自助完成链路先跑通，不影响普通业务接口仍受登录 MFA 约束。
func shouldBypassLoginMFACheck(admin *model.Admin, routeAlias string) bool {
	routeAlias = strings.TrimSpace(routeAlias)
	if shouldSkipMFAForPasswordReset(admin, routeAlias) {
		return true
	}
	return loginMFAAllowlist[routeAlias]
}

// loginCheckMFAFlagTTL 返回 MFA 登录校验标记的默认过期时间，后续 MFA 接口可复用。
func loginCheckMFAFlagTTL() time.Duration {
	return 4 * time.Hour
}
