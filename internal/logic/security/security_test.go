package security

import (
	corelogic "admin/internal/logic"
	cachelogic "admin/internal/logic/cache"
	rbaclogic "admin/internal/logic/rbac"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"admin/common/codes"
	keys "admin/common/rediskeys"
	"admin/internal/config"
	"admin/internal/model"
	"admin/internal/routealias"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestPermissionSQLContainsAllFrontendCodes 验证初始化 SQL 已覆盖前端当前维护的全部权限码。
func TestPermissionSQLContainsAllFrontendCodes(t *testing.T) {
	sqlUUIDSet, _, err := loadPermissionSQLSnapshot()
	if err != nil {
		t.Fatalf("loadPermissionSQLSnapshot() error = %v", err)
	}
	frontendFile := frontendPermissionCodesFilePath()
	if _, statErr := os.Stat(frontendFile); statErr != nil {
		if os.IsNotExist(statErr) {
			t.Skipf("前端权限码文件不存在，跳过联动校验: %s", frontendFile)
		}
		t.Fatalf("Stat(permission-codes.ts) error = %v", statErr)
	}
	frontendCodes, err := loadFrontendPermissionCodes()
	if err != nil {
		t.Fatalf("loadFrontendPermissionCodes() error = %v", err)
	}
	var missing []string
	for _, code := range frontendCodes {
		if _, ok := sqlUUIDSet[code]; !ok {
			missing = append(missing, code)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("bootstrap.sql missing frontend permission codes: %v", missing)
	}
}

// TestFrontendPermissionCodesAreUnique 验证前端当前维护的权限码不存在“一码多义”重复配置。
func TestFrontendPermissionCodesAreUnique(t *testing.T) {
	frontendFile := frontendPermissionCodesFilePath()
	if _, statErr := os.Stat(frontendFile); statErr != nil {
		if os.IsNotExist(statErr) {
			t.Skipf("前端权限码文件不存在，跳过联动校验: %s", frontendFile)
		}
		t.Fatalf("Stat(permission-codes.ts) error = %v", statErr)
	}
	content, err := os.ReadFile(frontendFile)
	if err != nil {
		t.Fatalf("ReadFile(permission-codes.ts) error = %v", err)
	}
	codePattern := regexp.MustCompile("'(\\d{6})'")
	matches := codePattern.FindAllStringSubmatch(string(content), -1)
	counts := make(map[string]int, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		counts[match[1]]++
	}
	var duplicates []string
	for code, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, code)
		}
	}
	sort.Strings(duplicates)
	if len(duplicates) > 0 {
		t.Fatalf("frontend permission codes duplicated: %v", duplicates)
	}
}

