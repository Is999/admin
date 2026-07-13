package runtimewatch

import (
	"context"
	"sync"
)

// State 保存 DB 运行配置轻量轮询资源，零值可用。
type State struct {
	watcher      *watcherRun // 当前 watcher；停止完成前保持占位，阻止并发重启
	stateMu      sync.Mutex  // 保护 watcher 生命周期
	lastVersion  uint64      // 最近一次已应用版本号
	lastChecksum string      // 最近一次已应用快照校验和
}

// watcherRun 保存单个 watcher 的取消与退出信号，避免 WaitGroup 的 Add/Wait 并发竞态。
type watcherRun struct {
	cancel context.CancelFunc // 取消当前 watcher
	done   chan struct{}      // watcher 完全退出后关闭
}

// Start 启动 watcher，并保证同一时间只有一个后台轮询。
func (s *State) Start(run func(context.Context)) bool {
	if s == nil || run == nil {
		return false
	}
	s.stateMu.Lock()
	if s.watcher != nil {
		s.stateMu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	watcher := &watcherRun{cancel: cancel, done: make(chan struct{})}
	s.watcher = watcher
	s.stateMu.Unlock()
	go func() {
		defer s.clearWatcher(watcher)
		run(ctx)
	}()
	return true
}

// Stop 停止 watcher，并受应用统一停止期限约束。
func (s *State) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.stateMu.Lock()
	watcher := s.watcher
	if watcher == nil {
		s.stateMu.Unlock()
		return nil
	}
	watcher.cancel()
	s.stateMu.Unlock()
	select {
	case <-watcher.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// clearWatcher 在 watcher 自然退出后清理运行标记，允许后续重新启动。
func (s *State) clearWatcher(watcher *watcherRun) {
	s.stateMu.Lock()
	if s.watcher == watcher {
		s.watcher = nil
	}
	close(watcher.done)
	s.stateMu.Unlock()
}

// MarkApplied 记录最近一次已应用的 active release 水位。
func (s *State) MarkApplied(version uint64, checksum string) {
	if s == nil {
		return
	}
	s.stateMu.Lock()
	s.lastVersion = version
	s.lastChecksum = checksum
	s.stateMu.Unlock()
}

// Applied 判断指定 active release 水位是否已经应用。
func (s *State) Applied(version uint64, checksum string) bool {
	if s == nil {
		return false
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.lastVersion == version && s.lastChecksum == checksum
}
