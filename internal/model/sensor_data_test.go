package entity

import (
	"encoding/json"
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
