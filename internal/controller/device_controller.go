package controller

import (
	"context"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"iot-platform.local/internal/middleware"
	entity "iot-platform.local/internal/model"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/common"
	"iot-platform.local/pkg/util"
)

// DeviceController 设备控制器
type DeviceController struct {
	deviceSvc       *service.DeviceService
	deviceReportSvc *service.DeviceReportService
	sensorSvc       *service.DeviceSensorService
	actuatorSvc     *service.DeviceActuatorService
}

// NewDeviceController 创建设备控制器
func NewDeviceController(deviceSvc *service.DeviceService, deviceReportSvc *service.DeviceReportService,
	sensorSvc *service.DeviceSensorService, actuatorSvc *service.DeviceActuatorService) *DeviceController {
	return &DeviceController{
		deviceSvc:       deviceSvc,
		deviceReportSvc: deviceReportSvc,
		sensorSvc:       sensorSvc,
		actuatorSvc:     actuatorSvc,
	}
}

// Register 注册设备 (POST /api/devices)
// @Summary      注册设备
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        body      body  service.DeviceParameters  true  "设备注册请求参数"
// @Success      200   {object}  common.ApiResponse{data=service.DeviceRegisterResponse}
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices [post]
func (ctl *DeviceController) Register(c *gin.Context) {
	var req service.DeviceParameters
	if err := c.ShouldBindJSON(&req); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	ownerID := middleware.GetUserID(c)

	resp, err := ctl.deviceSvc.Register(c.Request.Context(), ownerID, req)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	// Set-Cookie（httpOnly + 生产环境 secure + SameSite=Lax）
	common.SetAuthCookie(c, "X-Device-Token", resp.DeviceToken, 100*365*24*3600)

	common.Success(c, resp)
}

// List 查询用户设备列表 (GET /api/devices)
// @Summary      查询用户设备列表
// @Tags         devices
// @Accept       json
// @Produce      json
// @Success      200   {object}  common.ApiResponse{data=[]ent.Device}
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices [get]
func (ctl *DeviceController) List(c *gin.Context) {
	ownerID := middleware.GetUserID(c)

	devices, err := ctl.deviceSvc.ListByOwnerID(c.Request.Context(), ownerID)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.Success(c, devices)
}

// GetDeviceData 获取设备详情（物模型视图）。
//
// 响应 data.sensors 为传感器物模型数组：每项 = 定义字段 + latest（最近一次上报值，null = 从未上报）；
// data.actuators 为执行器物模型数组；设备无定义时两者均为 []。服务端已完成定义 ↔ 最近遥测的 join
// （上报 name 优先匹配定义 id，其次匹配定义 name），一次请求即可渲染完整设备页。
// @Summary      获取设备详情（物模型视图：定义 + 最近遥测 + 执行器）
// @Description  设备元信息 + 在线状态 + sensors（定义+latest）/ actuators（定义）物模型数组
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID（6位hex）"
// @Success      200       {object}  common.ApiResponse
// @Failure      403       {object}  common.ApiResponse
// @Failure      404       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId} [get]
func (ctl *DeviceController) GetDeviceData(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	ownerID := middleware.GetUserID(c)

	device, err := ctl.deviceSvc.GetByDeviceID(c.Request.Context(), deviceID)
	if err != nil {
		common.FailWithMsg(c, common.CodeNotFound, "设备不存在")
		return
	}

	// 检查设备归属
	if device.OwnerID != ownerID {
		common.FailWithMsg(c, common.CodeForbidden, "无权查看该设备")
		return
	}

	// 获取设备状态（含最近一次遥测快照，Redis 实时优先）
	status, _ := ctl.deviceReportSvc.GetDeviceStatus(c.Request.Context(), deviceID)

	// 合并返回
	result := util.MergeDeviceWithStatus(device.ID, device.DeviceName, device.OwnerID, device.Status, device.LastActiveTime)
	result["id"] = device.ID
	result["deviceType"] = device.DeviceType
	result["firmwareVersion"] = device.FirmwareVersion
	result["ipAddress"] = device.IPAddress
	result["macAddress"] = device.MACAddress
	result["location"] = device.Location
	result["createdAt"] = device.CreatedAt
	result["updatedAt"] = device.UpdatedAt

	var recent []entity.SensorData
	if status != nil {
		recent = status.Sensors
		if status.Status != "" {
			result["status"] = status.Status
		}
	}

	sensors, actuators := ctl.buildThingModel(c.Request.Context(), deviceID, recent)
	result["sensors"] = sensors
	result["actuators"] = actuators

	common.Success(c, result)
}

