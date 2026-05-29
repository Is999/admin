package runtimeconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"admin/internal/task/queue"
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
	ActiveReleaseID uint64 `json:"active_release_id"` // 当前发布 ID
	ActiveVersion   uint64 `json:"active_version"`    // 当前版本号
	ActiveChecksum  string `json:"active_checksum"`   // 当前快照 SHA256
	PublishedAtUnix int64  `json:"published_at_unix"` // 最近发布时间戳
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
	return ReleaseSnapshot{
		ArchiveJobs:  append([]config.ArchiveJobConfig(nil), cfg.Archive.Jobs...),
		TaskPeriodic: append([]config.TaskPeriodicConfig(nil), cfg.Task.Periodic...),
	}
}

// ApplySnapshot 把发布快照覆盖到完整配置；不会修改 YAML-only 段。
func ApplySnapshot(cfg *config.Config, snapshot ReleaseSnapshot) {
	if cfg == nil {
		return
	}
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
		return types.NewBizResult(codes.ParamError).WithError(corelogic.WrapLogicError(err, "RuntimeConfigLogic.ValidateDraft 预检失败")).
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

func (l *RuntimeConfigLogic) publishDraft(remark string, baseReleaseID uint64) (*types.RuntimeConfigPublishResp, error) {
	snapshot, err := l.buildDraftSnapshot()
	if err != nil {
		return nil, errors.Tag(err)
	}
	return l.publishSnapshot(snapshot, remark, baseReleaseID)
}

func (l *RuntimeConfigLogic) publishSnapshot(snapshot ReleaseSnapshot, remark string, baseReleaseID uint64) (*types.RuntimeConfigPublishResp, error) {
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
			PublishedByName:    admin.Name,
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

func (l *RuntimeConfigLogic) buildDraftSnapshot() (ReleaseSnapshot, error) {
	db, err := l.writeDB()
	if err != nil {
		return ReleaseSnapshot{}, errors.Tag(err)
	}
	appID, env := l.scope()
	var periodicRows []model.RuntimeTaskPeriodic
	if err = db.Where("app_id = ? AND env = ?", appID, env).
		Order("sort_order ASC").Order("id ASC").Find(&periodicRows).Error; err != nil {
		return ReleaseSnapshot{}, errors.Tag(err)
	}
	var archiveRows []model.RuntimeArchiveJob
	if err = db.Where("app_id = ? AND env = ?", appID, env).
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

func (l *RuntimeConfigLogic) replaceDraft(snapshot ReleaseSnapshot) error {
	db, err := l.writeDB()
	if err != nil {
		return errors.Tag(err)
	}
	appID, env := l.scope()
	adminID := l.adminID()
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("app_id = ? AND env = ?", appID, env).Delete(&model.RuntimeTaskPeriodic{}).Error; err != nil {
			return errors.Tag(err)
		}
		if err := tx.Where("app_id = ? AND env = ?", appID, env).Delete(&model.RuntimeArchiveJob{}).Error; err != nil {
			return errors.Tag(err)
		}
		for index, item := range snapshot.TaskPeriodic {
			row := periodicConfigToModel(item, appID, env, adminID, index)
			if err := tx.Create(&row).Error; err != nil {
				return errors.Tag(err)
			}
		}
		for index, item := range snapshot.ArchiveJobs {
			row := archiveConfigToModel(item, appID, env, adminID, index)
			if err := tx.Create(&row).Error; err != nil {
				return errors.Tag(err)
			}
		}
		return nil
	})
}

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

func (l *RuntimeConfigLogic) requireMFA(twoStepKey string, twoStepValue string) error {
	return (&adminlogic.AdminLogic{BaseLogic: l.BaseLogic}).RequireOperateMFATwoStep(securitylogic.MFAScenarioRuntimeConfigManage, twoStepKey, twoStepValue)
}

func (l *RuntimeConfigLogic) draftCounts() (int64, int64, error) {
	db, err := l.writeDB()
	if err != nil {
		return 0, 0, errors.Tag(err)
	}
	appID, env := l.scope()
	var periodicCount int64
	if err = db.Model(&model.RuntimeTaskPeriodic{}).Where("app_id = ? AND env = ?", appID, env).Count(&periodicCount).Error; err != nil {
		return 0, 0, errors.Tag(err)
	}
	var archiveCount int64
	if err = db.Model(&model.RuntimeArchiveJob{}).Where("app_id = ? AND env = ?", appID, env).Count(&archiveCount).Error; err != nil {
		return 0, 0, errors.Tag(err)
	}
	return periodicCount, archiveCount, nil
}

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

func (l *RuntimeConfigLogic) currentConfig() config.Config {
	if l == nil || l.Svc == nil {
		return config.Config{}
	}
	return l.Svc.CurrentConfig()
}

func (l *RuntimeConfigLogic) scope() (string, string) {
	cfg := l.currentConfig()
	return strings.TrimSpace(cfg.AppID), RuntimeEnv(cfg)
}

func (l *RuntimeConfigLogic) adminID() int {
	admin := l.GetCtxAdmin()
	if admin == nil {
		return 0
	}
	return admin.ID
}

// ValidateSnapshot 校验发布快照的周期任务和归档任务唯一性。
func ValidateSnapshot(snapshot ReleaseSnapshot) ([]string, error) {
	messages := []string{
		fmt.Sprintf("周期任务 %d 条", len(snapshot.TaskPeriodic)),
		fmt.Sprintf("归档任务 %d 条", len(snapshot.ArchiveJobs)),
	}
	if err := validatePeriodicConfigs(snapshot.TaskPeriodic); err != nil {
		return messages, errors.Tag(err)
	}
	if err := validateArchiveJobConfigs(snapshot.ArchiveJobs); err != nil {
		return messages, errors.Tag(err)
	}
	return messages, nil
}

func validatePeriodicConfigs(items []config.TaskPeriodicConfig) error {
	taskKeys := make(map[string]struct{}, len(items))
	uniqueKeys := make(map[string]struct{}, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Workflow) == "" {
			return errors.Errorf("周期任务 workflow 不能为空 name=%s", strings.TrimSpace(item.Name))
		}
		if strings.TrimSpace(item.Cron) == "" && item.EverySeconds <= 0 {
			return errors.Errorf("周期任务必须配置 cron 或 every_seconds name=%s", strings.TrimSpace(item.Name))
		}
		if strings.TrimSpace(item.Cron) != "" && item.EverySeconds > 0 {
			return errors.Errorf("周期任务 cron 与 every_seconds 不能同时配置 name=%s", strings.TrimSpace(item.Name))
		}
		if item.GrayPercent < 0 || item.GrayPercent > 100 {
			return errors.Errorf("周期任务 gray_percent 必须在 0 到 100 之间 name=%s", strings.TrimSpace(item.Name))
		}
		if item.Retry < 0 || item.TimeoutSeconds < 0 || item.ShardTotal < 0 || item.UniqueTTLSeconds < 0 {
			return errors.Errorf("周期任务数值参数不能小于 0 name=%s", strings.TrimSpace(item.Name))
		}
		taskKey, err := taskqueue.PeriodicTaskConfigKey(item)
		if err != nil {
			return errors.Wrapf(err, "周期任务配置无效 name=%s workflow=%s", strings.TrimSpace(item.Name), strings.TrimSpace(item.Workflow))
		}
		if _, ok := taskKeys[taskKey]; ok {
			return errors.Errorf("周期任务稳定键重复: %s", taskKey)
		}
		taskKeys[taskKey] = struct{}{}
		uniqueKey := strings.TrimSpace(item.UniqueKey)
		if uniqueKey == "" {
			continue
		}
		if _, ok := uniqueKeys[uniqueKey]; ok {
			return errors.Errorf("周期任务 unique_key 重复: %s", uniqueKey)
		}
		uniqueKeys[uniqueKey] = struct{}{}
	}
	return nil
}

