package runtimeconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	codes "admin/common/codes"
	i18n "admin/common/i18n"
	keys "admin/common/rediskeys"
	"admin/internal/config"
	corelogic "admin/internal/logic"
	adminlogic "admin/internal/logic/admin"
	cachelogic "admin/internal/logic/cache"
	securitylogic "admin/internal/logic/security"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	tablecache "github.com/Is999/table-cache"
	yaml "go.yaml.in/yaml/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// SourceFile 表示运行期大列表配置继续来自 YAML 文件。
	SourceFile = "file"
	// SourceDatabase 表示运行期大列表配置来自数据库发布快照。
	SourceDatabase = "database"
	// defaultRuntimeConfigEnv 表示未显式声明 env 时使用的默认环境。
	defaultRuntimeConfigEnv = "default"
	// defaultPollIntervalSeconds 表示 DB 模式轻量版本轮询默认间隔。
	defaultPollIntervalSeconds = 30
	// minPollIntervalSeconds 表示 DB 模式轮询最小间隔，避免配置错误刷 Redis。
	minPollIntervalSeconds = 5
)

// ReleaseSnapshot 是运行态发布快照，只包含数据库化的大列表配置。
type ReleaseSnapshot struct {
	ArchiveJobs  []config.ArchiveJobConfig   `json:"archive_jobs" yaml:"archive_jobs"`   // 归档任务列表
	TaskPeriodic []config.TaskPeriodicConfig `json:"task_periodic" yaml:"task_periodic"` // 周期任务列表
}

// StateCache 是 table-cache 中运行配置 active 版本状态。
type StateCache struct {
	ActiveReleaseID uint64 `json:"active_release_id,string"` // 当前发布 ID，按 Redis Hash 字符串读取
	ActiveVersion   uint64 `json:"active_version,string"`    // 当前版本号，按 Redis Hash 字符串读取
	ActiveChecksum  string `json:"active_checksum"`          // 当前快照 SHA256
	PublishedAtUnix int64  `json:"published_at_unix,string"` // 最近发布时间戳，按 Redis Hash 字符串读取
}

// ActiveRelease 保存一次 active 版本和对应快照。
type ActiveRelease struct {
	State    StateCache                 // 当前 active 状态
	Release  model.RuntimeConfigRelease // 发布记录
	Snapshot ReleaseSnapshot            // 发布快照
}

// RuntimeConfigLogic 封装运行期大列表配置管理逻辑。
type RuntimeConfigLogic struct {
	*corelogic.BaseLogic // 复用上下文、DB、Redis、审计和管理员上下文能力
}

// NewRuntimeConfigLogic 创建运行配置管理逻辑对象。
func NewRuntimeConfigLogic(r *http.Request, svcCtx *svc.ServiceContext) *RuntimeConfigLogic {
	return &RuntimeConfigLogic{BaseLogic: corelogic.NewBaseLogic(r, svcCtx)}
}

// NewRuntimeConfigLogicWithContext 创建绑定上下文的运行配置管理逻辑对象。
func NewRuntimeConfigLogicWithContext(ctx context.Context, svcCtx *svc.ServiceContext) *RuntimeConfigLogic {
	return &RuntimeConfigLogic{BaseLogic: corelogic.NewBaseLogicWithContext(ctx, svcCtx)}
}

// NormalizeSource 归一化运行配置来源。
func NormalizeSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case SourceDatabase:
		return SourceDatabase
	default:
		return SourceFile
	}
}

// IsDatabaseSource 判断当前配置是否启用数据库发布快照。
func IsDatabaseSource(cfg config.Config) bool {
	return NormalizeSource(cfg.RuntimeConfig.Source) == SourceDatabase
}

// RuntimeEnv 返回运行配置环境标识。
func RuntimeEnv(cfg config.Config) string {
	env := strings.TrimSpace(cfg.RuntimeConfig.Env)
	if env == "" {
		env = strings.TrimSpace(cfg.Mode)
	}
	if env == "" {
		env = defaultRuntimeConfigEnv
	}
	return env
}

// PollIntervalSeconds 返回数据库模式轻量版本轮询间隔秒数。
func PollIntervalSeconds(cfg config.Config) int {
	if cfg.RuntimeConfig.PollIntervalSeconds <= 0 {
		return defaultPollIntervalSeconds
	}
	if cfg.RuntimeConfig.PollIntervalSeconds < minPollIntervalSeconds {
		return minPollIntervalSeconds
	}
	return cfg.RuntimeConfig.PollIntervalSeconds
}

// CurrentSnapshotFromConfig 从当前配置提取数据库化的大列表片段。
func CurrentSnapshotFromConfig(cfg config.Config) ReleaseSnapshot {
	return normalizeReleaseSnapshot(ReleaseSnapshot{
		ArchiveJobs:  append([]config.ArchiveJobConfig(nil), cfg.Archive.Jobs...),
		TaskPeriodic: append([]config.TaskPeriodicConfig(nil), cfg.Task.Periodic...),
	})
}

