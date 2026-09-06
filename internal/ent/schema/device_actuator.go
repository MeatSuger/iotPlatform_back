package schema

import (
	"regexp"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// actuatorIDPattern 执行器标识符：小写字母开头，仅含小写字母/数字/下划线，
// ≤11 字符 —— 与固件 periph 设备名（控制命令 action / 内部缓冲）契约一致
var actuatorIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,10}$`)

// actuatorDrivers 固件侧支持的执行器驱动（与 firmware/main/peripherals/drivers 对齐）
var actuatorDrivers = []string{"led", "servo", "speaker"}

// DeviceActuator 设备执行器定义（物模型），对应表 iot_device_actuator
//
// 与 DeviceSensor 同构：定义本身仅持久化，经 Apply 编译进
// DeviceConfig.payload.actuators 后版本化下发；设备据此实例化/卸载执行器，
// 运行期动作经 type=control 命令按 id（=action）路由执行。
// config 字段（驱动参数 JSON 文本，如 gpio/count）结构由各驱动 probe 约定，
// 见 API 文档「Actuator」章节与固件 docs/mqtt-api.md。
type DeviceActuator struct {
	ent.Schema
}

func (DeviceActuator) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("iot_device_actuator"),
		edge.Annotation{StructTag: `json:"-"`},
	}
}

func (DeviceActuator) Fields() []ent.Field {
	return []ent.Field{
		field.Uint("id").
			StructTag(`json:"-"`),
		field.String("device_id").
			MaxLen(50).
			Optional().
			StructTag(`json:"deviceId"`),
		field.String("actuator_id").
			MaxLen(11).
			Match(actuatorIDPattern).
			Immutable().
			StructTag(`json:"id"`),
		field.String("name").
			MaxLen(100).
			Default("").
			StructTag(`json:"name"`),
		field.String("driver").
			MaxLen(50).
			NotEmpty().
			StructTag(`json:"driver"`),
		field.Text("params").
			Default("").
			StructTag(`json:"config"`),
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

// DeviceActuator 设备内执行器标识符唯一 → (device_id, actuator_id) 联合唯一索引
func (DeviceActuator) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("device_id", "actuator_id").Unique(),
	}
}

// O2O: 一个执行器定义归属一个设备；设备删除时级联删除其执行器定义
func (DeviceActuator) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("device", Device.Type).
			Ref("actuators").
			Unique().
			Field("device_id"),
	}
}
