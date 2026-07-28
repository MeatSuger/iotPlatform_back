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

func NewUserRepo(client *ent.Client) *UserRepo {
	return &UserRepo{client: client}
}

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

func (r *UserRepo) GetByID(ctx context.Context, id uint) (*ent.User, error) {
	return r.client.User.Get(ctx, id)
}

func (r *UserRepo) GetByAccount(ctx context.Context, account string) (*ent.User, error) {
	return r.client.User.Query().Where(entuser.AccountEQ(account)).First(ctx)
}

func (r *UserRepo) Update(ctx context.Context, user *ent.User) error {
	return r.client.User.UpdateOneID(user.ID).
		SetName(user.Name).
		SetAge(user.Age).
		SetEmail(user.Email).
		SetRole(user.Role).
		SetStatus(user.Status).
		Exec(ctx)
}

func (r *UserRepo) Delete(ctx context.Context, id uint) error {
	return r.client.User.DeleteOneID(id).Exec(ctx)
}

func (r *UserRepo) List(ctx context.Context) ([]*ent.User, error) {
	return r.client.User.Query().All(ctx)
}

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

func (r *UserRepo) IsExist(ctx context.Context, id uint) (bool, error) {
	n, err := r.client.User.Query().Where(entuser.IDEQ(id)).Count(ctx)
	return n > 0, err
}

// UpdatePassword 更新密码
func (r *UserRepo) UpdatePassword(ctx context.Context, id uint, hashed string) error {
	return r.client.User.UpdateOneID(id).SetPasswd(hashed).Exec(ctx)
}
