package logic

import (
	"context"
	"fmt"
	"strings"
	"testing"

	keys "admin_cron/common/rediskeys"
	"admin_cron/internal/config"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestMaskCacheValueForAdminInfo 验证管理员登录态缓存按字段级规则展示，
// 仅遮罩 token 等通用敏感字段，保留资料字段与备注原样展示。
func TestMaskCacheValueForAdminInfo(t *testing.T) {
	value := map[string]string{
		"LastLoginIpaddr":   "中国香港",
		"avatar":            "https://cdn.example.com/avatar.png",
		"description":       "超级管理员",
		"email":             "admin@example.com",
		"id":                "1",
		"lastLoginIP":       "127.0.0.1",
		"lastLoginTime":     "2026-05-04 18:20:00",
		"mfaStatus":         "1",
		"needResetPassword": "0",
		"phone":             "13800138000",
		"realName":          "管理员",
		"status":            "1",
		"token":             "jwt-token",
		"username":          "admin",
	}

	got := maskCacheValue("admin:info:1", value)
	masked, ok := got.(map[string]string)
	if !ok {
		t.Fatalf("maskCacheValue() type = %T, want map[string]string", got)
	}
	if masked["token"] != cacheMaskedValue {
		t.Fatalf("token = %q, want masked", masked["token"])
	}
	if masked["description"] != "超级管理员" {
		t.Fatalf("description = %q, want 超级管理员", masked["description"])
	}
	if masked["needResetPassword"] != "0" {
		t.Fatalf("needResetPassword = %q, want 0", masked["needResetPassword"])
	}
	if masked["username"] != "admin" {
		t.Fatalf("username = %q, want admin", masked["username"])
	}
	if masked["email"] != "admin@example.com" {
		t.Fatalf("email = %q, want admin@example.com", masked["email"])
	}
	if masked["LastLoginIpaddr"] != "中国香港" {
		t.Fatalf("LastLoginIpaddr = %q, want 中国香港", masked["LastLoginIpaddr"])
	}
}

// TestMaskCacheValueForSecretKeyHashes 验证 secret_key_* hash 仅按字段级规则脱敏，
// 避免把 status、version 这类排障所需状态字段整体遮蔽。
func TestMaskCacheValueForSecretKeyHashes(t *testing.T) {
	value := map[string]string{
		"aes_iv_ref":  "/etc/admin-cron/keys/demo/aes_iv",
		"aes_key_ref": "/etc/admin-cron/keys/demo/aes_key",
		"status":      "1",
		"version":     "v1",
	}

	got := maskCacheValue("secret_key_aes:demo:v1", value)
	masked, ok := got.(map[string]string)
	if !ok {
		t.Fatalf("maskCacheValue() type = %T, want map[string]string", got)
	}
	if masked["aes_key_ref"] != cacheMaskedValue {
		t.Fatalf("aes_key_ref = %q, want masked", masked["aes_key_ref"])
	}
	if masked["aes_iv_ref"] != cacheMaskedValue {
		t.Fatalf("aes_iv_ref = %q, want masked", masked["aes_iv_ref"])
	}
	if masked["status"] != "1" {
		t.Fatalf("status = %q, want 1", masked["status"])
	}
	if masked["version"] != "v1" {
		t.Fatalf("version = %q, want v1", masked["version"])
	}
}

// TestMaskCacheValueForReplayTicket 验证仍需整体脱敏的缓存类型保持原有保护行为。
func TestMaskCacheValueForReplayTicket(t *testing.T) {
	value := map[string]string{
		"two_step_value": "abc",
		"status":         "1",
	}

	got := maskCacheValue("admin:mfa:two_step:demo", value)
	masked, ok := got.(map[string]string)
	if !ok {
		t.Fatalf("maskCacheValue() type = %T, want map[string]string", got)
	}
	if masked["two_step_value"] != cacheMaskedValue {
		t.Fatalf("two_step_value = %q, want masked", masked["two_step_value"])
	}
	if masked["status"] != cacheMaskedValue {
		t.Fatalf("status = %q, want masked for whole-cache sensitive key", masked["status"])
	}
}

// TestNormalizeCacheSearchPatternRejectsDangerousBroadScan 验证缓存搜索会拦截危险条件。
func TestNormalizeCacheSearchPatternRejectsDangerousBroadScan(t *testing.T) {
	for _, pattern := range []string{"", "*", "a*", "ab?", "**a"} {
		if _, err := normalizeCacheSearchPattern(pattern); err == nil {
			t.Fatalf("期望搜索模式 %q 被拒绝，实际返回 nil", pattern)
		}
	}
	if got, err := normalizeCacheSearchPattern("admin:info:*"); err != nil || got != "admin:info:*" {
		t.Fatalf("期望合法搜索模式透传，实际 got=%q err=%v", got, err)
	}
}

// TestMatchCacheListItemSupportsTemplatePrefixOnly 验证模板缓存支持前缀筛选，普通缓存仍保持精确匹配。
func TestMatchCacheListItemSupportsTemplatePrefixOnly(t *testing.T) {
	templateItem := types.CacheItem{
		Index:      "admin_info",
		Key:        "admin:info:{adminID}",
		KeyTitle:   "admin:info:{adminID}",
		ExampleKey: "admin:info:1",
		IsTemplate: true,
	}
	if !matchCacheListItem(templateItem, "admin:info:") {
		t.Fatal("期望模板缓存支持按固定前缀匹配")
	}
	if !matchCacheListItem(templateItem, "admin:info:1") {
		t.Fatal("期望模板缓存支持按示例 key 精确匹配")
	}

	plainItem := types.CacheItem{
		Index:      "role_status",
		Key:        "role:status",
		KeyTitle:   "role:status",
		ExampleKey: "role:status",
		IsTemplate: false,
	}
	if matchCacheListItem(plainItem, "role:") {
		t.Fatal("期望普通缓存不支持前缀模糊匹配")
	}
	if !matchCacheListItem(plainItem, "role:status") {
		t.Fatal("期望普通缓存仍支持精确匹配")
	}
}

// TestSearchKeyUsesExactExistsPath 验证精确缓存 key 搜索会直接走 Exists 快路径，避免不必要的 SCAN。
func TestSearchKeyUsesExactExistsPath(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	if err := client.Set(context.Background(), "permission_tree", "demo", 0).Err(); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	logicObj := &SystemCacheLogic{
		BaseLogic: NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{}, svc.Dependencies{Rds: client})),
	}
	result := logicObj.SearchKey(&types.CacheKeyReq{Key: "permission_tree"})
	if result == nil {
		t.Fatal("SearchKey() result is nil")
	}
	resp, ok := result.Data.(types.CacheSearchResp)
	if !ok {
		t.Fatalf("SearchKey() data type = %T, want types.CacheSearchResp", result.Data)
	}
	if len(resp.List) != 1 || resp.List[0].Key != "permission_tree" {
		t.Fatalf("SearchKey() items = %+v, want single exact key", resp.List)
	}
	if resp.Total != 1 || resp.HasMore {
		t.Fatalf("SearchKey() total=%d hasMore=%v, want total=1 hasMore=false", resp.Total, resp.HasMore)
	}
}

