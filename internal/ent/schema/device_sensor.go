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

// sensorIDPattern 传感器标识符：小写字母开头，仅含小写字母/数字/下划线（新大陆 ApiTag 风格）
var sensorIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,49}$`)

// DeviceSensor 设备传感器定义（物模型），对应表 iot_device_sensor
//
// 参考新大陆 NLECloud 传感器子资源（ApiTag/Name/DataType/TypeAttrs）与
// 阿里云 TSL 物模型（identifier/dataType/specs）设计：
//   - sensor_id 为传感器标识符（对应 NLECloud ApiTag），设备内唯一且创建后不可变；
//   - specs / thresholds / attrs 以 JSON 文本持久化，结构约定见 API 文档 4.6 节；
//   - 定义本身仅持久化，经 Apply 编译进 DeviceConfig.payload.sensors 后版本化下发。
type DeviceSensor struct {
	ent.Schema
}

func (DeviceSensor) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("iot_device_sensor"),
		edge.Annotation{StructTag: `json:"-"`},
	}
}

func (DeviceSensor) Fields() []ent.Field {
	return []ent.Field{
		field.Uint("id").
			StructTag(`json:"-"`),
		field.String("device_id").
			MaxLen(50).
			Optional().
			StructTag(`json:"deviceId"`),
		field.String("sensor_id").
			MaxLen(50).
			Match(sensorIDPattern).
			Immutable().
			StructTag(`json:"id"`),
		field.String("name").
			MaxLen(100).
			Default("").
			StructTag(`json:"name"`),
		field.String("type").
			MaxLen(50).
			Default("").
			StructTag(`json:"type"`),
		field.String("data_type").
			MaxLen(20).
			Default("float").
			StructTag(`json:"dataType"`),
		field.String("unit").
			MaxLen(32).
			Default("").
			StructTag(`json:"unit"`),
		field.Text("specs").
			Default("").
			StructTag(`json:"specs"`),
		field.Int("report_interval").
			Default(0).
			StructTag(`json:"reportInterval"`),
		field.Text("thresholds").
			Default("").
			StructTag(`json:"thresholds"`),
		field.Text("attrs").
			Default("").
			StructTag(`json:"attrs"`),
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

// DeviceSensor 设备内传感器标识符唯一 → (device_id, sensor_id) 联合唯一索引
func (DeviceSensor) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("device_id", "sensor_id").Unique(),
	}
}

// O2O: 一个传感器定义归属一个设备；设备删除时级联删除其传感器定义
func (DeviceSensor) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("device", Device.Type).
			Ref("sensors").
			Unique().
			Field("device_id"),
	}
}
