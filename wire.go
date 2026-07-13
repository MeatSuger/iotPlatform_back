//go:build wireinject
// +build wireinject

package main

import (
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"github.com/yu/iot-platform-go/cache"
	"github.com/yu/iot-platform-go/config"
	"github.com/yu/iot-platform-go/repository"
	"github.com/yu/iot-platform-go/router"
	"github.com/yu/iot-platform-go/service"
	"github.com/yu/iot-platform-go/websocket"
	"gorm.io/gorm"
)

// AppComponents Wire 聚合结构体（用于多返回值注入）
type AppComponents struct {
	Services  *router.Services
	WsHandler *websocket.WsHandler
}

// InitializeApp Wire 依赖注入入口 — 自动解析依赖关系，生成 wire_gen.go
func InitializeApp(db *gorm.DB, rdb *redis.Client) (*AppComponents, error) {
	wire.Build(
		// 缓存
		cache.NewRedisCache,

		// Repository 层
		repository.NewUserRepo,
		repository.NewDeviceRepo,
		repository.NewMqttPublishLogRepo,
		repository.NewDownlinkCmdRepo,

		// Service 层
		service.NewUserService,
		service.NewDeviceService,
		provideInfluxDBService,
		service.NewDeviceReportService,
		service.NewMqttPublishLogService,
		service.NewMqttClientService,
		service.NewDownlinkService,

		// WebSocket
		websocket.NewHub,
		websocket.NewWsHandler,

		// Interface binding
		wire.Bind(new(websocket.MqttPublisher), new(*service.MqttClientService)),

		// Aggregation
		wire.Struct(new(AppComponents), "Services", "WsHandler"),
		provideRouterServices,
	)
	return nil, nil
}

// provideInfluxDBService 提供 InfluxDB 服务（4 个 string 参数需手动注入）
func provideInfluxDBService() *service.InfluxDBService {
	cfg := config.Cfg
	return service.NewInfluxDBService(cfg.InfluxDB.URL, cfg.InfluxDB.Token, cfg.InfluxDB.Org, cfg.InfluxDB.Bucket)
}

// provideRouterServices 聚合所有 Service 到 router.Services
func provideRouterServices(
	userSvc *service.UserService,
	deviceSvc *service.DeviceService,
	reportSvc *service.DeviceReportService,
	influxSvc *service.InfluxDBService,
	mqttSvc *service.MqttClientService,
	mqttLogSvc *service.MqttPublishLogService,
	downlinkSvc *service.DownlinkService,
) *router.Services {
	return &router.Services{
		User:     userSvc,
		Device:   deviceSvc,
		Report:   reportSvc,
		InfluxDB: influxSvc,
		MQTT:     mqttSvc,
		MQTTLog:  mqttLogSvc,
		Downlink: downlinkSvc,
	}
}