// TestPermissionSQLContainsRequiredCurrentModules 验证初始化 SQL 已包含当前项目核心路由别名对应的模块权限。
func TestPermissionSQLContainsRequiredCurrentModules(t *testing.T) {
	_, sqlModuleSet, err := loadPermissionSQLSnapshot()
	if err != nil {
		t.Fatalf("loadPermissionSQLSnapshot() error = %v", err)
	}
	requiredModules := []string{
		"admin.list",
		string(routealias.AdminAdd),
		"admin.info",
		string(routealias.AdminUpdate),
		string(routealias.AdminDelete),
		"admin.export",
		string(routealias.AdminPasswordReset),
		string(routealias.AdminResetInitialState),
		string(routealias.AdminMFAStatus),
		string(routealias.AdminBuildMFAURL),
		"role.list",
		"role.tree.list",
		"role.permission.tree",
		"permission.list",
		"permission.tree.list",
		"system.config.list",
		"cache.list",
		"admin.log.query",
		"secretKey.index",
		string(routealias.SecretKeyGet),
		"task.console.index",
		"task.workflow.status.index",
		"task.config.reload.index",
		"task.config.reload.items",
		"task.queue.list",
		"task.items.list",
		"user_tag.index",
		string(routealias.DocsIndex),
		"security.debug.index",
	}
	var missing []string
	for _, module := range requiredModules {
		if _, ok := sqlModuleSet[module]; !ok {
			missing = append(missing, module)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("bootstrap.sql missing required modules: %v", missing)
	}
}

// TestMFAResultByErrorRecognizesWrappedErrors 验证 MFA 业务错误经过 errors.Tag 包装后仍能映射为前端可识别的业务码。
func TestMFAResultByErrorRecognizesWrappedErrors(t *testing.T) {
	// cases 表示不同 MFA 错误转换入口对已包装错误的期望业务码。
	cases := []struct {
		name string
		got  *types.BizResult
		want int
	}{
		{
			name: "后台敏感操作MFA二次票据过期",
			got:  OperateMFABizResult(errors.Tag(ErrAdminMFATwoStepExpired), "test"),
			want: codes.CheckMFAAgain,
		},
	}
	for _, item := range cases {
		if item.got == nil {
			t.Fatalf("%s got nil result", item.name)
		}
		if item.got.Code != item.want {
			t.Fatalf("%s code = %d, want %d", item.name, item.got.Code, item.want)
		}
	}
}

// TestPermissionAllowlistContainsSelfServiceRoutes 验证个人中心与会话接口不要求额外业务权限码。
func TestPermissionAllowlistContainsSelfServiceRoutes(t *testing.T) {
	// routeAliases 表示只依赖登录态与账号安全状态的个人中心/会话接口集合。
	routeAliases := []routealias.Alias{
		routealias.AuthRefresh,
		routealias.AuthLogout,
		routealias.AuthCodes,
		routealias.AuthProfile,
		routealias.ProfileMine,
		routealias.ProfilePermissions,
		routealias.ProfileCheckSecure,
		routealias.ProfileCheckMFA,
		routealias.ProfileUpdatePassword,
		routealias.ProfileUpdateMine,
		routealias.ProfileUpdateMFAStatus,
		routealias.ProfileUpdateMFASecret,
		routealias.ProfileRefreshMFASecret,
		routealias.ProfileUpdateAvatar,
	}
	for _, routeAlias := range routeAliases {
		if !permissionAllowlist[routeAlias] {
			t.Fatalf("permissionAllowlist missing %s", routeAlias)
		}
	}
}

// TestPermissionAllowlistContainsSessionVerifyRoutes 验证锁屏解锁校验接口不要求额外业务权限码。
func TestPermissionAllowlistContainsSessionVerifyRoutes(t *testing.T) {
	// routeAliases 表示只依赖登录态与账号状态的会话校验路由别名集合。
	routeAliases := []routealias.Alias{
		routealias.ProfileCheckSecure,
		routealias.ProfileCheckMFA,
	}
	for _, routeAlias := range routeAliases {
		if !permissionAllowlist[routeAlias] {
			t.Fatalf("permissionAllowlist missing %s", routeAlias)
		}
	}
}

// TestCheckRoutePermissionAllowsSelfServiceWithoutPermissionStore 验证个人中心接口不依赖角色/权限缓存即可通过权限层。
func TestCheckRoutePermissionAllowsSelfServiceWithoutPermissionStore(t *testing.T) {
	logicObj := NewSecurityLogic(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{}))
	// routeAliases 表示不需要查询权限表的自助接口集合。
	routeAliases := []routealias.Alias{
		routealias.ProfileMine,
		routealias.ProfileUpdatePassword,
		routealias.ProfileUpdateMine,
		routealias.ProfileCheckSecure,
		routealias.ProfileCheckMFA,
		routealias.ProfileUpdateMFAStatus,
		routealias.ProfileRefreshMFASecret,
		routealias.ProfileUpdateAvatar,
	}
	for _, routeAlias := range routeAliases {
		allowed, err := logicObj.CheckRoutePermission(999, string(routeAlias))
		if err != nil {
			t.Fatalf("CheckRoutePermission(%s) error = %v", routeAlias, err)
		}
		if !allowed {
			t.Fatalf("CheckRoutePermission(%s) allowed = false, want true", routeAlias)
		}
	}
}

