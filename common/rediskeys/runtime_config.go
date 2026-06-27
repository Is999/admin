package keys

import "fmt"

// RuntimeConfigStateKey 返回运行配置状态缓存逻辑 key。
func RuntimeConfigStateKey() string {
	return "runtime_config:state"
}

// RuntimeConfigReleaseKey 返回指定发布快照的缓存逻辑 key。
func RuntimeConfigReleaseKey(releaseID uint64) string {
	return fmt.Sprintf("runtime_config:release:%d", releaseID)
}
