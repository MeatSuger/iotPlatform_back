package controller

import (
	"strconv"

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

// ReportData @Summary      上报传感器数据
// @Tags         data
// @Accept       JSON
// @Produce      JSON
// @Param        body      entity.DeviceStatusDTO  true  "传感器数据"
// @Param        deviceId  path      string                  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     DeviceAuth
// @Router       /api/data/{deviceId}/Data [post]
// ReportData 上报传感器数据 (POST /data/:deviceId/Data)
func (ctl *DataController) ReportData(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))

	// 从中间件上下文中获取已验证的设备Token
	rawToken, _ := c.Get("deviceToken")
	tokenStr, _ := rawToken.(string)

	var dto entity.DeviceStatusDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	if len(dto.Sensors) == 0 {
		common.FailWithMsg(c, common.CodeBadRequest, "传感器数据不能为空")
		return
	}

	if err := ctl.deviceReportSvc.ReportStatus(c.Request.Context(), deviceID, tokenStr, dto); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	common.SuccessWithMsg(c, "状态上报已接收", common.DateTimeNow().Format(common.DateTimeFormatWithZone))
}

// Heartbeat @Summary      设备心跳
// @Tags         data
// @Accept       JSON
// @Produce      JSON
// @Param        deviceId  path      string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     DeviceAuth
// @Router       /api/data/{deviceId}/ping [post]
// @Router       /api/data/{deviceId}/heartbeat [post]
// Heartbeat 设备心跳 (POST /data/:deviceId/ping 或 /data/:deviceId/heartbeat)
func (ctl *DataController) Heartbeat(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	rawToken, _ := c.Get("deviceToken")
	tokenStr, _ := rawToken.(string)

	if err := ctl.deviceReportSvc.Heartbeat(c.Request.Context(), deviceID, tokenStr); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	common.Success(c, gin.H{
		"serverTime":   common.DateTimeNow().Format(common.DateTimeFormatWithZone),
		"nextInterval": 60,
	})
}

// QueryData @Summary      查询设备传感器数据
// @Tags         data
// @Accept       JSON
// @Produce      JSON
// @Param        deviceId  path      string  true   "设备ID"
// @Param        limit     query     int     false  "数量限制"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Router       /api/data/{deviceId}/Data/list [get]
// QueryData 查询设备传感器数据 (GET /data/:deviceId/Data/list)
func (ctl *DataController) QueryData(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	limitStr := c.DefaultQuery("limit", "50")
	limit, _ := strconv.Atoi(limitStr)

	records, err := ctl.influxSvc.QueryRecentDeviceSensors(c.Request.Context(), deviceID, limit)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.Success(c, records)
}

// ListData @Summary      通用数据查询入口
// @Tags         data
// @Accept       JSON
// @Produce      JSON
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Router       /api/data/list [get]
// ListData 通用数据查询入口 (GET /data/list)
func (ctl *DataController) ListData(c *gin.Context) {
	// 暂未实现，留作扩展点
	common.Success(c, gin.H{"message": "请在具体设备下查询数据"})
}
