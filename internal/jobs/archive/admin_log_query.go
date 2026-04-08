package archive

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"time"

	"admin_cron/common/embedasset"
	"admin_cron/internal/model"
	"admin_cron/internal/svc"
	"admin_cron/internal/types"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

// adminLogOrderPattern 约束管理员日志列表动态排序字段，避免 ORDER BY 注入。
var adminLogOrderPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// adminLogSelectColumns 统一维护管理员日志查询的列清单，预留给后续冷热表 UNION 时保证字段顺序完全一致。
var adminLogSelectColumns = strings.Join([]string{
	"id",
	"user_id",
	"user_name",
	"action",
	"route",
	"method",
	"describe",
	"data",
	"ip",
	"ipaddr",
	"trace_id",
	"span_id",
	"http_status",
	"biz_code",
	"latency_ms",
	"success",
	"error_message",
	"created_at",
}, ", ")

// adminLogSubQueryTemplate 保存管理员日志冷热表 UNION 子查询模板。
// 模板中的条件开关由请求参数和冷热边界决定，参数值仍通过 args 绑定，避免把用户输入拼进 SQL。
//
//go:embed admin_log_sub_query.sql.tmpl
var adminLogSubQueryTemplate string

// adminLogSubQueryTemplateData 描述管理员日志子查询模板渲染上下文。
type adminLogSubQueryTemplateData struct {
	SelectColumns string // 查询列清单，必须保持热表与历史表字段顺序一致
	TableName     string // 已转义的物理表名，来源于热表或历史区间表
	HasTraceID    bool   // 是否按 trace_id 精确筛选，用于串联一次请求链路
	HasUserID     bool   // 是否按管理员 ID 精确筛选
	HasUserName   bool   // 是否按管理员账号精确筛选
	HasAction     bool   // 是否按审计动作精确筛选
	HasStartTime  bool   // 是否包含请求起始时间过滤，闭区间下界
	HasEndTime    bool   // 是否包含请求结束时间过滤，闭区间上界
	HasLowerBound bool   // 是否包含冷热切分下界，闭区间下界
	HasUpperBound bool   // 是否包含冷热切分上界，开区间上界，避免跨表重复
}

// AdminLogQueryMeta 描述管理员日志查询元信息；当前热表查询不会命中历史表。
type AdminLogQueryMeta struct {
	ArchiveEnabled  bool     `json:"archiveEnabled"`            // 是否启用归档查询
	WatermarkTime   string   `json:"watermarkTime,omitempty"`   // 当前查询使用的 watermark 排他边界
	SafeTime        string   `json:"safeTime,omitempty"`        // 当前安全时间
	HistoryTables   []string `json:"historyTables,omitempty"`   // 本次命中的历史表列表
	QueryWriteDB    bool     `json:"queryWriteDB"`              // 是否强制走主库查询
	HistorySegments int      `json:"historySegments,omitempty"` // 命中的历史表数量
}

// QueryAdminLogs 查询管理员审计日志。
// 当前策略只查询热表数据，不拼接历史归档表。
func (s *Service) QueryAdminLogs(ctx context.Context, req *types.AdminLogQueryReq) ([]model.AdminLog, int64, AdminLogQueryMeta, error) {
	if req == nil {
		return nil, 0, AdminLogQueryMeta{}, errors.Errorf("管理员日志查询参数不能为空")
	}
	startTime, endTime, err := req.TimeRange()
	if err != nil {
		return nil, 0, AdminLogQueryMeta{}, errors.Tag(err)
	}
	return s.queryAdminLogsDirect(ctx, req, startTime, endTime, s.adminLogQueryJob())
}