// runtimeConfigSnapshotEmpty 判断发布快照是否未包含任何运行期大列表配置。
func runtimeConfigSnapshotEmpty(snapshot ReleaseSnapshot) bool {
	return len(snapshot.ArchiveJobs) == 0 && len(snapshot.TaskPeriodic) == 0
}

// ApplySnapshot 把发布快照覆盖到完整配置；不会修改 YAML-only 段。
func ApplySnapshot(cfg *config.Config, snapshot ReleaseSnapshot) {
	if cfg == nil {
		return
	}
	snapshot = normalizeReleaseSnapshot(snapshot)
	cfg.Archive.Jobs = append([]config.ArchiveJobConfig(nil), snapshot.ArchiveJobs...)
	cfg.Task.Periodic = append([]config.TaskPeriodicConfig(nil), snapshot.TaskPeriodic...)
}

// DecodeSnapshotJSON 解析发布快照 JSON。
func DecodeSnapshotJSON(snapshotJSON string) (ReleaseSnapshot, error) {
	var snapshot ReleaseSnapshot
	if err := json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
		return snapshot, errors.Tag(err)
	}
	return snapshot, nil
}

// EncodeSnapshot 生成发布快照 JSON、YAML 和校验和。
func EncodeSnapshot(snapshot ReleaseSnapshot) (string, string, string, error) {
	jsonBytes, err := json.Marshal(snapshot)
	if err != nil {
		return "", "", "", errors.Tag(err)
	}
	yamlBytes, err := yaml.Marshal(snapshot)
	if err != nil {
		return "", "", "", errors.Tag(err)
	}
	return string(jsonBytes), string(yamlBytes), sha256Hex(jsonBytes), nil
}

// LoadActiveSnapshotCached 从 table-cache 读取当前 active 发布快照。
func LoadActiveSnapshotCached(ctx context.Context, svcCtx *svc.ServiceContext) (*ActiveRelease, error) {
	logicObj := NewRuntimeConfigLogicWithContext(ctx, svcCtx)
	return logicObj.loadActiveSnapshotCached()
}

// EnsureInitialRelease 确保 DB 模式首次启动时已有 active 发布快照。
// 当 active 为空且当前文件包含运行配置时，会自动导入当前文件快照为首个发布版本。
func EnsureInitialRelease(ctx context.Context, svcCtx *svc.ServiceContext) (*ActiveRelease, error) {
	logicObj := NewRuntimeConfigLogicWithContext(ctx, svcCtx)
	state, err := logicObj.loadActiveStateCached()
	if err != nil {
		return nil, errors.Tag(err)
	}
	if state.ActiveReleaseID != 0 {
		return logicObj.loadActiveSnapshotCached()
	}
	snapshot := CurrentSnapshotFromConfig(logicObj.currentConfig())
	if runtimeConfigSnapshotEmpty(snapshot) {
		return nil, errors.Errorf("运行配置未发布 active release env=%s，且当前文件没有可迁移的 task_periodic/archive_jobs", RuntimeEnv(logicObj.currentConfig()))
	}
	if _, err = ValidateSnapshot(snapshot); err != nil {
		return nil, errors.Wrap(err, "自动迁移当前文件运行配置失败")
	}
	if err = logicObj.replaceDraft(snapshot); err != nil {
		return nil, errors.Wrap(err, "写入运行配置初始草稿失败")
	}
	if _, err = logicObj.publishSnapshot(snapshot, "bootstrap import current runtime config", 0); err != nil {
		if active, loadErr := logicObj.loadActiveSnapshotCached(); loadErr == nil {
			return active, nil
		}
		return nil, errors.Wrap(err, "发布运行配置初始版本失败")
	}
	return logicObj.loadActiveSnapshotCached()
}

// LoadActiveStateCached 从 table-cache 读取当前 active 版本状态，不触碰发布快照。
func LoadActiveStateCached(ctx context.Context, svcCtx *svc.ServiceContext) (StateCache, error) {
	logicObj := NewRuntimeConfigLogicWithContext(ctx, svcCtx)
	return logicObj.loadActiveStateCached()
}

// Overview 查询运行配置来源、active 版本、草稿数量和当前快照。
func (l *RuntimeConfigLogic) Overview() *types.BizResult {
	cfg := l.currentConfig()
	state, _ := l.loadActiveStateCached()
	periodicCount, archiveCount, err := l.draftCounts()
	if err != nil {
		return types.DBError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.Overview 查询草稿数量失败").ToBizResult()
	}
	return types.NewBizResult(codes.FetchSuccess).WithData(&types.RuntimeConfigOverviewResp{
		Source:              NormalizeSource(cfg.RuntimeConfig.Source),
		Env:                 RuntimeEnv(cfg),
		PollIntervalSeconds: PollIntervalSeconds(cfg),
		State:               stateCacheToItem(state),
		Draft: types.RuntimeConfigDraftCount{
			PeriodicTasks: periodicCount,
			ArchiveJobs:   archiveCount,
		},
		CurrentSnapshot: snapshotToResp(CurrentSnapshotFromConfig(cfg)),
	})
}

