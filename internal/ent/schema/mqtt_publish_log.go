package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// MqttPublishLog MQTT发布日志，对应表 mqtt_publish_log
type MqttPublishLog struct {
	ent.Schema
}

func (MqttPublishLog) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("mqtt_publish_log"),
	}
}

func (MqttPublishLog) Fields() []ent.Field {
	return []ent.Field{
		field.Uint("id").
			StructTag(`json:"id"`),
		field.String("topic").
			MaxLen(255).
			NotEmpty().
			StructTag(`json:"topic"`),
		field.Text("payload").
			Default("").
			StructTag(`json:"payload"`),
		field.Int("qos").
			Default(0).Optional().
			StructTag(`json:"qos"`),
		field.Bool("retained").
			Default(false).Optional().
			StructTag(`json:"retained"`),
		field.String("client_id").
			MaxLen(128).
			Default("").
			StructTag(`json:"clientId"`),
		field.String("broker_url").
			MaxLen(255).
			Default("").
			StructTag(`json:"brokerUrl"`),
		field.Time("create_time").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Default(time.Now).
			StructTag(`json:"createTime"`),
	}
}
