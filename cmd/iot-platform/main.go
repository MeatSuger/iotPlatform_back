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
	"strings"
	"syscall"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/redis/go-redis/v9"
	satoken "github.com/sa-tokens/sa-token-go/core"
	sagin "github.com/sa-tokens/sa-token-go/integrations/gin"
	saredis "github.com/sa-tokens/sa-token-go/storage/redis"
	"github.com/sa-tokens/sa-token-go/stputil"
	"go.uber.org/zap"
	"iot-platform.local/internal/controller"
	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/ent/user"
	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/repository"
	"iot-platform.local/internal/router"
	"iot-platform.local/internal/server"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/common"
	"iot-platform.local/pkg/config"

	_ "github.com/lib/pq"
)

// 编译时注入（通过 -ldflags "-X main.Version=... -X main.BuildTime=..."）
var (
	Version   = "dev"
	BuildTime = "unknown"
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
	common.InitLogger(cfg.Server.Mode, cfg.Server.LogLevel)
	defer common.Sync()

	zap.L().Info("[Main] IoT Platform (Go) 正在启动...",
		zap.String("version", Version),
		zap.String("buildTime", BuildTime))

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
		Timeout(60 * 60 * 24 * 3).
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

	// 7.6 启动跨实例缓存失效 Pub-Sub 监听（多实例部署时自动同步 L1 缓存）
	go components.Cache.SubscribeInvalidate(context.Background())
	zap.L().Info("[Main] 跨实例缓存失效 Pub-Sub 已启动")

	influxSvc := svcs.InfluxDB

	defer influxSvc.Close()

	// 8. Ent 自动迁移
	if err := entClient.Schema.Create(context.Background()); err != nil {
		zap.L().Fatal("[Main] 数据库迁移失败", zap.Error(err))
	}
	zap.L().Info("[Main] 数据库迁移完成（Ent）")

	// 修复历史数据的零值时间戳（DB 直插或旧代码产生的 0001-01-01）
	if err := repairZeroTimestamps(entClient); err != nil {
		zap.L().Warn("[Main] 修复用户零值时间戳失败", zap.Error(err))
	}

	// 9. 检查InfluxDB连通性
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := influxSvc.Ping(ctx); err != nil {
		zap.L().Warn("[Main] InfluxDB 未连接", zap.Error(err))
	} else {
		zap.L().Info("[Main] InfluxDB 连接正常")
	}
	cancel()

	// 9.5 预热 InfluxDB gRPC Flight SQL 连接（gRPC HTTP/2 首次握手慢，避免首次查询 25s 延迟）
	go func() {
		warmCtx, warmCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer warmCancel()
		_, err := influxSvc.QueryDeviceSensorsByTime(warmCtx, "", "", time.Now().Add(-1*time.Hour), time.Now())
		if err != nil {
			zap.L().Warn("[Main] InfluxDB 预热失败", zap.Error(err))
		} else {
			zap.L().Info("[Main] InfluxDB gRPC Flight SQL 预热完成")
		}
	}()

	// 10. MQTT 桥接网关（设备真 MQTT/WSS 接入：框架 Sa-Token 鉴权 → 透明转发到外部Broker）
	// 设备入口: /api/ws/mqtt/broker，鉴权在 MQTT 协议层完成（CONNECT username/password 或 ?X-Device-Token=）
	var mqttGateway *controller.MqttGatewayController
	if cfg.MqttGateway.Enabled {
		logRepo := repository.NewMqttPublishLogRepo(entClient)
		mqttGateway = controller.NewMqttGatewayController(logRepo, svcs.Report)
		zap.L().Info("[Main] MQTT 桥接网关已启用（设备入口: /api/ws/mqtt/broker，转发到 " + cfg.MQTT.BrokerURL + "）")
	}

	// 11. 设置路由
	r := router.Setup(svcs, wsHandler, saginPlugin, mqttGateway)

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

	if mqttGateway != nil {
		zap.L().Info("[Main] MQTT 网关会话已随服务器关闭")
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

	clientName := cfg.Redis.ClientName
	if clientName == "" {
		clientName = "iot-platform"
	}

	var rdb *redis.Client

	switch cfg.Redis.Mode {
	case "sentinel":
		// Sentinel 高可用模式
		masterName := cfg.Redis.MasterName
		if masterName == "" {
			masterName = "mymaster"
		}
		sentinelAddrs := strings.Split(cfg.Redis.SentinelAddrs, ",")
		if len(sentinelAddrs) == 1 && sentinelAddrs[0] == "" {
			// 未配置 sentinel-addrs 时回退到 host:port 作为唯一哨兵地址
			sentinelAddrs = []string{cfg.Redis.Addr()}
		}
		rdb = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    masterName,
			SentinelAddrs: sentinelAddrs,
			Password:      cfg.Redis.Password,
			DB:            cfg.Redis.DB,
			ClientName:    clientName,
			PoolSize:      poolSize,
			MinIdleConns:  minIdle,
			DialTimeout:   time.Duration(dialTimeout) * time.Second,
			ReadTimeout:   time.Duration(readTimeout) * time.Second,
			WriteTimeout:  time.Duration(writeTimeout) * time.Second,
			PoolTimeout:   time.Duration(poolTimeout) * time.Second,
			MaxRetries:    maxRetries,
		})
	case "cluster":
		// Cluster 集群模式暂不支持（需要将 *redis.Client 改为 redis.UniversalClient 接口）
		return nil, fmt.Errorf("redis cluster 模式暂不支持，请使用 standalone 或 sentinel 模式")
	default:
		// standalone 单节点（默认）
		rdb = redis.NewClient(&redis.Options{
			Addr:         cfg.Redis.Addr(),
			Password:     cfg.Redis.Password,
			DB:           cfg.Redis.DB,
			ClientName:   clientName,
			PoolSize:     poolSize,
			MinIdleConns: minIdle,
			DialTimeout:  time.Duration(dialTimeout) * time.Second,
			ReadTimeout:  time.Duration(readTimeout) * time.Second,
			WriteTimeout: time.Duration(writeTimeout) * time.Second,
			PoolTimeout:  time.Duration(poolTimeout) * time.Second,
			MaxRetries:   maxRetries,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis连接失败: %w", err)
	}

	return rdb, nil
}

// repairZeroTimestamps 修复历史数据中零值时间戳（0001-01-01），统一设为当前时间
func repairZeroTimestamps(client *ent.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return client.User.Update().
		Where(user.CreateTimeLT(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC))).
		SetCreateTime(time.Now()).
		SetUpdateTime(time.Now()).
		Exec(ctx)
}