// ListPeriodicTasks 分页查询周期任务草稿。
func (l *RuntimeConfigLogic) ListPeriodicTasks(req *types.RuntimeTaskPeriodicQueryReq) *types.BizResult {
	if err := req.Validate(); err != nil {
		return types.ParamError(err).ToBizResult()
	}
	db, err := l.writeDB()
	if err != nil {
		return types.DBError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.ListPeriodicTasks DB未初始化").ToBizResult()
	}
	appID, env := l.scope()
	q := db.Model(&model.RuntimeTaskPeriodic{}).Where("app_id = ? AND env = ?", appID, env)
	if req.Workflow != "" {
		q = q.Where("workflow = ?", req.Workflow)
	}
	if req.Enabled != nil {
		q = q.Where("enabled = ?", *req.Enabled)
	}
	if req.Keyword != "" {
		keyword := "%" + req.Keyword + "%"
		q = q.Where("name LIKE ? OR queue LIKE ?", keyword, keyword)
	}
	var total int64
	if err = q.Count(&total).Error; err != nil {
		return types.DBError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.ListPeriodicTasks Count失败").ToBizResult()
	}
	var rows []model.RuntimeTaskPeriodic
	if total > 0 {
		err = q.Order("sort_order ASC").Order("id ASC").
			Limit(req.PageSize).Offset((req.Page - 1) * req.PageSize).
			Find(&rows).Error
		if err != nil {
			return types.DBError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.ListPeriodicTasks 查询失败").ToBizResult()
		}
	}
	items := make([]types.RuntimeTaskPeriodicItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, periodicModelToItem(row))
	}
	return types.NewBizResult(codes.FetchSuccess).WithData(&types.ListResp[types.RuntimeTaskPeriodicItem]{List: items, Total: total})
}

// SavePeriodicTask 保存周期任务草稿。
func (l *RuntimeConfigLogic) SavePeriodicTask(req *types.SaveRuntimeTaskPeriodicReq) *types.BizResult {
	if err := req.Validate(); err != nil {
		return types.ParamError(err).ToBizResult()
	}
	db, err := l.writeDB()
	if err != nil {
		return types.DBError(i18n.MsgKeySaveFail, err, "RuntimeConfigLogic.SavePeriodicTask DB未初始化").ToBizResult()
	}
	adminID := l.adminID()
	appID, env := l.scope()
	row := periodicReqToModel(req, appID, env, adminID)
	err = db.Transaction(func(tx *gorm.DB) error {
		if req.ID > 0 {
			row.ID = req.ID
			result := tx.Model(&model.RuntimeTaskPeriodic{}).
				Where("id = ? AND app_id = ? AND env = ?", req.ID, appID, env).
				Updates(periodicModelUpdateMap(row))
			return checkRuntimeConfigUpdated(result, req.ID, "周期任务草稿")
		}
		return errors.Tag(tx.Create(&row).Error)
	})
	if err != nil {
		return types.DBError(i18n.MsgKeySaveFail, err, "RuntimeConfigLogic.SavePeriodicTask 保存失败").ToBizResult()
	}
	return types.NewBizResult(codes.SaveSuccess).SetI18nMessage(i18n.MsgKeySaveSuccess)
}

// DeletePeriodicTask 删除周期任务草稿。
func (l *RuntimeConfigLogic) DeletePeriodicTask(req *types.RuntimeConfigIDReq) *types.BizResult {
	if err := req.Validate(); err != nil {
		return types.ParamError(err).ToBizResult()
	}
	if err := l.deleteByID(&model.RuntimeTaskPeriodic{}, req.ID); err != nil {
		return types.DBError(i18n.MsgKeyDeleteFail, err, "RuntimeConfigLogic.DeletePeriodicTask 删除失败").ToBizResult()
	}
	return types.NewBizResult(codes.DeleteSuccess).SetI18nMessage(i18n.MsgKeyDeleteSuccess)
}

