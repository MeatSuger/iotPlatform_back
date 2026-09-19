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

	"github.com/gin-gonic/gin"

	"iot-platform.local/internal/middleware"
	entity "iot-platform.local/internal/model"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/common"
	"iot-platform.local/pkg/util"
)

// DeviceSensorController 设备传感器定义（物模型）控制器
type DeviceSensorController struct {
	sensorSvc *service.DeviceSensorService
	deviceSvc *service.DeviceService
}

// NewDeviceSensorController 创建传感器定义控制器
func NewDeviceSensorController(sensorSvc *service.DeviceSensorService, deviceSvc *service.DeviceService) *DeviceSensorController {
	return &DeviceSensorController{sensorSvc: sensorSvc, deviceSvc: deviceSvc}
}

// ListSensors 传感器定义列表 (GET /api/devices/{deviceId}/sensors，用户侧)
// @Summary      查询设备传感器定义列表
// @Tags         devices
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      403       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/sensors [get]
func (ctl *DeviceSensorController) ListSensors(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	ownerID := middleware.GetUserID(c)

	if _, err := checkOwnership(c, ctl.deviceSvc, deviceID, ownerID, ""); err != nil {
		return
	}

	sensors, err := ctl.sensorSvc.List(c.Request.Context(), deviceID)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}
	common.Success(c, sensors)
}

// GetSensor 单个传感器定义 (GET /api/devices/{deviceId}/sensors/{sensorId}，用户侧)
// @Summary      查询单个传感器定义
// @Tags         devices
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Param        sensorId  path  string  true  "传感器标识符"
// @Success      200       {object}  common.ApiResponse
// @Failure      404       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/sensors/{sensorId} [get]
func (ctl *DeviceSensorController) GetSensor(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	sensorID := c.Param("sensorId")
	ownerID := middleware.GetUserID(c)

	if _, err := checkOwnership(c, ctl.deviceSvc, deviceID, ownerID, ""); err != nil {
		return
	}

	sensor, err := ctl.sensorSvc.Get(c.Request.Context(), deviceID, sensorID)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}
	if sensor == nil {
		common.FailWithMsg(c, common.CodeNotFound, "传感器不存在")
		return
	}
	common.Success(c, sensor)
}

// CreateSensor 创建传感器定义 (POST /api/devices/{deviceId}/sensors，用户侧)
// @Summary      创建传感器定义
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        deviceId  path   string                   true  "设备ID"
// @Param        body      body   entity.SensorCreateRequest true  "传感器定义"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/sensors [post]
func (ctl *DeviceSensorController) CreateSensor(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	ownerID := middleware.GetUserID(c)

	if _, err := checkOwnership(c, ctl.deviceSvc, deviceID, ownerID, ""); err != nil {
		return
	}

	var req entity.SensorCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	sensor, err := ctl.sensorSvc.Create(c.Request.Context(), deviceID, req)
	if err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}
	common.SuccessWithMsg(c, "传感器已创建", sensor)
}

// UpdateSensor 增量更新传感器定义 (POST /api/devices/{deviceId}/sensors/{sensorId}/update，用户侧)
// @Summary      增量更新传感器定义
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        deviceId  path   string                    true  "设备ID"
// @Param        sensorId  path   string                    true  "传感器标识符"
// @Param        body      body   entity.SensorUpdateRequest true  "更新字段（增量）"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/sensors/{sensorId}/update [post]
func (ctl *DeviceSensorController) UpdateSensor(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	sensorID := c.Param("sensorId")
	ownerID := middleware.GetUserID(c)

	if _, err := checkOwnership(c, ctl.deviceSvc, deviceID, ownerID, ""); err != nil {
		return
	}

	var req entity.SensorUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	sensor, err := ctl.sensorSvc.Update(c.Request.Context(), deviceID, sensorID, req)
	if err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}
	if sensor == nil {
		common.FailWithMsg(c, common.CodeNotFound, "传感器不存在")
		return
	}
	common.Success(c, sensor)
}

// DeleteSensor 删除传感器定义 (POST /api/devices/{deviceId}/sensors/{sensorId}/delete，用户侧)
// @Summary      删除传感器定义
// @Tags         devices
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Param        sensorId  path  string  true  "传感器标识符"
// @Success      200       {object}  common.ApiResponse
// @Failure      404       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/sensors/{sensorId}/delete [post]
func (ctl *DeviceSensorController) DeleteSensor(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	sensorID := c.Param("sensorId")
	ownerID := middleware.GetUserID(c)

	if _, err := checkOwnership(c, ctl.deviceSvc, deviceID, ownerID, ""); err != nil {
		return
	}

	if err := ctl.sensorSvc.Delete(c.Request.Context(), deviceID, sensorID); err != nil {
		if errors.Is(err, service.ErrSensorNotFound) {
			common.FailWithMsg(c, common.CodeNotFound, err.Error())
			return
		}
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}
	common.SuccessWithMsg(c, "删除成功", nil)
}

// ApplySensors 下发传感器配置 (POST /api/devices/{deviceId}/sensors/apply，用户侧)
// @Summary      下发传感器配置（编译进 DeviceConfig 并版本化下发）
// @Tags         devices
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      500       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/sensors/apply [post]
func (ctl *DeviceSensorController) ApplySensors(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	ownerID := middleware.GetUserID(c)

	if _, err := checkOwnership(c, ctl.deviceSvc, deviceID, ownerID, ""); err != nil {
		return
	}

	resp, err := ctl.sensorSvc.Apply(c.Request.Context(), deviceID)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}
	common.SuccessWithMsg(c, "传感器配置已下发", resp)
}
