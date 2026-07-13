package cache

import (
	_ "embed"

	"admin/common/embedasset"

	"github.com/redis/go-redis/v9"
)

// setAdminInfoFieldsScriptText 保存管理员缓存字段批量更新 Lua 脚本源码。
// 脚本会先确认缓存 key 和全部目标字段均存在，再原子 HSET，避免补写或部分更新不完整缓存。
//
//go:embed assets/set_admin_info_fields.lua
var setAdminInfoFieldsScriptText string

// setAdminInfoFieldsScript 原子更新管理员缓存中已经存在的受控字段。
var setAdminInfoFieldsScript = redis.NewScript(embedasset.StripLeadingLineComments(setAdminInfoFieldsScriptText, "--"))

// rotateAdminTokenScriptText 保存管理员会话 token 轮换 Lua 脚本源码。
// 脚本只在缓存 token 与当前请求 token 一致时更新 token 和 TTL，防止并发刷新或登出后复活会话。
//
//go:embed assets/rotate_admin_token.lua
var rotateAdminTokenScriptText string

// rotateAdminTokenScript 原子比较并轮换管理员会话 token。
var rotateAdminTokenScript = redis.NewScript(embedasset.StripLeadingLineComments(rotateAdminTokenScriptText, "--"))

// canUseAdminSessionTokenScriptText 保存会话终结路由专用 token 校验 Lua 脚本源码。
// 脚本允许当前 token，或短暂宽限期内的上一枚刷新 token；普通业务路由不会调用该脚本。
//
//go:embed assets/can_use_admin_session_token.lua
var canUseAdminSessionTokenScriptText string

// canUseAdminSessionTokenScript 原子判断 token 是否仍可用于刷新或退出当前会话。
var canUseAdminSessionTokenScript = redis.NewScript(embedasset.StripLeadingLineComments(canUseAdminSessionTokenScriptText, "--"))

// deleteAdminSessionForLogoutScriptText 保存管理员退出 CAS 删除 Lua 脚本源码。
// 脚本只接受当前 token 或宽限期内上一枚刷新 token，避免旧登录请求误删后来建立的新会话。
//
//go:embed assets/delete_admin_session_for_logout.lua
var deleteAdminSessionForLogoutScriptText string

// deleteAdminSessionForLogoutScript 原子校验退出 token 并删除管理员登录态。
var deleteAdminSessionForLogoutScript = redis.NewScript(embedasset.StripLeadingLineComments(deleteAdminSessionForLogoutScriptText, "--"))
