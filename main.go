// @title           IoT Platform Go API
// @version         1.0.0
// @description     IoT 物联网平台后端 API（Go 重构版）
// @termsOfService  http://swagger.io/terms/

// @contact.name   API Support
// @contact.email support@iot-platform.local

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
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	satoken "github.com/sa-tokens/sa-token-go/core"
	sagin "github.com/sa-tokens/sa-token-go/integrations/gin"
	saredis "github.com/sa-tokens/sa-token-go/storage/redis"
	"github.com/sa-tokens/sa-token-go/stputil"
	"github.com/yu/iot-platform-go/common"
	"github.com/yu/iot-platform-go/config"
	"github.com/yu/iot-platform-go/entity"
	"github.com/yu/iot-platform-go/middleware"
	"github.com/yu/iot-platform-go/router"
	"github.com/yu/iot-platform-go/server"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	// 1. 加载配置
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config/config.yaml"
	}
	if err := config.Load(configPath); err != nil {
		fmt.Fprintf(os.Stderr, "[Main] 配置加载失败: %v\n", err)
		os.Exit(1)
	}
	cfg := config.Cfg

	// 2. 初始化结构化日志（Zap）
	common.InitLogger(cfg.Server.Mode)
	defer common.Sync()

	zap.L().Info("[Main] IoT Platform (Go) 正在启动...")

	// 3. 连接PostgreSQL
	db, err := initDatabase(cfg)
	if err != nil {
		zap.L().Fatal("[Main] 数据库连接失败", zap.Error(err))
	}
	zap.L().Info("[Main] PostgreSQL 连接成功")

	// 4. 连接Redis
	rdb, err := initRedis(cfg)
	if err != nil {
		zap.L().Fatal("[Main] Redis连接失败", zap.Error(err))
	}
	zap.L().Info("[Main] Redis 连接成功")

	// 5. 初始化 Sa-Token 认证框架（Redis key 对齐 Java 后端）
	// Java key 格式: {tokenName}:token:{uuid}
	//
	// 用户 Token Manager（tokenName = "Authorization"，默认 timeout 30 天，isShare=true）
	userMgr := satoken.NewBuilder().
		Storage(saredis.NewStorageFromClient(rdb)).
		TokenName("Authorization").  // 对齐 Java sa-token.token-name
		KeyPrefix("Authorization:"). // 对齐 Java key: Authorization:token:xxx
		Timeout(2592000).            // 30 天（Java 默认）
		ActiveTimeout(-1).
		IsConcurrent(true).
		IsLog(true).
		IsReadCookie(true).
		CookieHttpOnly(false). // 允许前端 JS 读取 token
		TokenStyle(satoken.TokenStyleUUID).
		Build()

	// 初始化 stputil 全局管理器（middleware/auth.go 和 service/user_service.go 中使用 stputil.Login/IsLogin 等依赖此全局实例）
	stputil.SetManager(userMgr)

	// 设置 Gin 集成
	saginPlugin := sagin.NewPlugin(userMgr)

	// 设备 Token Manager（tokenName = "X-Device-Token", timeout=-1 永不过期, isShare=true）
	deviceMgr := satoken.NewBuilder().
		Storage(saredis.NewStorageFromClient(rdb)).
		TokenName("X-Device-Token").  // 对齐 Java DeviceUtil
		KeyPrefix("X-Device-Token:"). // 对齐 Java key: X-Device-Token:token:xxx
		Timeout(-1).                  // 永不过期（Java setTimeout(-1)）
		IsLog(true).
		TokenStyle(satoken.TokenStyleUUID).
		Build()
	middleware.SetDeviceManager(deviceMgr)

	zap.L().Info("[Main] Sa-Token 认证框架初始化完成（Redis key 对齐 Java）")

	// 6. Wire 依赖注入（自动装配所有组件）
	components, err := InitializeApp(db, rdb)
	if err != nil {
		zap.L().Fatal("[Main] 依赖注入初始化失败", zap.Error(err))
	}

	// 提取组件引用
	svcs := components.Services
	wsHandler := components.WsHandler

	// 配置设备 WebSocket（Token 校验 + 上行消息处理）
	wsHandler.SetupDeviceWS(deviceMgr, svcs.Report, svcs.Downlink)

	influxSvc := svcs.InfluxDB
	mqttClientSvc := svcs.MQTT

	defer rdb.Close()
	defer influxSvc.Close()

	// 7. 自动迁移数据库表
	if err := autoMigrate(db); err != nil {
		zap.L().Fatal("[Main] 数据库迁移失败", zap.Error(err))
	}
	zap.L().Info("[Main] 数据库表迁移完成")

	// 8. 检查InfluxDB连通性
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := influxSvc.Ping(ctx); err != nil {
		zap.L().Warn("[Main] InfluxDB 未连接 — InfluxDB相关功能将不可用", zap.Error(err))
	} else {
		zap.L().Info("[Main] InfluxDB 连接正常")
	}
	cancel()

	// 9. 可选：自动连接MQTT（enabled=false 时跳过）
	if cfg.MQTT.Enabled && cfg.MQTT.BrokerURL != "" {
		go func() {
			time.Sleep(2 * time.Second)
			if err := mqttClientSvc.Connect(); err != nil {
				zap.L().Warn("[Main] MQTT自动连接失败（可通过API手动连接）", zap.Error(err))
			}
		}()
	} else {
		zap.L().Info("[Main] MQTT 已禁用")
	}

	// 10. 设置路由
	r := router.Setup(svcs, wsHandler, saginPlugin)

	// 11. 启动 UDP 设备上报服务（udp-port > 0 时启用）
	var udpSrv *server.UDPServer
	if cfg.Server.UDPPort > 0 {
		udpSrv = server.NewUDPServer(svcs.Report)
		if err := udpSrv.Start(cfg.Server.UDPPort); err != nil {
			zap.L().Warn("[Main] UDP启动失败", zap.Error(err))
		}
	}

	// 12. 启动HTTP服务器
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		zap.L().Info("[Main] 服务器启动", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zap.L().Fatal("[Main] 服务器启动失败", zap.Error(err))
		}
	}()

	// 13. 优雅关闭
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

// initDatabase 初始化PostgreSQL数据库连接
func initDatabase(cfg *config.Config) (*gorm.DB, error) {
	logLevel := logger.Warn
	if cfg.Server.Mode == "debug" {
		logLevel = logger.Info
	}

	db, err := gorm.Open(postgres.Open(cfg.Database.DSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("连接PostgreSQL失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取*sql.DB失败: %w", err)
	}

	if cfg.Database.MaxOpen > 0 {
		sqlDB.SetMaxOpenConns(cfg.Database.MaxOpen)
	} else {
		sqlDB.SetMaxOpenConns(20)
	}

	if cfg.Database.MaxIdle > 0 {
		sqlDB.SetMaxIdleConns(cfg.Database.MaxIdle)
	} else {
		sqlDB.SetMaxIdleConns(5)
	}

	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}

// initRedis 初始化Redis连接
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
		return nil, fmt.Errorf("Redis连接失败: %w", err)
	}

	return rdb, nil
}

// autoMigrate 自动迁移数据库表
func autoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&entity.User{},
		&entity.Device{},
		&entity.MqttPublishLog{},
		&entity.DownlinkCmd{},
	)
}
