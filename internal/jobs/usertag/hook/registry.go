package hook

import (
	"context"
	"strings"
	"sync"

	"admin/internal/jobs/usertag/types"

	"github.com/Is999/go-utils/errors"
)

var (
	// ErrNoHandlers 表示事件 outbox 中已有待派发事件，但当前进程未注册任何 hook。
	ErrNoHandlers = errors.New("用户标签变更事件 hook 未注册")

	// defaultRegistry 保存进程级用户标签事件 hook 注册表。
	defaultRegistry = NewRegistry()
)

// Handler 定义用户标签得失事件 hook。
type Handler interface {
	Name() string
	Handle(ctx context.Context, events []types.TagChange) error
}

// Registry 保存用户标签事件 hook 注册表。
type Registry struct {
	mu       sync.RWMutex // 保护 handlers 注册表
	handlers []Handler    // 已注册的用户标签事件处理器
}

// NewRegistry 创建空 hook 注册表。
func NewRegistry() *Registry {
	return &Registry{}
}

// DefaultRegistry 返回进程级默认 hook 注册表。
func DefaultRegistry() *Registry {
	return defaultRegistry
}

// Register 注册一个标签得失事件 hook。
func (r *Registry) Register(handler Handler) error {
	if r == nil {
		return errors.Errorf("用户标签 hook 注册表为空")
	}
	if handler == nil {
		return errors.Errorf("用户标签 hook 不能为空")
	}
	name := strings.TrimSpace(handler.Name())
	if name == "" {
		return errors.Errorf("用户标签 hook 名称不能为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.handlers {
		if strings.TrimSpace(item.Name()) == name {
			return errors.Errorf("用户标签 hook 名称重复 name=%s", name)
		}
	}
	r.handlers = append(r.handlers, handler)
	return nil
}

// Dispatch 按注册顺序派发标签得失事件。
func (r *Registry) Dispatch(ctx context.Context, events []types.TagChange) error {
	if len(events) == 0 {
		return nil
	}
	if r == nil {
		return errors.Tag(ErrNoHandlers)
	}
	r.mu.RLock()
	handlers := append([]Handler(nil), r.handlers...)
	r.mu.RUnlock()
	if len(handlers) == 0 {
		return errors.Tag(ErrNoHandlers)
	}
	for _, handler := range handlers {
		if err := handler.Handle(ctx, append([]types.TagChange(nil), events...)); err != nil {
			return errors.Wrapf(err, "用户标签 hook 派发失败 hook=%s", strings.TrimSpace(handler.Name()))
		}
	}
	return nil
}
