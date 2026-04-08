package model

import (
	"context"
	"github.com/Is999/go-utils/errors"

	"gorm.io/gorm"
)

// Repo 泛型基础仓库，封装通用 CRUD 操作并保留底层 `gorm.DB` 访问能力。
type Repo[T any] struct {
	DB *gorm.DB // 当前仓库绑定的数据库会话，可被 WithContext 等方法派生
}

// NewRepo 创建泛型仓库实例。
func NewRepo[T any](db *gorm.DB) *Repo[T] {
	return &Repo[T]{DB: db}
}

// Create 新增一条实体记录。
func (r *Repo[T]) Create(entity *T) error {
	return r.DB.Create(entity).Error
}

// GetByID 根据主键查询单条实体记录。
func (r *Repo[T]) GetByID(id any) (*T, error) {
	var entity T
	err := r.DB.First(&entity, id).Error
	if err != nil {
		return nil, errors.Tag(err)
	}
	return &entity, nil
}

// Update 根据主键更新指定字段集合。
func (r *Repo[T]) Update(id any, updates map[string]any) error {
	var entity T
	return r.DB.Model(&entity).Where("id = ?", id).Updates(updates).Error
}

// Delete 根据主键执行物理删除。
func (r *Repo[T]) Delete(id any) error {
	var entity T
	return r.DB.Delete(&entity, id).Error
}

// DeleteSoft 根据主键执行软删除，要求模型包含 `DeletedAt` 字段。
func (r *Repo[T]) DeleteSoft(id any) error {
	var entity T
	return r.DB.Where("id = ?", id).Delete(&entity).Error
}

// Count 统计当前模型记录总数。
func (r *Repo[T]) Count() (int64, error) {
	var count int64
	var entity T
	err := r.DB.Model(&entity).Count(&count).Error
	return count, errors.Tag(err)
}

// Exist 检查指定主键记录是否存在。
func (r *Repo[T]) Exist(id any) (bool, error) {
	var entity T
	err := r.DB.Select("id").First(&entity, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, errors.Tag(err)
	}
	return true, nil
}

// WithContext 返回绑定上下文后的 Repo，便于把 trace/logger 一并透传到 model 查询。
func (r *Repo[T]) WithContext(ctx context.Context) *Repo[T] {
	return &Repo[T]{DB: r.DB.WithContext(ctx)}
}

// DBScope 暴露当前仓库绑定的 DB 会话，供调用方拼装复杂查询。
func (r *Repo[T]) DBScope() *gorm.DB {
	return r.DB
}
