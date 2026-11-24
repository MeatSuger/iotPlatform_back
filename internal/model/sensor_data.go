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
	"strings"
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

// sensorDataPayload 仅用于反序列化：字段/标签与 SensorData 一一对应，
// 区别是 timestamp 先以原始 JSON 收下。
//
// 为什么不能继续用 `type alias SensorData`：time.Time 只认 RFC 3339，
// 设备发 ""、秒级数字、"2006-01-02 15:04:05" 都会让 json.Unmarshal 直接报错，
// 而报错会拖垮整条上报（MQTT 直接丢弃、HTTP 返回 400）。
//
// ⚠️ 字段名与 struct tag 必须与 SensorData 保持同步（TestSensorDataPayload_TagsMatch 会校验）。
type sensorDataPayload struct {
	Name      string          `json:"name" binding:"required"`
	Type      string          `json:"type" binding:"required"`
	Value     any             `json:"value" binding:"required"`
	Timestamp json.RawMessage `json:"timestamp"`
}

// UnmarshalJSON 反序列化时宽松解析时间戳：终端时钟不可信也不统一，
// 约定「不带可用时间戳就用服务端当前时间」，且绝不因为时间戳格式问题丢整条数据。
//
// 具体规则：
//   - 缺失 / null / "" / 0          → 服务端当前时间
//   - RFC 3339 及空格分隔等兼容格式   → 按解析结果
//   - 秒级 / 毫秒级 Unix 数字或数字串  → 按解析结果
//   - 其它无法识别的值               → 服务端当前时间
func (s *SensorData) UnmarshalJSON(data []byte) error {
	var payload sensorDataPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	*s = SensorData{
		Name:      payload.Name,
		Type:      payload.Type,
		Value:     payload.Value,
		Timestamp: parseReportTimestamp(payload.Timestamp),
	}
	return nil
}

// parseReportTimestamp 宽松解析上报时间戳。
//
// 设备时钟不可信也不统一，统一约定「不带可用时间戳就用服务端当前时间」：
//   - 缺失 / null / "" / 0 / 负数 / 无法识别 → 服务端当前时间
//   - RFC 3339 及空格分隔等兼容格式       → 按解析结果
//   - 秒/毫秒/微秒/纳秒 Unix 数字或数字串  → 按解析结果
//   - 0001-01-01（Go 零值，设备未对时）   → 服务端当前时间
func parseReportTimestamp(raw json.RawMessage) time.Time {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return time.Now()
	}

	// 字符串：先按兼容布局解析，再尝试当作 Unix 时间戳数字串
	if strings.HasPrefix(s, `"`) {
		s = strings.Trim(s, `"`)
		if t, ok := common.ParseFlexibleTime(s); ok {
			return fallbackIfZero(t)
		}
	}

	// 数字（或数字串）：Unix 时间戳（秒/毫秒/微秒/纳秒）
	if t, ok := common.ParseEpochValue(s); ok {
		return t
	}
	return time.Now()
}

// fallbackIfZero 把零值时间（0001-01-01：Go 零值 / 设备未对时）
// 回退为服务端当前时间，避免 InfluxDB 里出现 0001 年的数据点。
func fallbackIfZero(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
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
		Timestamp: ts.Format(common.DateTimeFormatWithZone),
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

// PanelInfo 设备面板信息（随 telemetry 顺带上报，不参与传感器入库）
type PanelInfo struct {
	Chip   string `json:"chip"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// DeviceStatusDTO 设备状态上报请求（DTO）
// panel 为可选字段：设备连上 MQTT 后先发一条不带 sensors 的面板信息，
// 复用 telemetry 通道，不新增主题。
type DeviceStatusDTO struct {
	Sensors []SensorData `json:"sensors" binding:"required"`
	Panel   *PanelInfo   `json:"panel,omitempty"`
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
