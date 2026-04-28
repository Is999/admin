package health

import (
	corelogic "admin/internal/logic"
	"context"
	"database/sql"
	"os"
	"sort"
	"strings"
	"time"

	codes "admin/common/codes"
	"admin/internal/config"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

const (
	// healthCheckTimeout 表示单次依赖探测最大耗时，避免 ready 接口被慢依赖拖死。
	healthCheckTimeout = 2 * time.Second
	// healthStatusOK 表示依赖检查成功。
	healthStatusOK = "ok"
	// healthStatusError 表示依赖检查失败。
	healthStatusError = "error"
	// healthStatusSkipped 表示依赖未启用或无需检查。
	healthStatusSkipped = "skipped"
)

// HealthLogic 负责 live/ready 健康检查，handler 只做响应写出。
type HealthLogic struct {
	*corelogic.BaseLogic // BaseLogic 提供统一上下文、日志和 ServiceContext 访问能力。
}

// NewHealthLogic 创建健康检查 logic。
func NewHealthLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HealthLogic {
	return &HealthLogic{BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx)}
}

// Liveness 返回进程存活状态，不访问外部依赖，适合 Kubernetes livenessProbe。
func (l *HealthLogic) Liveness() *types.HealthStatusResp {
	return &types.HealthStatusResp{
		Status:  healthStatusOK,
		Mode:    runModeName(l.currentConfig().RunMode),
		Node:    hostName(),
		Version: l.currentVersion(),
	}
}

// Readiness 检查核心依赖是否可用，适合 Kubernetes readinessProbe 和发布流量切换。
func (l *HealthLogic) Readiness(ctx context.Context) (*types.HealthStatusResp, error) {
	cfg := l.currentConfig()
	statuses := make([]types.HealthDependencyStatus, 0, 9)
	var firstErr error

	appendStatus := func(status types.HealthDependencyStatus, err error) {
		statuses = append(statuses, status)
		if err != nil && firstErr == nil {
			firstErr = errors.Tag(err)
		}
	}

	if l.service() == nil {
		appendStatus(dependencyError("service_context", codes.DependencyUnavailable, errors.New("ServiceContext未初始化")))
	} else {
		appendStatus(l.checkGormDB(ctx, "mysql", l.service().SiteDBs.MainDB, codes.MySQLUnavailable))
		names := make([]string, 0, len(l.service().SiteDBs.NamedDBs))
		for name := range l.service().SiteDBs.NamedDBs {
			names = append(names, string(name))
		}
		sort.Strings(names)
		for _, name := range names {
			db := l.service().SiteDBs.NamedDBs[svc.DbName(name)]
			appendStatus(l.checkGormDB(ctx, "mysql_"+string(name), db, codes.MySQLUnavailable))
		}
	}
	appendStatus(l.checkRedis(ctx))
	appendStatus(l.checkKafka(cfg.Kafka.Enabled))
	appendStatus(l.checkTaskQueue(cfg.Task.Enabled))
	appendStatus(l.checkCollector(cfg.Collector.Enabled))

	resp := &types.HealthStatusResp{
		Status:       healthStatusOK,
		Mode:         runModeName(cfg.RunMode),
		Node:         hostName(),
		Version:      l.currentVersion(),
		Dependencies: statuses,
	}
	if firstErr != nil {
		resp.Status = healthStatusError
		return resp, firstErr
	}
	return resp, nil
}

// currentConfig 读取当前运行配置，空 ServiceContext 时返回零值配置。
func (l *HealthLogic) currentConfig() config.Config {
	if l == nil || l.service() == nil {
		return config.Config{}
	}
	return l.service().CurrentConfig()
}

// currentVersion 返回配置热加载版本，未启用时返回 unknown。
func (l *HealthLogic) currentVersion() string {
	if l == nil || l.service() == nil {
		return "unknown"
	}
	version := strings.TrimSpace(l.service().CurrentHotReloadStatus().ConfigVersion)
	if version == "" {
		return "unknown"
	}
	return version
}

// service 返回当前 logic 绑定的服务上下文。
func (l *HealthLogic) service() *svc.ServiceContext {
	if l == nil || l.Svc == nil {
		return nil
	}
	return l.Svc
}

