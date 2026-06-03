package taskqueue

import (
	"strings"
	"time"

	"admin/helper"
	"admin/internal/config"
	"admin/internal/types"
)

// schedulerRuntimeStatus 保存调度器运行期状态快照。
// 该结构只在 taskqueue 内部使用，对外统一转换为 types.TaskSchedulerItem。
type schedulerRuntimeStatus struct {
	Enabled                  bool      // 配置中是否启用调度器
	Running                  bool      // 当前进程是否已启动 leader 选举循环
	HasLeader                bool      // 当前进程是否持有 leader
	InstanceID               string    // 当前进程实例 ID
	LeaderLockKey            string    // leader 锁 key
	LeaseTTLSeconds          int       // leader 锁租约时长（秒）
	RenewIntervalSeconds     int       // leader 锁续租间隔（秒）
	SyncIntervalSeconds      int       // 周期任务配置同步间隔（秒）
	HeartbeatIntervalSeconds int       // 调度器心跳间隔（秒）
	PeriodicTaskCount        int       // 当前有效周期任务数量
	LastStatus               string    // 最近一次调度器总体状态
	LastMessage              string    // 最近一次调度器总体状态说明
	LastStartedAt            time.Time // 最近一次启动 leader 选举时间
	LastHeartbeatAt          time.Time // 最近一次心跳上报时间
	LastAcquireAt            time.Time // 最近一次获取 leader 时间
	LastReleaseAt            time.Time // 最近一次释放或丢失 leader 时间
	LastSyncAt               time.Time // 最近一次同步周期配置时间
	LastSyncStatus           string    // 最近一次同步结果：success/failed
	LastSyncMessage          string    // 最近一次同步结果说明
	LastEnqueueAt            time.Time // 最近一次周期任务入队时间
	LastEnqueueTaskName      string    // 最近一次入队的任务名称
	LastEnqueueTaskType      string    // 最近一次入队的任务类型
	LastEnqueueErrorAt       time.Time // 最近一次入队失败时间
	LastEnqueueErrorMessage  string    // 最近一次入队失败原因
}

// currentSchedulerStatus 返回当前调度器状态快照。
func (m *Manager) currentSchedulerStatus() schedulerRuntimeStatus {
	if m == nil {
		return schedulerRuntimeStatus{}
	}
	if status, ok := m.schedulerStatusValue.Load().(schedulerRuntimeStatus); ok {
		return status
	}
	return schedulerRuntimeStatus{}
}

// refreshSchedulerStatus 在当前调度器状态基础上执行原子更新。
func (m *Manager) refreshSchedulerStatus(mutator func(schedulerRuntimeStatus) schedulerRuntimeStatus) {
	if m == nil || mutator == nil {
		return
	}
	m.schedulerStatusMu.Lock()
	defer m.schedulerStatusMu.Unlock()
	status := m.currentSchedulerStatus()
	m.schedulerStatusValue.Store(mutator(status))
}

// syncSchedulerConfigStatus 把当前配置快照中的调度器参数同步到状态快照，便于接口直接返回运行期视图。
func (m *Manager) syncSchedulerConfigStatus(cfg config.TaskQueueConfig) {
	if m == nil {
		return
	}
	m.refreshSchedulerStatus(func(status schedulerRuntimeStatus) schedulerRuntimeStatus {
		status.Enabled = cfg.Scheduler.Enabled
		status.InstanceID = m.instance
		status.LeaderLockKey = m.schedulerLeaseKeyByConfig(cfg)
		status.LeaseTTLSeconds = int(m.schedulerLeaseTTLByConfig(cfg) / time.Second)
		status.RenewIntervalSeconds = int(m.schedulerRenewIntervalByConfig(cfg) / time.Second)
		status.SyncIntervalSeconds = int(m.schedulerSyncIntervalByConfig(cfg) / time.Second)
		status.HeartbeatIntervalSeconds = int(m.schedulerHeartbeatIntervalByConfig(cfg) / time.Second)
		status.PeriodicTaskCount = len(dedupePeriodicTasks(enabledPeriodicTasks(append([]config.TaskPeriodicConfig(nil), cfg.Periodic...))))
		if strings.TrimSpace(status.LastStatus) != "" {
			return status
		}
		if !cfg.Scheduler.Enabled {
			status.LastStatus = "disabled"
			status.LastMessage = "调度器开关未开启"
			return status
		}
		status.LastStatus = "idle"
		status.LastMessage = "调度器尚未启动"
		return status
	})
}

