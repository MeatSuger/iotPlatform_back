//go:build wireinject
// +build wireinject

package main

import (
	"time"

	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/mqtt"
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
	Cache     *cache.RedisCache
}

// InitializeApp Wire 依赖注入入口
func InitializeApp(entClient *ent.Client, rdb *redis.Client) (*AppComponents, error) {
	wire.Build(
		// 缓存
		cache.NewRedisCache,

		// Repository 层
		repository.NewUserRepo,
		repository.NewDeviceRepo,
		repository.NewMessageLogRepo,
		repository.NewDeviceConfigRepo,
		repository.NewDeviceThingRepo,

		// Service 层
		service.NewUserService,
		service.NewDeviceService,
		provideInfluxDBService,
		service.NewDeviceReportService,
		service.NewDownlinkService,
		provideMqttPublisher,
		service.NewDeviceConfigService,
		service.NewDeviceSensorService,
		service.NewDeviceActuatorService,

		// WebSocket（网关架构下 WS 不直接发布 MQTT，由 MQTT 桥接网关负责转发）
		websocket.NewHub,
		websocket.NewWsHandler,

		// Aggregation
		wire.Struct(new(AppComponents), "Services", "WsHandler", "Cache"),
		provideRouterServices,
	)
	return nil, nil
}

func provideInfluxDBService() *service.InfluxDBService {
	cfg := config.Cfg
	maxIdleConns := cfg.InfluxDB.MaxIdleConnections
	if maxIdleConns <= 0 {
		maxIdleConns = 50
	}
	return service.NewInfluxDBService(service.InfluxDBConfig{
		URL:                   cfg.InfluxDB.URL,
		Token:                 cfg.InfluxDB.Token,
		Database:              cfg.InfluxDB.Database,
		AuthScheme:            cfg.InfluxDB.AuthScheme,
		WriteTimeout:          10 * time.Second,
		QueryTimeout:          2 * time.Minute,
		IdleConnectionTimeout: 90 * time.Second,
		MaxIdleConnections:    maxIdleConns,
	})
}

// provideMqttPublisher MQTT 下行发布器：网关未启用（Broker 不可达）时返回 nil，
// 各服务将仅走 HTTP/WS/命令队列通道。wire 注入单实例供多个服务共享。
func provideMqttPublisher() service.MqttPublisher {
	if !config.Cfg.MqttGateway.Enabled {
		return nil
	}
	return mqtt.NewPublisher()
}

func provideRouterServices(
	userSvc *service.UserService,
	deviceSvc *service.DeviceService,
	reportSvc *service.DeviceReportService,
	influxSvc *service.InfluxDBService,
	downlinkSvc *service.DownlinkService,
	configSvc *service.DeviceConfigService,
	sensorSvc *service.DeviceSensorService,
	actuatorSvc *service.DeviceActuatorService,
) *router.Services {
	return &router.Services{
		User:      userSvc,
		Device:    deviceSvc,
		Report:    reportSvc,
		InfluxDB:  influxSvc,
		Downlink:  downlinkSvc,
		Config:    configSvc,
		Sensors:   sensorSvc,
		Actuators: actuatorSvc,
	}
}
