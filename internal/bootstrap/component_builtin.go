package bootstrap

import (
	"context"
	"strings"
	"time"

	keys "admin/common/rediskeys"
	"admin/internal/config"
	"admin/internal/handler"
	"admin/internal/infra/collectorx"
	"admin/internal/infra/larkx"
	"admin/internal/infra/redisx"
	"admin/internal/requestctx"
	"admin/internal/svc"
	"admin/internal/task/queue"
	"admin/internal/task/runtime"

	"github.com/Is999/go-utils/errors"
	"github.com/hibiken/asynq"
	"github.com/zeromicro/go-zero/rest"
)

// newCollectorComponent 创建通用收集器启动组件。
func newCollectorComponent() Component {
	return NewComponentFunc(componentNameCollector, func(ctx context.Context, state *ComponentState) error {
		_ = ctx
		if state == nil || state.ServiceContext == nil {
			return errors.Errorf("通用收集器组件缺少服务上下文")
		}
		// collector 使用主库写连接处理 DB outbox，Redis Stream 在装配期统一追加 app_id 前缀。
		manager, err := collectorx.New(collectorConfigWithAppID(state.Config), state.ServiceContext.WriteDB(svc.DatabaseMain), state.ServiceContext.Rds)
		if err != nil {
			return errors.Tag(err)
		}
		state.ServiceContext.Collector = manager
		// 通用收集器的 DB/Kafka/Redis 消费循环属于 Worker 职责，API-only 不启动后台消费。
		if shouldStartWorkerLifecycle(state.Mode) {
			if err := state.AddLifecycleHooks(componentNameCollector, func(context.Context) error {
				manager.Start()
				return nil
			}, func(ctx context.Context) error {
				return errors.Tag(manager.Stop(ctx))
			}); err != nil {
				return errors.Tag(err)
			}
		}
		return nil
	})
}

// collectorConfigWithAppID 把顶层 app_id 注入 Collector Redis Stream，避免多站点共用 Redis 时串流。
func collectorConfigWithAppID(c config.Config) config.CollectorConfig {
	cfg := c.Collector
	cfg.Redis.Stream = keys.WithPrefix(cfg.Redis.Stream)
	return cfg
}

// newTaskRuntimeComponent 创建任务队列与任务运行时启动组件。
func newTaskRuntimeComponent() Component {
	return NewComponentFunc("task_runtime", func(ctx context.Context, state *ComponentState) error {
		if state == nil || state.ServiceContext == nil {
			return errors.Errorf("任务运行时组件缺少服务上下文")
		}
		if !state.Config.Task.Enabled {
			return nil
		}

		// 任务配置注入 AppID，保证队列、调度锁和幂等键按站点隔离。
		taskCfg := taskConfigWithAppID(state.Config)
		taskRedis := state.ServiceContext.Rds
		if err := validateTaskRedisConfig(taskCfg.Redis); err != nil {
			return errors.Tag(err)
		}
		if hasTaskRedisConfig(taskCfg.Redis) {
			rdb, err := redisx.New(ctx, taskCfg.Redis, state.Config.Observability)
			if err != nil {
				return errors.Tag(err)
			}
			taskRedis = rdb
			state.TaskRedis = taskRedis
			state.TaskRedisOwned = true
		} else {
			state.TaskRedis = taskRedis
		}

		manager := taskqueue.New(taskCfg, taskRedis)
		if manager == nil {
			return nil
		}
		state.ServiceContext.Task = manager
		state.TaskManager = manager

		// 任务插件统一走 bootstrap 注册清单收口，避免内置插件列表散落在不同启动文件。
		plugins := resolveTaskPlugins(state.Options)
		if err := validateNameListUnique(registrationKindTaskPlugin, pluginNames(plugins)); err != nil {
			return errors.Tag(err)
		}
		runtime, err := taskruntime.Register(state.ServiceContext, manager, plugins...)
		if err != nil {
			return errors.Tag(err)
		}
		state.TaskRuntime = runtime
		if err := registerTaskFailureLarkAlert(state.Config, manager); err != nil {
			return errors.Tag(err)
		}
		if shouldStartTaskLifecycle(state.Mode) {
			// 启动期校验 workflow 注册完整性，避免配置漂移后反复失败。
			if err := manager.ValidatePeriodicTaskDefinitions(); err != nil {
				return errors.Tag(err)
			}
		}
		// 任务后台生命周期只在 Worker 或 Scheduler 模式启动；API-only 仅保留任务投递和查询能力。
		if shouldStartTaskLifecycle(state.Mode) {
			if err := state.AddLifecycleHooks(componentNameTaskRuntime, func(context.Context) error {
				if hasMode(state.Mode, ModeWorker) {
					if err := manager.StartWorker(); err != nil {
						return errors.Wrap(err, "启动任务 worker 失败")
					}
				}
				if hasMode(state.Mode, ModeScheduler) {
					if err := manager.StartScheduler(); err != nil {
						return errors.Wrap(err, "启动任务调度器失败")
					}
				}
				return nil
			}, func(ctx context.Context) error {
				return errors.Tag(manager.Stop(ctx))
			}); err != nil {
				return errors.Tag(err)
			}
		}
		return nil
	})
}