// ListArchiveJobs 分页查询归档任务草稿。
func (l *RuntimeConfigLogic) ListArchiveJobs(req *types.RuntimeArchiveJobQueryReq) *types.BizResult {
	if err := req.Validate(); err != nil {
		return types.ParamError(err).ToBizResult()
	}
	db, err := l.writeDB()
	if err != nil {
		return types.DBError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.ListArchiveJobs DB未初始化").ToBizResult()
	}
	appID, env := l.scope()
	q := db.Model(&model.RuntimeArchiveJob{}).Where("app_id = ? AND env = ?", appID, env)
	if req.Enabled != nil {
		q = q.Where("enabled = ?", *req.Enabled)
	}
	if req.Database != "" {
		q = q.Where("database_name = ?", req.Database)
	}
	if req.Keyword != "" {
		keyword := "%" + req.Keyword + "%"
		q = q.Where("name LIKE ? OR table_name LIKE ?", keyword, keyword)
	}
	var total int64
	if err = q.Count(&total).Error; err != nil {
		return types.DBError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.ListArchiveJobs Count失败").ToBizResult()
	}
	var rows []model.RuntimeArchiveJob
	if total > 0 {
		err = q.Order("sort_order ASC").Order("id ASC").
			Limit(req.PageSize).Offset((req.Page - 1) * req.PageSize).
			Find(&rows).Error
		if err != nil {
			return types.DBError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.ListArchiveJobs 查询失败").ToBizResult()
		}
	}
	items := make([]types.RuntimeArchiveJobItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, archiveModelToItem(row))
	}
	return types.NewBizResult(codes.FetchSuccess).WithData(&types.ListResp[types.RuntimeArchiveJobItem]{List: items, Total: total})
}

// SaveArchiveJob 保存归档任务草稿。
func (l *RuntimeConfigLogic) SaveArchiveJob(req *types.SaveRuntimeArchiveJobReq) *types.BizResult {
	if err := req.Validate(); err != nil {
		return types.ParamError(err).ToBizResult()
	}
	db, err := l.writeDB()
	if err != nil {
		return types.DBError(i18n.MsgKeySaveFail, err, "RuntimeConfigLogic.SaveArchiveJob DB未初始化").ToBizResult()
	}
	adminID := l.adminID()
	appID, env := l.scope()
	row := archiveReqToModel(req, appID, env, adminID)
	err = db.Transaction(func(tx *gorm.DB) error {
		if req.ID > 0 {
			row.ID = req.ID
			result := tx.Model(&model.RuntimeArchiveJob{}).
				Where("id = ? AND app_id = ? AND env = ?", req.ID, appID, env).
				Updates(archiveModelUpdateMap(row))
			return checkRuntimeConfigUpdated(result, req.ID, "归档任务草稿")
		}
		return errors.Tag(tx.Create(&row).Error)
	})
	if err != nil {
		return types.DBError(i18n.MsgKeySaveFail, err, "RuntimeConfigLogic.SaveArchiveJob 保存失败").ToBizResult()
	}
	return types.NewBizResult(codes.SaveSuccess).SetI18nMessage(i18n.MsgKeySaveSuccess)
}

// DeleteArchiveJob 删除归档任务草稿。
func (l *RuntimeConfigLogic) DeleteArchiveJob(req *types.RuntimeConfigIDReq) *types.BizResult {
	if err := req.Validate(); err != nil {
		return types.ParamError(err).ToBizResult()
	}
	if err := l.deleteByID(&model.RuntimeArchiveJob{}, req.ID); err != nil {
		return types.DBError(i18n.MsgKeyDeleteFail, err, "RuntimeConfigLogic.DeleteArchiveJob 删除失败").ToBizResult()
	}
	return types.NewBizResult(codes.DeleteSuccess).SetI18nMessage(i18n.MsgKeyDeleteSuccess)
}

// ValidateDraft 预检当前草稿并返回快照校验和。
func (l *RuntimeConfigLogic) ValidateDraft() *types.BizResult {
	snapshot, err := l.buildDraftSnapshot()
	if err != nil {
		return types.ServerError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.ValidateDraft 读取草稿失败").ToBizResult()
	}
	messages, err := ValidateSnapshot(snapshot)
	if err != nil {
		return types.NewBizResult(codes.Success).
			WithData(&types.RuntimeConfigValidateResp{Valid: false, Messages: append(messages, err.Error())})
	}
	_, _, checksum, err := EncodeSnapshot(snapshot)
	if err != nil {
		return types.ServerError(i18n.MsgKeyInternalError, err, "RuntimeConfigLogic.ValidateDraft 生成快照失败").ToBizResult()
	}
	return types.NewBizResult(codes.Success).WithData(&types.RuntimeConfigValidateResp{Valid: true, Messages: messages, Checksum: checksum})
}

// Publish 发布当前草稿。
func (l *RuntimeConfigLogic) Publish(req *types.RuntimeConfigPublishReq) *types.BizResult {
	if err := req.Validate(); err != nil {
		return types.ParamError(err).ToBizResult()
	}
	if err := l.requireMFA(req.TwoStepKey, req.TwoStepValue); err != nil {
		return (&adminlogic.AdminLogic{BaseLogic: l.BaseLogic}).MFABizResult(err)
	}
	resp, err := l.publishDraft(req.Remark, 0)
	if err != nil {
		return types.ServerError(i18n.MsgKeySaveFail, err, "RuntimeConfigLogic.Publish 发布失败").ToBizResult()
	}
	return types.NewBizResult(codes.SaveSuccess).SetI18nMessage(i18n.MsgKeySaveSuccess).WithData(resp)
}

