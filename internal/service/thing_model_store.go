package service

import (
	"encoding/json"

	"go.uber.org/zap"

	"iot-platform.local/internal/ent"
	entity "iot-platform.local/internal/model"
	"iot-platform.local/pkg/common"
)

// 物模型组件 kind 取值（iot_device_thing.kind）
const (
	ThingKindSensor   = "sensor"
	ThingKindActuator = "actuator"
)

// sensorThingBody iot_device_thing.specs 中 kind=sensor 的存储体：
// 因采用窄表，sensor 专有字段（type/dataType/unit/reportInterval）与定义体统一入 JSON。
type sensorThingBody struct {
	Type           string              `json:"type"`
	DataType       string              `json:"dataType"`
	Unit           string              `json:"unit"`
	Specs          *entity.SensorSpecs `json:"specs,omitempty"`
	ReportInterval *int                `json:"reportInterval,omitempty"`
}

// actuatorThingBody iot_device_thing.specs 中 kind=actuator 的存储体。
type actuatorThingBody struct {
	Driver string         `json:"driver"`
	Specs  map[string]any `json:"specs,omitempty"`
}

// marshalSensorBody 序列化 sensor 存储体；空定义落空串（无定义统一语义）。
func marshalSensorBody(b sensorThingBody) string {
	data, err := json.Marshal(b)
	if err != nil {
		zap.L().Warn("[物模型] sensor specs 序列化失败，落库为空串", zap.Error(err))
		return ""
	}
	if string(data) == "{}" {
		return ""
	}
	return string(data)
}

// parseSensorBody 反序列化 sensor 存储体；空串/非法返回零值。
func parseSensorBody(s string) sensorThingBody {
	var b sensorThingBody
	if s == "" {
		return b
	}
	if err := json.Unmarshal([]byte(s), &b); err != nil {
		zap.L().Warn("[物模型] sensor specs 解析失败，输出空定义", zap.String("specs", s), zap.Error(err))
		return sensorThingBody{}
	}
	return b
}

// marshalActuatorBody 序列化 actuator 存储体；空定义落空串。
func marshalActuatorBody(b actuatorThingBody) string {
	data, err := json.Marshal(b)
	if err != nil {
		zap.L().Warn("[物模型] actuator specs 序列化失败，落库为空串", zap.Error(err))
		return ""
	}
	if string(data) == "{}" {
		return ""
	}
	return string(data)
}

// parseActuatorBody 反序列化 actuator 存储体；空串/非法返回零值。
func parseActuatorBody(s string) actuatorThingBody {
	var b actuatorThingBody
	if s == "" {
		return b
	}
	if err := json.Unmarshal([]byte(s), &b); err != nil {
		zap.L().Warn("[物模型] actuator specs 解析失败，输出空定义", zap.String("specs", s), zap.Error(err))
		return actuatorThingBody{}
	}
	return b
}

// thingToSensor ent 实体 → 传感器管理侧 DTO
func thingToSensor(row *ent.DeviceThing) entity.Sensor {
	body := parseSensorBody(row.Specs)
	return entity.Sensor{
		ID:             row.ThingID,
		Name:           row.Name,
		Type:           body.Type,
		DataType:       body.DataType,
		Unit:           body.Unit,
		Specs:          body.Specs,
		ReportInterval: body.ReportInterval,
		Enabled:        row.Enabled,
		CreatedAt:      common.DateTimeFrom(row.CreatedAt),
		UpdatedAt:      common.DateTimeFrom(row.UpdatedAt),
	}
}

// thingToSensorWire ent 实体 → 传感器下行裁剪版
func thingToSensorWire(row *ent.DeviceThing) entity.SensorWire {
	body := parseSensorBody(row.Specs)
	return entity.SensorWire{
		ID:             row.ThingID,
		Type:           body.Type,
		DataType:       body.DataType,
		Unit:           body.Unit,
		Specs:          body.Specs,
		ReportInterval: body.ReportInterval,
		Enabled:        row.Enabled,
	}
}

// thingToActuator ent 实体 → 执行器管理侧 DTO
func thingToActuator(row *ent.DeviceThing) entity.Actuator {
	body := parseActuatorBody(row.Specs)
	return entity.Actuator{
		ID:        row.ThingID,
		Name:      row.Name,
		Driver:    body.Driver,
		Specs:     body.Specs,
		Enabled:   row.Enabled,
		CreatedAt: common.DateTimeFrom(row.CreatedAt),
		UpdatedAt: common.DateTimeFrom(row.UpdatedAt),
	}
}

// thingToActuatorWire ent 实体 → 执行器下行裁剪版
func thingToActuatorWire(row *ent.DeviceThing) entity.ActuatorWire {
	body := parseActuatorBody(row.Specs)
	return entity.ActuatorWire{
		ID:      row.ThingID,
		Driver:  body.Driver,
		Specs:   body.Specs,
		Enabled: row.Enabled,
	}
}