func validateArchiveJobConfigs(items []config.ArchiveJobConfig) error {
	names := make(map[string]struct{}, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return errors.Errorf("归档任务 name 不能为空")
		}
		if _, ok := names[name]; ok {
			return errors.Errorf("归档任务名称重复: %s", name)
		}
		names[name] = struct{}{}
		if strings.TrimSpace(item.TableName) == "" {
			return errors.Errorf("归档任务 table_name 不能为空 name=%s", name)
		}
		if item.CustomDays < 0 || item.HotKeepDays < 0 || item.ArchiveDelayDays < 0 ||
			item.ArchiveWindowSeconds < 0 || item.ArchiveMaxWindowsPerRun < 0 ||
			item.ArchiveAutoMaxWindows < 0 || item.ArchiveAutoLightRows < 0 ||
			item.ArchiveAutoLightMs < 0 || item.DeleteDelayDays < 0 ||
			item.DeleteWindowSeconds < 0 || item.DeleteMaxWindowsPerRun < 0 ||
			item.BatchSize < 0 || item.DeleteBatchSize < 0 || item.MaxHistoryTables < 0 {
			return errors.Errorf("归档任务数值参数不能小于 0 name=%s", name)
		}
	}
	return nil
}

func periodicReqToModel(req *types.SaveRuntimeTaskPeriodicReq, appID, env string, adminID int) model.RuntimeTaskPeriodic {
	return model.RuntimeTaskPeriodic{
		AppID:            appID,
		Env:              env,
		Name:             req.Name,
		Enabled:          req.Enabled,
		Cron:             req.Cron,
		EverySeconds:     req.EverySeconds,
		Workflow:         req.Workflow,
		Queue:            req.Queue,
		Targets:          model.StringSlice(req.Targets),
		ShardTotal:       req.ShardTotal,
		GrayPercent:      req.GrayPercent,
		Retry:            req.Retry,
		TimeoutSeconds:   req.TimeoutSeconds,
		Deadline:         req.Deadline,
		UniqueKey:        req.UniqueKey,
		UniqueTTLSeconds: req.UniqueTTLSeconds,
		SortOrder:        req.SortOrder,
		Remark:           req.Remark,
		CreatedByAdminID: adminID,
		UpdatedByAdminID: adminID,
	}
}

