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

// ListSensors @Summary      查询设备传感器定义列表
// @Tags         devices
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      403       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/sensors [get]
// ListSensors 传感器定义列表 (GET /api/devices/{deviceId}/sensors，用户侧)
func (ctl *DeviceSensorController) ListSensors(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	ownerID := middleware.GetUserID(c)

	if err := ctl.checkOwnership(c, deviceID, ownerID); err != nil {
		return
	}

	sensors, err := ctl.sensorSvc.List(c.Request.Context(), deviceID)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}
	common.Success(c, sensors)
}

// GetSensor @Summary      查询单个传感器定义
// @Tags         devices
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Param        sensorId  path  string  true  "传感器标识符"
// @Success      200       {object}  common.ApiResponse
// @Failure      404       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/sensors/{sensorId} [get]
// GetSensor 单个传感器定义 (GET /api/devices/{deviceId}/sensors/{sensorId}，用户侧)
func (ctl *DeviceSensorController) GetSensor(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	sensorID := c.Param("sensorId")
	ownerID := middleware.GetUserID(c)

	if err := ctl.checkOwnership(c, deviceID, ownerID); err != nil {
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

// CreateSensor @Summary      创建传感器定义
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        deviceId  path   string                   true  "设备ID"
// @Param        body      body   entity.SensorCreateRequest true  "传感器定义"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/sensors [post]
// CreateSensor 创建传感器定义 (POST /api/devices/{deviceId}/sensors，用户侧)
func (ctl *DeviceSensorController) CreateSensor(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	ownerID := middleware.GetUserID(c)

	if err := ctl.checkOwnership(c, deviceID, ownerID); err != nil {
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

// UpdateSensor @Summary      增量更新传感器定义
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
// UpdateSensor 增量更新传感器定义 (POST /api/devices/{deviceId}/sensors/{sensorId}/update，用户侧)
func (ctl *DeviceSensorController) UpdateSensor(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	sensorID := c.Param("sensorId")
	ownerID := middleware.GetUserID(c)

	if err := ctl.checkOwnership(c, deviceID, ownerID); err != nil {
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

// DeleteSensor @Summary      删除传感器定义
// @Tags         devices
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Param        sensorId  path  string  true  "传感器标识符"
// @Success      200       {object}  common.ApiResponse
// @Failure      404       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/sensors/{sensorId}/delete [post]
// DeleteSensor 删除传感器定义 (POST /api/devices/{deviceId}/sensors/{sensorId}/delete，用户侧)
func (ctl *DeviceSensorController) DeleteSensor(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	sensorID := c.Param("sensorId")
	ownerID := middleware.GetUserID(c)

	if err := ctl.checkOwnership(c, deviceID, ownerID); err != nil {
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

// ApplySensors @Summary      下发传感器配置（编译进 DeviceConfig 并版本化下发）
// @Tags         devices
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      500       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/sensors/apply [post]
// ApplySensors 下发传感器配置 (POST /api/devices/{deviceId}/sensors/apply，用户侧)
func (ctl *DeviceSensorController) ApplySensors(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	ownerID := middleware.GetUserID(c)

	if err := ctl.checkOwnership(c, deviceID, ownerID); err != nil {
		return
	}

	resp, err := ctl.sensorSvc.Apply(c.Request.Context(), deviceID)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}
	common.SuccessWithMsg(c, "传感器配置已下发", resp)
}

// checkOwnership 校验设备归属，失败时写响应并返回非 nil 错误
func (ctl *DeviceSensorController) checkOwnership(c *gin.Context, deviceID string, ownerID uint) error {
	device, err := ctl.deviceSvc.GetByDeviceID(c.Request.Context(), deviceID)
	if err != nil {
		common.FailWithMsg(c, common.CodeNotFound, "设备不存在")
		return err
	}
	if device.OwnerID != ownerID {
		common.FailWithMsg(c, common.CodeForbidden, "无权操作该设备")
		return errors.New("无权操作该设备")
	}
	return nil
}