// queryAdminLogsDirect 在未启用归档或尚未形成有效 watermark 时直接查询热表。
func (s *Service) queryAdminLogsDirect(ctx context.Context, req *types.AdminLogQueryReq, startTime, endTime *time.Time, job jobConfig) ([]model.AdminLog, int64, AdminLogQueryMeta, error) {
	db := s.jobQueryDB(job)
	if db == nil {
		db = withWriteResolver(s.jobSourceWriteDB(job))
	}
	query := db.WithContext(ctx).Model(&model.AdminLog{})
	query = applyAdminLogFilters(query, req, startTime, endTime)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, AdminLogQueryMeta{}, errors.Tag(err)
	}
	if total == 0 {
		return []model.AdminLog{}, 0, AdminLogQueryMeta{ArchiveEnabled: false, QueryWriteDB: job.QueryWriteDB}, nil
	}
	orderQuery, err := applyAdminLogOrder(query, req.OrderBy, req.Order)
	if err != nil {
		return nil, 0, AdminLogQueryMeta{}, errors.Tag(err)
	}
	items := make([]model.AdminLog, 0, req.PageSize)
	if err = orderQuery.Offset((req.Page - 1) * req.PageSize).Limit(req.PageSize).Find(&items).Error; err != nil {
		return nil, 0, AdminLogQueryMeta{}, errors.Tag(err)
	}
	return items, total, AdminLogQueryMeta{ArchiveEnabled: false, QueryWriteDB: job.QueryWriteDB}, nil
}

// adminLogQueryJob 返回管理员日志热表查询配置。
// 若归档 job 未配置，则回退到后台库 admin_log 热表，保证查询能力不受归档配置影响。
func (s *Service) adminLogQueryJob() jobConfig {
	if job, ok := s.jobByName(JobNameAdminLog); ok {
		return job
	}
	return jobConfig{
		Name:      JobNameAdminLog,
		Database:  svc.DatabaseMain,
		TableName: model.TableNameAdminLog,
	}
}

// planAdminLogQuery 根据请求时间范围和 watermark 计算应命中的历史表集合及是否需要补查热表。
// 当前 QueryAdminLogs 未启用该冷热合并计划，保留给后续恢复历史表查询时复用。
func (s *Service) planAdminLogQuery(ctx context.Context, controlDB *gorm.DB, historyDB *gorm.DB, job jobConfig, startTime, endTime *time.Time, watermark time.Time) ([]string, bool, error) {
	includeHot := endTime == nil || !endTime.Before(watermark)
	needHistory := startTime == nil || startTime.Before(watermark)
	if !needHistory {
		return nil, includeHot, nil
	}
	historyUpper := watermark
	if endTime != nil && endTime.Before(historyUpper) {
		historyUpper = *endTime
	}
	if !historyUpper.After(time.Time{}) {
		return nil, includeHot, nil
	}
	rangeStart := time.Time{}
	if startTime != nil {
		rangeStart = *startTime
	}
	var tables []string
	if err := controlDB.WithContext(ctx).
		Model(&Segment{}).
		Distinct("history_table_name").
		Where("job_name = ? AND status IN ? AND range_start < ? AND range_end > ?", job.Name, []string{statusDone, statusDeleting, statusDeleted}, historyUpper, rangeStart).
		Order("range_start ASC").
		Pluck("history_table_name", &tables).Error; err != nil {
		return nil, false, errors.Tag(err)
	}
	existing := make([]string, 0, len(tables))
	for _, tableName := range tables {
		if tableExists(ctx, historyDB, tableName) {
			existing = append(existing, tableName)
		}
	}
	return existing, includeHot, nil
}

// buildAdminLogSubQuery 构造单张日志表的查询子句，预留给后续冷热 UNION 路由复用。
func buildAdminLogSubQuery(tableName string, req *types.AdminLogQueryReq, startTime, endTime, lowerBound, upperBound *time.Time) (string, []any) {
	args := make([]any, 0, 16)
	data := adminLogSubQueryTemplateData{
		SelectColumns: adminLogSelectColumns,
		TableName:     quoteIdent(tableName),
		HasStartTime:  startTime != nil,
		HasEndTime:    endTime != nil,
		HasLowerBound: lowerBound != nil,
		HasUpperBound: upperBound != nil,
	}
	if req != nil {
		data.HasTraceID = req.TraceID != ""
		data.HasUserID = req.UserID != nil
		data.HasUserName = req.UserName != ""
		data.HasAction = req.Action != ""
	}
	if req != nil && data.HasTraceID {
		args = append(args, req.TraceID)
	}
	if req != nil && data.HasUserID {
		args = append(args, *req.UserID)
	}
	if req != nil && data.HasUserName {
		args = append(args, req.UserName)
	}
	if req != nil && data.HasAction {
		args = append(args, req.Action)
	}
	if startTime != nil {
		args = append(args, *startTime)
	}
	if endTime != nil {
		args = append(args, *endTime)
	}
	if lowerBound != nil {
		args = append(args, *lowerBound)
	}
	if upperBound != nil {
		args = append(args, *upperBound)
	}
	return renderAdminLogSubQuery(data), args
}

