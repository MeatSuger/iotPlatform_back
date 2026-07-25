package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Device 设备实体，对应表 iot_device
type Device struct {
	ent.Schema
}

func (Device) Fields() []ent.Field {
	return []ent.Field{
		field.Uint("id").
			StructTag(`json:"id"`),
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
			StructTag(`json:"firmwareVersion"`),
		field.String("ip_address").
			MaxLen(45).
			Default("").
			StructTag(`json:"ipAddress"`),
		field.String("mac_address").
			MaxLen(17).
			Default("").
			StructTag(`json:"macAddress"`),
		field.String("location").
			MaxLen(255).
			Default("").
			StructTag(`json:"location"`),
		field.Uint("owner_id").
			Default(0).
			StructTag(`json:"ownerId"`),
		field.String("status").
			MaxLen(50).
			Default("OFFLINE").
			StructTag(`json:"status"`),
		field.Time("last_active_time").
			Optional().
			StructTag(`json:"lastActiveTime"`),
		field.Time("created_at").
			Default(time.Now).
			StructTag(`json:"createdAt"`),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			StructTag(`json:"updatedAt"`),
	}
}

func (Device) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("device_id").Unique(),
	}
}

func (Device) Table() string {
	return "iot_device"
}
