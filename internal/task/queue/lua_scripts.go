package taskqueue

import (
	_ "embed"

	"admin/common/embedasset"

	"github.com/redis/go-redis/v9"
)

// recordNodeOutcomeScriptText 保存节点分片终态记录 Lua 脚本源码。
// Redis Cluster 下脚本只操作 workflowNodeKey 一个 hash，避免跨 slot 计数漂移。
//
//go:embed assets/record_node_outcome.lua
var recordNodeOutcomeScriptText string

// workflowArchivedTaskRerunRepairScriptText 保存归档任务重跑前修复节点失败计数的 Lua 脚本源码。
// 手动立即执行归档失败任务前会把旧失败标记改为可重入准备态，后续任务完成仍走节点结果记录脚本。
//
//go:embed assets/workflow_archived_task_rerun_repair.lua
var workflowArchivedTaskRerunRepairScriptText string

// transitionWorkflowStatusScriptText 保存工作流终态原子迁移 Lua 脚本源码。
// success/failed 一旦落库便不能被相反的迟到回执覆盖。
//
//go:embed assets/transition_workflow_status.lua
var transitionWorkflowStatusScriptText string

// reopenWorkflowForManualRerunScriptText 保存手工重跑前重开工作流状态的 Lua 脚本源码。
// 只允许 failed 重开，success 始终保持终态。
//
//go:embed assets/reopen_workflow_for_manual_rerun.lua
var reopenWorkflowForManualRerunScriptText string

// releaseWorkflowUniqueScriptText 保存工作流唯一键释放 Lua 脚本源码。
// 只有唯一键仍归属当前 workflow_id 时才删除，避免误删其它实例预占记录。
//
//go:embed assets/release_workflow_unique.lua
var releaseWorkflowUniqueScriptText string

// ensureWorkflowUniqueScriptText 保存工作流唯一键确认与续期 Lua 脚本源码。
// owner 校验、同 owner 续期和空 key 抢占必须在一个 Redis 命令内完成。
//
//go:embed assets/ensure_workflow_unique.lua
var ensureWorkflowUniqueScriptText string

// finishTaskRuntimeScriptText 保存任务 attempt 终态回写 Lua 脚本源码。
// 只有 hash 中 attemptToken 仍匹配时才会清理旧字段、写新终态并续期。
//
//go:embed assets/finish_task_runtime.lua
var finishTaskRuntimeScriptText string

// renewLeaderScriptText 保存任务调度 leader 续租 Lua 脚本源码。
// 续租前必须比对 owner，避免当前实例误续租其它实例持有的 leader 锁。
//
//go:embed assets/renew_leader.lua
var renewLeaderScriptText string

// releaseLeaderScriptText 保存任务调度 leader 释放 Lua 脚本源码。
// 释放前必须比对 owner，避免停止流程误删新 leader 的锁。
//
//go:embed assets/release_leader.lua
var releaseLeaderScriptText string

// deleteExpiredArchivedTasksScriptText 保存归档失败任务过期清理 Lua 脚本源码。
// 按 archived zset 分数批量删除过期任务 hash、唯一键和 zset 成员，避免失败任务长期堆积。
//
//go:embed assets/delete_expired_archived_tasks.lua
var deleteExpiredArchivedTasksScriptText string

var (
	// recordNodeOutcomeScript 通过 Lua 原子记录节点完成结果，避免重复 ack 或并发 shard 回写导致计数漂移。
	recordNodeOutcomeScript = redis.NewScript(embedasset.StripLeadingLineComments(recordNodeOutcomeScriptText, "--"))
	// workflowArchivedTaskRerunRepairScript 在归档失败任务被手动立即执行前，把该分片原子改为可重入准备态。
	workflowArchivedTaskRerunRepairScript = redis.NewScript(embedasset.StripLeadingLineComments(workflowArchivedTaskRerunRepairScriptText, "--"))
	// transitionWorkflowStatusScript 原子写入工作流终态，阻止晚到回执反向覆盖。
	transitionWorkflowStatusScript = redis.NewScript(embedasset.StripLeadingLineComments(transitionWorkflowStatusScriptText, "--"))
	// reopenWorkflowForManualRerunScript 原子重开手工重跑窗口，支持同一失败实例逐分片修复。
	reopenWorkflowForManualRerunScript = redis.NewScript(embedasset.StripLeadingLineComments(reopenWorkflowForManualRerunScriptText, "--"))
	// releaseWorkflowUniqueScript 仅在唯一键仍归属当前工作流实例时才释放预占记录。
	releaseWorkflowUniqueScript = redis.NewScript(embedasset.StripLeadingLineComments(releaseWorkflowUniqueScriptText, "--"))
	// ensureWorkflowUniqueScript 原子确认、续期或抢占唯一键，避免旧 owner 给新 owner 误续期。
	ensureWorkflowUniqueScript = redis.NewScript(embedasset.StripLeadingLineComments(ensureWorkflowUniqueScriptText, "--"))
	// finishTaskRuntimeScript 仅允许当前 attempt 原子回写终态快照。
	finishTaskRuntimeScript = redis.NewScript(embedasset.StripLeadingLineComments(finishTaskRuntimeScriptText, "--"))
	// renewLeaderScript 仅当当前实例仍持有 leader 锁时续租，避免误续租其他实例的锁。
	renewLeaderScript = redis.NewScript(embedasset.StripLeadingLineComments(renewLeaderScriptText, "--"))
	// releaseLeaderScript 仅释放当前实例持有的 leader 锁，避免并发误删。
	releaseLeaderScript = redis.NewScript(embedasset.StripLeadingLineComments(releaseLeaderScriptText, "--"))
	// deleteExpiredArchivedTasksScript 按失败时间清理过期 archived 任务。
	deleteExpiredArchivedTasksScript = redis.NewScript(embedasset.StripLeadingLineComments(deleteExpiredArchivedTasksScriptText, "--"))
)
