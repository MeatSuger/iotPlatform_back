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
	"testing"

	"github.com/stretchr/testify/assert"
)

// validEPDSpecs 完整的墨水屏 specs（所有 transport=epd 的必填键）
func validEPDSpecs() map[string]any {
	return map[string]any{
		"transport":   "epd",
		"size_inch":   4.2,
		"width":       float64(400),
		"height":      float64(300),
		"chip":        "ssd1683",
		"color_depth": "bwr",
	}
}

func TestValidateActuatorSpecs_EPD(t *testing.T) {
	cases := []struct {
		name    string
		specs   map[string]any
		wantErr string
	}{
		{name: "空 specs 放行（旧版定义）", specs: nil},
		{name: "非 epd transport 保持自由键值", specs: map[string]any{"transport": "pwm", "pin": float64(18)}},
		{name: "无 transport 放行", specs: map[string]any{"gpio": float64(48)}},
		{name: "epd 完整配置", specs: validEPDSpecs()},
		{name: "epd 四色 rgb 色深", specs: map[string]any{
			"transport": "epd", "size_inch": 7.5, "width": float64(768), "height": float64(552),
			"chip": "jd79665", "color_depth": "bwry",
		}},
		{name: "epd rgb565 宽度无需 8 的倍数", specs: map[string]any{
			"transport": "epd", "size_inch": 4.0, "width": float64(402), "height": float64(480),
			"chip": "custom", "color_depth": "rgb565",
		}},
		{name: "epd rgb888", specs: map[string]any{
			"transport": "epd", "size_inch": 10, "width": float64(800), "height": float64(480),
			"chip": "custom", "color_depth": "rgb888",
		}},
		{name: "transport 非字符串", specs: map[string]any{"transport": float64(1)}, wantErr: "specs.transport 必须是字符串"},
		{name: "缺少 width", specs: map[string]any{
			"transport": "epd", "height": float64(300), "chip": "ssd1683", "color_depth": "bwr",
		}, wantErr: "specs.width 必填"},
		{name: "缺少 height", specs: map[string]any{
			"transport": "epd", "width": float64(400), "chip": "ssd1683", "color_depth": "bwr",
		}, wantErr: "specs.height 必填"},
		{name: "width 非整数", specs: map[string]any{
			"transport": "epd", "width": 400.5, "height": float64(300), "chip": "ssd1683", "color_depth": "bwr",
		}, wantErr: "specs.width 必须是整数"},
		{name: "width 非 8 倍数", specs: map[string]any{
			"transport": "epd", "width": float64(402), "height": float64(300), "chip": "ssd1683", "color_depth": "bwr",
		}, wantErr: "specs.width 必须是 8 的倍数"},
		{name: "width 超出范围", specs: map[string]any{
			"transport": "epd", "width": float64(0), "height": float64(300), "chip": "ssd1683", "color_depth": "bwr",
		}, wantErr: "specs.width 超出范围"},
		{name: "缺少 chip", specs: map[string]any{
			"transport": "epd", "width": float64(400), "height": float64(300), "color_depth": "bwr",
		}, wantErr: "specs.chip 必填"},
		{name: "chip 空白", specs: map[string]any{
			"transport": "epd", "width": float64(400), "height": float64(300), "chip": "  ", "color_depth": "bwr",
		}, wantErr: "specs.chip 必填"},
		{name: "缺少 color_depth", specs: map[string]any{
			"transport": "epd", "width": float64(400), "height": float64(300), "chip": "ssd1683",
		}, wantErr: "specs.color_depth 必填"},
		{name: "非法 color_depth", specs: map[string]any{
			"transport": "epd", "width": float64(400), "height": float64(300), "chip": "ssd1683", "color_depth": "gray16",
		}, wantErr: "invalid specs.color_depth"},
		{name: "缺少 size_inch", specs: map[string]any{
			"transport": "epd", "width": float64(400), "height": float64(300), "chip": "ssd1683", "color_depth": "bwr",
		}, wantErr: "specs.size_inch 必填"},
		{name: "size_inch 非数值", specs: map[string]any{
			"transport": "epd", "size_inch": "4.2", "width": float64(400), "height": float64(300),
			"chip": "ssd1683", "color_depth": "bwr",
		}, wantErr: "specs.size_inch 必填"},
		{name: "size_inch 超出范围", specs: map[string]any{
			"transport": "epd", "size_inch": float64(0), "width": float64(400), "height": float64(300),
			"chip": "ssd1683", "color_depth": "bwr",
		}, wantErr: "specs.size_inch 必填"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateActuatorSpecs(tc.specs)
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestActuatorCreateRequest_ValidateEPD(t *testing.T) {
	req := ActuatorCreateRequest{
		ID:     "epd",
		Name:   "墨水屏",
		Driver: "led",
		Specs:  validEPDSpecs(),
	}
	assert.NoError(t, req.Validate())

	req.Specs = map[string]any{
		"transport": "epd", "height": float64(300),
		"chip": "ssd1683", "color_depth": "bwr",
	}
	assert.ErrorContains(t, req.Validate(), "specs.width 必填")
}

func TestActuatorUpdateRequest_ValidateEPD(t *testing.T) {
	raw := func(v any) *json.RawMessage {
		b, err := json.Marshal(v)
		assert.NoError(t, err)
		msg := json.RawMessage(b)
		return &msg
	}

	ok := ActuatorUpdateRequest{Specs: raw(validEPDSpecs())}
	assert.NoError(t, ok.Validate())

	bad := ActuatorUpdateRequest{Specs: raw(map[string]any{
		"transport": "epd", "width": float64(400), "height": float64(300),
		"chip": "ssd1683", "color_depth": "cmyk",
	})}
	assert.ErrorContains(t, bad.Validate(), "invalid specs.color_depth")

	// 只改 name 不触碰 specs：旧版 epd 定义仍可更新
	name := "新名称"
	onlyName := ActuatorUpdateRequest{Name: &name}
	assert.NoError(t, onlyName.Validate())
}
