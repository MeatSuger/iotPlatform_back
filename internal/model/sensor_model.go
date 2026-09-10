package entity

import (
	"encoding/json"
	"fmt"
	"regexp"

	"iot-platform.local/pkg/common"
)

// Sensor 传感器定义 / 物模型（DTO，对应 ent.DeviceThing(kind=sensor) 对外暴露）
//
// 参考新大陆 NLECloud 传感器模型（ApiTag/Name/DataType/TypeAttrs）与
// 阿里云 IoT TSL 物模型（identifier/dataType/specs）设计。
//
// 统一物模型定义 JSON：
//   - 原 specs / thresholds / attrs 三个自由 JSON 对象合并为一个强类型 specs（见 SensorSpecs）；
//   - reportInterval 为空表示继承设备级全局采样周期
//     （DefaultDeviceConfig.sensor.reportInterval），不重复下发；
//   - 下发（Apply）编译为裁剪版 SensorWire，见 Apply 文档。
type Sensor struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Type           string          `json:"type"`
	DataType       string          `json:"dataType"`
	Unit           string          `json:"unit"`
	Specs          *SensorSpecs    `json:"specs,omitempty"`
	ReportInterval *int            `json:"reportInterval,omitempty"`
	Enabled        bool            `json:"enabled"`
	CreatedAt      common.DateTime `json:"createdAt"`
	UpdatedAt      common.DateTime `json:"updatedAt"`
}

// SensorSpecs 传感器定义规格（统一物模型定义体，强类型）
//
// 统一前：specs（量程）、thresholds（告警）、attrs（自由扩展）为三个互不校验的
// 自由 JSON 对象；统一后全部并入一个 specs：
//   - float/int：min / max / step（数量程）
//   - enum：values（可选项，为空表示不限制取值）
//   - text：maxLen（最大长度）
//   - 任意 dataType：thresholds（告警阈值，min/max + 扩展键）、其余自由键（原 attrs 能力）
//
// 已知键强类型化（类型错误在请求绑定/落库前即被拒绝），未知键经 Extra 平铺透传，
// 保留扩展能力；数字以 float64 承载（JSON 无整数/浮点之分，整数值序列化仍为整数）。
type SensorSpecs struct {
	Min        *float64         `json:"min,omitempty"`
	Max        *float64         `json:"max,omitempty"`
	Step       *float64         `json:"step,omitempty"`
	Values     []string         `json:"values,omitempty"`
	MaxLen     *int             `json:"maxLen,omitempty"`
	Thresholds *SpecsThresholds `json:"thresholds,omitempty"`
	Extra      map[string]any   `json:"-"`
}

// SpecsThresholds 告警阈值（min/max 强类型 + 扩展键平铺，如 alarm/relay 等）
type SpecsThresholds struct {
	Min   *float64       `json:"min,omitempty"`
	Max   *float64       `json:"max,omitempty"`
	Extra map[string]any `json:"-"`
}

// MarshalJSON 已知键 + Extra 平铺输出（键排序由 encoding/json 保证，下发 payload 稳定）
func (s SensorSpecs) MarshalJSON() ([]byte, error) {
	m := make(map[string]any, 8+len(s.Extra))
	if s.Min != nil {
		m["min"] = *s.Min
	}
	if s.Max != nil {
		m["max"] = *s.Max
	}
	if s.Step != nil {
		m["step"] = *s.Step
	}
	if len(s.Values) > 0 {
		m["values"] = s.Values
	}
	if s.MaxLen != nil {
		m["maxLen"] = *s.MaxLen
	}
	if s.Thresholds != nil {
		m["thresholds"] = s.Thresholds
	}
	for k, v := range s.Extra {
		m[k] = v
	}
	return json.Marshal(m)
}

// UnmarshalJSON 接受任意数字形式（1 / 1.0 / 1e3），类型不符返回错误（强类型语义）
func (s *SensorSpecs) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = SensorSpecs{Extra: map[string]any{}}
	for k, v := range raw {
		switch k {
		case "min":
			f, ok := toFloat64(v)
			if !ok {
				return fmt.Errorf("specs.min 必须是数值, 收到 %T", v)
			}
			s.Min = &f
		case "max":
			f, ok := toFloat64(v)
			if !ok {
				return fmt.Errorf("specs.max 必须是数值, 收到 %T", v)
			}
			s.Max = &f
		case "step":
			f, ok := toFloat64(v)
			if !ok {
				return fmt.Errorf("specs.step 必须是数值, 收到 %T", v)
			}
			s.Step = &f
		case "values":
			arr, ok := v.([]any)
			if !ok {
				return fmt.Errorf("specs.values 必须是字符串数组, 收到 %T", v)
			}
			for _, item := range arr {
				str, ok := item.(string)
				if !ok {
					return fmt.Errorf("specs.values 必须是字符串数组, 元素为 %T", item)
				}
				s.Values = append(s.Values, str)
			}
		case "maxLen":
			n, ok := toInt(v)
			if !ok {
				return fmt.Errorf("specs.maxLen 必须是整数, 收到 %T", v)
			}
			s.MaxLen = &n
		case "thresholds":
			var th SpecsThresholds
			b, err := json.Marshal(v)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(b, &th); err != nil {
				return fmt.Errorf("specs.thresholds 非法: %w", err)
			}
			s.Thresholds = &th
		default:
			s.Extra[k] = v
		}
	}
	return nil
}

