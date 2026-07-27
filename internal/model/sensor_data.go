package entity

import (
	"encoding/json"
	"time"

	"iot-platform.local/pkg/common"
)

// SensorData 传感器数据（DTO）
type SensorData struct {
	Name      string    `json:"name" binding:"required"`
	Type      string    `json:"type" binding:"required"`
	Value     any       `json:"value" binding:"required"`
	Timestamp time.Time `json:"timestamp"`
}

// UnmarshalJSON 反序列化时自动修正零值时间戳
func (s *SensorData) UnmarshalJSON(data []byte) error {
	type alias SensorData
	tmp := alias{}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	if tmp.Timestamp.IsZero() {
		tmp.Timestamp = time.Now()
	}
	*s = SensorData(tmp)
	return nil
}

// MarshalJSON 序列化时统一输出 3 位毫秒精度（与 DateTime 格式一致）
func (s SensorData) MarshalJSON() ([]byte, error) {
	ts := s.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	return json.Marshal(struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		Value     any    `json:"value"`
		Timestamp string `json:"timestamp"`
	}{
		Name:      s.Name,
		Type:      s.Type,
		Value:     s.Value,
		Timestamp: ts.Format("2006-01-02T15:04:05.000-07:00"),
	})
}

// DeviceStatus 设备状态（DTO）
type DeviceStatus struct {
	ID             string          `json:"id"`
	DeviceID       string          `json:"deviceId"`
	OwnerID        uint            `json:"ownerId"`
	Status         string          `json:"status"`
	LastActiveTime common.DateTime `json:"lastActiveTime"`
	Sensors        []SensorData    `json:"sensors"`
}

// DeviceStatusDTO 设备状态上报请求（DTO）
type DeviceStatusDTO struct {
	Sensors []SensorData `json:"sensors" binding:"required"`
}