// markSchedulerWaitingLeader 记录当前进程已进入 leader 竞争状态。
func (m *Manager) markSchedulerWaitingLeader(message string) {
	now := time.Now()
	m.refreshSchedulerStatus(func(status schedulerRuntimeStatus) schedulerRuntimeStatus {
		status.Running = true
		status.HasLeader = false
		status.LastStartedAt = now
		status.LastStatus = "waiting_leader"
		status.LastMessage = helper.FirstNonEmptyString(strings.TrimSpace(message), "调度器 leader 选举已启动")
		return status
	})
}

// markSchedulerStopped 记录调度器已停止。
func (m *Manager) markSchedulerStopped(message string) {
	now := time.Now()
	m.refreshSchedulerStatus(func(status schedulerRuntimeStatus) schedulerRuntimeStatus {
		status.Running = false
		status.HasLeader = false
		status.LastReleaseAt = now
		status.LastStatus = "stopped"
		status.LastMessage = helper.FirstNonEmptyString(strings.TrimSpace(message), "调度器已停止")
		return status
	})
}

// markSchedulerLeaderAcquired 记录当前进程已获取 leader。
func (m *Manager) markSchedulerLeaderAcquired() {
	now := time.Now()
	m.refreshSchedulerStatus(func(status schedulerRuntimeStatus) schedulerRuntimeStatus {
		status.Running = true
		status.HasLeader = true
		status.LastAcquireAt = now
		status.LastStatus = "leader"
		status.LastMessage = "调度器已获取 leader"
		return status
	})
}

// markSchedulerLeaderReleased 记录当前进程已释放或丢失 leader。
func (m *Manager) markSchedulerLeaderReleased(message string) {
	now := time.Now()
	m.refreshSchedulerStatus(func(status schedulerRuntimeStatus) schedulerRuntimeStatus {
		status.HasLeader = false
		status.LastReleaseAt = now
		if status.Running {
			status.LastStatus = "waiting_leader"
			status.LastMessage = helper.FirstNonEmptyString(strings.TrimSpace(message), "调度器已释放 leader，等待重新竞争")
			return status
		}
		status.LastStatus = "stopped"
		status.LastMessage = helper.FirstNonEmptyString(strings.TrimSpace(message), "调度器已停止")
		return status
	})
}

// markSchedulerSyncSuccess 记录最近一次周期配置同步成功。
func (m *Manager) markSchedulerSyncSuccess(periodicCount int) {
	now := time.Now()
	m.refreshSchedulerStatus(func(status schedulerRuntimeStatus) schedulerRuntimeStatus {
		status.PeriodicTaskCount = periodicCount
		status.LastSyncAt = now
		status.LastSyncStatus = "success"
		status.LastSyncMessage = "周期任务配置同步成功"
		return status
	})
}

// markSchedulerSyncFailure 记录最近一次周期配置同步失败。
func (m *Manager) markSchedulerSyncFailure(message string) {
	now := time.Now()
	m.refreshSchedulerStatus(func(status schedulerRuntimeStatus) schedulerRuntimeStatus {
		status.LastSyncAt = now
		status.LastSyncStatus = "failed"
		status.LastSyncMessage = helper.FirstNonEmptyString(strings.TrimSpace(message), "周期任务配置同步失败")
		return status
	})
}

