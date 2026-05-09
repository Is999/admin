package bootstrap

import (
	"admin/common/runtimecfg"
	"admin/internal/config"
)

// publishRuntimeConfig 发布进程级运行配置快照，供 Redis key、签名和 MFA 等跨包能力读取。
func publishRuntimeConfig(c config.Config) runtimecfg.Snapshot {
	previous := runtimecfg.Get()
	runtimecfg.Set(c)
	return previous
}

// restoreRuntimeConfig 恢复发布前的进程级运行配置快照。
func restoreRuntimeConfig(snapshot runtimecfg.Snapshot) {
	runtimecfg.Restore(snapshot)
}
