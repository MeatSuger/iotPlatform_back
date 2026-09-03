package repository

import (
	"context"

	"iot-platform.local/internal/ent"
	entuser "iot-platform.local/internal/ent/user"
)

// UserRepo 用户数据访问
type UserRepo struct {
	client *ent.Client
}

// NewUserRepo 创建用户仓库
func NewUserRepo(client *ent.Client) *UserRepo {
	return &UserRepo{client: client}
}

// Create 创建用户
func (r *UserRepo) Create(ctx context.Context, user *ent.User) (*ent.User, error) {
	return r.client.User.Create().
		SetName(user.Name).
		SetAge(user.Age).
		SetEmail(user.Email).
		SetAccount(user.Account).
		SetPasswd(user.Passwd).
		SetRole(user.Role).
		SetStatus(user.Status).
		SetCreateTime(user.CreateTime).
		SetUpdateTime(user.UpdateTime).
		Save(ctx)
}

// GetByID 按 ID 查询用户
func (r *UserRepo) GetByID(ctx context.Context, id uint) (*ent.User, error) {
	return r.client.User.Get(ctx, id)
}

// GetByAccount 按账号查询用户
func (r *UserRepo) GetByAccount(ctx context.Context, account string) (*ent.User, error) {
	return r.client.User.Query().Where(entuser.AccountEQ(account)).First(ctx)
}

// Update 更新用户资料（账号、密码与时间字段不更新）
func (r *UserRepo) Update(ctx context.Context, user *ent.User) error {
	return r.client.User.UpdateOneID(user.ID).
		SetName(user.Name).
		SetAge(user.Age).
		SetEmail(user.Email).
		SetRole(user.Role).
		SetStatus(user.Status).
		Exec(ctx)
}

// Delete 按 ID 删除用户
func (r *UserRepo) Delete(ctx context.Context, id uint) error {
	return r.client.User.DeleteOneID(id).Exec(ctx)
}

// List 查询全部用户
func (r *UserRepo) List(ctx context.Context) ([]*ent.User, error) {
	return r.client.User.Query().All(ctx)
}

// Page 分页查询用户（支持按名称模糊过滤），返回用户列表与总数
func (r *UserRepo) Page(ctx context.Context, page, size int, name string) ([]*ent.User, int, error) {
	q := r.client.User.Query()
	if name != "" {
		q = q.Where(entuser.NameContains(name))
	}
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	users, err := q.
		Order(ent.Desc(entuser.FieldID)).
		Offset((page - 1) * size).
		Limit(size).
		All(ctx)
	return users, total, err
}

// IsExist 判断用户是否存在
func (r *UserRepo) IsExist(ctx context.Context, id uint) (bool, error) {
	n, err := r.client.User.Query().Where(entuser.IDEQ(id)).Count(ctx)
	return n > 0, err
}

// UpdatePassword 更新密码
func (r *UserRepo) UpdatePassword(ctx context.Context, id uint, hashed string) error {
	return r.client.User.UpdateOneID(id).SetPasswd(hashed).Exec(ctx)
}
