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

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// User 用户实体，对应表 app_user
type User struct {
	ent.Schema
}

func (User) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("app_user"),
		edge.Annotation{StructTag: `json:"-"`},
	}
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.Uint("id").
			StorageKey("id").
			StructTag(`json:"id"`),
		field.String("name").
			MaxLen(100).
			Default("").
			Optional().
			StructTag(`json:"name"`),
		field.Int("age").
			Default(0).
			Optional().
			StructTag(`json:"age"`),
		field.String("email").
			MaxLen(255).
			Default("").
			Optional().
			StructTag(`json:"email"`),
		// 变更1: 移除字段级 .Unique() —— 与 Indexes() 里的唯一索引重复，
		// 这正是 app_user.account 上出现 3 个重复唯一索引的根因
		field.String("account").
			MaxLen(100).
			NotEmpty().
			StructTag(`json:"account"`),
		field.String("passwd").
			MaxLen(255).
			NotEmpty().
			StructTag(`json:"-"`),
		field.String("role").
			MaxLen(50).
			Default("user").Optional().
			StructTag(`json:"role"`),
		// 变更2: 默认值 "active" → "ACTIVE"（库内实际数据均为大写 ACTIVE）
		field.String("status").
			MaxLen(50).
			Default("ACTIVE").Optional().
			StructTag(`json:"status"`),
		field.Time("create_time").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Default(time.Now).
			Optional().
			StructTag(`json:"createTime"`),
		field.Time("update_time").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Default(time.Now).
			UpdateDefault(time.Now).
			Optional().
			StructTag(`json:"updateTime"`),
	}
}

func (User) Indexes() []ent.Index {
	return []ent.Index{
		// 唯一索引只保留这一处声明
		index.Fields("account").Unique(),
	}
}

// 变更3: 新增 Edges —— 对应数据库外键 fk_device_owner
func (User) Edges() []ent.Edge {
	return []ent.Edge{
		// ON DELETE CASCADE: 删除用户时级联删除其名下设备
		edge.To("devices", Device.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}