// TestCheckRoutePermissionAllowsMiddlewareIgnoreWithoutPermissionStore 验证通用上传等 Ignore 路由只校验登录态，不查询业务权限表。
func TestCheckRoutePermissionAllowsMiddlewareIgnoreWithoutPermissionStore(t *testing.T) {
	logicObj := NewSecurityLogic(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{}))
	allowed, err := logicObj.CheckRoutePermission(999, string(routePermissionBypassAlias))
	if err != nil {
		t.Fatalf("CheckRoutePermission(%s) error = %v", routePermissionBypassAlias, err)
	}
	if !allowed {
		t.Fatalf("CheckRoutePermission(%s) allowed = false, want true", routePermissionBypassAlias)
	}
}

// TestPermissionAllowlistContainsRoleTreeOptions 验证角色树下拉接口只要求登录态与账号状态，不额外绑定角色管理权限。
func TestPermissionAllowlistContainsRoleTreeOptions(t *testing.T) {
	if !permissionAllowlist[routealias.RoleTreeOptions] {
		t.Fatalf("permissionAllowlist missing role.tree.options")
	}
}

// TestPermissionAllowlistContainsPermissionMaxUUID 验证权限 UUID 预览接口只要求登录态，不额外绑定权限管理权限。
func TestPermissionAllowlistContainsPermissionMaxUUID(t *testing.T) {
	if !permissionAllowlist[routealias.PermissionMaxUUID] {
		t.Fatalf("permissionAllowlist missing permission.max_uuid")
	}
}

// TestPermissionAllowlistContainsPersonalMessageRoutes 验证消息中心属于个人收件箱能力，只依赖登录态和账号安全状态。
func TestPermissionAllowlistContainsPersonalMessageRoutes(t *testing.T) {
	// routeAliases 表示个人消息收发、已读和处理接口集合；这些接口不按后台角色权限码二次授权。
	routeAliases := []routealias.Alias{
		routealias.AdminMessageList,
		routealias.AdminMessageSentList,
		routealias.AdminMessageReceivers,
		routealias.AdminMessageUnreadCount,
		routealias.AdminMessageNotifications,
		routealias.AdminMessageMarkRead,
		routealias.AdminMessageDelete,
		routealias.AdminMessageSend,
		routealias.AdminMessageHandle,
	}
	for _, routeAlias := range routeAliases {
		if !permissionAllowlist[routeAlias] {
			t.Fatalf("permissionAllowlist missing personal message route %s", routeAlias)
		}
	}
}

// TestPasswordResetAllowlistContainsForcedResetFlow 验证首次/强制改密阶段不会拦截个人中心必要接口。
func TestPasswordResetAllowlistContainsForcedResetFlow(t *testing.T) {
	// routeAliases 表示必须改密状态下仍可访问的自助闭环接口集合。
	routeAliases := []routealias.Alias{
		routealias.AuthRefresh,
		routealias.AuthLogout,
		routealias.AuthCodes,
		routealias.AuthProfile,
		routealias.ProfileMine,
		routealias.ProfilePermissions,
		routealias.ProfileCheckSecure,
		routealias.ProfileCheckMFA,
		routealias.ProfileUpdatePassword,
		routealias.ProfileUpdateMine,
		routealias.ProfileUpdateMFAStatus,
		routealias.ProfileUpdateMFASecret,
		routealias.ProfileRefreshMFASecret,
		routealias.ProfileUpdateAvatar,
	}
	for _, routeAlias := range routeAliases {
		if !passwordResetAllowlist[routeAlias] {
			t.Fatalf("passwordResetAllowlist missing %s", routeAlias)
		}
	}
}

// TestLoginMFAAllowlistContainsBindFlow 验证登录 MFA 未完成时，绑定/校验 MFA 的闭环接口不会被自己递归拦截。
func TestLoginMFAAllowlistContainsBindFlow(t *testing.T) {
	// routeAliases 表示登录 MFA 前置拦截期间允许访问的最小接口集合。
	routeAliases := []routealias.Alias{
		routealias.AuthRefresh,
		routealias.AuthLogout,
		routealias.AuthCodes,
		routealias.ProfileCheckMFA,
		routealias.ProfileRefreshMFASecret,
		routealias.ProfileUpdateMFAStatus,
		routealias.ProfileMine,
		routealias.AdminMessageNotifications,
	}
	for _, routeAlias := range routeAliases {
		if !loginMFAAllowlist[routeAlias] {
			t.Fatalf("loginMFAAllowlist missing %s", routeAlias)
		}
	}
}

