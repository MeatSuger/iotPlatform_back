package entity

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func testDef(id, name string) Sensor {
	return Sensor{ID: id, Name: name, Type: "temperature", DataType: "float", Unit: "°C"}
}

func TestAttachLatest_MatchByID(t *testing.T) {
	ts := time.Now()
	defs := []Sensor{testDef("temperature", "温度"), testDef("humidity", "湿度")}
	recent := []SensorData{
		{Name: "temperature", Type: "temperature", Value: 25.5, Timestamp: ts},
	}

	out := AttachLatest(defs, recent)
	assert.Len(t, out, 2)

	assert.NotNil(t, out[0].Latest)
	assert.Equal(t, 25.5, out[0].Latest.Value)
	assert.Equal(t, ts, out[0].Latest.Timestamp.Time)
	// 定义字段平铺保留
	assert.Equal(t, "温度", out[0].Name)

	// humidity 未上报 → latest nil
	assert.Nil(t, out[1].Latest)
}

func TestAttachLatest_FallbackByName(t *testing.T) {
	ts := time.Now()
	// 存量设备上报 name 用中文显示名（无 id 契约的历史上报）
	defs := []Sensor{testDef("temperature", "温度")}
	recent := []SensorData{
		{Name: "温度", Type: "temperature", Value: 26.1, Timestamp: ts},
	}

	out := AttachLatest(defs, recent)
	assert.NotNil(t, out[0].Latest)
	assert.Equal(t, 26.1, out[0].Latest.Value)
}

func TestAttachLatest_IDOverridesName(t *testing.T) {
	ts := time.Now()
	// 同一条上报既匹配某定义 id、又匹配另一定义 name 时，各自按其键归位，
	// 不会交叉污染：id 匹配优先于 name 匹配。
	defs := []Sensor{testDef("温度", "room_temp"), testDef("humidity", "湿度")}
	recent := []SensorData{
		{Name: "温度", Type: "temperature", Value: 20.0, Timestamp: ts},        // → def0（按 id）
		{Name: "room_temp", Type: "temperature", Value: 21.0, Timestamp: ts}, // → 无 def id 为 room_temp
	}

	out := AttachLatest(defs, recent)
	assert.NotNil(t, out[0].Latest)
	assert.Equal(t, 20.0, out[0].Latest.Value)
	assert.Nil(t, out[1].Latest)
}

func TestAttachLatest_DuplicateNameTakesLatest(t *testing.T) {
	base := time.Now()
	defs := []Sensor{testDef("temperature", "温度")}
	recent := []SensorData{
		{Name: "temperature", Type: "temperature", Value: 1.0, Timestamp: base.Add(-time.Minute)},
		{Name: "temperature", Type: "temperature", Value: 3.0, Timestamp: base.Add(-30 * time.Second)},
		{Name: "temperature", Type: "temperature", Value: 2.0, Timestamp: base.Add(-time.Second)},
	}

	out := AttachLatest(defs, recent)
	assert.Equal(t, 2.0, out[0].Latest.Value)
	assert.Equal(t, base.Add(-time.Second), out[0].Latest.Timestamp.Time)
}

func TestAttachLatest_EdgeCases(t *testing.T) {
	ts := time.Now()

	// 无定义 → 空切片（非 nil，保证 JSON 输出 []）
	assert.Equal(t, []SensorWithLatest{}, AttachLatest(nil, []SensorData{{Name: "x", Value: 1, Timestamp: ts}}))
	assert.Len(t, AttachLatest(nil, nil), 0)

	// 无遥测 / 名称全不匹配 → latest 全 nil
	defs := []Sensor{testDef("temperature", "温度"), testDef("humidity", "湿度")}
	out := AttachLatest(defs, nil)
	assert.Len(t, out, 2)
	assert.Nil(t, out[0].Latest)
	assert.Nil(t, out[1].Latest)

	// 遥测空 name 被忽略
	out = AttachLatest(defs, []SensorData{{Name: "", Type: "x", Value: 1, Timestamp: ts}})
	assert.Nil(t, out[0].Latest)

	// 空 name 定义兜底匹配不 panic
	noName := []Sensor{{ID: "only_id"}}
	out = AttachLatest(noName, []SensorData{{Name: "only_id", Value: true, Timestamp: ts}})
	assert.NotNil(t, out[0].Latest)
	assert.Equal(t, true, out[0].Latest.Value)
}

func TestSensorWithLatest_MarshalShape(t *testing.T) {
	// 结构形状：sensor 定义字段平铺 + latest 内嵌对象；actuator 定义平铺
	ts := time.Now()
	sensors := AttachLatest([]Sensor{testDef("temperature", "温度")},
		[]SensorData{{Name: "temperature", Type: "temperature", Value: 25.5, Timestamp: ts}})
	actuators := []Actuator{{ID: "led1", Name: "指示灯", Driver: "led", Config: map[string]any{"gpio": 2}}}

	raw, err := json.Marshal(map[string]any{"sensors": sensors, "actuators": actuators})
	assert.NoError(t, err)
	assert.Contains(t, string(raw), `"latest":{"value":25.5`)
	assert.Contains(t, string(raw), `"id":"temperature"`)
	assert.Contains(t, string(raw), `"unit":"°C"`)
	assert.Contains(t, string(raw), `"driver":"led"`)
	assert.Contains(t, string(raw), `"gpio":2`)
}
