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

// DeviceThing 设备物模型组件（传感器/执行器统一存储），对应表 iot_device_thing
//
// 窄表设计：仅保留通用列（device_id/kind/thing_id/name/specs/enabled/时间戳），
// 按 kind 不同的专有字段（sensor: type/dataType/unit/reportInterval；
// actuator: driver）统一序列化进 specs JSON，结构由 service 层信封约定。
// kind = "sensor" | "actuator"，与 (device_id, thing_id) 组合唯一。
type DeviceThing struct {
	ent.Schema
}

func (DeviceThing) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("iot_device_thing"),
		edge.Annotation{StructTag: `json:"-"`},
	}
}

func (DeviceThing) Fields() []ent.Field {
	return []ent.Field{
		field.Uint("id").
			StructTag(`json:"-"`),
		field.String("device_id").
			MaxLen(50).
			Optional().
			StructTag(`json:"deviceId"`),
		field.String("kind").
			MaxLen(16).
			NotEmpty().
			Immutable().
			StructTag(`json:"kind"`),
		field.String("thing_id").
			MaxLen(50).
			NotEmpty().
			Immutable().
			StructTag(`json:"id"`),
		field.String("name").
			MaxLen(100).
			Default("").
			StructTag(`json:"name"`),
		// specs 以 JSON 文本持久化：kind 专有字段 + 物模型定义体
		field.Text("specs").
			Default("").
			StructTag(`json:"specs"`),
		field.Bool("enabled").
			Default(true).
			StructTag(`json:"enabled"`),
		field.Time("created_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Default(time.Now).
			StructTag(`json:"createdAt"`),
		field.Time("updated_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Default(time.Now).
			UpdateDefault(time.Now).
			StructTag(`json:"updatedAt"`),
	}
}

// 设备内同 kind 的组件标识符唯一 → (device_id, kind, thing_id) 联合唯一索引
func (DeviceThing) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("device_id", "kind", "thing_id").Unique(),
	}
}

// O2M: 一个设备有多个物模型组件；设备删除时级联删除
func (DeviceThing) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("device", Device.Type).
			Ref("things").
			Unique().
			Field("device_id"),
	}
}
