package bootstrap

import (
	"context"

	"admin/internal/bootstrap/configload"
	"admin/internal/bootstrap/runmode"
	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
)

// LoadConfig 读取并校验启动配置，保持 bootstrap 对外装配入口稳定。
func LoadConfig(file string) (config.Config, error) {
	return configload.Load(file)
}

// Wire 作为应用装配入口，统一负责读取配置并构建 App。
// main 入口或其他部署形态只需要决定配置文件、运行模式和扩展选项。
func Wire(ctx context.Context, configFile string, mode int, options ...Option) (*App, error) {
	cfg, err := LoadConfig(configFile)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return newBoundApp(ctx, cfg, configFile, mode, options...)
}

// WireWithConfigMode 先读取配置，再按“命令行优先、配置兜底”的规则决定最终启动模式。
func WireWithConfigMode(ctx context.Context, configFile string, cliMode *int, options ...Option) (*App, error) {
	cfg, err := LoadConfig(configFile)
	if err != nil {
		return nil, errors.Tag(err)
	}
	mode, err := runmode.Resolve(cliMode, cfg.RunMode)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return newBoundApp(ctx, cfg, configFile, mode, options...)
}

// newBoundApp 创建应用并绑定配置文件路径，供标准装配入口复用。
func newBoundApp(ctx context.Context, cfg config.Config, configFile string, mode int, options ...Option) (*App, error) {
	app, err := New(ctx, cfg, mode, options...)
	if err != nil {
		return nil, errors.Tag(err)
	}
	app.BindConfigFile(configFile)
	return app, nil
}
