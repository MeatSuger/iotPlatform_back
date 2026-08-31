//go:build wireinject
// +build wireinject

package main

import (
	"time"

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
		repository.NewDownlinkCmdRepo,

		// Service 层
		service.NewUserService,
		service.NewDeviceService,
		provideInfluxDBService,
		service.NewDeviceReportService,
		service.NewDownlinkService,

		// WebSocket（MqttPublisher 传入 nil——网关架构下 WS 不直接发布 MQTT）
		websocket.NewHub,
		websocket.NewWsHandler,

		// Aggregation
		wire.Struct(new(AppComponents), "Services", "WsHandler"),
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

func provideRouterServices(
	userSvc *service.UserService,
	deviceSvc *service.DeviceService,
	reportSvc *service.DeviceReportService,
	influxSvc *service.InfluxDBService,
	downlinkSvc *service.DownlinkService,
) *router.Services {
	return &router.Services{
		User:     userSvc,
		Device:   deviceSvc,
		Report:   reportSvc,
		InfluxDB: influxSvc,
		Downlink: downlinkSvc,
	}
}