func periodicModelUpdateMap(row model.RuntimeTaskPeriodic) map[string]any {
	return map[string]any{
		"name":                row.Name,
		"enabled":             row.Enabled,
		"cron":                row.Cron,
		"every_seconds":       row.EverySeconds,
		"workflow":            row.Workflow,
		"queue":               row.Queue,
		"targets_json":        row.Targets,
		"shard_total":         row.ShardTotal,
		"gray_percent":        row.GrayPercent,
		"retry":               row.Retry,
		"timeout_seconds":     row.TimeoutSeconds,
		"deadline":            row.Deadline,
		"unique_key":          row.UniqueKey,
		"unique_ttl_seconds":  row.UniqueTTLSeconds,
		"sort_order":          row.SortOrder,
		"remark":              row.Remark,
		"updated_by_admin_id": row.UpdatedByAdminID,
	}
}

func periodicModelToConfig(row model.RuntimeTaskPeriodic) config.TaskPeriodicConfig {
	enabled := row.Enabled
	return config.TaskPeriodicConfig{
		Enabled:          &enabled,
		Name:             row.Name,
		Cron:             row.Cron,
		EverySeconds:     row.EverySeconds,
		Workflow:         row.Workflow,
		Queue:            row.Queue,
		Targets:          []string(row.Targets),
		ShardTotal:       row.ShardTotal,
		GrayPercent:      row.GrayPercent,
		Retry:            row.Retry,
		TimeoutSeconds:   row.TimeoutSeconds,
		Deadline:         row.Deadline,
		UniqueKey:        row.UniqueKey,
		UniqueTTLSeconds: row.UniqueTTLSeconds,
	}
}

