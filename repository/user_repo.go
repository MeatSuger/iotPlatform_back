package repository

import (
	"context"

	"github.com/yu/iot-platform-go/entity"
	"gorm.io/gorm"
)

// UserRepo 用户数据访问（嵌入泛型 BaseRepo）
type UserRepo struct {
	BaseRepo[entity.User]
}

// NewUserRepo 创建用户仓库
func NewUserRepo(db *gorm.DB) *UserRepo {
	return &UserRepo{BaseRepo: *NewBaseRepo[entity.User](db)}
}

// GetByAccount 按账号查询用户（特化查询）
func (r *UserRepo) GetByAccount(ctx context.Context, account string) (*entity.User, error) {
	var user entity.User
	err := r.DB.WithContext(ctx).Where("account = ?", account).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// Page 分页查询用户（封装 name 过滤条件为 scope）
func (r *UserRepo) Page(ctx context.Context, page, size int, name string) ([]entity.User, int64, error) {
	return r.BaseRepo.Page(ctx, page, size,
		func(db *gorm.DB) *gorm.DB {
			if name != "" {
				return db.Where("name LIKE ?", "%"+name+"%")
			}
			return db
		},
	)
}
