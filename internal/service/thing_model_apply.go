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

package service

import (
	"context"
	"encoding/json"
	"fmt"

	"iot-platform.local/internal/ent"
)

// buildConfigWithThingModel 读取设备现有配置 payload，写入指定物模型列表（key=sensors/actuators）
// 后版本化保存，供传感器/执行器 Apply 复用。
func buildConfigWithThingModel(ctx context.Context, configSvc *DeviceConfigService, deviceID, key string, items any) (*ent.DeviceConfig, error) {
	payload := map[string]any{}
	if existing, err := configSvc.Get(ctx, deviceID); err != nil {
		return nil, err
	} else if existing != nil && existing.Payload != "" {
		if err := json.Unmarshal([]byte(existing.Payload), &payload); err != nil {
			return nil, fmt.Errorf("解析现有配置失败: %w", err)
		}
	}
	payload[key] = items
	return configSvc.Save(ctx, deviceID, payload)
}