// Rollback 把指定历史快照写回草稿并发布为新版本。
func (l *RuntimeConfigLogic) Rollback(req *types.RuntimeConfigRollbackReq) *types.BizResult {
	if err := req.Validate(); err != nil {
		return types.ParamError(err).ToBizResult()
	}
	if err := l.requireMFA(req.TwoStepKey, req.TwoStepValue); err != nil {
		return (&adminlogic.AdminLogic{BaseLogic: l.BaseLogic}).MFABizResult(err)
	}
	release, snapshot, err := l.loadReleaseByID(req.ReleaseID)
	if err != nil {
		return types.ServerError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.Rollback 查询发布快照失败").ToBizResult()
	}
	if err = l.replaceDraft(snapshot); err != nil {
		return types.DBError(i18n.MsgKeySaveFail, err, "RuntimeConfigLogic.Rollback 写回草稿失败").ToBizResult()
	}
	remark := req.Remark
	if remark == "" {
		remark = fmt.Sprintf("rollback to release %d", req.ReleaseID)
	}
	resp, err := l.publishSnapshot(snapshot, remark, release.ID)
	if err != nil {
		return types.ServerError(i18n.MsgKeySaveFail, err, "RuntimeConfigLogic.Rollback 发布回滚版本失败").ToBizResult()
	}
	return types.NewBizResult(codes.SaveSuccess).SetI18nMessage(i18n.MsgKeySaveSuccess).WithData(resp)
}

// ImportCurrent 导入当前运行态配置并发布首个或新版本。
func (l *RuntimeConfigLogic) ImportCurrent(req *types.RuntimeConfigImportReq) *types.BizResult {
	if err := req.Validate(); err != nil {
		return types.ParamError(err).ToBizResult()
	}
	if err := l.requireMFA(req.TwoStepKey, req.TwoStepValue); err != nil {
		return (&adminlogic.AdminLogic{BaseLogic: l.BaseLogic}).MFABizResult(err)
	}
	snapshot := CurrentSnapshotFromConfig(l.currentConfig())
	if err := l.replaceDraft(snapshot); err != nil {
		return types.DBError(i18n.MsgKeySaveFail, err, "RuntimeConfigLogic.ImportCurrent 写入草稿失败").ToBizResult()
	}
	remark := req.Remark
	if remark == "" {
		remark = "import current runtime config"
	}
	resp, err := l.publishSnapshot(snapshot, remark, 0)
	if err != nil {
		return types.ServerError(i18n.MsgKeySaveFail, err, "RuntimeConfigLogic.ImportCurrent 发布导入版本失败").ToBizResult()
	}
	return types.NewBizResult(codes.SaveSuccess).SetI18nMessage(i18n.MsgKeySaveSuccess).WithData(resp)
}

// ListReleases 分页查询发布历史。
func (l *RuntimeConfigLogic) ListReleases(req *types.RuntimeConfigReleaseQueryReq) *types.BizResult {
	if err := req.Validate(); err != nil {
		return types.ParamError(err).ToBizResult()
	}
	db, err := l.writeDB()
	if err != nil {
		return types.DBError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.ListReleases DB未初始化").ToBizResult()
	}
	appID, env := l.scope()
	q := db.Model(&model.RuntimeConfigRelease{}).Where("app_id = ? AND env = ?", appID, env)
	var total int64
	if err = q.Count(&total).Error; err != nil {
		return types.DBError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.ListReleases Count失败").ToBizResult()
	}
	var rows []model.RuntimeConfigRelease
	if total > 0 {
		err = q.Order("version_no DESC").Limit(req.PageSize).Offset((req.Page - 1) * req.PageSize).Find(&rows).Error
		if err != nil {
			return types.DBError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.ListReleases 查询失败").ToBizResult()
		}
	}
	items := make([]types.RuntimeConfigReleaseItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, releaseModelToItem(row))
	}
	return types.NewBizResult(codes.FetchSuccess).WithData(&types.ListResp[types.RuntimeConfigReleaseItem]{List: items, Total: total})
}

// GetRelease 查询发布快照详情。
func (l *RuntimeConfigLogic) GetRelease(req *types.RuntimeConfigReleaseIDReq) *types.BizResult {
	if err := req.Validate(); err != nil {
		return types.ParamError(err).ToBizResult()
	}
	release, _, err := l.loadReleaseByID(req.ReleaseID)
	if err != nil {
		return types.ServerError(i18n.MsgKeyQueryFail, err, "RuntimeConfigLogic.GetRelease 查询发布快照失败").ToBizResult()
	}
	item := releaseModelToItem(release)
	return types.NewBizResult(codes.FetchSuccess).WithData(&types.RuntimeConfigReleaseDetailResp{
		RuntimeConfigReleaseItem: item,
		SnapshotJSON:             release.SnapshotJSON,
		SnapshotYAML:             release.SnapshotYAML,
	})
}

// publishDraft 从草稿表构造快照并发布为 active 版本。
func (l *RuntimeConfigLogic) publishDraft(remark string, baseReleaseID uint64) (*types.RuntimeConfigPublishResp, error) {
	snapshot, err := l.buildDraftSnapshot()
	if err != nil {
		return nil, errors.Tag(err)
	}
	return l.publishSnapshot(snapshot, remark, baseReleaseID)
}

