// @title           IoT Platform Go API
// @version         1.0.0
// @description     IoT 物联网平台后端 API（Go 重构版）
// @termsOfService  http://swagger.io/terms/

// @contact.name   API Support
// @contact.email support@iot-platform.local.local

// @license.name MIT
// @license.url  https://opensource.org/licenses/MIT

// @host      localhost:8182
// @BasePath  /api

// @securityDefinitions.apikey  UserAuth
// @in                          header
// @name                        Authorization
// @description                 Bearer Token（用户 Sa-Token），登录后获取

// @securityDefinitions.apikey  DeviceAuth
// @in                          header
// @name                        X-Device-Token
// @description                 设备 Token（设备注册后获取）

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/redis/go-redis/v9"
	satoken "github.com/sa-tokens/sa-token-go/core"
	sagin "github.com/sa-tokens/sa-token-go/integrations/gin"
	saredis "github.com/sa-tokens/sa-token-go/storage/redis"
	"github.com/sa-tokens/sa-token-go/stputil"
	"go.uber.org/zap"
	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/router"
	"iot-platform.local/internal/server"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/common"
	"iot-platform.local/pkg/config"

	_ "github.com/lib/pq"
)

func main() {
	// 1. 加载配置
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}
	if err := config.Load(configPath); err != nil {
		_, err := fmt.Fprintf(os.Stderr, "[Main] 配置加载失败: %v\n", err)
		if err != nil {
			return
		}
		os.Exit(1)
	}
	cfg := config.Cfg

	// 2. 初始化结构化日志（Zap）
	common.InitLogger(cfg.Server.Mode)
	defer common.Sync()

	zap.L().Info("[Main] IoT Platform (Go) 正在启动...")

	// 3. 连接PostgreSQL（Ent）
	entClient, err := initEntClient(cfg)
	if err != nil {
		zap.L().Fatal("[Main] 数据库连接失败", zap.Error(err))
	}
	defer func(entClient *ent.Client) {
		err := entClient.Close()
		if err != nil {

		}
	}(entClient)
	zap.L().Info("[Main] PostgreSQL 连接成功（Ent）")

	// 4. 连接Redis
	rdb, err := initRedis(cfg)
	if err != nil {
		zap.L().Fatal("[Main] Redis连接失败", zap.Error(err))
	}
	defer func(rdb *redis.Client) {
		err := rdb.Close()
		if err != nil {

		}
	}(rdb)

	zap.L().Info("[Main] Redis 连接成功")

	// 5. 初始化 Sa-Token 认证框架
	userMgr := satoken.NewBuilder().
		Storage(saredis.NewStorageFromClient(rdb)).
		TokenName("Authorization").
		KeyPrefix("Authorization:").
		Timeout(2592000).
		ActiveTimeout(-1).
		IsConcurrent(true).
		IsLog(true).
		IsReadCookie(true).
		CookieHttpOnly(false).
		TokenStyle(satoken.TokenStyleUUID).
		Build()

	stputil.SetManager(userMgr)
	saginPlugin := sagin.NewPlugin(userMgr)

	deviceMgr := satoken.NewBuilder().
		Storage(saredis.NewStorageFromClient(rdb)).
		TokenName("X-Device-Token").
		KeyPrefix("X-Device-Token:").
		Timeout(-1).
		IsLog(true).
		TokenStyle(satoken.TokenStyleUUID).
		Build()
	middleware.SetDeviceManager(deviceMgr)

	zap.L().Info("[Main] Sa-Token 认证框架初始化完成")

	// 6. Wire 依赖注入
	components, err := InitializeApp(entClient, rdb)
	if err != nil {
		zap.L().Fatal("[Main] 依赖注入初始化失败", zap.Error(err))
	}

	svcs := components.Services
	wsHandler := components.WsHandler

	// 7. 配置 WebSocket
	wsHandler.SetupDeviceWS(deviceMgr, svcs.Report, svcs.Downlink)

	wsHandler.SetDeviceStatusUpdater(func(deviceID string, status string) error {
		return svcs.Device.UpdateStatus(context.Background(), deviceID, status)
	})

	wsHandler.SetOwnerResolver(func(deviceID string) (uint, error) {
		device, err := svcs.Device.GetByDeviceID(context.Background(), deviceID)
		if err != nil {
			return 0, err
		}
		return device.OwnerID, nil
	})

	wsHandler.SetUserCommandHandler(func(ownerID uint, deviceID string, cmdType string, payload json.RawMessage) (uint, error) {
		device, err := svcs.Device.GetByDeviceID(context.Background(), deviceID)
		if err != nil {
			return 0, fmt.Errorf("设备不存在")
		}
		if device.OwnerID != ownerID {
			return 0, fmt.Errorf("无权操作该设备")
		}
		cmd, err := svcs.Downlink.EnqueueCmd(context.Background(), deviceID, service.DownlinkCmdRequest{
			Type:    cmdType,
			Payload: payload,
		})
		if err != nil {
			return 0, err
		}
		return cmd.ID, nil
	})

	// 7.5 初始化 Redis 设备数据缓冲器（支撑 1000+ 并发上报）
	dataBuffer := service.NewDeviceDataBuffer(rdb, svcs.Report)
	dataBuffer.Start()
	defer dataBuffer.Stop()
	svcs.Report.SetBuffer(dataBuffer)
	zap.L().Info("[Main] Redis 设备数据缓冲器已启用")

	influxSvc := svcs.InfluxDB
	mqttClientSvc := svcs.MQTT

	defer influxSvc.Close()

	// 8. Ent 自动迁移
	if err := entClient.Schema.Create(context.Background()); err != nil {
		zap.L().Fatal("[Main] 数据库迁移失败", zap.Error(err))
	}
	zap.L().Info("[Main] 数据库迁移完成（Ent）")

	// 9. 检查InfluxDB连通性
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := influxSvc.Ping(ctx); err != nil {
		zap.L().Warn("[Main] InfluxDB 未连接", zap.Error(err))
	} else {
		zap.L().Info("[Main] InfluxDB 连接正常")
	}
	cancel()

	// 10. MQTT
	if cfg.MQTT.Enabled && cfg.MQTT.BrokerURL != "" {
		go func() {
			time.Sleep(2 * time.Second)
			if err := mqttClientSvc.Connect(); err != nil {
				zap.L().Warn("[Main] MQTT自动连接失败", zap.Error(err))
			}
		}()
	} else {
		zap.L().Info("[Main] MQTT 已禁用")
	}

	// 11. 设置路由
	r := router.Setup(svcs, wsHandler, saginPlugin)

	// 12. UDP
	var udpSrv *server.UDPServer
	if cfg.Server.UDPPort > 0 {
		udpSrv = server.NewUDPServer(svcs.Report)
		if err := udpSrv.Start(cfg.Server.UDPPort); err != nil {
			zap.L().Warn("[Main] UDP启动失败", zap.Error(err))
		}
	}

	// 13. HTTP 服务器
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		zap.L().Info("[Main] 服务器启动", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			zap.L().Fatal("[Main] 服务器启动失败", zap.Error(err))
		}
	}()

	// 14. 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	zap.L().Info("[Main] 正在关闭服务器...")

	if cfg.MQTT.Enabled {
		mqttClientSvc.Disconnect()
	}
	if udpSrv != nil {
		udpSrv.Stop()
	}

	ctxShutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := srv.Shutdown(ctxShutdown); err != nil {
		zap.L().Fatal("[Main] 服务器关闭失败", zap.Error(err))
	}

	zap.L().Info("[Main] 服务器已关闭")
}

