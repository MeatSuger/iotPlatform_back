package entity

import (
	"encoding/json"
	"fmt"
	"regexp"

	"iot-platform.local/pkg/common"
)

// Actuator 执行器定义 / 物模型（DTO，对应 ent.DeviceActuator 对外暴露）
//
// 与 DeviceSensor 同构：定义仅持久化，经 Apply 编译进
// DeviceConfig.payload.actuators 后版本化下发；id 即固件 periph 设备名
// （控制命令 action 路由键），driver 为驱动名（led/servo/speaker）。
//
// 命名统一：驱动参数原 config 改名为 specs（与 Sensor 定义体同名），
// DB 列 params 同步改名为 specs；specs 保持自由键值（结构由固件各驱动
// probe 约定，如 gpio/count/脉宽范围），平台侧不校验其内部结构。
type Actuator struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Driver    string          `json:"driver"`
	Specs     map[string]any  `json:"specs,omitempty"`
	Enabled   bool            `json:"enabled"`
	CreatedAt common.DateTime `json:"createdAt"`
	UpdatedAt common.DateTime `json:"updatedAt"`
}

// ActuatorCreateRequest 创建执行器定义请求
type ActuatorCreateRequest struct {
	ID      string         `json:"id" binding:"required"`
	Name    string         `json:"name"`
	Driver  string         `json:"driver" binding:"required"`
	Specs   map[string]any `json:"specs"`
	Enabled *bool          `json:"enabled"`
}

// ActuatorUpdateRequest 执行器定义增量更新请求体
//
// 全部字段为指针 / RawMessage：仅出现在请求体中的字段会被更新，未传字段保持原值；
// specs 传 {} 表示显式清空。id 为资源标识，不可更新（AIP-136）。
type ActuatorUpdateRequest struct {
	Name    *string          `json:"name"`
	Driver  *string          `json:"driver"`
	Specs   *json.RawMessage `json:"specs"`
	Enabled *bool            `json:"enabled"`
}

// ActuatorApplyResponse 执行器配置下发响应
type ActuatorApplyResponse struct {
	DeviceID string `json:"deviceId"`
	Version  uint   `json:"version"`
	Status   string `json:"status"`
	Count    int    `json:"count"`
}

// ActuatorWire 下发到设备的执行器定义（Apply 编译进 DeviceConfig.payload.actuators）
//
// 裁剪版：仅保留设备运行所需字段（id/driver/specs/enabled），
// 不含 name/createdAt/updatedAt 等管理字段；空 specs 省略，最小化下行 payload。
type ActuatorWire struct {
	ID      string         `json:"id"`
	Driver  string         `json:"driver"`
	Specs   map[string]any `json:"specs,omitempty"`
	Enabled bool           `json:"enabled"`
}

// actuatorIDPattern 执行器标识符：小写字母开头，仅含小写字母/数字/下划线，≤11字符
// （固件 periph 设备名契约：控制命令 action / 驱动缓冲上限）
var actuatorIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,10}$`)

// ActuatorDrivers 固件支持的执行器驱动名
var ActuatorDrivers = []string{"led", "servo", "speaker"}

// ValidateActuatorID 校验执行器标识符格式
func ValidateActuatorID(id string) error {
	if !actuatorIDPattern.MatchString(id) {
		return fmt.Errorf("执行器标识符格式无效（需小写字母开头，仅含小写字母/数字/下划线，≤11字符）: %s", id)
	}
	return nil
}

// ValidateDriver 校验驱动名取值
func ValidateDriver(driver string) error {
	for _, d := range ActuatorDrivers {
		if driver == d {
			return nil
		}
	}
	return fmt.Errorf("无效的驱动名: %s（可选 led/servo/speaker）", driver)
}

// Validate 创建请求校验
func (r ActuatorCreateRequest) Validate() error {
	if err := ValidateActuatorID(r.ID); err != nil {
		return err
	}
	if len(r.Name) > 100 {
		return fmt.Errorf("执行器名称过长（≤100字符）")
	}
	if err := ValidateDriver(r.Driver); err != nil {
		return err
	}
	return nil
}

// Validate 增量更新请求校验（仅校验出现的字段，至少一个字段）
func (r ActuatorUpdateRequest) Validate() error {
	empty := true
	if r.Name != nil {
		empty = false
		if len(*r.Name) > 100 {
			return fmt.Errorf("执行器名称过长（≤100字符）")
		}
	}
	if r.Driver != nil {
		empty = false
		if err := ValidateDriver(*r.Driver); err != nil {
			return err
		}
	}
	if r.Specs != nil {
		empty = false
		if err := validateJSONObject(*r.Specs); err != nil {
			return fmt.Errorf("specs 非法: %w", err)
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

// validateJSONObject 校验 RawMessage 为合法 JSON 对象（{} 合法，用于显式清空；
// null 视为非法——统一语义为「不传字段不更新，传 {} 清空」）
func validateJSONObject(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("字段缺失")
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	if obj == nil {
		return fmt.Errorf("字段不能为 null（如需清空请传 {}）")
	}
	return nil
}