// publishSnapshot 校验并持久化发布快照，同时触发运行态热加载。
func (l *RuntimeConfigLogic) publishSnapshot(snapshot ReleaseSnapshot, remark string, baseReleaseID uint64) (*types.RuntimeConfigPublishResp, error) {
	snapshot = normalizeReleaseSnapshot(snapshot)
	if _, err := ValidateSnapshot(snapshot); err != nil {
		return nil, errors.Tag(err)
	}
	db, err := l.writeDB()
	if err != nil {
		return nil, errors.Tag(err)
	}
	appID, env := l.scope()
	admin := l.GetCtxAdmin()
	jsonText, yamlText, checksum, err := EncodeSnapshot(snapshot)
	if err != nil {
		return nil, errors.Tag(err)
	}
	publishedByName := admin.Name
	if strings.TrimSpace(publishedByName) == "" {
		publishedByName = "system"
	}
	var release model.RuntimeConfigRelease
	var previousReleaseID uint64
	err = db.Transaction(func(tx *gorm.DB) error {
		state, lockErr := l.loadStateForUpdate(tx, appID, env)
		if lockErr != nil {
			return errors.Tag(lockErr)
		}
		version := state.ActiveVersion + 1
		now := time.Now()
		release = model.RuntimeConfigRelease{
			AppID:              appID,
			Env:                env,
			VersionNo:          version,
			SnapshotJSON:       jsonText,
			SnapshotYAML:       yamlText,
			Checksum:           checksum,
			BaseReleaseID:      baseReleaseID,
			Remark:             strings.TrimSpace(remark),
			PublishedByAdminID: admin.ID,
			PublishedByName:    publishedByName,
			PublishedAt:        now,
		}
		if err = tx.Create(&release).Error; err != nil {
			return errors.Tag(err)
		}
		previousReleaseID = state.ActiveReleaseID
		state.ActiveReleaseID = release.ID
		state.ActiveVersion = version
		state.ActiveChecksum = checksum
		state.PublishedAt = now
		if state.ID == 0 {
			state.AppID = appID
			state.Env = env
			if err = tx.Create(&state).Error; err != nil {
				return errors.Tag(err)
			}
			return nil
		}
		return errors.Tag(tx.Save(&state).Error)
	})
	if err != nil {
		return nil, errors.Tag(err)
	}
	if err = l.invalidateRuntimeConfigCache(env, release.ID, previousReleaseID); err != nil {
		return nil, errors.Tag(err)
	}
	reload := svc.RuntimeConfigReloadResult{}
	if l.Svc != nil && l.Svc.ConfigReload != nil {
		reload, err = l.Svc.ConfigReload.ReloadRuntimeConfig(l.Ctx, "runtime_config_publish")
		if err != nil {
			return nil, errors.Tag(err)
		}
	}
	if reload.RestartRequired || reload.RestartReason != "" {
		_ = db.Model(&model.RuntimeConfigRelease{}).Where("id = ?", release.ID).Updates(map[string]any{
			"restart_required": reload.RestartRequired,
			"restart_reason":   reload.RestartReason,
		}).Error
	}
	return &types.RuntimeConfigPublishResp{
		ReleaseID:       release.ID,
		VersionNo:       release.VersionNo,
		Checksum:        release.Checksum,
		RestartRequired: reload.RestartRequired,
		RestartReason:   reload.RestartReason,
	}, nil
}

// buildDraftSnapshot 从当前环境草稿表组装可发布快照。
func (l *RuntimeConfigLogic) buildDraftSnapshot() (ReleaseSnapshot, error) {
	db, err := l.writeDB()
	if err != nil {
		return ReleaseSnapshot{}, errors.Tag(err)
	}
	appID, env := l.scope()
	var periodicRows []model.RuntimeTaskPeriodic
	// writeDB 带路由 clause，跨模型查询必须新开 Session，避免沿用上一个 Statement。
	if err = db.Session(&gorm.Session{NewDB: true}).Where("app_id = ? AND env = ?", appID, env).
		Order("sort_order ASC").Order("id ASC").Find(&periodicRows).Error; err != nil {
		return ReleaseSnapshot{}, errors.Tag(err)
	}
	var archiveRows []model.RuntimeArchiveJob
	if err = db.Session(&gorm.Session{NewDB: true}).Where("app_id = ? AND env = ?", appID, env).
		Order("sort_order ASC").Order("id ASC").Find(&archiveRows).Error; err != nil {
		return ReleaseSnapshot{}, errors.Tag(err)
	}
	snapshot := ReleaseSnapshot{
		ArchiveJobs:  make([]config.ArchiveJobConfig, 0, len(archiveRows)),
		TaskPeriodic: make([]config.TaskPeriodicConfig, 0, len(periodicRows)),
	}
	for _, row := range archiveRows {
		snapshot.ArchiveJobs = append(snapshot.ArchiveJobs, archiveModelToConfig(row))
	}
	for _, row := range periodicRows {
		snapshot.TaskPeriodic = append(snapshot.TaskPeriodic, periodicModelToConfig(row))
	}
	return snapshot, nil
}

