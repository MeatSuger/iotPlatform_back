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
	"errors"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Device 设备实体，对应表 iot_device
// 主键为 device_id（6位hex，历史上曾存在遗留的 id bigint identity 列，已于 2026-09 清理）
type Device struct {
	ent.Schema
}

func (Device) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("iot_device"),
		// 生成实体时把 Edges 字段的 json tag 设为 "-"，避免 API 返回 ent 实体时带出 "edges":{}
		edge.Annotation{StructTag: `json:"-"`},
	}
}

func (Device) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			StorageKey("device_id").
			MaxLen(50).
			NotEmpty().
			Immutable().
			StructTag(`json:"deviceId"`),
		field.String("device_name").
			MaxLen(100).
			Default("").
			StructTag(`json:"deviceName"`),
		field.String("device_type").
			MaxLen(50).
			Default("").
			StructTag(`json:"deviceType"`),
		field.String("firmware_version").
			MaxLen(50).
			Default("").
			Optional().
			StructTag(`json:"firmwareVersion"`),
		field.String("ip_address").
			MaxLen(45).
			Default("").
			Optional().
			StructTag(`json:"ipAddress"`),
		field.String("mac_address").
			MaxLen(17).
			Default("").
			Optional().
			StructTag(`json:"macAddress"`),
		field.String("location").
			MaxLen(255).
			Default("").
			Optional().
			StructTag(`json:"location"`),
		// 边缘绑定外键字段（Go: uint 非指针，DB: NOT NULL）
		field.Uint("owner_id").
			Default(1).
			StructTag(`json:"ownerId"`),
		// 变更2: 追加 Validate —— 与数据库 CHECK chk_device_status 保持一致
		field.String("status").
			MaxLen(50).
			Default("OFFLINE").Optional().
			Validate(func(s string) error {
				switch s {
				case "ONLINE", "OFFLINE", "ACTIVE":
					return nil
				}
				return errors.New("invalid device status: " + s)
			}).
			StructTag(`json:"status"`),
		field.Time("last_active_time").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Optional().
			StructTag(`json:"lastActiveTime"`),
		field.Time("created_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Default(time.Now).
			Optional().
			StructTag(`json:"createdAt"`),
		field.Time("updated_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Default(time.Now).
			Optional().
			UpdateDefault(time.Now).
			StructTag(`json:"updatedAt"`),
	}
}

func (Device) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("owner_id"),
	}
}

// 变更3: 新增 Edges —— 对应外键 fk_device_owner / fk_cmd_device
func (Device) Edges() []ent.Edge {
	return []ent.Edge{
		// owner_id -> app_user.id（M2O）
		edge.From("owner", User.Type).
			Ref("devices").
			Unique().
			Field("owner_id").
			Required(),
		// 配置快照 (O2O): 一个设备对应一份配置；删除设备时级联删除其配置
		edge.To("config", DeviceConfig.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		// 物模型组件 (O2M): 传感器/执行器统一存储；删除设备时级联删除
		edge.To("things", DeviceThing.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		// 消息日志 (O2M): 下行命令 + 上行 MQTT 发布；删除设备时级联删除
		edge.To("messages", MessageLog.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}