// MarshalJSON 已知键 + Extra 平铺输出
func (s SpecsThresholds) MarshalJSON() ([]byte, error) {
	m := make(map[string]any, 4+len(s.Extra))
	if s.Min != nil {
		m["min"] = *s.Min
	}
	if s.Max != nil {
		m["max"] = *s.Max
	}
	for k, v := range s.Extra {
		m[k] = v
	}
	return json.Marshal(m)
}

// UnmarshalJSON 接受任意数字形式，类型不符返回错误
func (s *SpecsThresholds) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = SpecsThresholds{Extra: map[string]any{}}
	for k, v := range raw {
		switch k {
		case "min":
			f, ok := toFloat64(v)
			if !ok {
				return fmt.Errorf("thresholds.min 必须是数值, 收到 %T", v)
			}
			s.Min = &f
		case "max":
			f, ok := toFloat64(v)
			if !ok {
				return fmt.Errorf("thresholds.max 必须是数值, 收到 %T", v)
			}
			s.Max = &f
		default:
			s.Extra[k] = v
		}
	}
	return nil
}

// toFloat64 数值统一转 float64（JSON 数字均为 float64；容忍程序内 int 形式）
func toFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

// toInt 数值统一转 int（仅接受整数值）
func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		if x != float64(int(x)) {
			return 0, false
		}
		return int(x), true
	case int:
		return x, true
	case int64:
		return int(x), true
	case json.Number:
		n, err := x.Int64()
		return int(n), err == nil
	}
	return 0, false
}

// Validate 校验 specs 结构（创建/更新共用；范围与取值约束）
func (s *SensorSpecs) Validate() error {
	if s == nil {
		return nil
	}
	if s.Min != nil && s.Max != nil && *s.Min > *s.Max {
		return fmt.Errorf("specs.min 不能大于 specs.max")
	}
	if s.Step != nil && *s.Step <= 0 {
		return fmt.Errorf("specs.step 必须大于 0")
	}
	if s.MaxLen != nil && *s.MaxLen <= 0 {
		return fmt.Errorf("specs.maxLen 必须大于 0")
	}
	if s.Thresholds != nil {
		if s.Thresholds.Min != nil && s.Min != nil && *s.Thresholds.Min < *s.Min {
			return fmt.Errorf("specs.thresholds.min 不能小于量程下限 specs.min")
		}
		if s.Thresholds.Max != nil && s.Max != nil && *s.Thresholds.Max > *s.Max {
			return fmt.Errorf("specs.thresholds.max 不能大于量程上限 specs.max")
		}
		if s.Thresholds.Min != nil && s.Thresholds.Max != nil && *s.Thresholds.Min > *s.Thresholds.Max {
			return fmt.Errorf("specs.thresholds.min 不能大于 thresholds.max")
		}
		// 阈值区间不得整体落在量程之外（单侧越界也拒绝）
		if s.Thresholds.Min != nil && s.Max != nil && *s.Thresholds.Min > *s.Max {
			return fmt.Errorf("specs.thresholds.min 超出量程上限 specs.max")
		}
		if s.Thresholds.Max != nil && s.Min != nil && *s.Thresholds.Max < *s.Min {
			return fmt.Errorf("specs.thresholds.max 低于量程下限 specs.min")
		}
	}
	return nil
}

// Validate 校验 specs 结构（创建/更新共用；范围与取值约束）

// SensorCreateRequest 创建传感器定义请求
type SensorCreateRequest struct {
	ID             string       `json:"id" binding:"required"`
	Name           string       `json:"name" binding:"required"`
	Type           string       `json:"type" binding:"required"`
	DataType       string       `json:"dataType"`
	Unit           string       `json:"unit"`
	Specs          *SensorSpecs `json:"specs"`
	ReportInterval *int         `json:"reportInterval"`
	Enabled        *bool        `json:"enabled"`
}

// SensorUpdateRequest 传感器定义增量更新请求体
//
// 全部字段为指针 / RawMessage：仅出现在请求体中的字段会被更新，未传字段保持原值；
// specs 传 {} 表示显式清空，reportInterval 传 null 表示恢复继承全局周期；
// id 为资源标识，不可更新（AIP-136）。
type SensorUpdateRequest struct {
	Name           *string          `json:"name"`
	Type           *string          `json:"type"`
	DataType       *string          `json:"dataType"`
	Unit           *string          `json:"unit"`
	Specs          *json.RawMessage `json:"specs"`
	ReportInterval *json.RawMessage `json:"reportInterval"`
	Enabled        *bool            `json:"enabled"`
}