// registerTaskFailureLarkAlert 注册后台任务终态失败 Lark 预警。
func registerTaskFailureLarkAlert(cfg config.Config, manager *taskqueue.Manager) error {
	if manager == nil {
		return nil
	}
	notifier, err := larkx.New(cfg.Alert.Lark)
	if err != nil {
		return errors.Wrap(err, "初始化 Lark 告警失败")
	}
	if notifier == nil {
		return nil
	}
	return manager.RegisterFinalFailureHook(func(ctx context.Context, task *asynq.Task, meta taskqueue.WorkflowTaskMeta, runErr error) error {
		if err := notifier.SendTaskFailure(taskFailureAlertSendContext(ctx), buildTaskFailureAlert(cfg, ctx, task, meta, runErr)); err != nil {
			return errors.Wrap(err, "发送任务失败 Lark 告警失败")
		}
		return nil
	})
}

// taskFailureAlertSendContext 让外部告警使用自身 HTTP 超时，不继承任务收尾写入 deadline。
func taskFailureAlertSendContext(parent context.Context) context.Context {
	ctx := context.Background()
	if meta := requestctx.FromContext(parent); meta != nil {
		metaCopy := *meta
		ctx = requestctx.WithMeta(ctx, &metaCopy)
	}
	return ctx
}

// buildTaskFailureAlert 从任务上下文和工作流元数据中提取 Lark 告警字段。
func buildTaskFailureAlert(cfg config.Config, ctx context.Context, task *asynq.Task, workflowMeta taskqueue.WorkflowTaskMeta, runErr error) larkx.TaskFailureAlert {
	meta := requestctx.FromContext(ctx)
	alert := larkx.TaskFailureAlert{
		ServiceName:  strings.TrimSpace(cfg.Observability.ServiceName),
		Environment:  strings.TrimSpace(cfg.Observability.Environment),
		AppID:        strings.TrimSpace(cfg.AppID),
		TaskType:     taskTypeOf(task),
		TaskName:     taskqueue.TaskDisplayName(task),
		TaskSource:   taskqueue.TaskSource(task),
		WorkflowID:   strings.TrimSpace(workflowMeta.WorkflowID),
		WorkflowName: strings.TrimSpace(workflowMeta.WorkflowName),
		WorkflowNode: strings.TrimSpace(workflowMeta.WorkflowNode),
		ShardIndex:   workflowMeta.ShardIndex,
		ShardTotal:   workflowMeta.ShardTotal,
		OccurredAt:   time.Now(),
		Err:          runErr,
	}
	if alert.ServiceName == "" {
		alert.ServiceName = strings.TrimSpace(cfg.Name)
	}
	if alert.Environment == "" {
		alert.Environment = strings.TrimSpace(cfg.Mode)
	}
	if meta != nil {
		alert.TaskID = strings.TrimSpace(meta.TaskID)
		alert.TaskQueue = strings.TrimSpace(meta.TaskQueue)
		alert.Mode = strings.TrimSpace(meta.Mode)
		alert.TraceID = strings.TrimSpace(meta.TraceID)
		if alert.TaskName == "" {
			alert.TaskName = strings.TrimSpace(meta.TaskName)
		}
		if alert.WorkflowID == "" {
			alert.WorkflowID = strings.TrimSpace(meta.WorkflowID)
		}
		if alert.WorkflowName == "" {
			alert.WorkflowName = strings.TrimSpace(meta.WorkflowName)
		}
		if alert.WorkflowNode == "" {
			alert.WorkflowNode = strings.TrimSpace(meta.WorkflowNode)
		}
		if alert.ShardTotal == 0 {
			alert.ShardIndex = meta.ShardIndex
			alert.ShardTotal = meta.ShardTotal
		}
	}
	return alert
}

// taskTypeOf 安全读取 Asynq 任务类型，兼容空任务指针。
func taskTypeOf(task *asynq.Task) string {
	if task == nil {
		return ""
	}
	return strings.TrimSpace(task.Type())
}

// newHTTPServerComponent 创建 HTTP 服务与路由启动组件。
func newHTTPServerComponent() Component {
	return NewComponentFunc("http_server", func(ctx context.Context, state *ComponentState) error {
		_ = ctx
		if state == nil || state.ServiceContext == nil {
			return errors.Errorf("HTTP 服务组件缺少服务上下文")
		}
		if !hasMode(state.Mode, ModeAPI) {
			return nil
		}

		restConf := state.Config.RestConf
		// 项目已接入自定义 access log 中间件，关闭 go-zero 默认 HTTP 日志，避免高频路由产生双份日志。
		restConf.Middlewares.Log = false
		server, err := rest.NewServer(restConf)
		if err != nil {
			return errors.Wrapf(err, "创建 HTTP 服务失败 host=%s port=%d", restConf.Host, restConf.Port)
		}
		// HTTP 路由模块从 bootstrap 统一清单解析，handler 只负责按给定模块完成注册。
		routeModules := resolveRouteModules(state.Options)
		if err := validateNameListUnique(registrationKindRoute, routeModuleNames(routeModules)); err != nil {
			return errors.Tag(err)
		}
		handler.RegisterHandlersWithModules(server, state.ServiceContext, routeModules...)
		state.Server = server
		return nil
	})
}
