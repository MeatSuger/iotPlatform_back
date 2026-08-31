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
// @Accept       JSON
// @Produce      JSON
// @Param        body      service.DeviceRegisterRequest  true  "设备注册请求参数"
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/device/register [post]
// Register 注册设备 (POST /device/register)
func (ctl *DeviceController) Register(c *gin.Context) {
	var req service.DeviceRegisterRequest
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
// @Accept       JSON
// @Produce      JSON
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/device/list [get]
// List 查询用户设备列表 (GET /device/list)
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
// @Accept       JSON
// @Produce      JSON
// @Param        deviceId  path  string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/device/{deviceId}/Data [get]
// GetDeviceData 获取设备详情 (GET /device/:deviceId/Data)
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

// GetDeviceToken @Summary      获取设备Token
// @Description  使用设备6位hex ID认证（路径参数），无需额外Token
// @Tags         devices
// @Accept       JSON
// @Produce      JSON
// @Param        deviceId  path  string  true  "设备ID（6位hex）"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Router       /api/device/{deviceId}/login [get]
// GetDeviceToken 获取设备Token (GET /device/:deviceId/login)
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
// @Router       /api/device/{deviceId}/delete [post]
// Delete 删除设备 (POST /device/:deviceId/delete)
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
