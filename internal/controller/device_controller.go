package controller

import (
	"github.com/gin-gonic/gin"
	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/common"
	"iot-platform.local/pkg/util"
)

// DeviceController 设备控制器
type DeviceController struct {
	deviceSvc       *service.DeviceService
	deviceReportSvc *service.DeviceReportService
}

// NewDeviceController 创建设备控制器
func NewDeviceController(deviceSvc *service.DeviceService, deviceReportSvc *service.DeviceReportService) *DeviceController {
	return &DeviceController{
		deviceSvc:       deviceSvc,
		deviceReportSvc: deviceReportSvc,
	}
}

// Register @Summary      注册设备
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        body      body  service.DeviceParameters  true  "设备注册请求参数"
// @Success      200   {object}  common.ApiResponse{data=service.DeviceRegisterResponse}
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices [post]
// Register 注册设备 (POST /api/devices)
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

// List @Summary      查询用户设备列表
// @Tags         devices
// @Accept       json
// @Produce      json
// @Success      200   {object}  common.ApiResponse{data=[]ent.Device}
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices [get]
// List 查询用户设备列表 (GET /api/devices)
func (ctl *DeviceController) List(c *gin.Context) {
	ownerID := middleware.GetUserID(c)

	devices, err := ctl.deviceSvc.ListByOwnerID(c.Request.Context(), ownerID)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.Success(c, devices)
}

// GetDeviceData @Summary      获取设备详情
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId} [get]
// GetDeviceData 获取设备详情 (GET /api/devices/{deviceId})
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

	// 获取设备状态
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

	if status != nil {
		result["sensors"] = status.Sensors
		if status.Status != "" {
			result["status"] = status.Status
		}
	}

	common.Success(c, result)
}

// UpdateDevice @Summary  更新设备信息（增量）
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

// GetDeviceToken @Summary      获取设备Token
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

// Delete @Summary      删除设备
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        deviceId  path  string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/delete [post]
// Delete 删除设备 (POST /api/devices/{deviceId}/delete)
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
