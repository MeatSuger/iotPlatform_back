package controller

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/model"
	mqttEntity "iot-platform.local/internal/model/mqtt"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/common"
	"iot-platform.local/pkg/util"
)

// MqttController MQTT控制器
type MqttController struct {
	mqttClientSvc   *service.MqttClientService
	mqttLogSvc      *service.MqttPublishLogService
	deviceReportSvc *service.DeviceReportService
}

// NewMqttController 创建MQTT控制器
func NewMqttController(
	mqttClientSvc *service.MqttClientService,
	mqttLogSvc *service.MqttPublishLogService,
	deviceReportSvc *service.DeviceReportService,
) *MqttController {
	return &MqttController{
		mqttClientSvc:   mqttClientSvc,
		mqttLogSvc:      mqttLogSvc,
		deviceReportSvc: deviceReportSvc,
	}
}

// @Summary      MQTT方式上报传感器数据
// @Tags         mqtt
// @Accept       json
// @Produce      json
// @Param        body      body      entity.DeviceStatusDTO  true  "传感器数据"
// @Param        deviceId  path      string                  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     DeviceAuth
// @Router       /api/mqtt/{deviceId}/Data [post]
// ReportData MQTT方式上报传感器数据 (POST /mqtt/:deviceId/Data)
func (ctl *MqttController) ReportData(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	// 从中间件上下文中获取已验证的设备Token
	rawToken, _ := c.Get("deviceToken")
	tokenStr, _ := rawToken.(string)

	var dto entity.DeviceStatusDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	if err := ctl.deviceReportSvc.ReportStatus(c.Request.Context(), deviceID, tokenStr, dto); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	common.SuccessWithMsg(c, "状态上报已接收", nil)
}

// @Summary      MQTT方式心跳
// @Tags         mqtt
// @Accept       json
// @Produce      json
// @Param        deviceId  path      string  true  "设备ID"
// @Success      200       {object}  common.ApiResponse
// @Failure      400       {object}  common.ApiResponse
// @Security     DeviceAuth
// @Router       /api/mqtt/{deviceId}/ping [post]
// @Router       /api/mqtt/{deviceId}/heartbeat [post]
// Heartbeat MQTT方式心跳 (POST /mqtt/:deviceId/ping 或 /mqtt/:deviceId/heartbeat)
func (ctl *MqttController) Heartbeat(c *gin.Context) {
	deviceID := util.NormalizeDeviceID(c.Param("deviceId"))
	// 从中间件上下文中获取已验证的设备Token
	rawToken, _ := c.Get("deviceToken")
	tokenStr, _ := rawToken.(string)

	if err := ctl.deviceReportSvc.Heartbeat(c.Request.Context(), deviceID, tokenStr); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	common.Success(c, gin.H{
		"serverTime":   common.DateTimeNow().Format(common.DateTimeFormat),
		"nextInterval": 60,
	})
}

// @Summary      连接MQTT Broker
// @Tags         mqtt
// @Accept       json
// @Produce      json
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/mqtt/client/connect [post]
// Connect 连接MQTT Broker (POST /mqtt/client/connect)
func (ctl *MqttController) Connect(c *gin.Context) {
	if err := ctl.mqttClientSvc.Connect(); err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}
	common.SuccessWithMsg(c, "MQTT连接成功", nil)
}

// @Summary      断开MQTT连接
// @Tags         mqtt
// @Accept       json
// @Produce      json
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/mqtt/client/disconnect [post]
// Disconnect 断开MQTT连接 (POST /mqtt/client/disconnect)
func (ctl *MqttController) Disconnect(c *gin.Context) {
	ctl.mqttClientSvc.Disconnect()
	common.SuccessWithMsg(c, "MQTT已断开", nil)
}

// @Summary      订阅主题
// @Tags         mqtt
// @Accept       json
// @Produce      json
// @Param        body  body      mqttEntity.SubscribeRequest  true  "订阅请求参数"
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/mqtt/client/subscribe [post]
// Subscribe 订阅主题 (POST /mqtt/client/subscribe)
func (ctl *MqttController) Subscribe(c *gin.Context) {
	var req mqttEntity.SubscribeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	if err := ctl.mqttClientSvc.Subscribe(req); err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.SuccessWithMsg(c, "订阅成功", gin.H{"topic": req.Topic})
}

// @Summary      取消订阅
// @Tags         mqtt
// @Accept       json
// @Produce      json
// @Param        body  body      mqttEntity.TopicRequest  true  "取消订阅请求参数"
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/mqtt/client/unsubscribe [post]
// Unsubscribe 取消订阅 (POST /mqtt/client/unsubscribe)
func (ctl *MqttController) Unsubscribe(c *gin.Context) {
	var req mqttEntity.TopicRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	if err := ctl.mqttClientSvc.Unsubscribe(req.Topic); err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.SuccessWithMsg(c, "取消订阅成功", nil)
}

// @Summary      发布消息
// @Tags         mqtt
// @Accept       json
// @Produce      json
// @Param        body  body      mqttEntity.PublishRequest  true  "发布消息请求参数"
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/mqtt/client/publish [post]
// Publish 发布消息 (POST /mqtt/client/publish)
func (ctl *MqttController) Publish(c *gin.Context) {
	var req mqttEntity.PublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	if err := ctl.mqttClientSvc.Publish(req); err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.SuccessWithMsg(c, "发布成功", nil)
}

// @Summary      获取MQTT客户端状态
// @Tags         mqtt
// @Accept       json
// @Produce      json
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/mqtt/client/status [get]
// Status 获取MQTT客户端状态 (GET /mqtt/client/status)
func (ctl *MqttController) Status(c *gin.Context) {
	status := ctl.mqttClientSvc.Status()
	common.Success(c, status)
}

// @Summary      获取最近的MQTT消息
// @Tags         mqtt
// @Accept       json
// @Produce      json
// @Param        limit  query     int     false  "数量限制"
// @Success      200    {object}  common.ApiResponse
// @Failure      400    {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/mqtt/client/messages [get]
// Messages 获取最近的MQTT消息 (GET /mqtt/client/messages)
func (ctl *MqttController) Messages(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "50")
	limit, _ := strconv.Atoi(limitStr)

	messages := ctl.mqttClientSvc.RecentMessages(limit)
	common.Success(c, messages)
}

// GetDeviceID 获取设备ID辅助方法
func GetDeviceID(c *gin.Context) string {
	return util.NormalizeDeviceID(middleware.GetDeviceID(c))
}