// TestAdminSensitiveRoutesRemainPermissionProtected 验证后台代操作敏感接口仍必须走权限表，不被个人中心白名单误放行。
func TestAdminSensitiveRoutesRemainPermissionProtected(t *testing.T) {
	// routeAliases 表示管理员管理与后台代操作类敏感接口集合。
	routeAliases := []routealias.Alias{
		routealias.AdminAdd,
		routealias.AdminUpdate,
		routealias.AdminDelete,
		routealias.AdminStatusUpdate,
		routealias.AdminPasswordReset,
		routealias.AdminResetInitialState,
		routealias.AdminMFAStatus,
		routealias.AdminBuildMFAURL,
	}
	for _, routeAlias := range routeAliases {
		if permissionAllowlist[routeAlias] {
			t.Fatalf("permissionAllowlist should not contain sensitive route %s", routeAlias)
		}
		if passwordResetAllowlist[routeAlias] {
			t.Fatalf("passwordResetAllowlist should not contain sensitive route %s", routeAlias)
		}
		if loginMFAAllowlist[routeAlias] {
			t.Fatalf("loginMFAAllowlist should not contain sensitive route %s", routeAlias)
		}
	}
}

// TestCheckAdminNeedResetPassword 验证必须改密状态会拦截非白名单接口，但放行个人中心改密接口。
func TestCheckAdminNeedResetPassword(t *testing.T) {
	logicObj := NewSecurityLogic(context.Background(), svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{}))
	admin := &model.Admin{ID: 8, Name: "admin999", NeedResetPassword: 1}
	if err := logicObj.checkAdminNeedResetPassword(admin, "admin.list"); err != ErrAdminPasswordResetRequired {
		t.Fatalf("checkAdminNeedResetPassword(admin.list) = %v, want %v", err, ErrAdminPasswordResetRequired)
	}
	if err := logicObj.checkAdminNeedResetPassword(admin, string(routealias.ProfileUpdatePassword)); err != nil {
		t.Fatalf("checkAdminNeedResetPassword(profile.update_password) = %v, want nil", err)
	}
	if err := logicObj.checkAdminNeedResetPassword(admin, string(routealias.ProfileUpdateMFAStatus)); err != nil {
		t.Fatalf("checkAdminNeedResetPassword(profile.update_mfa_status) = %v, want nil", err)
	}
	if err := logicObj.checkAdminNeedResetPassword(admin, string(routealias.AdminPasswordReset)); err != ErrAdminPasswordResetRequired {
		t.Fatalf("checkAdminNeedResetPassword(admin.password.reset) = %v, want %v", err, ErrAdminPasswordResetRequired)
	}
}

// TestShouldSkipMFAForPasswordReset 验证必须改密阶段允许白名单路由先跳过登录 MFA 校验。
func TestShouldSkipMFAForPasswordReset(t *testing.T) {
	admin := &model.Admin{ID: 8, Name: "admin999", NeedResetPassword: 1}
	for _, routeAlias := range []routealias.Alias{routealias.ProfileUpdatePassword, routealias.ProfileCheckMFA, routealias.ProfileUpdateMFAStatus} {
		if !shouldSkipMFAForPasswordReset(admin, string(routeAlias)) {
			t.Fatalf("shouldSkipMFAForPasswordReset(%s) = false, want true", routeAlias)
		}
	}
	if shouldSkipMFAForPasswordReset(admin, "admin.list") {
		t.Fatalf("shouldSkipMFAForPasswordReset(admin.list) = true, want false")
	}
	if shouldSkipMFAForPasswordReset(admin, string(routealias.AdminPasswordReset)) {
		t.Fatalf("shouldSkipMFAForPasswordReset(admin.password.reset) = true, want false")
	}
	if shouldSkipMFAForPasswordReset(&model.Admin{ID: 8, Name: "admin999"}, string(routealias.ProfileUpdatePassword)) {
		t.Fatalf("shouldSkipMFAForPasswordReset(non-reset profile.update_password) = true, want false")
	}
}

