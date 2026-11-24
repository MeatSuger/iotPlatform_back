// 物联网设备接入与物模型管理平台软件（物咸通）V1.0
// Copyright (C) 2025-2026 余昊
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package entity

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"iot-platform.local/pkg/common"
)

func TestSensorData_JSON(t *testing.T) {
	tests := []struct {
		name string
		json string
		want SensorData
	}{
		{
			name: "number sensor",
			json: `{"name":"temperature","type":"number","value":26.5}`,
			want: SensorData{Name: "temperature", Type: "number", Value: 26.5},
		},
		{
			name: "string sensor",
			json: `{"name":"co2","type":"CO2-SENSOR","value":"400ppm"}`,
			want: SensorData{Name: "co2", Type: "CO2-SENSOR", Value: "400ppm"},
		},
		{
			name: "with timestamp",
			json: `{"name":"temp","type":"number","value":22.0,"timestamp":"2024-01-15T10:30:00Z"}`,
			want: SensorData{Name: "temp", Type: "number", Value: float64(22)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s SensorData
			err := json.Unmarshal([]byte(tt.json), &s)
			assert.NoError(t, err)
			assert.Equal(t, tt.want.Name, s.Name)
			assert.Equal(t, tt.want.Type, s.Type)
			assert.Equal(t, tt.want.Value, s.Value)
		})
	}
}

func TestSensorData_UnmarshalJSON_TimestampFallback(t *testing.T) {
	// 设备时钟不可信：不带时间戳（或时间戳不可用）时必须补服务端时间，
	// 且绝不能因为时间戳格式问题让整条上报失败。
	tests := []struct {
		name     string
		tsField  string
		wantTime time.Time // 非零时断言具体值，零值时只断言「不是零值且接近当前时间」
	}{
		{name: "缺失 timestamp", tsField: ""},
		{name: "null", tsField: `,"timestamp":null`},
		{name: "空字符串", tsField: `,"timestamp":""`},
		{name: "数字 0", tsField: `,"timestamp":0`},
		{name: "负数", tsField: `,"timestamp":-1`},
		{name: "无法识别的字符串", tsField: `,"timestamp":"not-a-time"`},
		{name: "对象", tsField: `,"timestamp":{"a":1}`},
		{name: "毫秒时间戳（数字）", tsField: `,"timestamp":1737000000000`, wantTime: time.UnixMilli(1737000000000)},
		{name: "秒时间戳（数字）", tsField: `,"timestamp":1737000000`, wantTime: time.Unix(1737000000, 0)},
		{name: "毫秒时间戳（字符串）", tsField: `,"timestamp":"1737000000000"`, wantTime: time.UnixMilli(1737000000000)},
		{name: "空格分隔本地时间", tsField: `,"timestamp":"2024-01-15 10:30:00"`, wantTime: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)},
		{name: "RFC3339", tsField: `,"timestamp":"2024-01-15T10:30:00Z"`, wantTime: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := `{"name":"temp1","type":"temperature","value":26.32` + tt.tsField + `}`
			var s SensorData
			err := json.Unmarshal([]byte(payload), &s)
			assert.NoError(t, err, "时间戳异常不应导致整条数据反序列化失败")
			assert.Equal(t, "temp1", s.Name)
			assert.False(t, s.Timestamp.IsZero(), "时间戳不应为零值")

			if tt.wantTime.IsZero() {
				assert.WithinDuration(t, time.Now(), s.Timestamp, 5*time.Second, "应回退为服务端当前时间")
			} else {
				assert.True(t, tt.wantTime.Equal(s.Timestamp), "want %v, got %v", tt.wantTime, s.Timestamp)
			}
		})
	}
}

func TestDeviceStatusDTO_UnmarshalJSON_NoTimestamp(t *testing.T) {
	// 固件实际上报格式（见 firmware/docs/mqtt-api.md）：sensors[] 不带 timestamp
	payload := `{"sensors":[{"name":"temp1","type":"temperature","value":26.32}]}`
	var dto DeviceStatusDTO
	assert.NoError(t, json.Unmarshal([]byte(payload), &dto))
	assert.Len(t, dto.Sensors, 1)
	assert.False(t, dto.Sensors[0].Timestamp.IsZero())
	assert.WithinDuration(t, time.Now(), dto.Sensors[0].Timestamp, 5*time.Second)
}

// TestSensorDataPayload_TagsMatch 防止 sensorDataPayload 与 SensorData 字段/标签脱节：
// 反序列化载体漏掉字段会静默丢数据，漏掉 binding 标签会让 HTTP 上报校验失效。
func TestSensorDataPayload_TagsMatch(t *testing.T) {
	src := reflect.TypeFor[SensorData]()
	dst := reflect.TypeFor[sensorDataPayload]()

	assert.Equal(t, src.NumField(), dst.NumField(), "字段数量不一致")
	for i := 0; i < src.NumField(); i++ {
		s, d := src.Field(i), dst.Field(i)
		assert.Equal(t, s.Name, d.Name, "第 %d 个字段名不一致", i)
		assert.Equal(t, string(s.Tag), string(d.Tag), "字段 %s 的 struct tag 不一致", s.Name)
	}
}