// buildThingModel 组装设备物模型（传感器定义 + 最近遥测 join、执行器定义）。
// 物模型读取失败仅降级为空列表并告警（详情主链路不因物模型异常而失败），
// 定义列表本身走缓存（见 DeviceSensorService.List），详情页一次请求即可渲染。
func (ctl *DeviceController) buildThingModel(ctx context.Context, deviceID string, recent []entity.SensorData) ([]entity.SensorWithLatest, []entity.Actuator) {
	sensors := []entity.SensorWithLatest{}
	actuators := []entity.Actuator{}

	defs, err := ctl.sensorSvc.List(ctx, deviceID)
	if err != nil {
		zap.L().Warn("[Device] 读取传感器定义失败，详情物模型降级",
			zap.String("deviceID", deviceID), zap.Error(err))
	} else {
		sensors = entity.AttachLatest(defs, recent)
	}

	actuatorDefs, err := ctl.actuatorSvc.List(ctx, deviceID)
	if err != nil {
		zap.L().Warn("[Device] 读取执行器定义失败，详情物模型降级",
			zap.String("deviceID", deviceID), zap.Error(err))
	} else {
		actuators = actuatorDefs
	}

	return sensors, actuators
}

// UpdateDevice 更新设备信息（增量） (POST /api/devices/{deviceId}/update)
// @Summary      更新设备信息（增量）
// @Description  仅设备所有者或设备自身可更新；增量更新，仅请求体中出现的字段会被更新，未传字段保持原值
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        deviceId  path      string                            true  "设备ID（6位hex）"
// @Param        body      body  service.DeviceUpdateParameters  true  "待更新的设备字段（至少一个）"
// @Success      200       {object}  common.ApiResponse{data=ent.Device}
// @Failure      400       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/update [post]
// UpdateDevice 更新设备信息 (POST /api/devices/{deviceId}/update)
func (ctl *DeviceController) UpdateDevice(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))

	var req service.DeviceUpdateParameters
	if err := c.ShouldBindJSON(&req); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	// 双认证：用户 Token 更新自己的设备；设备 Token 只能更新自己
	var ownerID uint
	switch middleware.GetAuthType(c) {
	case "device":
		if middleware.GetDeviceID(c) != deviceID {
			common.FailWithMsg(c, common.CodeForbidden, "无权操作该设备")
			return
		}
		ownerID = 0 // 设备自更新，跳过归属校验
	default: // user
		ownerID = middleware.GetUserID(c)
	}

	resp, err := ctl.deviceSvc.Update(c.Request.Context(), deviceID, ownerID, req)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.Success(c, resp)
}

// GetDeviceToken 获取设备Token (GET /api/devices/{deviceId}/token 或 /api/devices/{deviceId}/login)
// @Summary      获取设备Token
// @Description  使用设备6位hex ID认证（路径参数），无需额外Token
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID（6位hex）"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Router       /api/devices/{deviceId}/token [get]
// @Router       /api/devices/{deviceId}/login [get]
// GetDeviceToken 获取设备Token (GET /api/devices/{deviceId}/token 或 /api/devices/{deviceId}/login)
func (ctl *DeviceController) GetDeviceToken(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	ownerID := middleware.GetUserID(c)

	token, err := ctl.deviceSvc.GetDeviceToken(c.Request.Context(), deviceID, ownerID)
	if err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	// Set-Cookie（httpOnly + 生产环境 secure + SameSite=Lax）
	common.SetAuthCookie(c, "X-Device-Token", token, 100*365*24*3600)

	common.Success(c, gin.H{
		"deviceId":    deviceID,
		"deviceToken": token,
	})
}

// Delete 删除设备 (POST /api/devices/{deviceId}/delete)
// @Summary      删除设备
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/delete [post]
func (ctl *DeviceController) Delete(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	ownerID := middleware.GetUserID(c)

	// 检查设备归属
	device, err := ctl.deviceSvc.GetByDeviceID(c.Request.Context(), deviceID)
	if err != nil {
		common.FailWithMsg(c, common.CodeNotFound, "设备不存在")
		return
	}
	if device.OwnerID != ownerID {
		common.FailWithMsg(c, common.CodeForbidden, "无权删除该设备")
		return
	}

	if err := ctl.deviceSvc.Delete(c.Request.Context(), deviceID); err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.SuccessWithMsg(c, "删除成功", nil)
}
