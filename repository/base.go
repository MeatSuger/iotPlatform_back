package repository

import (
	"context"

	"gorm.io/gorm"
)

// BaseRepo 泛型 Repository 基类，提供通用 CRUD 操作
// 使用方式：
//
//	type UserRepo struct {
//	    BaseRepo[entity.User]
//	}
//
//	func NewUserRepo(db *gorm.DB) *UserRepo {
//	    return &UserRepo{BaseRepo: *NewBaseRepo[entity.User](db)}
//	}
type BaseRepo[T any] struct {
	DB *gorm.DB
}

// NewBaseRepo 创建泛型 Repository
func NewBaseRepo[T any](db *gorm.DB) *BaseRepo[T] {
	return &BaseRepo[T]{DB: db}
}

// dbWithCtx 获取带 context 的 GORM 查询，并绑定泛型模型
func (r *BaseRepo[T]) dbWithCtx(ctx context.Context) *gorm.DB {
	return r.DB.WithContext(ctx).Model(new(T))
}

// Create 创建记录
func (r *BaseRepo[T]) Create(ctx context.Context, entity *T) error {
	return r.dbWithCtx(ctx).Create(entity).Error
}

// GetByID 按主键 ID 查询
func (r *BaseRepo[T]) GetByID(ctx context.Context, id uint) (*T, error) {
	var entity T
	err := r.DB.WithContext(ctx).First(&entity, id).Error
	if err != nil {
		return nil, err
	}
	return &entity, nil
}

// Update 保存实体（全量更新）
func (r *BaseRepo[T]) Update(ctx context.Context, entity *T) error {
	return r.DB.WithContext(ctx).Save(entity).Error
}

// Delete 按主键 ID 删除
func (r *BaseRepo[T]) Delete(ctx context.Context, id uint) error {
	return r.DB.WithContext(ctx).Delete(new(T), id).Error
}

// List 查询所有记录，支持可选的 GORM scope 条件
func (r *BaseRepo[T]) List(ctx context.Context, scopes ...func(*gorm.DB) *gorm.DB) ([]T, error) {
	var entities []T
	db := r.dbWithCtx(ctx)
	for _, scope := range scopes {
		db = scope(db)
	}
	err := db.Find(&entities).Error
	return entities, err
}

// Page 分页查询，支持可选 GORM scope 条件
// 返回记录列表、总条数
func (r *BaseRepo[T]) Page(ctx context.Context, page, size int, scopes ...func(*gorm.DB) *gorm.DB) ([]T, int64, error) {
	var entities []T
	var total int64

	db := r.dbWithCtx(ctx)
	for _, scope := range scopes {
		db = scope(db)
	}

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := db.Offset((page - 1) * size).Limit(size).Order("id DESC").Find(&entities).Error
	return entities, total, err
}

// Count 计数查询
func (r *BaseRepo[T]) Count(ctx context.Context, scopes ...func(*gorm.DB) *gorm.DB) (int64, error) {
	var count int64
	db := r.dbWithCtx(ctx)
	for _, scope := range scopes {
		db = scope(db)
	}
	err := db.Count(&count).Error
	return count, err
}

// IsExist 检查 ID 对应的记录是否存在
func (r *BaseRepo[T]) IsExist(ctx context.Context, id uint) (bool, error) {
	var count int64
	err := r.DB.WithContext(ctx).Model(new(T)).Where("id = ?", id).Count(&count).Error
	return count > 0, err
}
