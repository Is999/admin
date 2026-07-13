package security

import (
	_ "embed"

	"admin/common/embedasset"

	"github.com/redis/go-redis/v9"
)

// setMFATwoStepTicketScriptText 保存 MFA 二次票据写入 Lua 脚本源码。
//
//go:embed assets/set_mfa_two_step_ticket.lua
var setMFATwoStepTicketScriptText string

// setMFATwoStepTicketScript 原子写入票据并保留当前 Hash 中最长的剩余 TTL。
var setMFATwoStepTicketScript = redis.NewScript(embedasset.StripLeadingLineComments(setMFATwoStepTicketScriptText, "--"))