// SensorApplyResponse 传感器配置下发响应
type SensorApplyResponse struct {
	DeviceID string `json:"deviceId"`
	Version  uint   `json:"version"`
	Status   string `json:"status"`
	Count    int    `json:"count"`
}

// SensorWire 下发到设备的传感器定义（Apply 编译进 DeviceConfig.payload.sensors）
//
// 裁剪版：仅保留设备运行所需字段（id/type/dataType/unit/specs/reportInterval/enabled），
// 不含 name/createdAt/updatedAt 等管理字段；空 specs、继承全局周期的 reportInterval 均省略，
// 最小化下行 payload（目标设备为受限 MCU）。
type SensorWire struct {
	ID             string       `json:"id"`
	Type           string       `json:"type,omitempty"`
	DataType       string       `json:"dataType"`
	Unit           string       `json:"unit,omitempty"`
	Specs          *SensorSpecs `json:"specs,omitempty"`
	ReportInterval *int         `json:"reportInterval,omitempty"`
	Enabled        bool         `json:"enabled"`
}

// sensorIDPattern 传感器标识符：字母开头，仅含字母/数字/下划线
var sensorIDPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,49}$`)

// 允许的数据类型（dataType 字段取值）
var sensorDataTypes = map[string]bool{
	"float": true,
	"int":   true,
	"bool":  true,
	"text":  true,
	"enum":  true,
}

// ValidateSensorID 校验传感器标识符格式
func ValidateSensorID(id string) error {
	if !sensorIDPattern.MatchString(id) {
		return fmt.Errorf("传感器标识符格式无效（需字母开头，仅含字母/数字/下划线，≤50字符）: %s", id)
	}
	return nil
}

// ValidateDataType 校验数据类型取值，空值回退默认 float
func ValidateDataType(dataType string) (string, error) {
	if dataType == "" {
		return "float", nil
	}
	if !sensorDataTypes[dataType] {
		return "", fmt.Errorf("无效的数据类型: %s（可选 float/int/bool/text/enum）", dataType)
	}
	return dataType, nil
}

// Validate 创建请求校验
func (r SensorCreateRequest) Validate() error {
	if err := ValidateSensorID(r.ID); err != nil {
		return err
	}
	if len(r.Name) > 100 {
		return fmt.Errorf("传感器名称过长（≤100字符）")
	}
	if len(r.Type) > 50 {
		return fmt.Errorf("传感器类型过长（≤50字符）")
	}
	if _, err := ValidateDataType(r.DataType); err != nil {
		return err
	}
	if len(r.Unit) > 32 {
		return fmt.Errorf("单位过长（≤32字符）")
	}
	if r.ReportInterval != nil && *r.ReportInterval <= 0 {
		return fmt.Errorf("上报周期必须大于 0（传 null 继承全局周期）")
	}
	if err := r.Specs.Validate(); err != nil {
		return err
	}
	return nil
}

// Validate 增量更新请求校验（仅校验出现的字段，至少一个字段）
func (r SensorUpdateRequest) Validate() error {
	empty := true
	if r.Name != nil {
		empty = false
		if len(*r.Name) > 100 {
			return fmt.Errorf("传感器名称过长（≤100字符）")
		}
	}
	if r.Type != nil {
		empty = false
		if len(*r.Type) > 50 {
			return fmt.Errorf("传感器类型过长（≤50字符）")
		}
	}
	if r.DataType != nil {
		empty = false
		if _, err := ValidateDataType(*r.DataType); err != nil {
			return err
		}
	}
	if r.Unit != nil {
		empty = false
		if len(*r.Unit) > 32 {
			return fmt.Errorf("单位过长（≤32字符）")
		}
	}
	if r.Specs != nil {
		empty = false
		specs, err := parseRawSpecs(*r.Specs)
		if err != nil {
			return fmt.Errorf("specs 非法: %w", err)
		}
		if err := specs.Validate(); err != nil {
			return err
		}
	}
	if r.ReportInterval != nil {
		empty = false
		raw := string(*r.ReportInterval)
		if raw != "null" {
			var n int
			if err := json.Unmarshal(*r.ReportInterval, &n); err != nil || n <= 0 {
				return fmt.Errorf("reportInterval 必须是正整数或 null（null=继承全局周期）")
			}
		}
	}
	if r.Enabled != nil {
		empty = false
	}
	if empty {
		return fmt.Errorf("无更新字段")
	}
	return nil
}

// parseRawSpecs 解析增量更新中的 specs RawMessage。
//
// 语义：{} 合法（显式清空）；null 非法（统一「不传不更新，传 {} 清空」）；
// 其余必须为合法强类型 SensorSpecs（类型错误直接拒绝）。
func parseRawSpecs(raw json.RawMessage) (*SensorSpecs, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("字段缺失")
	}
	if string(raw) == "null" {
		return nil, fmt.Errorf("字段不能为 null（如需清空请传 {}）")
	}
	var specs SensorSpecs
	if err := json.Unmarshal(raw, &specs); err != nil {
		return nil, err
	}
	return &specs, nil
}
