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
		if err := ctl.checkOwnership(c, deviceID, middleware.GetUserID(c)); err != nil {
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

	if err := ctl.checkOwnership(c, deviceID, ownerID); err != nil {
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

// checkOwnership 校验设备归属，失败时写响应并返回非 nil 错误
func (ctl *DeviceConfigController) checkOwnership(c *gin.Context, deviceID string, ownerID uint) error {
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