// replaceDraft 用快照内容覆盖当前环境草稿表。
func (l *RuntimeConfigLogic) replaceDraft(snapshot ReleaseSnapshot) error {
	db, err := l.writeDB()
	if err != nil {
		return errors.Tag(err)
	}
	appID, env := l.scope()
	adminID := l.adminID()
	return db.Transaction(func(tx *gorm.DB) error {
		// 同一事务内跨模型操作也要清理 Statement，事务连接仍由 tx 保持。
		if err := tx.Session(&gorm.Session{NewDB: true}).Where("app_id = ? AND env = ?", appID, env).Delete(&model.RuntimeTaskPeriodic{}).Error; err != nil {
			return errors.Tag(err)
		}
		if err := tx.Session(&gorm.Session{NewDB: true}).Where("app_id = ? AND env = ?", appID, env).Delete(&model.RuntimeArchiveJob{}).Error; err != nil {
			return errors.Tag(err)
		}
		for index, item := range snapshot.TaskPeriodic {
			row := periodicConfigToModel(item, appID, env, adminID, index)
			if err := tx.Session(&gorm.Session{NewDB: true}).Create(&row).Error; err != nil {
				return errors.Tag(err)
			}
		}
		for index, item := range snapshot.ArchiveJobs {
			row := archiveConfigToModel(item, appID, env, adminID, index)
			if err := tx.Session(&gorm.Session{NewDB: true}).Create(&row).Error; err != nil {
				return errors.Tag(err)
			}
		}
		return nil
	})
}

// loadActiveSnapshotCached 从缓存回源读取当前 active 发布快照。
func (l *RuntimeConfigLogic) loadActiveSnapshotCached() (*ActiveRelease, error) {
	state, err := l.loadActiveStateCached()
	if err != nil {
		return nil, errors.Tag(err)
	}
	if state.ActiveReleaseID == 0 {
		return nil, errors.Errorf("运行配置未发布 active release env=%s", RuntimeEnv(l.currentConfig()))
	}
	var snapshotJSON string
	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		return nil, errors.Tag(err)
	}
	releaseKey := cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.RuntimeConfigReleaseKey(state.ActiveReleaseID))
	result, err := manager.LoadThrough(l.Ctx, releaseKey, &snapshotJSON, nil)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if result.State == tablecache.LookupStateEmpty || strings.TrimSpace(snapshotJSON) == "" {
		return nil, errors.Errorf("运行配置发布快照不存在 release_id=%d", state.ActiveReleaseID)
	}
	snapshot, err := DecodeSnapshotJSON(snapshotJSON)
	if err != nil {
		return nil, errors.Tag(err)
	}
	release := model.RuntimeConfigRelease{
		ID:        state.ActiveReleaseID,
		VersionNo: state.ActiveVersion,
		Checksum:  state.ActiveChecksum,
	}
	return &ActiveRelease{State: state, Release: release, Snapshot: snapshot}, nil
}

// loadActiveStateCached 从缓存回源读取当前 active 状态。
func (l *RuntimeConfigLogic) loadActiveStateCached() (StateCache, error) {
	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		return StateCache{}, errors.Tag(err)
	}
	env := RuntimeEnv(l.currentConfig())
	stateKey := cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.RuntimeConfigStateKey(env))
	var state StateCache
	result, err := manager.LoadThrough(l.Ctx, stateKey, &state, nil)
	if err != nil {
		return StateCache{}, errors.Tag(err)
	}
	if result.State == tablecache.LookupStateEmpty {
		return StateCache{}, nil
	}
	return state, nil
}

// loadStateForUpdate 在事务内锁定运行配置状态行。
func (l *RuntimeConfigLogic) loadStateForUpdate(tx *gorm.DB, appID string, env string) (model.RuntimeConfigState, error) {
	var state model.RuntimeConfigState
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("app_id = ? AND env = ?", appID, env).First(&state).Error
	if err == nil {
		return state, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.RuntimeConfigState{}, nil
	}
	return model.RuntimeConfigState{}, errors.Tag(err)
}

// loadReleaseByID 按发布 ID 读取当前环境的发布记录和快照。
func (l *RuntimeConfigLogic) loadReleaseByID(releaseID uint64) (model.RuntimeConfigRelease, ReleaseSnapshot, error) {
	db, err := l.writeDB()
	if err != nil {
		return model.RuntimeConfigRelease{}, ReleaseSnapshot{}, errors.Tag(err)
	}
	appID, env := l.scope()
	var row model.RuntimeConfigRelease
	if err = db.Where("id = ? AND app_id = ? AND env = ?", releaseID, appID, env).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return row, ReleaseSnapshot{}, errors.Errorf("发布快照不存在: %d", releaseID)
		}
		return row, ReleaseSnapshot{}, errors.Tag(err)
	}
	snapshot, err := DecodeSnapshotJSON(row.SnapshotJSON)
	return row, snapshot, errors.Tag(err)
}

