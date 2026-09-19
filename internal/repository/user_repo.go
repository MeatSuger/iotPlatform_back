// 物联网设备接入与物模型管理平台软件（物咸通）V1.0
// Copyright (C) 2025-2026 余昊
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

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
