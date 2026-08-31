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
// 注意: 库中还存在一个 ent 未建模的 id bigint identity 列（历史遗留，无业务使用）
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
		field.String("category_id").
			MaxLen(100).
			Default("").
			Optional().
			StructTag(`json:"categoryId"`),
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
		// 下行指令 (M2O / O2M): 一个设备有多个下行指令
		// ON DELETE CASCADE: 删除设备时级联删除其下行指令
		edge.To("cmds", DownlinkCmd.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}
