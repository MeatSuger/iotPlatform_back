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

// DeviceActuatorController 设备执行器定义（物模型）控制器
type DeviceActuatorController struct {
	actuatorSvc *service.DeviceActuatorService
	deviceSvc   *service.DeviceService
}

// NewDeviceActuatorController 创建执行器定义控制器
func NewDeviceActuatorController(actuatorSvc *service.DeviceActuatorService, deviceSvc *service.DeviceService) *DeviceActuatorController {
	return &DeviceActuatorController{actuatorSvc: actuatorSvc, deviceSvc: deviceSvc}
}

// ListActuators 执行器定义列表 (GET /api/devices/{deviceId}/actuators，用户侧)
// @Summary      查询设备执行器定义列表
// @Tags         devices
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      403       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/actuators [get]
func (ctl *DeviceActuatorController) ListActuators(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	ownerID := middleware.GetUserID(c)

	if _, err := checkOwnership(c, ctl.deviceSvc, deviceID, ownerID, ""); err != nil {
		return
	}

	actuators, err := ctl.actuatorSvc.List(c.Request.Context(), deviceID)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}
	common.Success(c, actuators)
}

// GetActuator 单个执行器定义 (GET /api/devices/{deviceId}/actuators/{actuatorId}，用户侧)
// @Summary      查询单个执行器定义
// @Tags         devices
// @Produce      json
// @Param        deviceId    path  string  true  "设备ID"
// @Param        actuatorId  path  string  true  "执行器标识符"
// @Success      200         {object}  common.ApiResponse
// @Failure      404         {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/actuators/{actuatorId} [get]
func (ctl *DeviceActuatorController) GetActuator(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	actuatorID := c.Param("actuatorId")
	ownerID := middleware.GetUserID(c)

	if _, err := checkOwnership(c, ctl.deviceSvc, deviceID, ownerID, ""); err != nil {
		return
	}

	actuator, err := ctl.actuatorSvc.Get(c.Request.Context(), deviceID, actuatorID)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}
	if actuator == nil {
		common.FailWithMsg(c, common.CodeNotFound, "执行器不存在")
		return
	}
	common.Success(c, actuator)
}

// CreateActuator 创建执行器定义 (POST /api/devices/{deviceId}/actuators，用户侧)
// @Summary      创建执行器定义
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        deviceId    path  string                       true  "设备ID"
// @Param        body        body  entity.ActuatorCreateRequest true  "执行器定义"
// @Success      200         {object}  common.ApiResponse
// @Failure      400         {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/actuators [post]
func (ctl *DeviceActuatorController) CreateActuator(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	ownerID := middleware.GetUserID(c)

	if _, err := checkOwnership(c, ctl.deviceSvc, deviceID, ownerID, ""); err != nil {
		return
	}

	var req entity.ActuatorCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	actuator, err := ctl.actuatorSvc.Create(c.Request.Context(), deviceID, req)
	if err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}
	common.SuccessWithMsg(c, "执行器已创建", actuator)
}

// UpdateActuator 增量更新执行器定义 (POST /api/devices/{deviceId}/actuators/{actuatorId}/update，用户侧)
// @Summary      增量更新执行器定义
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        deviceId    path  string                        true  "设备ID"
// @Param        actuatorId  path  string                        true  "执行器标识符"
// @Param        body        body  entity.ActuatorUpdateRequest true  "更新字段（增量）"
// @Success      200         {object}  common.ApiResponse
// @Failure      400         {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/actuators/{actuatorId}/update [post]
func (ctl *DeviceActuatorController) UpdateActuator(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	actuatorID := c.Param("actuatorId")
	ownerID := middleware.GetUserID(c)

	if _, err := checkOwnership(c, ctl.deviceSvc, deviceID, ownerID, ""); err != nil {
		return
	}

	var req entity.ActuatorUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	actuator, err := ctl.actuatorSvc.Update(c.Request.Context(), deviceID, actuatorID, req)
	if err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}
	if actuator == nil {
		common.FailWithMsg(c, common.CodeNotFound, "执行器不存在")
		return
	}
	common.Success(c, actuator)
}

// DeleteActuator 删除执行器定义 (POST /api/devices/{deviceId}/actuators/{actuatorId}/delete，用户侧)
// @Summary      删除执行器定义
// @Tags         devices
// @Produce      json
// @Param        deviceId    path  string  true  "设备ID"
// @Param        actuatorId  path  string  true  "执行器标识符"
// @Success      200         {object}  common.ApiResponse
// @Failure      404         {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/actuators/{actuatorId}/delete [post]
func (ctl *DeviceActuatorController) DeleteActuator(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	actuatorID := c.Param("actuatorId")
	ownerID := middleware.GetUserID(c)

	if _, err := checkOwnership(c, ctl.deviceSvc, deviceID, ownerID, ""); err != nil {
		return
	}

	if err := ctl.actuatorSvc.Delete(c.Request.Context(), deviceID, actuatorID); err != nil {
		if errors.Is(err, service.ErrActuatorNotFound) {
			common.FailWithMsg(c, common.CodeNotFound, err.Error())
			return
		}
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}
	common.SuccessWithMsg(c, "删除成功", nil)
}

// ApplyActuators 下发执行器配置 (POST /api/devices/{deviceId}/actuators/apply，用户侧)
// @Summary      下发执行器配置（编译进 DeviceConfig 并版本化下发）
// @Tags         devices
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      500       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/actuators/apply [post]
func (ctl *DeviceActuatorController) ApplyActuators(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	ownerID := middleware.GetUserID(c)

	if _, err := checkOwnership(c, ctl.deviceSvc, deviceID, ownerID, ""); err != nil {
		return
	}

	resp, err := ctl.actuatorSvc.Apply(c.Request.Context(), deviceID)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}
	common.SuccessWithMsg(c, "执行器配置已下发", resp)
}
