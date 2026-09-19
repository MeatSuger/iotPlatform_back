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
	"encoding/json"

	"github.com/gin-gonic/gin"

	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/common"
	"iot-platform.local/pkg/util"
)

// DownlinkController 下发控制器
type DownlinkController struct {
	downlinkSvc *service.DownlinkService
	deviceSvc   *service.DeviceService
}

// NewDownlinkController 创建下发控制器
func NewDownlinkController(downlinkSvc *service.DownlinkService, deviceSvc *service.DeviceService) *DownlinkController {
	return &DownlinkController{
		downlinkSvc: downlinkSvc,
		deviceSvc:   deviceSvc,
	}
}

// PostCmd 用户向设备下发命令 POST /api/devices/{deviceId}/commands
// @Summary      向设备下发命令
// @Tags         device
// @Accept       json
// @Produce      json
// @Param        deviceId  path      string                     true  "设备ID"
// @Param        body      body  service.DownlinkCmdRequest  true  "命令内容"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/devices/{deviceId}/commands [post]
func (ctl *DownlinkController) PostCmd(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	ownerID := middleware.GetUserID(c)

	// 验证设备归属
	if _, err := checkOwnership(c, ctl.deviceSvc, deviceID, ownerID, ""); err != nil {
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

// GetCmd 设备拉取待消费命令 GET /api/devices/{deviceId}/commands（设备认证）
// @Summary      设备拉取待消费命令
// @Tags         device
// @Accept       json
// @Produce      json
// @Param        deviceId  path      string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     DeviceAuth
// @Router       /api/devices/{deviceId}/commands [get]
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
