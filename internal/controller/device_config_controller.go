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
	"github.com/gin-gonic/gin"

	"iot-platform.local/internal/middleware"
	entity "iot-platform.local/internal/model"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/common"
	"iot-platform.local/pkg/util"
)

// DeviceConfigController 设备配置控制器
type DeviceConfigController struct {
	configSvc *service.DeviceConfigService
	deviceSvc *service.DeviceService
}

// NewDeviceConfigController 创建设备配置控制器
func NewDeviceConfigController(configSvc *service.DeviceConfigService, deviceSvc *service.DeviceService) *DeviceConfigController {
	return &DeviceConfigController{
		configSvc: configSvc,
		deviceSvc: deviceSvc,
	}
}

// GetConfig 查询设备配置快照 (GET /api/devices/{deviceId}/config)
//
// 双认证：设备属主（UserAuth）或设备本人（DeviceAuth 且 Token 与路径设备一致）。
// 设备端主动拉取期望配置的兜底通道（配合下行命令通道使用）。
// @Summary      查询设备配置快照
// @Tags         devices
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     UserAuth
// @Security     DeviceAuth
// @Router       /api/devices/{deviceId}/config [get]
func (ctl *DeviceConfigController) GetConfig(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))

	switch middleware.GetAuthType(c) {
	case "device":
		if middleware.GetDeviceID(c) != deviceID {
			common.FailWithMsg(c, common.CodeForbidden, "设备Token与路径设备不一致")
			return
		}
	default:
		if _, err := checkOwnership(c, ctl.deviceSvc, deviceID, middleware.GetUserID(c), ""); err != nil {
			return
		}
	}

	cfg, err := ctl.configSvc.Get(c.Request.Context(), deviceID)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}
	if cfg == nil {
		common.Success(c, gin.H{
			"deviceId":        deviceID,
			"version":         0,
			"payload":         nil,
			"status":          "",
			"reportedVersion": 0,
			"reportedPayload": nil,
		})
		return
	}

	common.Success(c, gin.H{
		"deviceId":        cfg.DeviceID,
		"version":         cfg.Version,
		"payload":         parsePayload(cfg.Payload),
		"status":          cfg.Status,
		"reportedVersion": cfg.ReportedVersion,
		"reportedPayload": parsePayload(cfg.ReportedPayload),
		"updatedAt":       cfg.UpdatedAt,
	})
}

// SaveConfig 设置设备配置并下发 (POST /api/devices/{deviceId}/config，用户侧)
// @Summary      设置设备配置并下发
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Param        body      body  entity.DeviceConfigSaveRequest  true  "配置内容"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/config [post]
func (ctl *DeviceConfigController) SaveConfig(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	ownerID := middleware.GetUserID(c)

	if _, err := checkOwnership(c, ctl.deviceSvc, deviceID, ownerID, ""); err != nil {
		return
	}

	var req entity.DeviceConfigSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	cfg, err := ctl.configSvc.Save(c.Request.Context(), deviceID, req.Config)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.SuccessWithMsg(c, "配置已保存并下发", gin.H{
		"deviceId": cfg.DeviceID,
		"version":  cfg.Version,
		"status":   cfg.Status,
	})
}

// ReportConfig 设备上报实际生效配置 (POST /api/devices/{deviceId}/config/report，设备侧)
// @Summary      设备配置回执
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Param        body      body  entity.DeviceConfigReport  true  "实际生效配置与版本"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     DeviceAuth
// @Router       /api/devices/{deviceId}/config/report [post]
func (ctl *DeviceConfigController) ReportConfig(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))

	var req entity.DeviceConfigReport
	if err := c.ShouldBindJSON(&req); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	if err := ctl.configSvc.Report(c.Request.Context(), deviceID, req); err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.SuccessWithMsg(c, "配置回执已记录", nil)
}
