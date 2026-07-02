package components

import (
	"context"
	"strings"

	"admin/internal/infra/loggerx"

	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/core/logx"
)

// Component 描述应用启动阶段可插拔组件。
type Component interface {
	Name() string                           // Name 返回组件名称
	Register(context.Context, *State) error // Register 注册组件依赖
}

// Func 允许通过函数快速声明启动组件。
type Func struct {
	name     string                              // 组件名称
	register func(context.Context, *State) error // 组件注册逻辑
}

// NewFunc 创建函数式启动组件。
func NewFunc(name string, register func(context.Context, *State) error) Component {
	return Func{name: strings.TrimSpace(name), register: register}
}

// Name 返回组件名称。
func (c Func) Name() string {
	return c.name
}

// Register 执行组件注册逻辑。
func (c Func) Register(ctx context.Context, state *State) error {
	if c.register == nil {
		return nil
	}
	return c.register(ctx, state)
}

// Registry 负责按固定顺序注册启动组件。
type Registry struct {
	components []Component // 待注册组件列表
}

// NewRegistry 创建启动组件注册中心。
func NewRegistry(components ...Component) *Registry {
	copied := make([]Component, 0, len(components))
	for _, component := range components {
		if component != nil {
			copied = append(copied, component)
		}
	}
	return &Registry{components: copied}
}

// Components 返回当前注册中心持有的组件副本。
func (r *Registry) Components() []Component {
	if r == nil {
		return nil
	}
	copied := make([]Component, len(r.components))
	copy(copied, r.components)
	return copied
}

// Register 按声明顺序注册组件，并校验组件名称唯一。
func (r *Registry) Register(ctx context.Context, state *State) error {
	if r == nil {
		return nil
	}
	registered := make(map[string]struct{}, len(r.components))
	for _, component := range r.components {
		if component == nil {
			continue
		}
		name := strings.TrimSpace(component.Name())
		if name == "" {
			return errors.Errorf("启动组件名称不能为空")
		}
		if _, exists := registered[name]; exists {
			return errors.Errorf("启动组件已注册: %s", name)
		}
		// 每个组件只做装配注册，后台协程统一留给 App.Start 启动。
		if err := component.Register(ctx, state); err != nil {
			return errors.Wrapf(err, "注册启动组件失败 %s", name)
		}
		registered[name] = struct{}{}
		loggerx.Infow(ctx, "启动 组件注册成功",
			logx.Field("component", name),
		)
	}
	return nil
}