func periodicConfigToModel(item config.TaskPeriodicConfig, appID, env string, adminID int, index int) model.RuntimeTaskPeriodic {
	enabled := true
	if item.Enabled != nil {
		enabled = *item.Enabled
	}
	return model.RuntimeTaskPeriodic{
		AppID:            appID,
		Env:              env,
		Name:             strings.TrimSpace(item.Name),
		Enabled:          enabled,
		Cron:             strings.TrimSpace(item.Cron),
		EverySeconds:     item.EverySeconds,
		Workflow:         strings.TrimSpace(item.Workflow),
		Queue:            strings.TrimSpace(item.Queue),
		Targets:          model.StringSlice(uniqueStrings(item.Targets)),
		ShardTotal:       item.ShardTotal,
		GrayPercent:      item.GrayPercent,
		Retry:            item.Retry,
		TimeoutSeconds:   item.TimeoutSeconds,
		Deadline:         strings.TrimSpace(item.Deadline),
		UniqueKey:        strings.TrimSpace(item.UniqueKey),
		UniqueTTLSeconds: item.UniqueTTLSeconds,
		SortOrder:        index + 1,
		CreatedByAdminID: adminID,
		UpdatedByAdminID: adminID,
	}
}

func periodicModelToItem(row model.RuntimeTaskPeriodic) types.RuntimeTaskPeriodicItem {
	return types.RuntimeTaskPeriodicItem{
		ID:               row.ID,
		Enabled:          row.Enabled,
		Name:             row.Name,
		Cron:             row.Cron,
		EverySeconds:     row.EverySeconds,
		Workflow:         row.Workflow,
		Queue:            row.Queue,
		Targets:          []string(row.Targets),
		ShardTotal:       row.ShardTotal,
		GrayPercent:      row.GrayPercent,
		Retry:            row.Retry,
		TimeoutSeconds:   row.TimeoutSeconds,
		Deadline:         row.Deadline,
		UniqueKey:        row.UniqueKey,
		UniqueTTLSeconds: row.UniqueTTLSeconds,
		SortOrder:        row.SortOrder,
		Remark:           row.Remark,
		CreatedAt:        corelogic.FormatDateTime(row.CreatedAt),
		UpdatedAt:        corelogic.FormatDateTime(row.UpdatedAt),
	}
}

func archiveReqToModel(req *types.SaveRuntimeArchiveJobReq, appID, env string, adminID int) model.RuntimeArchiveJob {
	return model.RuntimeArchiveJob{
		AppID:                   appID,
		Env:                     env,
		Name:                    req.Name,
		Enabled:                 req.Enabled,
		Database:                req.Database,
		HotTableName:            req.TableName,
		TimeColumn:              req.TimeColumn,
		TimeColumnType:          req.TimeColumnType,
		TimeColumnFormat:        req.TimeColumnFormat,
		TimeColumnUnixUnit:      req.TimeColumnUnixUnit,
		PrimaryKey:              req.PrimaryKey,
		ArchiveCondition:        req.ArchiveCondition,
		DeleteCondition:         req.DeleteCondition,
		SplitUnit:               req.SplitUnit,
		CustomDays:              req.CustomDays,
		HotKeepDays:             req.HotKeepDays,
		ArchiveDelayDays:        req.ArchiveDelayDays,
		ArchiveWindowSeconds:    req.ArchiveWindowSeconds,
		ArchiveWindowMode:       req.ArchiveWindowMode,
		ArchiveMaxWindowsPerRun: req.ArchiveMaxWindowsPerRun,
		ArchiveAutoMaxWindows:   req.ArchiveAutoMaxWindows,
		ArchiveAutoLightRows:    req.ArchiveAutoLightRows,
		ArchiveAutoLightMs:      req.ArchiveAutoLightMs,
		DeleteDisabled:          req.DeleteDisabled,
		DeleteDelayDays:         req.DeleteDelayDays,
		DeleteWindowSeconds:     req.DeleteWindowSeconds,
		DeleteMaxWindowsPerRun:  req.DeleteMaxWindowsPerRun,
		BatchSize:               req.BatchSize,
		DeleteBatchSize:         req.DeleteBatchSize,
		MaxHistoryTables:        req.MaxHistoryTables,
		HistoryTablePrefix:      req.HistoryTablePrefix,
		HistoryTableNameRule:    req.HistoryTableNameRule,
		StartAt:                 req.StartAt,
		QueryWriteDB:            req.QueryWriteDB,
		SortOrder:               req.SortOrder,
		Remark:                  req.Remark,
		CreatedByAdminID:        adminID,
		UpdatedByAdminID:        adminID,
	}
}

