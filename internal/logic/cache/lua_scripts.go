package cache

import (
	_ "embed"

	"admin/common/embedasset"

	"github.com/redis/go-redis/v9"
)

// setAdminInfoByFieldScriptText 保存管理员缓存字段更新 Lua 脚本源码。
// 脚本会先确认缓存 key 和目标字段均存在，再执行 HSET，避免补写不完整缓存。
//
//go:embed assets/set_admin_info_by_field.lua
var setAdminInfoByFieldScriptText string

// setAdminInfoByFieldScript 原子更新管理员缓存的已存在字段。
var setAdminInfoByFieldScript = redis.NewScript(embedasset.StripLeadingLineComments(setAdminInfoByFieldScriptText, "--"))
