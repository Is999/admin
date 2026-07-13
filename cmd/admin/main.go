package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"admin/internal/bootstrap"
	"admin/internal/infra/loggerx"

	"github.com/zeromicro/go-zero/core/proc"
)

const (
	// shutdownTimeout 为 HTTP 排空后的后台任务和基础设施关闭预留时间。
	shutdownTimeout = 20 * time.Second
	// forceQuitTimeout 必须晚于应用停止期限，并早于容器默认 30 秒终止宽限期。
	forceQuitTimeout = 29 * time.Second
)

// configFile 支持通过 -f 指定配置文件，便于区分本地、测试和线上环境。
var configFile = flag.String("f", "./etc/config.yaml", "the config file")

// buildVersion 由构建阶段通过 -ldflags 注入，用于发布排查。
var buildVersion = "dev"

// showVersion 控制是否只输出二进制版本并退出。
var showVersion = flag.Bool("version", false, "print build version and exit")

// runMode 使用位掩码控制启动模块：
// 1=api, 2=worker, 4=scheduler，支持组合（3/5/6/7）。
// 未显式传入时会回退到 config.yaml 的 `run_mode`；若配置中也未设置，则默认使用 7。
var runMode = flag.Int("mode", 0, "run mode bitmask: 1=api,2=worker,4=scheduler,3/5/6 combination,7=all; 0 means use config.run_mode or fallback to 7")

// resolveExplicitRunMode 只在命令行显式传入 `-mode` 时返回该参数。
// 这样才能区分“运维明确指定了 mode”和“未传 mode、应回退到 config.run_mode”两种场景。
func resolveExplicitRunMode(flagSet *flag.FlagSet, mode *int) *int {
	if flagSet == nil || mode == nil {
		return nil
	}
	explicit := false
	flagSet.Visit(func(f *flag.Flag) {
		if f.Name == "mode" {
			explicit = true
		}
	})
	if !explicit {
		return nil
	}
	resolvedMode := *mode
	return &resolvedMode
}

// main 负责解析命令行参数并交给 runApp 控制进程退出码。
func main() {
	flag.Parse()
	if *showVersion {
		fmt.Println(buildVersion)
		return
	}
	os.Exit(runApp(context.Background(), *configFile, resolveExplicitRunMode(flag.CommandLine, runMode)))
}

// runApp 执行应用装配、启动和停止，并返回进程退出码。
func runApp(ctx context.Context, configFile string, explicitMode *int) int {
	if ctx == nil {
		ctx = context.Background()
	}
	proc.SetTimeToForceQuit(forceQuitTimeout)
	// 入口只保留参数解析和生命周期控制；`-mode` 未传时回退到配置文件中的 `run_mode`。
	app, err := bootstrap.WireWithConfigMode(ctx, configFile, explicitMode)
	if err != nil {
		loggerx.Errorw(ctx, "应用启动装配失败", err)
		return 1
	}
	defer func() {
		// 退出时统一关闭 server、tracer provider 等资源，避免后台批量上报丢失。
		// 使用带超时的 Context 避免因队列断连等极端情况导致进程无限挂起。
		stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := app.Stop(stopCtx); err != nil {
			loggerx.Errorw(stopCtx, "应用停止失败", err)
		}
	}()

	if err = app.Start(); err != nil {
		loggerx.Errorw(ctx, "应用启动失败", err)
		return 1
	}
	return 0
}