// TestShouldBypassLoginMFACheck 验证登录后首次绑定 MFA 所需的自助接口可以跳过登录态 MFA 前置拦截。
func TestShouldBypassLoginMFACheck(t *testing.T) {
	admin := &model.Admin{ID: 8, Name: "admin999"}
	for _, routeAlias := range []routealias.Alias{routealias.ProfileCheckMFA, routealias.ProfileRefreshMFASecret, routealias.ProfileUpdateMFAStatus} {
		if !shouldBypassLoginMFACheck(admin, string(routeAlias)) {
			t.Fatalf("shouldBypassLoginMFACheck(%s) = false, want true", routeAlias)
		}
	}
	if shouldBypassLoginMFACheck(admin, "admin.list") {
		t.Fatalf("shouldBypassLoginMFACheck(admin.list) = true, want false")
	}
	if shouldBypassLoginMFACheck(admin, string(routealias.AdminPasswordReset)) {
		t.Fatalf("shouldBypassLoginMFACheck(admin.password.reset) = true, want false")
	}
}

// TestRoutePermissionIDsUsesCachedSet 验证路由权限候选缓存命中时可直接解析权限 ID 集合。
func TestRoutePermissionIDsUsesCachedSet(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	svcCtx := svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client})
	logicObj := NewSecurityLogic(context.Background(), svcCtx)
	cacheKey := fmt.Sprintf(keys.RoutePermissionIDs, "admin.list")
	physicalCacheKey := cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, cacheKey)
	if err := client.SAdd(context.Background(), physicalCacheKey, "2", "5").Err(); err != nil {
		t.Fatalf("SAdd(%s) error = %v", cacheKey, err)
	}
	got, err := logicObj.routePermissionIDs("admin.list")
	if err != nil {
		t.Fatalf("routePermissionIDs(admin.list) error = %v", err)
	}
	if len(got) != 2 || got[0] != 2 || got[1] != 5 {
		t.Fatalf("routePermissionIDs(admin.list) = %v, want [2 5]", got)
	}
	aliasIndexKey := cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, keys.RoutePermissionAliasIndex)
	if ok, err := client.SIsMember(context.Background(), aliasIndexKey, "admin.list").Result(); err != nil || !ok {
		t.Fatalf("routePermissionIDs(admin.list) did not track route permission alias index")
	}
}

// TestRoutePermissionIDsRebuildsFromPermissionCaches 验证路由候选权限缓存缺失时可基于当前 module 缓存自动重建。
func TestRoutePermissionIDsRebuildsFromPermissionCaches(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	svcCtx := svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client})
	logicObj := NewSecurityLogic(context.Background(), svcCtx)
	ctx := context.Background()

	permissionModuleKey := cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, keys.PermissionModule)
	if err := client.HSet(ctx, permissionModuleKey, "2", "admin.list").Err(); err != nil {
		t.Fatalf("HSet(permission_module) error = %v", err)
	}

	got, err := logicObj.routePermissionIDs("admin.list")
	if err != nil {
		t.Fatalf("routePermissionIDs(admin.list) error = %v", err)
	}
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("routePermissionIDs(admin.list) = %v, want [2]", got)
	}
	routeCacheKey := cachelogic.TableCachePhysicalKey(logicObj.BaseLogic, fmt.Sprintf(keys.RoutePermissionIDs, "admin.list"))
	if !server.Exists(routeCacheKey) {
		t.Fatalf("routePermissionIDs(admin.list) did not rebuild route_permission_ids cache")
	}
}

