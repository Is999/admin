package keys

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// adminInfoLogical 表示去掉 app_id 命名空间后的管理员登录态业务段模板。
	adminInfoLogical = "admin:info:%d"
	// adminInfoLogicalPattern 表示去掉 app_id 命名空间后的管理员登录态展示模板。
	adminInfoLogicalPattern = "admin:info:{adminID}"
	// adminLogoutTokenLogical 表示去掉 app_id 命名空间后的管理员登出标记业务段模板。
	adminLogoutTokenLogical = "admin:logout_token:%d"
	// adminMFATwoStepTicketLogical 表示去掉 app_id 命名空间后的 MFA 二次票据业务段模板。
	adminMFATwoStepTicketLogical = "admin:mfa:two_step:%d:%s"
)

// AdminInfoRedisKey 返回管理员登录态缓存 Redis key。
func AdminInfoRedisKey(adminID int) string {
	return WithPrefix(fmt.Sprintf(AdminInfo, adminID))
}

// AdminInfoPatternRedisKey 返回管理员登录态缓存展示模板 Redis key。
func AdminInfoPatternRedisKey() string {
	return prefixedPattern(AdminInfoPattern)
}

// AdminLogoutTokenRedisKey 返回管理员显式登出标记 Redis key。
func AdminLogoutTokenRedisKey(adminID int) string {
	return WithPrefix(fmt.Sprintf(AdminLogoutToken, adminID))
}

// LoginCheckMFAFlagRedisKey 返回管理员登录 MFA 完成标记 Redis key。
func LoginCheckMFAFlagRedisKey(adminID int) string {
	return WithPrefix(fmt.Sprintf(LoginCheckMFAFlag, adminID))
}

// AdminMFATwoStepTicketRedisKey 返回管理员 MFA 二次票据 Redis key。
func AdminMFATwoStepTicketRedisKey(adminID int, ticketKey string) string {
	return WithPrefix(fmt.Sprintf(AdminMFATwoStepTicket, adminID, strings.TrimSpace(ticketKey)))
}

// AdminMFATwoStepIndexRedisKey 返回管理员 MFA 二次票据索引 Redis key。
func AdminMFATwoStepIndexRedisKey(adminID int) string {
	return WithPrefix(fmt.Sprintf(AdminMFATwoStepIndex, adminID))
}

// AdminInfoLogicalKey 返回去掉 app_id 命名空间后的管理员登录态业务段。
func AdminInfoLogicalKey(adminID int) string {
	return fmt.Sprintf(adminInfoLogical, adminID)
}

// AdminLogoutTokenLogicalKey 返回去掉 app_id 命名空间后的管理员登出标记业务段。
func AdminLogoutTokenLogicalKey(adminID int) string {
	return fmt.Sprintf(adminLogoutTokenLogical, adminID)
}

// AdminInfoLogicalPattern 返回去掉 app_id 命名空间后的管理员登录态展示模板。
func AdminInfoLogicalPattern() string {
	return adminInfoLogicalPattern
}

// IsAdminInfoRedisKey 判断 key 是否为管理员登录态缓存，支持完整 Redis key 和业务段。
func IsAdminInfoRedisKey(key string) bool {
	return strings.HasPrefix(TrimPrefix(key), KeyTemplatePrefix(adminInfoLogical))
}

// AdminInfoIDFromRedisKey 解析管理员登录态缓存 key 中的管理员 ID。
func AdminInfoIDFromRedisKey(key string) (int, bool) {
	prefix := KeyTemplatePrefix(adminInfoLogical)
	adminIDText := strings.TrimPrefix(TrimPrefix(key), prefix)
	if adminIDText == "" {
		return 0, false
	}
	adminID, err := strconv.Atoi(adminIDText)
	return adminID, err == nil && adminID > 0
}

// AdminMFATwoStepTicketBelongsToAdmin 判断二次票据 key 是否归属指定管理员。
func AdminMFATwoStepTicketBelongsToAdmin(key string, adminID int) bool {
	if adminID <= 0 {
		return false
	}
	prefix := KeyTemplatePrefix(fmt.Sprintf(adminMFATwoStepTicketLogical, adminID, ""))
	return strings.HasPrefix(TrimPrefix(key), prefix)
}

// prefixedPattern 把展示模板转换为当前应用完整 Redis key 模板。
func prefixedPattern(pattern string) string {
	return WithPrefix(strings.TrimSpace(pattern))
}
