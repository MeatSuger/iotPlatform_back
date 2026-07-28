package controller

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/common"
	"iot-platform.local/pkg/util"
)

// DownlinkController 下放控制器
type DownlinkController struct {
	downlinkSvc *service.DownlinkService
	deviceSvc   *service.DeviceService
}

// NewDownlinkController 创建下放控制器
func NewDownlinkController(downlinkSvc *service.DownlinkService, deviceSvc *service.DeviceService) *DownlinkController {
	return &DownlinkController{
		downlinkSvc: downlinkSvc,
		deviceSvc:   deviceSvc,
	}
}

// PostCmd @Summary      向设备下发命令
// @Tags         device
// @Accept       JSON
// @Produce      JSON
// @Param        deviceId  path      string                     true  "设备ID"
// @Param        body      service.DownlinkCmdRequest  true  "命令内容"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/device/{deviceId}/cmd [post]
// PostCmd 用户向设备下发命令 POST /device/:deviceId/cmd
func (ctl *DownlinkController) PostCmd(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	ownerID := middleware.GetUserID(c)

	// 验证设备归属
	device, err := ctl.deviceSvc.GetByDeviceID(c.Request.Context(), deviceID)
	if err != nil {
		common.FailWithMsg(c, common.CodeNotFound, "设备不存在")
		return
	}
	if device.OwnerID != ownerID {
		common.FailWithMsg(c, common.CodeForbidden, "无权操作该设备")
		return
	}

	var req service.DownlinkCmdRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	cmd, err := ctl.downlinkSvc.EnqueueCmd(c.Request.Context(), deviceID, req)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.SuccessWithMsg(c, "命令已下发", gin.H{
		"id":      cmd.ID,
		"type":    cmd.Type,
		"payload": parsePayload(cmd.Payload),
	})
}

// GetCmd @Summary      设备拉取待消费命令
// @Tags         device
// @Accept       JSON
// @Produce      JSON
// @Param        deviceId  path      string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     DeviceAuth
// @Router       /api/device/{deviceId}/cmd [get]
// GetCmd 设备拉取待消费命令 GET /device/:deviceId/cmd（设备认证）
func (ctl *DownlinkController) GetCmd(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))

	cmds, err := ctl.downlinkSvc.PollCmd(c.Request.Context(), deviceID)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.Success(c, cmds)
}

// parsePayload 将 JSON 字符串解析为 any，保持 JSON 结构输出
func parsePayload(raw string) any {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw // 解析失败则返回原始字符串
	}
	return v
}