// TestRefreshPermissionRelatedCacheDeletesRoutePermissionCache 验证权限缓存刷新会按索引精确清理路由候选缓存。
func TestRefreshPermissionRelatedCacheDeletesRoutePermissionCache(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	svcCtx := svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client})
	base := corelogic.NewBaseLogicWithContext(context.Background(), svcCtx)
	logicObj := &rbaclogic.AdminPermissionLogic{BaseLogic: base}
	ctx := context.Background()

	keysToPrepare := []string{
		cachelogic.TableCachePhysicalKey(base, keys.PermissionTree),
		cachelogic.TableCachePhysicalKey(base, keys.PermissionModule),
		cachelogic.TableCachePhysicalKey(base, keys.PermissionUUID),
		cachelogic.TableCachePhysicalKey(base, fmt.Sprintf(keys.RoutePermissionIDs, "admin.list")),
		cachelogic.TableCachePhysicalKey(base, keys.RoutePermissionAliasIndex),
	}
	for _, key := range keysToPrepare {
		if key == cachelogic.TableCachePhysicalKey(base, keys.PermissionModule) || key == cachelogic.TableCachePhysicalKey(base, keys.PermissionUUID) {
			if err := client.HSet(ctx, key, "1", "value").Err(); err != nil {
				t.Fatalf("HSet(%s) error = %v", key, err)
			}
			continue
		}
		if key == cachelogic.TableCachePhysicalKey(base, fmt.Sprintf(keys.RoutePermissionIDs, "admin.list")) {
			if err := client.SAdd(ctx, key, "1").Err(); err != nil {
				t.Fatalf("SAdd(%s) error = %v", key, err)
			}
			continue
		}
		if key == cachelogic.TableCachePhysicalKey(base, keys.RoutePermissionAliasIndex) {
			if err := client.SAdd(ctx, key, "admin.list").Err(); err != nil {
				t.Fatalf("SAdd(%s) error = %v", key, err)
			}
			continue
		}
		if err := client.Set(ctx, key, "value", 0).Err(); err != nil {
			t.Fatalf("Set(%s) error = %v", key, err)
		}
	}

	logicObj.RefreshPermissionRelatedCache()

	for _, key := range keysToPrepare {
		if server.Exists(key) {
			t.Fatalf("refreshPermissionRelatedCache() key %s still exists", key)
		}
	}
}

// TestBusinessLogicDoesNotUseRedisScanOrPrefixDelete 验证业务逻辑不再通过 Redis 扫描或 table-cache 前缀删除处理高基数 key。
func TestBusinessLogicDoesNotUseRedisScanOrPrefixDelete(t *testing.T) {
	root := testFilePath("../../../internal/logic")
	forbidden := []string{"DeleteByPrefix(", "DeletePattern(", "HScan(", "SScan(", "ZScan(", "ForEachMaster(", "scanDeletePattern"}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return errors.Tag(walkErr)
		}
		if info == nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return errors.Tag(readErr)
		}
		for _, keyword := range forbidden {
			if strings.Contains(string(content), keyword) {
				t.Fatalf("业务逻辑禁止使用Redis前缀/模板删除: file=%s keyword=%s", path, keyword)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk(internal/logic) error = %v", err)
	}
}

// TestInvalidateAdminRelationCacheDeletesPermissionUUIDs 验证管理员关系缓存失效会同步删除最终权限码缓存。
func TestInvalidateAdminRelationCacheDeletesPermissionUUIDs(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	svcCtx := svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client})
	base := corelogic.NewBaseLogicWithContext(context.Background(), svcCtx)
	ctx := context.Background()
	adminID := 7

	stringKeys := []string{
		keys.AdminInfoRedisKey(adminID),
		cachelogic.TableCachePhysicalKey(base, fmt.Sprintf(keys.AdminProfile, adminID)),
	}
	setKeys := []string{
		cachelogic.TableCachePhysicalKey(base, fmt.Sprintf(keys.AdminRoleIDs, adminID)),
		cachelogic.TableCachePhysicalKey(base, fmt.Sprintf(keys.AdminRolesDetail, adminID)),
		cachelogic.TableCachePhysicalKey(base, fmt.Sprintf(keys.AdminPermissionIDs, adminID)),
		cachelogic.TableCachePhysicalKey(base, fmt.Sprintf(keys.AdminPermissionUUIDs, adminID)),
	}
	for _, key := range stringKeys {
		if err := client.Set(ctx, key, "value", 0).Err(); err != nil {
			t.Fatalf("Set(%s) error = %v", key, err)
		}
	}
	for _, key := range setKeys {
		if err := client.SAdd(ctx, key, "value").Err(); err != nil {
			t.Fatalf("SAdd(%s) error = %v", key, err)
		}
	}

	cachelogic.InvalidateAdminRelationCache(base, adminID)

	for _, key := range append(stringKeys, setKeys...) {
		if server.Exists(key) {
			t.Fatalf("cachelogic.InvalidateAdminRelationCache() key %s still exists", key)
		}
	}
}

