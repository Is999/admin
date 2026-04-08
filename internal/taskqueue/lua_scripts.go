package taskqueue

import (
	_ "embed"

	"admin_cron/common/embedasset"

	"github.com/redis/go-redis/v9"
)

// recordNodeOutcomeScriptText 保存节点分片终态记录 Lua 脚本源码。
// Redis Cluster 下脚本只操作 workflowNodeKey 一个 hash，避免跨 slot 计数漂移。
//
//go:embed record_node_outcome.lua
var recordNodeOutcomeScriptText string

// workflowArchivedTaskRerunRepairScriptText 保存归档任务重跑前修复节点失败计数的 Lua 脚本源码。
// 手动立即执行归档失败任务前会撤销旧失败标记，后续任务完成仍重新走节点终态记录脚本。
//
//go:embed workflow_archived_task_rerun_repair.lua
var workflowArchivedTaskRerunRepairScriptText string

// releaseWorkflowUniqueScriptText 保存工作流唯一键释放 Lua 脚本源码。
// 只有唯一键仍归属当前 workflow_id 时才删除，避免误删其它实例预占记录。
//
//go:embed release_workflow_unique.lua
var releaseWorkflowUniqueScriptText string

// renewLeaderScriptText 保存任务调度 leader 续租 Lua 脚本源码。
// 续租前必须比对 owner，避免当前实例误续租其它实例持有的 leader 锁。
//
//go:embed renew_leader.lua
var renewLeaderScriptText string

// releaseLeaderScriptText 保存任务调度 leader 释放 Lua 脚本源码。
// 释放前必须比对 owner，避免停止流程误删新 leader 的锁。
//
//go:embed release_leader.lua
var releaseLeaderScriptText string

// deleteExpiredArchivedTasksScriptText 保存归档失败任务过期清理 Lua 脚本源码。
// 按 archived zset 分数批量删除旧任务 hash、唯一键和 zset 成员，避免失败任务长期堆积。
//
//go:embed delete_expired_archived_tasks.lua
var deleteExpiredArchivedTasksScriptText string

var (
	// recordNodeOutcomeScript 通过 Lua 原子记录节点完成结果，避免重复 ack 或并发 shard 回写导致计数漂移。
	recordNodeOutcomeScript = redis.NewScript(embedasset.StripLeadingLineComments(recordNodeOutcomeScriptText, "--"))
	// workflowArchivedTaskRerunRepairScript 在归档失败任务被手动立即执行前，撤销该分片的失败终态计数。
	workflowArchivedTaskRerunRepairScript = redis.NewScript(embedasset.StripLeadingLineComments(workflowArchivedTaskRerunRepairScriptText, "--"))
	// releaseWorkflowUniqueScript 仅在唯一键仍归属当前工作流实例时才释放预占记录。
	releaseWorkflowUniqueScript = redis.NewScript(embedasset.StripLeadingLineComments(releaseWorkflowUniqueScriptText, "--"))
	// renewLeaderScript 仅当当前实例仍持有 leader 锁时续租，避免误续租其他实例的锁。
	renewLeaderScript = redis.NewScript(embedasset.StripLeadingLineComments(renewLeaderScriptText, "--"))
	// releaseLeaderScript 仅释放当前实例持有的 leader 锁，避免并发误删。
	releaseLeaderScript = redis.NewScript(embedasset.StripLeadingLineComments(releaseLeaderScriptText, "--"))
	// deleteExpiredArchivedTasksScript 按失败时间清理过期 archived 任务。
	deleteExpiredArchivedTasksScript = redis.NewScript(embedasset.StripLeadingLineComments(deleteExpiredArchivedTasksScriptText, "--"))
)
