package svc

import (
	"context"
	"sync/atomic"
	"time"

	"admin/internal/audit"
	"admin/internal/config"
	"admin/internal/infra/collectorx"
	"admin/internal/infra/kafkax"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// SiteDatabases 保存主库和可选命名扩展库连接。
type SiteDatabases struct {
	MainDB   *gorm.DB            // 默认主库连接
	NamedDBs map[DbName]*gorm.DB // 可选扩展库连接，按 site_mysql.<name> 注册
}

// Dependencies 表示 ServiceContext 运行所需的外部依赖集合。
// 该结构只承载已经初始化完成的资源引用，初始化顺序、失败回滚和关闭策略仍由 bootstrap 生命周期管理。
type Dependencies struct {
	SiteDBs SiteDatabases         // 主库与可选扩展库连接集合
	Kafka   *kafkax.Producer      // Kafka 生产者，用户标签事件同步使用
	Rds     redis.UniversalClient // Redis 客户端，频控、锁和任务队列等链路复用
	Audit   *audit.Recorder       // 审计日志记录器，后台敏感操作统一落审计
}

// ServiceContext 将外部依赖集中管理：
// - SiteDBs: 主库与可选扩展库连接集合
// - Rds: Redis（频控、计数、最后发言时间等）
type ServiceContext struct {
	configValue  atomic.Value          // 当前生效的配置快照，供运行期按原子方式读取
	reloadValue  atomic.Value          // 配置热加载运行状态快照，供管理接口和日志复用
	storageValue atomic.Value          // 文件存储运行时缓存，保存 *StorageRuntime
	uploadValue  atomic.Value          // 文件上传运行时缓存，保存 *FileTransferRuntime
	SiteDBs      SiteDatabases         // 主库与可选扩展库连接集合
	Kafka        *kafkax.Producer      // Kafka 生产者，未启用时为空
	Rds          redis.UniversalClient // Redis 客户端（兼容单机/集群）
	Audit        *audit.Recorder       // 审计日志记录器
	Task         TaskQueue             // 任务系统接口（支持调度、DAG、队列管理）
	ConfigReload ConfigReloadExecutor  // 配置热加载执行器，供管理接口手动触发重载
	Collector    *collectorx.Manager   // 通用收集器（Kafka/Redis/DB outbox 回退与重试）
}

// ConfigReloadExecutor 约束配置重载执行能力，避免 logic 层直接依赖 bootstrap 实现。
type ConfigReloadExecutor interface {
	ReloadConfig(ctx context.Context, source string) error
}

// HotReloadStatus 描述 config.yaml 热加载的当前运行状态。
// 该结构只记录监听与加载结果，不承诺基础设施连接已在线重建。
type HotReloadStatus struct {
	Enabled                bool      // 是否启用热加载
	Watching               bool      // 当前是否已启动后台监听
	ConfigFile             string    // 当前监听的配置文件路径
	CheckIntervalSeconds   int       // 当前轮询间隔，单位秒
	ConfigVersion          string    // 当前生效配置版本指纹
	ConfigSummary          string    // 当前配置摘要，便于快速确认关键开关是否已生效
	RestartRequired        bool      // 本次热加载后是否存在“需重启进程才能完全生效”的配置变更
	RestartReason          string    // 需要重启才能完全生效的原因摘要
	LastStatus             string    // 最近一次处理结果：idle/success/failed
	LastMessage            string    // 最近一次处理结果说明
	LastTriggerSource      string    // 最近一次触发来源：watcher/manual_api/startup 等
	LastFailureCategory    string    // 最近一次失败分类：fingerprint/load/reload/not_bound 等
	LastCheckedAt          time.Time // 最近一次检查配置文件时间
	LastReloadAt           time.Time // 最近一次触发配置重载时间
	LastSuccessAt          time.Time // 最近一次成功加载时间
	LastFailureAt          time.Time // 最近一次失败时间
	ReloadCount            int64     // 累计成功加载次数
	SuppressedFailureCount int64     // 限频压制的重复失败日志次数
}

// NewServiceContext 只接收已经初始化完成的依赖，避免把初始化细节继续堆到 ServiceContext 内部。
func NewServiceContext(c config.Config, deps Dependencies) *ServiceContext {
	svcCtx := &ServiceContext{
		SiteDBs: deps.SiteDBs,
		Kafka:   deps.Kafka,
		Rds:     deps.Rds,
		Audit:   deps.Audit,
	}
	svcCtx.UpdateConfig(c)
	svcCtx.UpdateHotReloadStatus(HotReloadStatus{LastStatus: "idle"})
	svcCtx.storageValue.Store(NewStorageRuntime())
	svcCtx.uploadValue.Store(NewFileTransferRuntime())
	return svcCtx
}

// ScopedWithContext 基于当前 ServiceContext 构造一份绑定请求上下文的只读作用域副本。
// 这里只复制当前快照，不发布 runtimecfg，避免请求作用域覆盖进程级运行配置。
func (s *ServiceContext) ScopedWithContext(ctx context.Context) *ServiceContext {
	if s == nil {
		return nil
	}
	scoped := &ServiceContext{
		SiteDBs: s.SiteDBs.WithContext(ctx),
		Kafka:   s.Kafka,
		Rds:     s.Rds,
		Audit:   s.Audit,
	}
	scoped.configValue.Store(s.CurrentConfig())
	scoped.Task = s.Task
	scoped.ConfigReload = s.ConfigReload
	scoped.Collector = s.Collector
	if storageRuntime, ok := s.storageValue.Load().(*StorageRuntime); ok && storageRuntime != nil {
		scoped.storageValue.Store(storageRuntime)
	}
	if uploadRuntime, ok := s.uploadValue.Load().(*FileTransferRuntime); ok && uploadRuntime != nil {
		scoped.uploadValue.Store(uploadRuntime)
	}
	scoped.UpdateHotReloadStatus(s.CurrentHotReloadStatus())
	return scoped
}

// CurrentConfig 返回当前生效的配置快照，供运行期读取最新配置。
func (s *ServiceContext) CurrentConfig() config.Config {
	if s == nil {
		return config.Config{}
	}
	if cfg, ok := s.configValue.Load().(config.Config); ok {
		return cfg
	}
	return config.Config{}
}

// UpdateConfig 原子替换运行期配置快照。
func (s *ServiceContext) UpdateConfig(c config.Config) {
	if s == nil {
		return
	}
	s.configValue.Store(c)
}

// CurrentHotReloadStatus 返回当前热加载状态快照。
func (s *ServiceContext) CurrentHotReloadStatus() HotReloadStatus {
	if s == nil {
		return HotReloadStatus{}
	}
	if status, ok := s.reloadValue.Load().(HotReloadStatus); ok {
		return status
	}
	return HotReloadStatus{}
}

// UpdateHotReloadStatus 原子替换热加载状态快照。
func (s *ServiceContext) UpdateHotReloadStatus(status HotReloadStatus) {
	if s == nil {
		return
	}
	s.reloadValue.Store(status)
}

// Lookup 根据数据库名称返回连接，空名称和 main 都指向主库。
func (s SiteDatabases) Lookup(database DbName) *gorm.DB {
	name := NormalizeDbName(database)
	if name == DatabaseMain {
		return s.MainDB
	}
	if s.NamedDBs == nil {
		return nil
	}
	return s.NamedDBs[name]
}

// WithContext 为所有站点库连接绑定请求上下文，方便日志和 trace 贯穿到 GORM。
func (s SiteDatabases) WithContext(ctx context.Context) SiteDatabases {
	s.MainDB = withDBContext(s.MainDB, ctx)
	if len(s.NamedDBs) > 0 {
		namedDBs := make(map[DbName]*gorm.DB, len(s.NamedDBs))
		for name, db := range s.NamedDBs {
			namedDBs[name] = withDBContext(db, ctx)
		}
		s.NamedDBs = namedDBs
	}
	return s
}

// withDBContext 对单个 GORM 连接绑定上下文，空连接保持为空。
func withDBContext(db *gorm.DB, ctx context.Context) *gorm.DB {
	if db == nil {
		return nil
	}
	return db.WithContext(ctx)
}