// TestGetUserPermissionCodesRebuildsPermissionUUIDCache 验证权限码查询会从权限 UUID 缓存重建最终权限码缓存。
func TestGetUserPermissionCodesRebuildsPermissionUUIDCache(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	svcCtx := svc.NewServiceContext(config.Config{AppID: "site-a"}, svc.Dependencies{Rds: client})
	ctx := context.Background()

	base := corelogic.NewBaseLogicWithContext(ctx, svcCtx)
	if err := client.SAdd(ctx, cachelogic.TableCachePhysicalKey(base, fmt.Sprintf(keys.AdminPermissionIDs, 7)), "2").Err(); err != nil {
		t.Fatalf("SAdd(admin_permission_ids:7) error = %v", err)
	}
	if err := client.HSet(ctx, cachelogic.TableCachePhysicalKey(base, keys.PermissionUUID), "2", "100002").Err(); err != nil {
		t.Fatalf("HSet(permission_uuid) error = %v", err)
	}

	values, err := (&SecurityLogic{BaseLogic: base}).UserPermissionUUIDsWithCache(7)
	if err != nil {
		t.Fatalf("UserPermissionUUIDsWithCache(7) error = %v", err)
	}
	if len(values) != 1 || values[0] != "100002" {
		t.Fatalf("GetUserPermissionCodes(7) values = %v, want [100002]", values)
	}
	if !server.Exists(cachelogic.TableCachePhysicalKey(base, fmt.Sprintf(keys.AdminPermissionUUIDs, 7))) {
		t.Fatalf("GetUserPermissionCodes(7) did not rebuild admin_permission_uuids cache")
	}
}

// loadPermissionSQLSnapshot 读取初始化 SQL 中的权限 UUID 与 module 集合，供权限清单回归测试复用。
func loadPermissionSQLSnapshot() (map[string]struct{}, map[string]struct{}, error) {
	content, err := os.ReadFile(testFilePath("../../../internal/database/assets/bootstrap.sql"))
	if err != nil {
		return nil, nil, errors.Tag(err)
	}
	statementPattern := regexp.MustCompile("(?s)INSERT INTO `admin_permission`.*?VALUES \\((\\d+),\\s*'([^']+)',\\s*'([^']*)',\\s*'([^']*)'")
	matches := statementPattern.FindAllStringSubmatch(string(content), -1)
	uuidSet := make(map[string]struct{}, len(matches))
	moduleSet := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) < 5 {
			continue
		}
		uuidSet[match[2]] = struct{}{}
		module := match[4]
		if module != "" {
			moduleSet[module] = struct{}{}
		}
	}
	return uuidSet, moduleSet, nil
}

// loadFrontendPermissionCodes 读取前端常量文件中的全部 6 位权限码，确保 SQL 与前端显隐权限保持一致。
func loadFrontendPermissionCodes() ([]string, error) {
	content, err := os.ReadFile(frontendPermissionCodesFilePath())
	if err != nil {
		return nil, errors.Tag(err)
	}
	codePattern := regexp.MustCompile("'\\d{6}'")
	matches := codePattern.FindAllString(string(content), -1)
	codeSet := map[string]struct{}{}
	for _, match := range matches {
		codeSet[match[1:len(match)-1]] = struct{}{}
	}
	codes := make([]string, 0, len(codeSet))
	for code := range codeSet {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes, nil
}

// frontendPermissionCodesFilePath 返回前端权限码常量文件路径，供联动测试统一复用。
func frontendPermissionCodesFilePath() string {
	return testFilePath("../../../../gozero-admin-vue/apps/web-antd/src/constants/permission-codes.ts")
}

// testFilePath 基于当前测试文件计算仓库内/相邻仓库文件路径，避免依赖 `go test` 执行目录。
func testFilePath(relativePath string) string {
	_, currentFile, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), relativePath))
}
