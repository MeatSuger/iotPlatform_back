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

// MessageLog 设备消息日志，对应表 iot_message_log
//
// 统一记录两类消息：
//   - direction=down：平台下发命令（status: pending/sent/delivered）
//   - direction=up：设备经 MQTT 上行发布（category: telemetry/config_report/other）
type MessageLog struct {
	ent.Schema
}

func (MessageLog) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("iot_message_log"),
	}
}

func (MessageLog) Fields() []ent.Field {
	return []ent.Field{
		field.Uint("id").
			StructTag(`json:"id"`),
		field.String("direction").
			MaxLen(8).
			NotEmpty().
			Immutable().
			StructTag(`json:"direction"`),
		// category: down=cmd/config；up=telemetry/config_report/other
		field.String("category").
			MaxLen(20).
			Default("").
			StructTag(`json:"category"`),
		field.String("device_id").
			MaxLen(50).
			Optional().
			StructTag(`json:"deviceId"`),
		// type: 下行命令类型
		field.String("type").
			MaxLen(50).
			Default("").
			StructTag(`json:"type"`),
		// topic: 上行 MQTT 主题
		field.String("topic").
			MaxLen(255).
			Default("").
			StructTag(`json:"topic"`),
		field.Text("payload").
			Default("").
			StructTag(`json:"payload"`),
		field.Int("qos").
			Default(0).
			Optional().
			StructTag(`json:"qos"`),
		field.Bool("retained").
			Default(false).
			Optional().
			StructTag(`json:"retained"`),
		field.String("client_id").
			MaxLen(128).
			Default("").
			StructTag(`json:"clientId"`),
		field.String("broker_url").
			MaxLen(255).
			Default("").
			StructTag(`json:"brokerUrl"`),
		// status: 仅下行有值（pending/sent/delivered），上行为空
		field.String("status").
			MaxLen(20).
			Default("").
			StructTag(`json:"status"`),
		field.Time("created_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Default(time.Now).
			StructTag(`json:"createdAt"`),
	}
}

// create_time 使用 BRIN 索引：写多读少的时间序列场景
func (MessageLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("device_id"),
		index.Fields("direction", "created_at"),
		index.Fields("topic"),
		index.Fields("created_at").
			Annotations(entsql.IndexType("BRIN")),
	}
}

// O2M: 一个设备有多条消息日志；设备删除时级联删除
func (MessageLog) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("device", Device.Type).
			Ref("messages").
			Unique().
			Field("device_id"),
	}
}
