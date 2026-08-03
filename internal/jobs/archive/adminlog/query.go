package adminlog

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"admin/internal/jobs/archive"
	"admin/internal/model"
	"admin/internal/svc"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

const (
	// JobName 是管理员审计日志对应的归档任务名。
	JobName = "admin_log"
)

// Meta 描述管理员日志查询元信息。
type Meta struct {
	ArchiveEnabled bool `json:"archiveEnabled"` // 是否启用归档查询能力
	QueryWriteDB   bool `json:"queryWriteDB"`   // 是否强制走写库查询
}

// adminLogOrderPattern 约束管理员日志列表动态排序字段，避免 ORDER BY 注入。
var adminLogOrderPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Query 查询管理员审计日志热表，并复用同名归档任务的数据源路由配置。
func Query(ctx context.Context, svcCtx *svc.ServiceContext, req *types.AdminLogQueryReq) ([]model.AdminLog, int64, Meta, error) {
	if req == nil {
		return nil, 0, Meta{}, errors.Errorf("管理员日志查询参数不能为空")
	}
	startTime, endTime, err := req.TimeRange()
	if err != nil {
		return nil, 0, Meta{}, errors.Tag(err)
	}
	source := querySource(svcCtx)
	return queryDirect(ctx, queryDB(svcCtx, source), req, startTime, endTime, source.QueryWriteDB)
}

// querySource 返回管理员日志查询的数据源属性，未配置归档任务时回退默认主库。
func querySource(svcCtx *svc.ServiceContext) archive.JobQuerySource {
	if source, ok := archive.NewService(svcCtx).JobQuerySource(JobName); ok {
		return source
	}
	return archive.JobQuerySource{Database: svc.DatabaseMain}
}

// queryDB 根据归档任务配置选择管理员日志查询连接。
func queryDB(svcCtx *svc.ServiceContext, source archive.JobQuerySource) *gorm.DB {
	if svcCtx == nil {
		return nil
	}
	if source.QueryWriteDB {
		return svcCtx.WriteDB(source.Database)
	}
	if db := svcCtx.ReadDB(source.Database); db != nil {
		return db
	}
	return svcCtx.WriteDB(source.Database)
}

// queryDirect 使用已选定的数据库连接查询管理员审计日志热表。
func queryDirect(ctx context.Context, db *gorm.DB, req *types.AdminLogQueryReq, startTime, endTime *time.Time, queryWriteDB bool) ([]model.AdminLog, int64, Meta, error) {
	if req == nil {
		return nil, 0, Meta{}, errors.Errorf("管理员日志查询参数不能为空")
	}
	if db == nil {
		return nil, 0, Meta{}, errors.Errorf("管理员日志查询数据库未初始化")
	}
	query := db.WithContext(ctx).Model(&model.AdminLog{})
	query = applyFilters(query, req, startTime, endTime)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, Meta{}, errors.Tag(err)
	}
	meta := Meta{ArchiveEnabled: false, QueryWriteDB: queryWriteDB}
	if total == 0 {
		return []model.AdminLog{}, 0, meta, nil
	}

	orderQuery, err := applyOrder(query, req.OrderBy, req.Order)
	if err != nil {
		return nil, 0, Meta{}, errors.Tag(err)
	}
	items := make([]model.AdminLog, 0, req.PageSize)
	if err = orderQuery.Offset((req.Page - 1) * req.PageSize).Limit(req.PageSize).Find(&items).Error; err != nil {
		return nil, 0, Meta{}, errors.Tag(err)
	}
	return items, total, meta, nil
}

// applyFilters 将审计日志筛选条件追加到 GORM 查询。
func applyFilters(query *gorm.DB, req *types.AdminLogQueryReq, startTime, endTime *time.Time) *gorm.DB {
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

// applyOrder 应用白名单校验后的审计日志排序。
func applyOrder(query *gorm.DB, orderBy, order string) (*gorm.DB, error) {
	orderClause, err := buildOrderClause(orderBy, order)
	if err != nil {
		return nil, errors.Tag(err)
	}
	if orderClause == "created_at DESC, id DESC" {
		return query.Order("created_at DESC").Order("id DESC"), nil
	}
	return query.Order(orderClause), nil
}

// buildOrderClause 构造安全的 ORDER BY 片段。
func buildOrderClause(orderBy, order string) (string, error) {
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

// quoteIdent 转义 MySQL 标识符，避免动态排序字段注入。
func quoteIdent(name string) string {
	name = strings.TrimSpace(name)
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
