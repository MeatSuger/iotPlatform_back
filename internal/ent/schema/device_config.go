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

// DeviceConfig 设备配置实体（整体配置快照），对应表 iot_device_config
//
// 每个设备仅保留一份「当前期望配置」快照，云端每次编辑 version+1 后
// 复用下行命令队列（type=config）下发；设备回执经 config/report 回写
// reported 字段并置 status=acked。OTA 升级预留 category=ota 扩展点。
type DeviceConfig struct {
	ent.Schema
}

func (DeviceConfig) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("iot_device_config"),
		edge.Annotation{StructTag: `json:"-"`},
	}
}

func (DeviceConfig) Fields() []ent.Field {
	return []ent.Field{
		field.Uint("id").
			StructTag(`json:"id"`),
		field.String("device_id").
			MaxLen(50).
			Optional().
			StructTag(`json:"deviceId"`),
		field.Uint("version").
			Default(1).
			StructTag(`json:"version"`),
		field.Text("payload").
			Default("").
			StructTag(`json:"payload"`),
		field.String("status").
			MaxLen(20).
			Default("pending").Optional().
			StructTag(`json:"status"`),
		field.Uint("reported_version").
			Default(0).
			StructTag(`json:"reportedVersion"`),
		field.Text("reported_payload").
			Default("").
			StructTag(`json:"reportedPayload"`),
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

// DeviceConfig 每设备仅一份 → device_id 唯一索引
func (DeviceConfig) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("device_id").Unique(),
	}
}

// O2O: 一个设备对应一份配置快照；设备删除时级联删除其配置
func (DeviceConfig) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("device", Device.Type).
			Ref("config").
			Unique().
			Field("device_id"),
	}
}
