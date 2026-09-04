package entity

import (
	"encoding/json"
	"fmt"
	"regexp"

	"iot-platform.local/pkg/common"
)

// Sensor 传感器定义 / 物模型（DTO，对应 ent.DeviceSensor 对外暴露）
//
// 参考新大陆 NLECloud 传感器模型（ApiTag/Name/DataType/TypeAttrs）与
// 阿里云 IoT TSL 物模型（identifier/dataType/specs）设计。
type Sensor struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Type           string          `json:"type"`
	DataType       string          `json:"dataType"`
	Unit           string          `json:"unit"`
	Specs          map[string]any  `json:"specs"`
	ReportInterval int             `json:"reportInterval"`
	Thresholds     map[string]any  `json:"thresholds"`
	Attrs          map[string]any  `json:"attrs"`
	Enabled        bool            `json:"enabled"`
	CreatedAt      common.DateTime `json:"createdAt"`
	UpdatedAt      common.DateTime `json:"updatedAt"`
}

// SensorCreateRequest 创建传感器定义请求
type SensorCreateRequest struct {
	ID             string         `json:"id" binding:"required"`
	Name           string         `json:"name" binding:"required"`
	Type           string         `json:"type" binding:"required"`
	DataType       string         `json:"dataType"`
	Unit           string         `json:"unit"`
	Specs          map[string]any `json:"specs"`
	ReportInterval int            `json:"reportInterval"`
	Thresholds     map[string]any `json:"thresholds"`
	Attrs          map[string]any `json:"attrs"`
	Enabled        *bool          `json:"enabled"`
}

// SensorUpdateRequest 传感器定义增量更新请求体
//
// 全部字段为指针 / RawMessage：仅出现在请求体中的字段会被更新，未传字段保持原值；
// specs/thresholds/attrs 传 {} 表示显式清空。id 为资源标识，不可更新（AIP-136）。
type SensorUpdateRequest struct {
	Name           *string          `json:"name"`
	Type           *string          `json:"type"`
	DataType       *string          `json:"dataType"`
	Unit           *string          `json:"unit"`
	Specs          *json.RawMessage `json:"specs"`
	ReportInterval *int             `json:"reportInterval"`
	Thresholds     *json.RawMessage `json:"thresholds"`
	Attrs          *json.RawMessage `json:"attrs"`
	Enabled        *bool            `json:"enabled"`
}

// SensorApplyResponse 传感器配置下发响应
type SensorApplyResponse struct {
	DeviceID string `json:"deviceId"`
	Version  uint   `json:"version"`
	Status   string `json:"status"`
	Count    int    `json:"count"`
}

// sensorIDPattern 传感器标识符：小写字母开头，仅含小写字母/数字/下划线
var sensorIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,49}$`)

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
		return fmt.Errorf("传感器标识符格式无效（需小写字母开头，仅含小写字母/数字/下划线，≤50字符）: %s", id)
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
	if r.ReportInterval < 0 {
		return fmt.Errorf("上报周期不能为负数")
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
		if err := validateJSONObject(*r.Specs); err != nil {
			return fmt.Errorf("specs 非法: %w", err)
		}
	}
	if r.ReportInterval != nil {
		empty = false
		if *r.ReportInterval < 0 {
			return fmt.Errorf("上报周期不能为负数")
		}
	}
	if r.Thresholds != nil {
		empty = false
		if err := validateJSONObject(*r.Thresholds); err != nil {
			return fmt.Errorf("thresholds 非法: %w", err)
		}
	}
	if r.Attrs != nil {
		empty = false
		if err := validateJSONObject(*r.Attrs); err != nil {
			return fmt.Errorf("attrs 非法: %w", err)
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

// validateJSONObject 校验 RawMessage 为合法 JSON 对象（{} 合法，用于显式清空）
func validateJSONObject(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("字段缺失")
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	return nil
}
