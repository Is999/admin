package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Is999/go-utils/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// safeOrderByPattern 仅允许字母/数字/下划线字段名，防止拼接 ORDER BY 时出现 SQL 注入。
var safeOrderByPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// normalizeJSONBValue 统一处理 JSONB 字段扫描时可能出现的返回类型（nil/[]byte/string）。
func normalizeJSONBValue(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, errors.Errorf("解析 JSONB 字段失败: %v", value)
	}
}

// Int64Slice 为数据库 JSON 字段提供 []int64 的扫描与写入能力。
type Int64Slice []int64

// Value 把 `[]int64` 转成数据库可写入的 JSON 值。
func (s Int64Slice) Value() (driver.Value, error) {
	if len(s) == 0 {
		return nil, nil
	}
	return json.Marshal(s)
}

// Scan 把数据库中的 JSON 值解析回 `[]int64`。
func (s *Int64Slice) Scan(value interface{}) error {
	bytes, err := normalizeJSONBValue(value)
	if err != nil {
		return errors.Tag(err)
	}
	if len(bytes) == 0 {
		*s = Int64Slice{}
		return nil
	}
	return errors.Tag(json.Unmarshal(bytes, s))
}

// StringSlice 为数据库 JSON 字段提供 []string 的扫描与写入能力。
type StringSlice []string

// Value 把 `[]string` 转成数据库可写入的 JSON 值。
func (s StringSlice) Value() (driver.Value, error) {
	if len(s) == 0 {
		return nil, nil
	}
	return json.Marshal(s)
}

// Scan 把数据库中的 JSON 值解析回 `[]string`。
func (s *StringSlice) Scan(value interface{}) error {
	bytes, err := normalizeJSONBValue(value)
	if err != nil {
		return errors.Tag(err)
	}
	if len(bytes) == 0 {
		*s = StringSlice{}
		return nil
	}
	return errors.Tag(json.Unmarshal(bytes, s))
}

// FloatSlice 为数据库 JSON 字段提供 []float64 的扫描与写入能力。
type FloatSlice []float64

// Value 把 `[]float64` 转成数据库可写入的 JSON 值。
func (s FloatSlice) Value() (driver.Value, error) {
	if len(s) == 0 {
		return nil, nil
	}
	return json.Marshal(s)
}

// Scan 把数据库中的 JSON 值解析回 `[]float64`。
func (s *FloatSlice) Scan(value interface{}) error {
	bytes, err := normalizeJSONBValue(value)
	if err != nil {
		return errors.Tag(err)
	}
	if len(bytes) == 0 {
		*s = FloatSlice{}
		return nil
	}
	return errors.Tag(json.Unmarshal(bytes, s))
}

// Query 封装通用模型查询能力，减少 model 层重复样板代码。
type Query[T schema.Tabler] struct {
	db    *gorm.DB // 当前查询对象绑定的数据库会话
	model T        // 当前查询对象对应的模型原型
}

// NewQuery 构造一个通用查询对象。
func NewQuery[T schema.Tabler](db *gorm.DB, model T) *Query[T] {
	return &Query[T]{
		db:    db,
		model: model,
	}
}

// Available 检查查询对象是否可用
func (q *Query[T]) Available() bool { return q.db != nil }

// Exists 判断记录是否存在
func (q *Query[T]) Exists(scopes ...func(db *gorm.DB) *gorm.DB) (exists bool, err error) {
	err = q.db.Model(&q.model).Select("1").Scopes(scopes...).Limit(1).Scan(&exists).Error
	return
}

// validateOrder 校验并标准化排序方向，仅允许 asc/desc。
func validateOrder(order string) (string, error) {
	order = strings.ToLower(strings.TrimSpace(order))
	switch order {
	case "", "desc":
		return "desc", nil
	case "asc":
		return "asc", nil
	default:
		return "", errors.Errorf("排序方向不合法: %s", order)
	}
}

// validatePage 校验分页参数，非法参数直接返回错误而不是兜底默认值。
func validatePage(page, pageSize int) (int, int, error) {
	if page < 1 {
		return 0, 0, errors.Errorf("页码不合法: %d", page)
	}
	if pageSize < 1 {
		return 0, 0, errors.Errorf("每页条数不合法: %d", pageSize)
	}
	return page, pageSize, nil
}

// applySafeOrder 在通过字段名/方向校验后才拼接排序，避免动态 SQL 带来注入风险。
func applySafeOrder(db *gorm.DB, orderBy, order string) (*gorm.DB, error) {
	orderBy = strings.TrimSpace(orderBy)
	if orderBy == "" {
		return db, nil
	}
	if !safeOrderByPattern.MatchString(orderBy) {
		return nil, errors.Errorf("排序字段不合法: %s", orderBy)
	}
	normalizedOrder, err := validateOrder(order)
	if err != nil {
		return nil, errors.Tag(err)
	}
	return db.Order(fmt.Sprintf("`%s` %s", orderBy, normalizedOrder)), nil
}

// List 分页获取记录列表
func List[T any](db *gorm.DB, page, pageSize int, orderBy, order string) ([]T, int64, error) {
	page, pageSize, err := validatePage(page, pageSize)
	if err != nil {
		return nil, 0, errors.Tag(err)
	}

	// 获取总数
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, errors.Tag(err)
	}

	// 判断总数是否为0
	var list []T
	if total == 0 {
		return list, 0, nil
	}

	// 排序
	db, err = applySafeOrder(db, orderBy, order)
	if err != nil {
		return nil, 0, errors.Tag(err)
	}

	// 获取分页数据
	err = db.Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, errors.Tag(err)
}
