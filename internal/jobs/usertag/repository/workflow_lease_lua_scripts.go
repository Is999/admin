package repository

import (
	_ "embed"

	"admin/common/embedasset"

	"github.com/redis/go-redis/v9"
)

// userTagWorkflowLeaseRenewScriptText 保存用户标签工作流租约续期 Lua 脚本源码。
// 脚本只操作 write_lock 单 key，owner 匹配后才刷新 TTL。
//
//go:embed assets/user_tag_workflow_lease_renew.lua
var userTagWorkflowLeaseRenewScriptText string

// userTagWorkflowLeaseReleaseScriptText 保存用户标签工作流租约释放 Lua 脚本源码。
// Redis Cluster 要求 Lua 的 KEYS 位于同一 slot；这里脚本只操作 write_lock，避免 CROSSSLOT。
//
//go:embed assets/user_tag_workflow_lease_release.lua
var userTagWorkflowLeaseReleaseScriptText string

// userTagWorkflowShardDoneScriptText 保存最终分片完成屏障 Lua 脚本源码。
// 每个 workflow_id 独立使用一个 final_done 集合，避免不同工作流之间互相影响。
//
//go:embed assets/user_tag_workflow_shard_done.lua
var userTagWorkflowShardDoneScriptText string

var (
	// userTagWorkflowLeaseRenewScript 在 Redis 内原子比较 owner 并刷新租约 TTL。
	userTagWorkflowLeaseRenewScript = redis.NewScript(embedasset.StripLeadingLineComments(userTagWorkflowLeaseRenewScriptText, "--"))
	// userTagWorkflowLeaseReleaseScript 在 Redis 内原子比较 owner 并释放租约。
	userTagWorkflowLeaseReleaseScript = redis.NewScript(embedasset.StripLeadingLineComments(userTagWorkflowLeaseReleaseScriptText, "--"))
	// userTagWorkflowShardDoneScript 记录一个最终分片完成并返回当前已完成分片数。
	userTagWorkflowShardDoneScript = redis.NewScript(embedasset.StripLeadingLineComments(userTagWorkflowShardDoneScriptText, "--"))
)
