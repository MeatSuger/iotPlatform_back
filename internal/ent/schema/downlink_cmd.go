package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DownlinkCmd 下放命令实体，对应表 iot_downlink_cmd
type DownlinkCmd struct {
	ent.Schema
}

func (DownlinkCmd) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("iot_downlink_cmd"),
	}
}

func (DownlinkCmd) Fields() []ent.Field {
	return []ent.Field{
		field.Uint("id").
			StructTag(`json:"id"`),
		field.String("device_id").
			MaxLen(50).
			NotEmpty().
			StructTag(`json:"deviceId"`),
		field.String("type").
			MaxLen(50).
			Default("").
			StructTag(`json:"type"`),
		field.Text("payload").
			Default("").
			StructTag(`json:"-"`),
		field.String("status").
			MaxLen(20).
			Default("pending").Optional().
			StructTag(`json:"status"`),
		field.Time("created_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Default(time.Now).
			StructTag(`json:"createdAt"`),
	}
}

func (DownlinkCmd) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("device_id"),
	}
}