// markSchedulerHeartbeat 记录最近一次调度器心跳时间。
func (m *Manager) markSchedulerHeartbeat() {
	now := time.Now()
	m.refreshSchedulerStatus(func(status schedulerRuntimeStatus) schedulerRuntimeStatus {
		status.LastHeartbeatAt = now
		if status.HasLeader {
			status.LastStatus = "leader"
			status.LastMessage = "调度器 leader 心跳正常"
		}
		return status
	})
}

// markSchedulerEnqueueSuccess 记录最近一次周期任务入队成功。
func (m *Manager) markSchedulerEnqueueSuccess(taskName, taskType string) {
	now := time.Now()
	m.refreshSchedulerStatus(func(status schedulerRuntimeStatus) schedulerRuntimeStatus {
		status.LastEnqueueAt = now
		status.LastEnqueueTaskName = strings.TrimSpace(taskName)
		status.LastEnqueueTaskType = strings.TrimSpace(taskType)
		status.LastEnqueueErrorAt = time.Time{}
		status.LastEnqueueErrorMessage = ""
		return status
	})
}

// markSchedulerEnqueueFailure 记录最近一次周期任务入队失败。
func (m *Manager) markSchedulerEnqueueFailure(taskName, taskType, message string) {
	now := time.Now()
	m.refreshSchedulerStatus(func(status schedulerRuntimeStatus) schedulerRuntimeStatus {
		status.LastEnqueueTaskName = strings.TrimSpace(taskName)
		status.LastEnqueueTaskType = strings.TrimSpace(taskType)
		status.LastEnqueueErrorAt = now
		status.LastEnqueueErrorMessage = helper.FirstNonEmptyString(strings.TrimSpace(message), "周期任务入队失败")
		return status
	})
}

// schedulerStatusSnapshot 把内部调度器状态转换成接口返回结构。
func (m *Manager) schedulerStatusSnapshot() *types.TaskSchedulerItem {
	if m == nil {
		return nil
	}
	status := m.currentSchedulerStatus()
	return &types.TaskSchedulerItem{
		Enabled:                  status.Enabled,
		Running:                  status.Running,
		HasLeader:                status.HasLeader,
		InstanceID:               status.InstanceID,
		LeaderLockKey:            status.LeaderLockKey,
		LeaseTTLSeconds:          status.LeaseTTLSeconds,
		RenewIntervalSeconds:     status.RenewIntervalSeconds,
		SyncIntervalSeconds:      status.SyncIntervalSeconds,
		HeartbeatIntervalSeconds: status.HeartbeatIntervalSeconds,
		PeriodicTaskCount:        status.PeriodicTaskCount,
		LastStatus:               status.LastStatus,
		LastMessage:              status.LastMessage,
		LastStartedAt:            formatSchedulerStatusTime(status.LastStartedAt),
		LastHeartbeatAt:          formatSchedulerStatusTime(status.LastHeartbeatAt),
		LastAcquireAt:            formatSchedulerStatusTime(status.LastAcquireAt),
		LastReleaseAt:            formatSchedulerStatusTime(status.LastReleaseAt),
		LastSyncAt:               formatSchedulerStatusTime(status.LastSyncAt),
		LastSyncStatus:           status.LastSyncStatus,
		LastSyncMessage:          status.LastSyncMessage,
		LastEnqueueAt:            formatSchedulerStatusTime(status.LastEnqueueAt),
		LastEnqueueTaskName:      status.LastEnqueueTaskName,
		LastEnqueueTaskType:      status.LastEnqueueTaskType,
		LastEnqueueErrorAt:       formatSchedulerStatusTime(status.LastEnqueueErrorAt),
		LastEnqueueErrorMessage:  status.LastEnqueueErrorMessage,
	}
}

// formatSchedulerStatusTime 把零值时间转换为空串，避免接口返回 Go 零时间。
func formatSchedulerStatusTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}