// TestPaginateCacheSearchKeys 验证缓存搜索只返回当前页数据，并正确暴露是否还有下一页。
func TestPaginateCacheSearchKeys(t *testing.T) {
	keys := []string{"k1", "k2", "k3", "k4", "k5"}
	pageKeys, hasMore := paginateCacheSearchKeys(keys, 2, 2)
	if hasMore != true {
		t.Fatal("期望第二页后仍有下一页")
	}
	if strings.Join(pageKeys, ",") != "k3,k4" {
		t.Fatalf("pageKeys = %v, want [k3 k4]", pageKeys)
	}
	lastPageKeys, lastHasMore := paginateCacheSearchKeys(keys, 3, 2)
	if lastHasMore {
		t.Fatal("期望最后一页没有下一页")
	}
	if strings.Join(lastPageKeys, ",") != "k5" {
		t.Fatalf("lastPageKeys = %v, want [k5]", lastPageKeys)
	}
}

// TestFilterExistingKeysCapsResultAllocationAndDeduplicates 验证模板候选校验会去重并遵守最大返回数量。
func TestFilterExistingKeysCapsResultAllocationAndDeduplicates(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	for _, key := range []string{"admin:info:1", "admin:info:2", "admin:info:3"} {
		if err := client.Set(context.Background(), key, "demo", 0).Err(); err != nil {
			t.Fatalf("Set(%s) error = %v", key, err)
		}
	}
	logicObj := &SystemCacheLogic{
		BaseLogic: NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{}, svc.Dependencies{Rds: client})),
	}
	keys, stats, err := logicObj.filterExistingKeys([]string{
		"admin:info:3",
		"admin:info:missing",
		"admin:info:1",
		"admin:info:1",
		"admin:info:2",
	}, 2)
	if err != nil {
		t.Fatalf("filterExistingKeys() error = %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("keys len = %d, want 2, keys=%v", len(keys), keys)
	}
	if stats.existingCount < 2 {
		t.Fatalf("existingCount = %d, want >=2", stats.existingCount)
	}
	if keys[0] > keys[1] {
		t.Fatalf("keys 未排序: %v", keys)
	}
}