func archiveModelUpdateMap(row model.RuntimeArchiveJob) map[string]any {
	return map[string]any{
		"name":                        row.Name,
		"enabled":                     row.Enabled,
		"database_name":               row.Database,
		"table_name":                  row.HotTableName,
		"time_column":                 row.TimeColumn,
		"time_column_type":            row.TimeColumnType,
		"time_column_format":          row.TimeColumnFormat,
		"time_column_unix_unit":       row.TimeColumnUnixUnit,
		"primary_key":                 row.PrimaryKey,
		"archive_condition":           row.ArchiveCondition,
		"delete_condition":            row.DeleteCondition,
		"split_unit":                  row.SplitUnit,
		"custom_days":                 row.CustomDays,
		"hot_keep_days":               row.HotKeepDays,
		"archive_delay_days":          row.ArchiveDelayDays,
		"archive_window_seconds":      row.ArchiveWindowSeconds,
		"archive_window_mode":         row.ArchiveWindowMode,
		"archive_max_windows_per_run": row.ArchiveMaxWindowsPerRun,
		"archive_auto_max_windows":    row.ArchiveAutoMaxWindows,
		"archive_auto_light_rows":     row.ArchiveAutoLightRows,
		"archive_auto_light_ms":       row.ArchiveAutoLightMs,
		"delete_disabled":             row.DeleteDisabled,
		"delete_delay_days":           row.DeleteDelayDays,
		"delete_window_seconds":       row.DeleteWindowSeconds,
		"delete_max_windows_per_run":  row.DeleteMaxWindowsPerRun,
		"batch_size":                  row.BatchSize,
		"delete_batch_size":           row.DeleteBatchSize,
		"max_history_tables":          row.MaxHistoryTables,
		"history_table_prefix":        row.HistoryTablePrefix,
		"history_table_name_rule":     row.HistoryTableNameRule,
		"start_at":                    row.StartAt,
		"query_write_db":              row.QueryWriteDB,
		"sort_order":                  row.SortOrder,
		"remark":                      row.Remark,
		"updated_by_admin_id":         row.UpdatedByAdminID,
	}
}

func archiveModelToConfig(row model.RuntimeArchiveJob) config.ArchiveJobConfig {
	return config.ArchiveJobConfig{
		Name:                    row.Name,
		Enabled:                 row.Enabled,
		Database:                row.Database,
		TableName:               row.HotTableName,
		TimeColumn:              row.TimeColumn,
		TimeColumnType:          row.TimeColumnType,
		TimeColumnFormat:        row.TimeColumnFormat,
		TimeColumnUnixUnit:      row.TimeColumnUnixUnit,
		PrimaryKey:              row.PrimaryKey,
		ArchiveCondition:        row.ArchiveCondition,
		DeleteCondition:         row.DeleteCondition,
		SplitUnit:               row.SplitUnit,
		CustomDays:              row.CustomDays,
		HotKeepDays:             row.HotKeepDays,
		ArchiveDelayDays:        row.ArchiveDelayDays,
		ArchiveWindowSeconds:    row.ArchiveWindowSeconds,
		ArchiveWindowMode:       row.ArchiveWindowMode,
		ArchiveMaxWindowsPerRun: row.ArchiveMaxWindowsPerRun,
		ArchiveAutoMaxWindows:   row.ArchiveAutoMaxWindows,
		ArchiveAutoLightRows:    row.ArchiveAutoLightRows,
		ArchiveAutoLightMs:      row.ArchiveAutoLightMs,
		DeleteDisabled:          row.DeleteDisabled,
		DeleteDelayDays:         row.DeleteDelayDays,
		DeleteWindowSeconds:     row.DeleteWindowSeconds,
		DeleteMaxWindowsPerRun:  row.DeleteMaxWindowsPerRun,
		BatchSize:               row.BatchSize,
		DeleteBatchSize:         row.DeleteBatchSize,
		MaxHistoryTables:        row.MaxHistoryTables,
		HistoryTablePrefix:      row.HistoryTablePrefix,
		HistoryTableNameRule:    row.HistoryTableNameRule,
		StartAt:                 row.StartAt,
		QueryWriteDB:            row.QueryWriteDB,
	}
}

