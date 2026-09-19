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
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	entity "iot-platform.local/internal/model"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/common"
	"iot-platform.local/pkg/util"
)

// DataController 数据控制器
type DataController struct {
	deviceReportSvc *service.DeviceReportService
	influxSvc       *service.InfluxDBService
}

// NewDataController 创建数据控制器
func NewDataController(deviceReportSvc *service.DeviceReportService, influxSvc *service.InfluxDBService) *DataController {
	return &DataController{
		deviceReportSvc: deviceReportSvc,
		influxSvc:       influxSvc,
	}
}

// ReportData 上报传感器数据 (POST /devices/:deviceId/sensorData)
// @Summary      上报传感器数据
// @Tags         data
// @Accept       json
// @Produce      json
// @Param        body      body  entity.DeviceStatusDTO  true  "传感器数据"
// @Param        deviceId  path      string                  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     DeviceAuth
// @Router       /api/devices/{deviceId}/sensorData [post]
func (ctl *DataController) ReportData(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))

	var dto entity.DeviceStatusDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	if len(dto.Sensors) == 0 {
		common.FailWithMsg(c, common.CodeBadRequest, "传感器数据不能为空")
		return
	}

	// 快速路径：中间件已校验 Token，跳过二次校验（避免多余 Redis 往返）
	if err := ctl.deviceReportSvc.ReportStatusFast(c.Request.Context(), deviceID, dto); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	common.SuccessWithMsg(c, "状态上报已接收", common.DateTimeNow().Format(common.DateTimeFormatWithZone))
}

// Heartbeat 设备心跳 (POST /devices/:deviceId/heartbeat 或 /devices/:deviceId/ping)
// @Summary      设备心跳
// @Tags         data
// @Accept       json
// @Produce      json
// @Param        deviceId  path      string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     DeviceAuth
// @Router       /api/devices/{deviceId}/heartbeat [post]
// @Router       /api/devices/{deviceId}/ping [post]
func (ctl *DataController) Heartbeat(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))

	// 快速路径：跳过 Token 二次校验（中间件已校验）
	if err := ctl.deviceReportSvc.HeartbeatFast(c.Request.Context(), deviceID); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	common.Success(c, gin.H{
		"serverTime":   common.DateTimeNow().Format(common.DateTimeFormatWithZone),
		"nextInterval": 60,
	})
}

// QueryData 查询设备传感器数据 (GET /devices/:deviceId/sensorData)
// @Summary      查询设备传感器数据
// @Tags         data
// @Accept       json
// @Produce      json
// @Param        deviceId  path      string  false  "设备ID"
// @Param        limit     query     int     false  "数量限制，默认50"
// @Param        start     query     string  false  "起始时间（RFC3339），默认3天前"
// @Param        end       query     string  false  "结束时间（RFC3339），默认当前时间"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Router       /api/devices/{deviceId}/sensorData [get]
func (ctl *DataController) QueryData(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	limitStr := c.DefaultQuery("limit", "50")
	limit, _ := strconv.Atoi(limitStr)

	// 解析时间范围（默认最近 3 天）
	start, end := parseTimeRange(c)

	records, err := ctl.influxSvc.QueryRecentDeviceSensors(c.Request.Context(), deviceID, limit, start, end)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.Success(c, records)
}

// parseTimeRange 解析 URL 查询参数 start/end
// 未传参数时返回零值（零值 → 服务端使用默认最近 3 天，可命中缓存）
func parseTimeRange(c *gin.Context) (start, end time.Time) {
	if s := c.Query("start"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			start = t
		}
	}
	if e := c.Query("end"); e != "" {
		if t, err := time.Parse(time.RFC3339, e); err == nil {
			end = t
		}
	}
	return
}