// deleteByID 按 ID 删除当前环境草稿记录，并校验记录真实存在。
func (l *RuntimeConfigLogic) deleteByID(modelPtr any, id uint64) error {
	db, err := l.writeDB()
	if err != nil {
		return errors.Tag(err)
	}
	appID, env := l.scope()
	result := db.Where("id = ? AND app_id = ? AND env = ?", id, appID, env).Delete(modelPtr)
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected == 0 {
		return errors.Errorf("记录不存在: %d", id)
	}
	return nil
}

// checkRuntimeConfigUpdated 区分数据库错误和按 ID 更新不到草稿的假成功。
func checkRuntimeConfigUpdated(result *gorm.DB, id uint64, subject string) error {
	if result == nil {
		return errors.Errorf("%s更新结果为空: %d", subject, id)
	}
	if result.Error != nil {
		return errors.Tag(result.Error)
	}
	if result.RowsAffected == 0 {
		return errors.Errorf("%s不存在: %d", subject, id)
	}
	return nil
}

// invalidateRuntimeConfigCache 删除运行配置状态和指定发布快照缓存。
func (l *RuntimeConfigLogic) invalidateRuntimeConfigCache(env string, releaseIDs ...uint64) error {
	manager, err := cachelogic.TableCacheManager(l.BaseLogic)
	if err != nil {
		return errors.Tag(err)
	}
	env = strings.TrimSpace(env)
	if env == "" {
		env = RuntimeEnv(l.currentConfig())
	}
	cacheKeys := []string{cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.RuntimeConfigStateKey(env))}
	for _, releaseID := range releaseIDs {
		if releaseID == 0 {
			continue
		}
		cacheKeys = append(cacheKeys, cachelogic.TableCachePhysicalKey(l.BaseLogic, keys.RuntimeConfigReleaseKey(releaseID)))
	}
	for _, cacheKey := range uniqueStrings(cacheKeys) {
		if cacheKey == "" {
			continue
		}
		if err = manager.DeleteByKey(l.Ctx, cacheKey); err != nil {
			return errors.Tag(err)
		}
	}
	return nil
}

// requireMFA 校验运行配置敏感操作的二次认证。
func (l *RuntimeConfigLogic) requireMFA(twoStepKey string, twoStepValue string) error {
	return (&adminlogic.AdminLogic{BaseLogic: l.BaseLogic}).RequireOperateMFATwoStep(securitylogic.MFAScenarioRuntimeConfigManage, twoStepKey, twoStepValue)
}

// draftCounts 统计当前环境周期任务和归档任务草稿数量。
func (l *RuntimeConfigLogic) draftCounts() (int64, int64, error) {
	db, err := l.writeDB()
	if err != nil {
		return 0, 0, errors.Tag(err)
	}
	appID, env := l.scope()
	var periodicCount int64
	// 统计两个草稿表时不复用 Statement，避免表名被上一条 Count 污染。
	if err = db.Session(&gorm.Session{NewDB: true}).Model(&model.RuntimeTaskPeriodic{}).Where("app_id = ? AND env = ?", appID, env).Count(&periodicCount).Error; err != nil {
		return 0, 0, errors.Tag(err)
	}
	var archiveCount int64
	if err = db.Session(&gorm.Session{NewDB: true}).Model(&model.RuntimeArchiveJob{}).Where("app_id = ? AND env = ?", appID, env).Count(&archiveCount).Error; err != nil {
		return 0, 0, errors.Tag(err)
	}
	return periodicCount, archiveCount, nil
}

// writeDB 返回运行配置写库连接。
func (l *RuntimeConfigLogic) writeDB() (*gorm.DB, error) {
	if l == nil || l.Svc == nil {
		return nil, errors.Errorf("服务上下文未初始化")
	}
	db := l.Svc.WriteDB(svc.DatabaseMain)
	if db == nil {
		return nil, errors.Errorf("main主库未初始化")
	}
	return db, nil
}

// currentConfig 返回当前服务配置快照。
func (l *RuntimeConfigLogic) currentConfig() config.Config {
	if l == nil || l.Svc == nil {
		return config.Config{}
	}
	return l.Svc.CurrentConfig()
}

// scope 返回运行配置的 appID 和环境边界。
func (l *RuntimeConfigLogic) scope() (string, string) {
	cfg := l.currentConfig()
	return strings.TrimSpace(cfg.AppID), RuntimeEnv(cfg)
}

// adminID 返回当前操作管理员 ID，缺失时返回 0。
func (l *RuntimeConfigLogic) adminID() int {
	admin := l.GetCtxAdmin()
	if admin == nil {
		return 0
	}
	return admin.ID
}
