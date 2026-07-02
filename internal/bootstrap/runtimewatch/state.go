package runtimewatch

import (
	"context"
	"sync"
)

// State 保存 DB 运行配置轻量轮询资源，零值可用。
type State struct {
	cancel       context.CancelFunc // 后台轮询取消函数
	stateMu      sync.Mutex         // 保护 watcher 生命周期
	wg           sync.WaitGroup     // 等待 watcher 退出
	watcherSeq   uint64             // watcher 启动序号，避免旧协程退出时清掉新协程状态
	activeSeq    uint64             // 当前运行中的 watcher 序号
	lastVersion  uint64             // 最近一次已应用版本号
	lastChecksum string             // 最近一次已应用快照校验和
}

// Start 启动 watcher，并保证同一时间只有一个后台轮询。
func (s *State) Start(run func(context.Context)) bool {
	if s == nil || run == nil {
		return false
	}
	s.stateMu.Lock()
	if s.cancel != nil {
		s.stateMu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.watcherSeq++
	seq := s.watcherSeq
	s.cancel = cancel
	s.activeSeq = seq
	s.stateMu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.clearWatcher(seq)
		defer s.wg.Done()
		run(ctx)
	}()
	return true
}

// Stop 停止 watcher 并等待退出。
func (s *State) Stop() {
	if s == nil {
		return
	}
	s.stateMu.Lock()
	if s.cancel == nil {
		s.stateMu.Unlock()
		return
	}
	cancel := s.cancel
	s.cancel = nil
	s.activeSeq = 0
	s.stateMu.Unlock()
	cancel()
	s.wg.Wait()
}

// clearWatcher 在 watcher 自然退出后清理运行标记，允许后续重新启动。
func (s *State) clearWatcher(seq uint64) {
	s.stateMu.Lock()
	if s.activeSeq == seq {
		s.cancel = nil
		s.activeSeq = 0
	}
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