func TestSensorData_MarshalJSON(t *testing.T) {
	s := SensorData{
		Name:      "temperature",
		Type:      "number",
		Value:     26.5,
		Timestamp: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
	}
	b, err := json.Marshal(s)
	assert.NoError(t, err)
	assert.Contains(t, string(b), `"name":"temperature"`)
	assert.Contains(t, string(b), `"type":"number"`)
	assert.Contains(t, string(b), `"value":26.5`)
}

func TestSensorData_MarshalJSON_ZeroTimestamp(t *testing.T) {
	s := SensorData{
		Name:  "test",
		Type:  "data",
		Value: 100,
	}
	b, err := json.Marshal(s)
	assert.NoError(t, err)
	// 零值时间戳应被修正为当前时间，不应该是 0001-01-01
	assert.NotContains(t, string(b), "0001-01-01")
}

func TestSensorData_MarshalJSON_Milliseconds(t *testing.T) {
	s := SensorData{
		Name:      "hum",
		Type:      "int",
		Value:     60,
		Timestamp: time.Date(2026, 7, 13, 15, 25, 33, 25000000, time.FixedZone("CST", 8*3600)),
	}
	b, err := json.Marshal(s)
	assert.NoError(t, err)
	// timestamp 字段应为 RFC 3339 + 3 位毫秒
	assert.Regexp(t, `"timestamp":"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}[+-]\d{2}:\d{2}"`, string(b))
}

func TestSensorData_MarshalJSON_FormatNoZero(t *testing.T) {
	// 正常时间戳不应出现 0001 年
	s := SensorData{
		Name:      "temp",
		Type:      "float",
		Value:     25.0,
		Timestamp: time.Now(),
	}
	b, err := json.Marshal(s)
	assert.NoError(t, err)
	assert.NotContains(t, string(b), "0001-01-01")
}

func TestSensorData_UnmarshalJSON_FixesZero(t *testing.T) {
	// 零值时间戳会在 UnmarshalJSON 中被修正为当前时间
	var s SensorData
	err := json.Unmarshal([]byte(`{"name":"t","type":"n","value":1,"timestamp":"0001-01-01T00:00:00Z"}`), &s)
	assert.NoError(t, err)
	assert.False(t, s.Timestamp.IsZero())
}

func TestDeviceStatus_Fields(t *testing.T) {
	status := DeviceStatus{
		ID:             "abc123",
		DeviceID:       "abc123",
		OwnerID:        100,
		Status:         "ONLINE",
		LastActiveTime: common.DateTimeNow(),
		Sensors: []SensorData{
			{Name: "temp", Type: "number", Value: 25.0},
		},
	}
	assert.Equal(t, "abc123", status.ID)
	assert.Equal(t, "abc123", status.DeviceID)
	assert.Equal(t, uint(100), status.OwnerID)
	assert.Equal(t, "ONLINE", status.Status)
	assert.Len(t, status.Sensors, 1)
}

func TestDeviceStatusDTO_JSON(t *testing.T) {
	jsonBody := `{"sensors":[{"name":"temp","type":"number","value":26.5}]}`
	var dto DeviceStatusDTO
	err := json.Unmarshal([]byte(jsonBody), &dto)
	assert.NoError(t, err)
	assert.Len(t, dto.Sensors, 1)
	assert.Equal(t, "temp", dto.Sensors[0].Name)
}

func TestDeviceStatusDTO_EmptySensors(t *testing.T) {
	var dto DeviceStatusDTO
	err := json.Unmarshal([]byte(`{"sensors":[]}`), &dto)
	assert.NoError(t, err)
	assert.Len(t, dto.Sensors, 0)
}

func TestDeviceStatusDTO_MissingSensors(t *testing.T) {
	var dto DeviceStatusDTO
	err := json.Unmarshal([]byte(`{}`), &dto)
	assert.NoError(t, err)
	assert.Len(t, dto.Sensors, 0)
}

func TestDeviceStatusDTO_PanelOnly(t *testing.T) {
	// 设备连上 MQTT 后先发一条不带 sensors 的面板信息（复用 telemetry 通道）
	var dto DeviceStatusDTO
	err := json.Unmarshal([]byte(`{"panel":{"chip":"ssd1683","width":400,"height":300}}`), &dto)
	assert.NoError(t, err)
	assert.Len(t, dto.Sensors, 0)
	if assert.NotNil(t, dto.Panel) {
		assert.Equal(t, "ssd1683", dto.Panel.Chip)
		assert.Equal(t, 400, dto.Panel.Width)
		assert.Equal(t, 300, dto.Panel.Height)
	}
}

func TestSensorData_JSONRoundTrip(t *testing.T) {
	// 缓存往返（sensorsJSON 写 Redis → 详情接口读回）必须保持时间戳不变：
	// MarshalJSON 输出 "2006-01-02T15:04:05.000-07:00"，UnmarshalJSON 必须认得。
	loc := time.FixedZone("CST", 8*3600)
	original := SensorData{
		Name:      "hum",
		Type:      "int",
		Value:     float64(60),
		Timestamp: time.Date(2026, 7, 13, 15, 25, 33, 25000000, loc),
	}

	b, err := json.Marshal(original)
	assert.NoError(t, err)

	var restored SensorData
	assert.NoError(t, json.Unmarshal(b, &restored))
	assert.True(t, original.Timestamp.Equal(restored.Timestamp),
		"往返后时间戳不一致: want %v, got %v", original.Timestamp, restored.Timestamp)
}
