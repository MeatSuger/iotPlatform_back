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

	"iot-platform.local/internal/ent"
)

// buildConfigWithThingModel 原子合并指定物模型列表（key=sensors/actuators）到设备配置，
// 再由 DeviceConfigService 统一版本化下发。并发 Apply 时以 version 为条件重试，
// 不会出现「后写覆盖先写」丢失另一个 key 的情况。
func buildConfigWithThingModel(ctx context.Context, configSvc *DeviceConfigService, deviceID, key string, items any) (*ent.DeviceConfig, error) {
	return configSvc.ApplySection(ctx, deviceID, key, items)
}
