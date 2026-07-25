package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DownlinkCmd 下放命令实体，对应表 iot_downlink_cmd
type DownlinkCmd struct {
	ent.Schema
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
			Default("pending").
			StructTag(`json:"status"`),
		field.Time("created_at").
			Default(time.Now).
			StructTag(`json:"createdAt"`),
	}
}

func (DownlinkCmd) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("device_id"),
	}
}

func (DownlinkCmd) Table() string {
	return "iot_downlink_cmd"
}
