package adminlog

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"admin/internal/model"
	"admin/internal/types"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
)

// Meta 描述管理员日志查询元信息。
type Meta struct {
	ArchiveEnabled bool `json:"archiveEnabled"`
	QueryWriteDB   bool `json:"queryWriteDB"`
}

// adminLogOrderPattern 约束管理员日志列表动态排序字段，避免 ORDER BY 注入。
var adminLogOrderPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// QueryDirect 查询管理员审计日志热表。
func QueryDirect(ctx context.Context, db *gorm.DB, req *types.AdminLogQueryReq, startTime, endTime *time.Time, queryWriteDB bool) ([]model.AdminLog, int64, Meta, error) {
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

func quoteIdent(name string) string {
	name = strings.TrimSpace(name)
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
