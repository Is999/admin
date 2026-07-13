package keys

import (
	"fmt"
	"strconv"
	"strings"
)

// AdminSessionRedisKey 返回管理员会话缓存 Redis key。
func AdminSessionRedisKey(adminID int) string {
	return WithPrefix(fmt.Sprintf(AdminSession, adminID))
}

// AdminSessionPatternRedisKey 返回管理员会话缓存展示模板 Redis key。
func AdminSessionPatternRedisKey() string {
	return prefixedPattern(AdminSessionPattern)
}

// SecurityCacheSyncBarrierRedisKey 返回安全缓存失效补偿阻断键。
func SecurityCacheSyncBarrierRedisKey() string {
	return WithPrefix(SecurityCacheSyncBarrier)
}

// SecurityCacheSyncLockRedisKey 返回安全缓存失效补偿 worker 锁键。
func SecurityCacheSyncLockRedisKey() string {
	return WithPrefix(SecurityCacheSyncLock)
}

// LoginCheckMFAFlagRedisKey 返回管理员登录 MFA 完成标记 Redis key。
func LoginCheckMFAFlagRedisKey(adminID int) string {
	return WithPrefix(fmt.Sprintf(LoginCheckMFAFlag, adminID))
}

// AdminMFATwoStepRedisKey 返回管理员 MFA 二次票据 Hash Redis key。
func AdminMFATwoStepRedisKey(adminID int) string {
	return WithPrefix(fmt.Sprintf(AdminMFATwoStep, adminID))
}

// AdminSessionLogicalKey 返回去掉 app_id 命名空间后的管理员会话业务段。
func AdminSessionLogicalKey(adminID int) string {
	return fmt.Sprintf(AdminSession, adminID)
}

// AdminSessionLogicalPattern 返回去掉 app_id 命名空间后的管理员会话展示模板。
func AdminSessionLogicalPattern() string {
	return AdminSessionPattern
}

// IsAdminSessionRedisKey 判断 key 是否为管理员会话缓存，支持完整 Redis key 和业务段。
func IsAdminSessionRedisKey(key string) bool {
	return strings.HasPrefix(TrimPrefix(key), KeyTemplatePrefix(AdminSession))
}

// AdminSessionIDFromRedisKey 解析管理员会话缓存 key 中的管理员 ID。
func AdminSessionIDFromRedisKey(key string) (int, bool) {
	prefix := KeyTemplatePrefix(AdminSession)
	adminIDText := strings.TrimPrefix(TrimPrefix(key), prefix)
	if adminIDText == "" {
		return 0, false
	}
	adminID, err := strconv.Atoi(adminIDText)
	return adminID, err == nil && adminID > 0
}

// prefixedPattern 把展示模板转换为当前应用完整 Redis key 模板。
func prefixedPattern(pattern string) string {
	return WithPrefix(strings.TrimSpace(pattern))
}