// TestKeyInfoPreviewsLargeString 验证大 string 详情只读取有限预览，避免详情接口拉取完整大值。
func TestKeyInfoPreviewsLargeString(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	body := strings.Repeat("a", cacheKeyInfoPreviewStringBytes+128)
	if err := client.Set(context.Background(), "large:string", body, 0).Err(); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	logicObj := &SystemCacheLogic{
		BaseLogic: NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{}, svc.Dependencies{Rds: client})),
	}
	result := logicObj.KeyInfo(&types.CacheKeyReq{Key: "large:string"})
	if result == nil {
		t.Fatal("KeyInfo() result is nil")
	}
	info, ok := result.Data.(*types.CacheKeyInfoResp)
	if !ok {
		t.Fatalf("KeyInfo() data type = %T, want *types.CacheKeyInfoResp", result.Data)
	}
	value, ok := info.Value.(string)
	if !ok {
		t.Fatalf("KeyInfo() value type = %T, want string", info.Value)
	}
	if info.Total != int64(len(body)) {
		t.Fatalf("Total = %d, want %d", info.Total, len(body))
	}
	if len(value) != cacheKeyInfoPreviewStringBytes {
		t.Fatalf("preview len = %d, want %d", len(value), cacheKeyInfoPreviewStringBytes)
	}
}

// TestKeyInfoPreviewsLargeHash 验证大 hash 详情只返回预览字段，但 total 保留完整字段数。
func TestKeyInfoPreviewsLargeHash(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	values := make(map[string]string)
	for index := 0; index < cacheKeyInfoPreviewItems+50; index++ {
		values[fmt.Sprintf("field_%03d", index)] = "value"
	}
	if err := client.HSet(context.Background(), "large:hash", values).Err(); err != nil {
		t.Fatalf("HSet() error = %v", err)
	}
	logicObj := &SystemCacheLogic{
		BaseLogic: NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{}, svc.Dependencies{Rds: client})),
	}
	result := logicObj.KeyInfo(&types.CacheKeyReq{Key: "large:hash"})
	if result == nil {
		t.Fatal("KeyInfo() result is nil")
	}
	info, ok := result.Data.(*types.CacheKeyInfoResp)
	if !ok {
		t.Fatalf("KeyInfo() data type = %T, want *types.CacheKeyInfoResp", result.Data)
	}
	preview, ok := info.Value.(map[string]string)
	if !ok {
		t.Fatalf("KeyInfo() value type = %T, want map[string]string", info.Value)
	}
	if info.Total != int64(len(values)) {
		t.Fatalf("Total = %d, want %d", info.Total, len(values))
	}
	if len(preview) > cacheKeyInfoPreviewItems {
		t.Fatalf("preview len = %d, want <= %d", len(preview), cacheKeyInfoPreviewItems)
	}
}