func archiveConfigToModel(item config.ArchiveJobConfig, appID, env string, adminID int, index int) model.RuntimeArchiveJob {
	database := strings.TrimSpace(item.Database)
	if database == "" {
		database = "main"
	}
	return model.RuntimeArchiveJob{
		AppID:                   appID,
		Env:                     env,
		Name:                    strings.TrimSpace(item.Name),
		Enabled:                 item.Enabled,
		Database:                database,
		HotTableName:            strings.TrimSpace(item.TableName),
		TimeColumn:              strings.TrimSpace(item.TimeColumn),
		TimeColumnType:          strings.TrimSpace(item.TimeColumnType),
		TimeColumnFormat:        strings.TrimSpace(item.TimeColumnFormat),
		TimeColumnUnixUnit:      strings.TrimSpace(item.TimeColumnUnixUnit),
		PrimaryKey:              strings.TrimSpace(item.PrimaryKey),
		ArchiveCondition:        strings.TrimSpace(item.ArchiveCondition),
		DeleteCondition:         strings.TrimSpace(item.DeleteCondition),
		SplitUnit:               strings.TrimSpace(item.SplitUnit),
		CustomDays:              item.CustomDays,
		HotKeepDays:             item.HotKeepDays,
		ArchiveDelayDays:        item.ArchiveDelayDays,
		ArchiveWindowSeconds:    item.ArchiveWindowSeconds,
		ArchiveWindowMode:       strings.TrimSpace(item.ArchiveWindowMode),
		ArchiveMaxWindowsPerRun: item.ArchiveMaxWindowsPerRun,
		ArchiveAutoMaxWindows:   item.ArchiveAutoMaxWindows,
		ArchiveAutoLightRows:    item.ArchiveAutoLightRows,
		ArchiveAutoLightMs:      item.ArchiveAutoLightMs,
		DeleteDisabled:          item.DeleteDisabled,
		DeleteDelayDays:         item.DeleteDelayDays,
		DeleteWindowSeconds:     item.DeleteWindowSeconds,
		DeleteMaxWindowsPerRun:  item.DeleteMaxWindowsPerRun,
		BatchSize:               item.BatchSize,
		DeleteBatchSize:         item.DeleteBatchSize,
		MaxHistoryTables:        item.MaxHistoryTables,
		HistoryTablePrefix:      strings.TrimSpace(item.HistoryTablePrefix),
		HistoryTableNameRule:    strings.TrimSpace(item.HistoryTableNameRule),
		StartAt:                 strings.TrimSpace(item.StartAt),
		QueryWriteDB:            item.QueryWriteDB,
		SortOrder:               index + 1,
		CreatedByAdminID:        adminID,
		UpdatedByAdminID:        adminID,
	}
}

