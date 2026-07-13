package hotreload

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"admin/internal/config"
	"admin/internal/svc"
)

// State 保存 config.yaml 热加载运行态资源，零值可用。
type State struct {
	configFile string       // 当前应用对应的配置文件路径
	watcher    *watcherRun  // 当前 watcher；停止完成前保持占位，阻止并发重启
	stateMu    sync.RWMutex // 保护 watcher 生命周期与配置文件绑定状态
	statusMu   sync.Mutex   // 保护热加载状态快照更新
	logMu      sync.Mutex   // 保护重复失败日志限频状态
	execMu     sync.Mutex   // 串行化实际配置重载，避免 watcher 与手动触发并发覆盖
	lastError  string       // 最近一次失败日志签名，用于重复失败限频
	lastLogAt  time.Time    // 最近一次失败日志实际输出时间
}

// watcherRun 保存单个 watcher 的取消与退出信号，避免 WaitGroup 的 Add/Wait 并发竞态。
type watcherRun struct {
	cancel context.CancelFunc // 取消当前 watcher
	done   chan struct{}      // watcher 完全退出后关闭
}

// LoadedFile 描述一次配置文件重载读取结果。
type LoadedFile struct {
	Before              config.Config // 重载前当前进程配置
	Requested           config.Config // 本次配置文件和 DB snapshot 合并后的请求配置
	CurrentFingerprint  string        // 当前配置文件包指纹
	PreviousFingerprint string        // 重载前状态中的配置指纹
	Unchanged           bool          // 配置包指纹是否未变化
}

// AppliedConfig 描述一次配置重载后的实际运行快照。
type AppliedConfig struct {
	Effective       config.Config // 当前进程实际生效的配置
	RestartRequired bool          // 是否存在需要重启才能完全生效的配置
	RestartReason   string        // 需要重启的配置边界说明
}

// LockExec 锁定配置重载执行通道。
func (s *State) LockExec() {
	if s == nil {
		return
	}
	s.execMu.Lock()
}

// UnlockExec 释放配置重载执行通道。
func (s *State) UnlockExec() {
	if s == nil {
		return
	}
	s.execMu.Unlock()
}

// SetConfigFile 绑定并返回裁剪后的配置文件路径。
func (s *State) SetConfigFile(configFile string) string {
	if s == nil {
		return ""
	}
	configFile = strings.TrimSpace(configFile)
	s.stateMu.Lock()
	s.configFile = configFile
	s.stateMu.Unlock()
	return configFile
}

// ConfigFile 返回当前绑定的配置文件路径快照。
func (s *State) ConfigFile() string {
	if s == nil {
		return ""
	}
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.configFile
}

// StartWatcher 启动热加载 watcher，并保证同一时间只有一个 watcher。
func (s *State) StartWatcher(run func(context.Context)) bool {
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

// StopWatcher 停止热加载 watcher，并受应用统一停止期限约束。
func (s *State) StopWatcher(ctx context.Context) error {
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

// WatcherRunning 返回当前是否已有热加载 watcher 在运行。
func (s *State) WatcherRunning() bool {
	if s == nil {
		return false
	}
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.watcher != nil
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

// UpdateStatus 在当前状态基础上执行原子更新。
func (s *State) UpdateStatus(svcCtx *svc.ServiceContext, mutator func(svc.HotReloadStatus) svc.HotReloadStatus) {
	if s == nil || svcCtx == nil || mutator == nil {
		return
	}
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	status := svcCtx.CurrentHotReloadStatus()
	svcCtx.UpdateHotReloadStatus(mutator(status))
}

// SuppressFailure 判断本次失败日志是否应被限频抑制。
func (s *State) SuppressFailure(errorKey string, now time.Time, window time.Duration) bool {
	if s == nil {
		return false
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()
	sameError := errorKey == s.lastError && !s.lastLogAt.IsZero() && now.Sub(s.lastLogAt) < window
	if sameError {
		s.lastError = errorKey
		return true
	}
	s.lastError = errorKey
	s.lastLogAt = now
	return false
}

// ResetFailureLog 清理重复失败限频状态。
func (s *State) ResetFailureLog() {
	if s == nil {
		return
	}
	s.logMu.Lock()
	s.lastError = ""
	s.lastLogAt = time.Time{}
	s.logMu.Unlock()
}

// Summary 生成关键配置摘要，便于管理接口和日志快速确认核心开关。
func Summary(cfg config.Config) string {
	return fmt.Sprintf(
		"mode=%s user_route=%d task=%t periodic=%d hot_reload=%t kafka=%t",
		strings.TrimSpace(cfg.Mode),
		cfg.User.RouteShardCount,
		cfg.Task.Enabled,
		len(cfg.Task.Periodic),
		cfg.HotReload.Enabled,
		cfg.Kafka.Enabled,
	)
}

// CheckInterval 统一热加载轮询间隔的最小兜底值。
func CheckInterval(seconds int) time.Duration {
	if seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 5 * time.Second
}

// Source 统一热加载触发来源，避免状态与日志出现空值。
func Source(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "unknown"
	}
	return source
}

// FailureCategory 统一失败分类，避免状态与日志出现空值。
func FailureCategory(category string) string {
	category = strings.TrimSpace(category)
	if category == "" {
		return "unknown"
	}
	return category
}
