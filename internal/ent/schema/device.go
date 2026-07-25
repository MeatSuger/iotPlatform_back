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

// Device 设备实体，对应表 iot_device
type Device struct {
	ent.Schema
}

func (Device) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("iot_device"),
	}
}

func (Device) Fields() []ent.Field {
	return []ent.Field{
		field.String("device_id").
			MaxLen(50).
			NotEmpty().
			Unique().
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
		field.Uint("owner_id").
			Default(1).
			StructTag(`json:"ownerId"`),
		field.String("status").
			MaxLen(50).
			Default("OFFLINE").Optional().
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
		field.String("category_id").
			MaxLen(100).
			Default("").
			Optional().
			StructTag(`json:"categoryId"`),
	}
}

func (Device) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("device_id").Unique(),
	}
}