// renderAdminLogSubQuery 渲染管理员日志子查询模板。
// 模板文件只包含固定 SQL 结构，动态表名已在调用前转义，用户输入全部通过 args 参数绑定。
func renderAdminLogSubQuery(data adminLogSubQueryTemplateData) string {
	tpl := template.Must(template.New("admin_log_sub_query").Parse(embedasset.StripLeadingLineComments(adminLogSubQueryTemplate, "--")))
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		panic(err)
	}
	return strings.TrimSpace(buf.String())
}

// applyAdminLogFilters 把管理员日志查询条件统一追加到 GORM 查询链上。
func applyAdminLogFilters(query *gorm.DB, req *types.AdminLogQueryReq, startTime, endTime *time.Time) *gorm.DB {
	if req.TraceID != "" {
		query = query.Where("trace_id = ?", req.TraceID)
	}
	if req.UserID != nil {
		query = query.Where("user_id = ?", *req.UserID)
	}
	if req.UserName != "" {
		query = query.Where("user_name = ?", req.UserName)
	}
	if req.Action != "" {
		query = query.Where("action = ?", req.Action)
	}
	if startTime != nil {
		query = query.Where("created_at >= ?", *startTime)
	}
	if endTime != nil {
		query = query.Where("created_at <= ?", *endTime)
	}
	return query
}

// applyAdminLogOrder 统一处理管理员日志查询排序。
func applyAdminLogOrder(query *gorm.DB, orderBy, order string) (*gorm.DB, error) {
	orderClause, err := buildAdminLogOrderClause(orderBy, order)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if orderClause == "created_at DESC, id DESC" {
		return query.Order("created_at DESC").Order("id DESC"), nil
	}
	return query.Order(orderClause), nil
}

// buildAdminLogOrderClause 生成安全的 ORDER BY 片段。
func buildAdminLogOrderClause(orderBy, order string) (string, error) {
	orderBy = strings.TrimSpace(orderBy)
	if orderBy == "" {
		return "created_at DESC, id DESC", nil
	}
	if !adminLogOrderPattern.MatchString(orderBy) {
		return "", errors.Errorf("排序字段不合法: %s", orderBy)
	}
	normalizedOrder := strings.ToLower(strings.TrimSpace(order))
	if normalizedOrder == "" {
		normalizedOrder = "desc"
	}
	if normalizedOrder != "asc" && normalizedOrder != "desc" {
		return "", errors.Errorf("排序方向不合法: %s", order)
	}
	return fmt.Sprintf("%s %s", quoteIdent(orderBy), normalizedOrder), nil
}

// jobQueryDB 返回管理员日志查询应使用的连接。
// 对归档敏感场景可配置强制走主库，避免主从延迟影响冷热切分一致性。
func (s *Service) jobQueryDB(job jobConfig) *gorm.DB {
	if s == nil || s.svcCtx == nil {
		return nil
	}
	if job.QueryWriteDB {
		return s.jobSourceWriteDB(job)
	}
	if db := s.svcCtx.ReadDB(job.Database); db != nil {
		return db
	}
	return s.jobSourceWriteDB(job)
}

// jobByTableName 根据热表名反查归档任务配置，预留给后续其他查询路由复用。
func (s *Service) jobByTableName(tableName string) (jobConfig, bool) {
	tableName = strings.TrimSpace(tableName)
	for _, job := range s.normalizedJobs() {
		if job.TableName == tableName {
			return job, true
		}
	}
	return jobConfig{}, false
}