// checkGormDB 检查单个 GORM 数据库连接。
func (l *HealthLogic) checkGormDB(ctx context.Context, name string, db *gorm.DB, code int) (types.HealthDependencyStatus, error) {
	if db == nil {
		return dependencyError(name, code, errors.New("数据库连接未初始化"))
	}
	sqlDB, err := db.DB()
	if err != nil {
		return dependencyError(name, code, errors.Wrap(err, "数据库连接池不可用"))
	}
	return checkSQLDB(ctx, name, sqlDB, code)
}

// checkRedis 检查 Redis 连接。
func (l *HealthLogic) checkRedis(ctx context.Context) (types.HealthDependencyStatus, error) {
	if l.service() == nil || l.service().Rds == nil {
		return dependencyError("redis", codes.RedisUnavailable, errors.New("Redis客户端未初始化"))
	}
	checkCtx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()
	if err := l.service().Rds.Ping(checkCtx).Err(); err != nil {
		return dependencyError("redis", codes.RedisUnavailable, errors.Wrap(err, "Redis PING失败"))
	}
	return dependencyOK("redis"), nil
}

// checkKafka 检查 Kafka 生产者是否按配置就绪。
func (l *HealthLogic) checkKafka(enabled bool) (types.HealthDependencyStatus, error) {
	if !enabled {
		return dependencySkipped("kafka", "Kafka未启用"), nil
	}
	if l.service() == nil || l.service().Kafka == nil || !l.service().Kafka.Enabled() {
		return dependencyError("kafka", codes.KafkaUnavailable, errors.New("Kafka生产者未初始化"))
	}
	return dependencyOK("kafka"), nil
}

// checkTaskQueue 检查任务队列组件是否按配置就绪。
func (l *HealthLogic) checkTaskQueue(enabled bool) (types.HealthDependencyStatus, error) {
	if !enabled {
		return dependencySkipped("task_queue", "任务队列未启用"), nil
	}
	if l.service() == nil || l.service().Task == nil || !l.service().Task.IsEnabled() {
		return dependencyError("task_queue", codes.TaskQueueUnavailable, errors.New("任务队列未初始化或未启用"))
	}
	return dependencyOK("task_queue"), nil
}

// checkCollector 检查 Collector 组件是否按配置就绪。
func (l *HealthLogic) checkCollector(enabled bool) (types.HealthDependencyStatus, error) {
	if !enabled {
		return dependencySkipped("collector", "Collector未启用"), nil
	}
	if l.service() == nil || l.service().Collector == nil {
		return dependencyError("collector", codes.CollectorUnavailable, errors.New("Collector未初始化"))
	}
	return dependencyOK("collector"), nil
}

// checkSQLDB 使用 database/sql 连接池执行 PING 探测。
func checkSQLDB(ctx context.Context, name string, db *sql.DB, code int) (types.HealthDependencyStatus, error) {
	checkCtx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()
	if err := db.PingContext(checkCtx); err != nil {
		return dependencyError(name, code, errors.Wrap(err, "数据库PING失败"))
	}
	return dependencyOK(name), nil
}

// dependencyOK 构造成功依赖状态。
func dependencyOK(name string) types.HealthDependencyStatus {
	return types.HealthDependencyStatus{Name: name, Status: healthStatusOK}
}

// dependencySkipped 构造跳过依赖状态。
func dependencySkipped(name, message string) types.HealthDependencyStatus {
	return types.HealthDependencyStatus{Name: name, Status: healthStatusSkipped, Message: message}
}

// dependencyError 构造失败依赖状态，并返回带业务码上下文的错误。
func dependencyError(name string, code int, err error) (types.HealthDependencyStatus, error) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	status := types.HealthDependencyStatus{Name: name, Status: healthStatusError, Code: code, Message: message}
	return status, errors.Wrapf(err, "ready依赖检查失败 name=%s code=%d", name, code)
}

// hostName 返回当前节点名，失败时使用 unknown。
func hostName() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "unknown"
	}
	return name
}

// runModeName 把启动位掩码转换为稳定可读的部署模式。
func runModeName(mode int) string {
	switch mode {
	case 1:
		return "api"
	case 2:
		return "worker"
	case 4:
		return "scheduler"
	case 3:
		return "api_worker"
	case 5:
		return "api_scheduler"
	case 6:
		return "worker_scheduler"
	case 7:
		return "all"
	default:
		return "unknown"
	}
}
