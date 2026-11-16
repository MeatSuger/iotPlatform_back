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

package controller

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/common"
)

// checkOwnership 校验设备存在且归属指定 owner，失败时写响应并返回非 nil 错误。
// forbiddenMsg 为空时使用默认文案「无权操作该设备」。
func checkOwnership(c *gin.Context, deviceSvc *service.DeviceService, deviceID string, ownerID uint, forbiddenMsg string) (*ent.Device, error) {
	device, err := deviceSvc.GetByDeviceID(c.Request.Context(), deviceID)
	if err != nil {
		common.FailWithMsg(c, common.CodeNotFound, "设备不存在")
		return nil, err
	}
	if device.OwnerID != ownerID {
		if forbiddenMsg == "" {
			forbiddenMsg = "无权操作该设备"
		}
		common.FailWithMsg(c, common.CodeForbidden, forbiddenMsg)
		return nil, errors.New(forbiddenMsg)
	}
	return device, nil
}

// checkDeviceSelf 校验设备 Token 对应的设备与路径 deviceId 一致。
// 设备令牌只能操作自身资源，防止 A 设备用合法 Token 读写 B 设备的数据/命令。
func checkDeviceSelf(c *gin.Context, deviceID string) bool {
	if strings.ToLower(middleware.GetDeviceID(c)) != deviceID {
		common.FailWithMsg(c, common.CodeForbidden, "设备Token与路径设备不一致")
		return false
	}
	return true
}
