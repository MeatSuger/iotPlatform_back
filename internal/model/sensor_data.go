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
	"fmt"
	"slices"
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

// ValidateSensorValue 校验上报值类型与物模型定义 dataType 对齐（JSON 格式统一）。
//
// float/int：必须为数值；bool：布尔；text：字符串；enum：字符串且在 definitions
// specs.values 内（values 为空表示不限制取值）。类型不符返回错误，由上报服务
// 丢弃该条数据并告警，避免类型错乱的数据进入类型敏感的 InfluxDB。
func ValidateSensorValue(dataType string, value any, specs *SensorSpecs) error {
	switch dataType {
	case "float", "int":
		if _, ok := toFloat64(value); !ok {
			return fmt.Errorf("dataType=%s 要求数值, 收到 %T", dataType, value)
		}
	case "bool":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("dataType=bool 要求布尔值, 收到 %T", value)
		}
	case "text":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("dataType=text 要求字符串, 收到 %T", value)
		}
	case "enum":
		str, ok := value.(string)
		if !ok {
			return fmt.Errorf("dataType=enum 要求字符串值, 收到 %T", value)
		}
		if specs != nil && len(specs.Values) > 0 && !slices.Contains(specs.Values, str) {
			return fmt.Errorf("dataType=enum 取值 %q 不在定义 values %v 内", str, specs.Values)
		}
	}
	return nil
}