func archiveModelToItem(row model.RuntimeArchiveJob) types.RuntimeArchiveJobItem {
	return types.RuntimeArchiveJobItem{
		ID:                      row.ID,
		Enabled:                 row.Enabled,
		Name:                    row.Name,
		Database:                row.Database,
		TableName:               row.HotTableName,
		TimeColumn:              row.TimeColumn,
		TimeColumnType:          row.TimeColumnType,
		TimeColumnFormat:        row.TimeColumnFormat,
		TimeColumnUnixUnit:      row.TimeColumnUnixUnit,
		PrimaryKey:              row.PrimaryKey,
		ArchiveCondition:        row.ArchiveCondition,
		DeleteCondition:         row.DeleteCondition,
		SplitUnit:               row.SplitUnit,
		CustomDays:              row.CustomDays,
		HotKeepDays:             row.HotKeepDays,
		ArchiveDelayDays:        row.ArchiveDelayDays,
		ArchiveWindowSeconds:    row.ArchiveWindowSeconds,
		ArchiveWindowMode:       row.ArchiveWindowMode,
		ArchiveMaxWindowsPerRun: row.ArchiveMaxWindowsPerRun,
		ArchiveAutoMaxWindows:   row.ArchiveAutoMaxWindows,
		ArchiveAutoLightRows:    row.ArchiveAutoLightRows,
		ArchiveAutoLightMs:      row.ArchiveAutoLightMs,
		DeleteDisabled:          row.DeleteDisabled,
		DeleteDelayDays:         row.DeleteDelayDays,
		DeleteWindowSeconds:     row.DeleteWindowSeconds,
		DeleteMaxWindowsPerRun:  row.DeleteMaxWindowsPerRun,
		BatchSize:               row.BatchSize,
		DeleteBatchSize:         row.DeleteBatchSize,
		MaxHistoryTables:        row.MaxHistoryTables,
		HistoryTablePrefix:      row.HistoryTablePrefix,
		HistoryTableNameRule:    row.HistoryTableNameRule,
		StartAt:                 row.StartAt,
		QueryWriteDB:            row.QueryWriteDB,
		SortOrder:               row.SortOrder,
		Remark:                  row.Remark,
		CreatedAt:               corelogic.FormatDateTime(row.CreatedAt),
		UpdatedAt:               corelogic.FormatDateTime(row.UpdatedAt),
	}
}

func stateCacheToItem(state StateCache) types.RuntimeConfigStateItem {
	return types.RuntimeConfigStateItem{
		ActiveReleaseID: state.ActiveReleaseID,
		ActiveVersion:   state.ActiveVersion,
		ActiveChecksum:  state.ActiveChecksum,
		PublishedAt:     formatUnix(state.PublishedAtUnix),
	}
}

func releaseModelToItem(row model.RuntimeConfigRelease) types.RuntimeConfigReleaseItem {
	return types.RuntimeConfigReleaseItem{
		ID:                 row.ID,
		VersionNo:          row.VersionNo,
		Checksum:           row.Checksum,
		BaseReleaseID:      row.BaseReleaseID,
		RestartRequired:    row.RestartRequired,
		RestartReason:      row.RestartReason,
		Remark:             row.Remark,
		PublishedByAdminID: row.PublishedByAdminID,
		PublishedByName:    row.PublishedByName,
		PublishedAt:        corelogic.FormatDateTime(row.PublishedAt),
	}
}

func snapshotToResp(snapshot ReleaseSnapshot) types.RuntimeConfigSnapshot {
	resp := types.RuntimeConfigSnapshot{
		ArchiveJobs:  make([]types.RuntimeArchiveJobItem, 0, len(snapshot.ArchiveJobs)),
		TaskPeriodic: make([]types.RuntimeTaskPeriodicItem, 0, len(snapshot.TaskPeriodic)),
	}
	for index, item := range snapshot.ArchiveJobs {
		resp.ArchiveJobs = append(resp.ArchiveJobs, archiveModelToItem(archiveConfigToModel(item, "", "", 0, index)))
	}
	for index, item := range snapshot.TaskPeriodic {
		resp.TaskPeriodic = append(resp.TaskPeriodic, periodicModelToItem(periodicConfigToModel(item, "", "", 0, index)))
	}
	return resp
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func formatUnix(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return corelogic.FormatDateTime(time.Unix(ts, 0))
}

func uniqueStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}
