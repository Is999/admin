package keys

import "fmt"

// RuntimeConfigStateKey 返回指定环境的运行配置状态缓存逻辑 key。
func RuntimeConfigStateKey(env string) string {
	return "runtime_config:state:" + env
}

// RuntimeConfigReleaseKey 返回指定发布快照的缓存逻辑 key。
func RuntimeConfigReleaseKey(releaseID uint64) string {
	return fmt.Sprintf("runtime_config:release:%d", releaseID)
}