// TestKeyInfoPreviewsLargeSet 验证大 set 详情只返回预览成员，并且 total 保留完整成员数。
func TestKeyInfoPreviewsLargeSet(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	values := make([]any, 0, cacheKeyInfoPreviewItems+50)
	for index := 0; index < cacheKeyInfoPreviewItems+50; index++ {
		values = append(values, fmt.Sprintf("member_%03d", index))
	}
	if err := client.SAdd(context.Background(), "large:set", values...).Err(); err != nil {
		t.Fatalf("SAdd() error = %v", err)
	}
	logicObj := &SystemCacheLogic{
		BaseLogic: NewBaseLogicWithContext(context.Background(), svc.NewServiceContext(config.Config{}, svc.Dependencies{Rds: client})),
	}
	result := logicObj.KeyInfo(&types.CacheKeyReq{Key: "large:set"})
	if result == nil {
		t.Fatal("KeyInfo() result is nil")
	}
	info, ok := result.Data.(*types.CacheKeyInfoResp)
	if !ok {
		t.Fatalf("KeyInfo() data type = %T, want *types.CacheKeyInfoResp", result.Data)
	}
	preview, ok := info.Value.([]string)
	if !ok {
		t.Fatalf("KeyInfo() value type = %T, want []string", info.Value)
	}
	if info.Total != int64(len(values)) {
		t.Fatalf("Total = %d, want %d", info.Total, len(values))
	}
	if len(preview) > cacheKeyInfoPreviewItems {
		t.Fatalf("preview len = %d, want <= %d", len(preview), cacheKeyInfoPreviewItems)
	}
}

