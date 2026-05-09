package runtimecfg

import (
	"strings"
	"sync/atomic"

	"admin/internal/config"
)

// Snapshot 保存当前进程可全局读取的轻量运行配置。
type Snapshot struct {
	AppID string // 当前应用唯一标识，用于 Redis key、签名、MFA issuer 等站点隔离场景
}

// current 保存当前进程运行配置快照。
var current atomic.Value

// Set 从应用配置中提取运行期公共配置并原子替换当前快照。
func Set(cfg config.Config) {
	current.Store(snapshotFromConfig(cfg))
}

// snapshotFromConfig 提取运行期需要跨包读取的配置字段。
func snapshotFromConfig(cfg config.Config) Snapshot {
	return Snapshot{
		AppID: strings.TrimSpace(cfg.AppID),
	}
}

// Get 返回当前进程运行配置快照。
func Get() Snapshot {
	cfg, _ := current.Load().(Snapshot)
	return cfg
}

// Restore 原子恢复已保存的运行配置快照。
func Restore(snapshot Snapshot) {
	current.Store(snapshot)
}

// AppID 返回当前应用唯一标识。
func AppID() string {
	return Get().AppID
}
