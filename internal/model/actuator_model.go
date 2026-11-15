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
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"iot-platform.local/pkg/common"
)

// Actuator 执行器定义 / 物模型（DTO，对应 ent.DeviceThing(kind=actuator) 对外暴露）
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
	Specs   *json.RawMessage `json:"specs" swaggertype:"object"`
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

// ── EPD（墨水屏）specs 约定 ───────────────────────────────────────────────
//
// specs.transport=epd 时，平台对以下键做结构校验（其余 transport 继续保留
// 自由键值，由固件驱动 probe 约定）：
//
//	size_inch       屏幕尺寸（英寸，如 4.2 / 7.5），前端只展示该值
//	width / height  显示逻辑分辨率（像素，8 的倍数，便于整字节打包）
//	chip            显示控制器型号（如 ssd1683 / jd79665），≤40 字符
//	color_depth     色深/配色：bw / bwr / bwry / rgb565 / rgb888
//
// 存量定义（仅 {"transport":"epd"}）继续可读可下发，固件回退到编译期面板；
// 新建/更新时要求补全上述字段，避免云端与面板配置漂移。
const (
	// ActuatorTransportEPD 墨水屏 transport 名
	ActuatorTransportEPD = "epd"

	// EPD 色深/配色（specs.color_depth）
	EPDColorDepthBW     = "bw"     // 黑白（1bpp）
	EPDColorDepthBWR    = "bwr"    // 三色：黑白红（BWR）
	EPDColorDepthBWRY   = "bwry"   // 四色：黑白红黄（BWRY，2bpp）
	EPDColorDepthRGB565 = "rgb565" // 16 位 RGB
	EPDColorDepthRGB888 = "rgb888" // 24 位 RGB

	epdMinSide    = 8
	epdMaxSide    = 4096
	epdMaxChipLen = 40
	epdMaxInch    = 100.0
)

// EPDColorDepths EPD 支持的色深 / 配色取值（specs.color_depth）
var EPDColorDepths = []string{
	EPDColorDepthBW,
	EPDColorDepthBWR,
	EPDColorDepthBWRY,
	EPDColorDepthRGB565,
	EPDColorDepthRGB888,
}

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

// ValidateActuatorSpecs 校验执行器 specs 结构。
//
// 仅对 specs.transport=epd 做约束：width/height/chip/color_depth 必填；
// 其他 transport 与空 specs 保持自由键值（结构由固件各驱动 probe 约定）。
func ValidateActuatorSpecs(specs map[string]any) error {
	if len(specs) == 0 {
		return nil
	}
	raw, ok := specs["transport"]
	if !ok || raw == nil {
		return nil
	}
	transport, ok := raw.(string)
	if !ok {
		return fmt.Errorf("specs.transport 必须是字符串, 收到 %T", raw)
	}
	if transport != ActuatorTransportEPD {
		return nil
	}
	return validateEPDSpecs(specs)
}

// validateEPDSpecs 校验墨水屏显示参数
func validateEPDSpecs(specs map[string]any) error {
	depth, ok := specs["color_depth"].(string)
	if !ok || depth == "" {
		return fmt.Errorf("specs.color_depth 必填（可选 %s）", strings.Join(EPDColorDepths, "/"))
	}
	if !slices.Contains(EPDColorDepths, depth) {
		return fmt.Errorf("invalid specs.color_depth: %s（可选 %s）", depth, strings.Join(EPDColorDepths, "/"))
	}

	width, err := epdPixelSpec(specs, "width")
	if err != nil {
		return err
	}
	if _, err := epdPixelSpec(specs, "height"); err != nil {
		return err
	}
	// BW/BWR/BWRY 走整字节位图打包，宽度须为 8 的倍数；RGB565/RGB888 无此约束
	if depth != EPDColorDepthRGB565 && depth != EPDColorDepthRGB888 && width%8 != 0 {
		return fmt.Errorf("specs.width 必须是 8 的倍数（位图整字节打包）: %d", width)
	}

	chip, ok := specs["chip"].(string)
	if !ok || strings.TrimSpace(chip) == "" {
		return fmt.Errorf("specs.chip 必填（显示控制器型号，如 ssd1683）")
	}
	if utf8.RuneCountInString(chip) > epdMaxChipLen {
		return fmt.Errorf("specs.chip 过长（≤%d 字符）", epdMaxChipLen)
	}

	inch, ok := toFloat64(specs["size_inch"])
	if !ok || inch <= 0 || inch > epdMaxInch {
		return fmt.Errorf("specs.size_inch 必填（屏幕尺寸，英寸，0–%.0f）", epdMaxInch)
	}
	return nil
}

// epdPixelSpec 读取并校验 EPD 边长（像素，整数）
func epdPixelSpec(specs map[string]any, key string) (int, error) {
	raw, ok := specs[key]
	if !ok || raw == nil {
		return 0, fmt.Errorf("specs.%s 必填（像素）", key)
	}
	n, ok := toInt(raw)
	if !ok {
		return 0, fmt.Errorf("specs.%s 必须是整数, 收到 %T", key, raw)
	}
	if n < epdMinSide || n > epdMaxSide {
		return 0, fmt.Errorf("specs.%s 超出范围（%d–%d）: %d", key, epdMinSide, epdMaxSide, n)
	}
	return n, nil
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
	if err := ValidateActuatorSpecs(r.Specs); err != nil {
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
		var specs map[string]any
		if err := json.Unmarshal(*r.Specs, &specs); err != nil {
			return fmt.Errorf("specs 非法: %w", err)
		}
		if err := ValidateActuatorSpecs(specs); err != nil {
			return err
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