// TestCacheSearchPatternMatch 验证模板快路径使用的 glob 匹配规则兼容 `*` 与 `?`。
func TestCacheSearchPatternMatch(t *testing.T) {
	tests := []struct {
		pattern string
		value   string
		want    bool
	}{
		{pattern: "config_uuid:*", value: "config_uuid:abc", want: true},
		{pattern: "role_permission:1?", value: "role_permission:12", want: true},
		{pattern: "role_permission:1?", value: "role_permission:123", want: false},
		{pattern: "admin_profile:*", value: "admin_roles_detail:1", want: false},
	}
	for _, tt := range tests {
		if got := cacheSearchPatternMatch(tt.pattern, tt.value); got != tt.want {
			t.Fatalf("cacheSearchPatternMatch(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.want)
		}
	}
}

// TestMatchSearchTemplateTarget 验证已知模板前缀会命中白名单模板枚举策略。
func TestMatchSearchTemplateTarget(t *testing.T) {
	logicObj := &SystemCacheLogic{}
	target := logicObj.matchSearchTemplateTarget("config_uuid:*")
	wantConfigTemplate := tableCachePhysicalKey(logicObj.BaseLogic, "config_uuid:{uuid}")
	if target == nil || target.templateKey != wantConfigTemplate {
		t.Fatalf("matchSearchTemplateTarget(config_uuid:*) = %+v, want %s", target, wantConfigTemplate)
	}
	adminTarget := logicObj.matchSearchTemplateTarget("admin_profile:*")
	wantAdminTemplate := tableCachePhysicalKey(logicObj.BaseLogic, "admin_profile:{adminID}")
	if adminTarget == nil || adminTarget.templateKey != wantAdminTemplate {
		t.Fatalf("matchSearchTemplateTarget(admin_profile:*) = %+v, want %s", adminTarget, wantAdminTemplate)
	}
	if got := logicObj.matchSearchTemplateTarget("permission_tree"); got != nil {
		t.Fatalf("matchSearchTemplateTarget(permission_tree) = %+v, want nil", got)
	}
}

// TestCacheSearchCandidateMatchesLogicalTableCachePattern 验证旧逻辑模板能匹配新版 table-cache 物理 key。
func TestCacheSearchCandidateMatchesLogicalTableCachePattern(t *testing.T) {
	logicObj := &SystemCacheLogic{}
	target := logicObj.matchSearchTemplateTarget("config_uuid:*")
	if target == nil {
		t.Fatal("matchSearchTemplateTarget(config_uuid:*) returned nil")
	}
	candidate := tableCachePhysicalKey(logicObj.BaseLogic, "config_uuid:site.name")
	if !logicObj.cacheSearchCandidateMatches("config_uuid:*", candidate, target) {
		t.Fatalf("cacheSearchCandidateMatches(logical pattern, %s) = false, want true", candidate)
	}

	adminTarget := logicObj.matchSearchTemplateTarget("admin:info:*")
	if adminTarget == nil {
		t.Fatal("matchSearchTemplateTarget(admin:info:*) returned nil")
	}
	if !logicObj.cacheSearchCandidateMatches("admin:info:*", "admin:info:1", adminTarget) {
		t.Fatal("cacheSearchCandidateMatches(admin info) = false, want true")
	}
}

// TestCacheTemplateDefinitionsUsePhysicalKeys 验证缓存管理页、搜索和预热暴露的 table-cache 模板均使用物理 key。
func TestCacheTemplateDefinitionsUsePhysicalKeys(t *testing.T) {
	logicObj := &SystemCacheLogic{}
	prefix := keys.TableCachePrefix("")
	for _, item := range logicObj.cacheItems() {
		if item.Index == "admin_info" {
			continue
		}
		if !strings.HasPrefix(item.Key, prefix) || !strings.HasPrefix(item.KeyTitle, prefix) {
			t.Fatalf("cache item %s key=%s keyTitle=%s should use prefix %s", item.Index, item.Key, item.KeyTitle, prefix)
		}
		if item.IsTemplate {
			if !strings.HasPrefix(item.ExampleKey, prefix) {
				t.Fatalf("cache item %s exampleKey=%s should use prefix %s", item.Index, item.ExampleKey, prefix)
			}
			if strings.ContainsAny(item.ExampleKey, "{}%") {
				t.Fatalf("cache item %s exampleKey=%s should be concrete", item.Index, item.ExampleKey)
			}
			logicalExampleKey := keys.TrimTableCachePrefix(item.ExampleKey)
			if got := logicObj.matchCacheItem(logicalExampleKey); got == nil {
				t.Fatalf("matchCacheItem(%s) returned nil, want item %s", logicalExampleKey, item.Index)
			}
		}
	}
	for _, target := range logicObj.warmupTemplateTargets() {
		if !strings.HasPrefix(target.templateKey, prefix) {
			t.Fatalf("warmup template %s should use prefix %s", target.templateKey, prefix)
		}
	}
	for _, target := range logicObj.searchTemplateTargets() {
		if target.templateKey == "admin:info:{adminID}" {
			continue
		}
		if !strings.HasPrefix(target.templateKey, prefix) {
			t.Fatalf("search template %s should use prefix %s", target.templateKey, prefix)
		}
	}
}

// TestSearchKeysExactLogicalTableCacheKeyFindsPhysicalKey 验证精确搜索旧逻辑 key 时会读取新版物理 key。
func TestSearchKeysExactLogicalTableCacheKeyFindsPhysicalKey(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	logicObj := &SystemCacheLogic{BaseLogic: NewBaseLogicWithContext(context.Background(), &svc.ServiceContext{Rds: client})}
	physicalKey := tableCachePhysicalKey(logicObj.BaseLogic, keys.RoleStatus)
	if err := client.HSet(context.Background(), physicalKey, "1", "1").Err(); err != nil {
		t.Fatalf("HSet(%s) error = %v", physicalKey, err)
	}

	got, path, stats, err := logicObj.searchKeys(keys.RoleStatus, cacheSearchMaxResults)
	if err != nil {
		t.Fatalf("searchKeys(%s) error = %v", keys.RoleStatus, err)
	}
	if path != "exact_exists" || stats.existingCount != 1 {
		t.Fatalf("searchKeys path=%s stats=%+v, want exact_exists existing=1", path, stats)
	}
	if len(got) != 1 || got[0] != physicalKey {
		t.Fatalf("searchKeys(%s) = %v, want [%s]", keys.RoleStatus, got, physicalKey)
	}
}

// TestValidateSearchPatternRejectsUnknownWildcard 验证非模板通配符搜索会被拒绝，避免触发 Redis 全库扫描。
func TestValidateSearchPatternRejectsUnknownWildcard(t *testing.T) {
	logicObj := &SystemCacheLogic{}
	if err := logicObj.validateSearchPattern("*permission*"); err == nil {
		t.Fatal("期望前导通配符搜索被拒绝，实际返回 nil")
	}
	if err := logicObj.validateSearchPattern("permission*"); err == nil {
		t.Fatal("期望未知通配符搜索被拒绝，实际返回 nil")
	}
	if err := logicObj.validateSearchPattern("config_uuid:*"); err != nil {
		t.Fatalf("期望已登记模板搜索放行，实际 error = %v", err)
	}
	if err := logicObj.validateSearchPattern("permission_tree"); err != nil {
		t.Fatalf("期望精确 key 搜索放行，实际 error = %v", err)
	}
}