func initEntClient(cfg *config.Config) (*ent.Client, error) {
	dsn := cfg.Database.DSN()
	drv, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库: %w", err)
	}

	db := drv.DB()
	if cfg.Database.MaxOpen > 0 {
		db.SetMaxOpenConns(cfg.Database.MaxOpen)
	} else {
		db.SetMaxOpenConns(20)
	}
	if cfg.Database.MaxIdle > 0 {
		db.SetMaxIdleConns(cfg.Database.MaxIdle)
	} else {
		db.SetMaxIdleConns(5)
	}
	db.SetConnMaxLifetime(time.Hour)

	return ent.NewClient(ent.Driver(drv)), nil
}

func initRedis(cfg *config.Config) (*redis.Client, error) {
	poolSize, minIdle, dialTimeout, readTimeout, writeTimeout, poolTimeout, maxRetries := cfg.Redis.PoolConfig()

	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.Addr(),
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     poolSize,
		MinIdleConns: minIdle,
		DialTimeout:  time.Duration(dialTimeout) * time.Second,
		ReadTimeout:  time.Duration(readTimeout) * time.Second,
		WriteTimeout: time.Duration(writeTimeout) * time.Second,
		PoolTimeout:  time.Duration(poolTimeout) * time.Second,
		MaxRetries:   maxRetries,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis连接失败: %w", err)
	}

	return rdb, nil
}
