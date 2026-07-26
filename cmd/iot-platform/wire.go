//go:build wireinject
// +build wireinject

package main

import (
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/repository"
	"iot-platform.local/internal/router"
	"iot-platform.local/internal/service"
	"iot-platform.local/internal/websocket"
	"iot-platform.local/pkg/cache"
	"iot-platform.local/pkg/config"
)

// AppComponents Wire 聚合结构体
type AppComponents struct {
	Services  *router.Services
	WsHandler *websocket.WsHandler
}

// InitializeApp Wire 依赖注入入口
func InitializeApp(entClient *ent.Client, rdb *redis.Client) (*AppComponents, error) {
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

func provideInfluxDBService() *service.InfluxDBService {
	cfg := config.Cfg
	return service.NewInfluxDBService(cfg.InfluxDB.URL, cfg.InfluxDB.Token, cfg.InfluxDB.Org, cfg.InfluxDB.Bucket)
}

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
