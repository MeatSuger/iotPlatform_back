package router

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	sagin "github.com/sa-tokens/sa-token-go/integrations/gin"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/yu/iot-platform-go/api/swagger" // Swagger 生成的文档
	"github.com/yu/iot-platform-go/internal/controller"
	"github.com/yu/iot-platform-go/internal/middleware"
	"github.com/yu/iot-platform-go/internal/service"
	"github.com/yu/iot-platform-go/internal/websocket"
	"github.com/yu/iot-platform-go/pkg/config"
)

// Services 服务集合（用于依赖注入）
type Services struct {
	User     *service.UserService
	Device   *service.DeviceService
	Report   *service.DeviceReportService
	InfluxDB *service.InfluxDBService
	MQTT     *service.MqttClientService
	MQTTLog  *service.MqttPublishLogService
	Downlink *service.DownlinkService
}

// Setup 配置路由
func Setup(svcs *Services, wsHandler *websocket.WsHandler, userPlugin *sagin.Plugin) *gin.Engine {
	// 设置Gin模式
	if config.Cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()

	// 全局中间件
	r.Use(middleware.Recovery())
	r.Use(middleware.Logger())

	// CORS配置
	corsCfg := cors.Config{
		AllowOrigins:     config.Cfg.CORS.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Device-Token", "X-Requested-With"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}
	if len(config.Cfg.CORS.AllowedOrigins) == 0 {
		corsCfg.AllowAllOrigins = true
	}
	r.Use(cors.New(corsCfg))

	// 中间件快捷变量
	userAuth := middleware.AuthMiddleware()             // 用户 Sa-Token 认证
	deviceAuth := middleware.DeviceAuthMiddleware()     // 设备 Sa-Token 认证（保留用于兼容）
	deviceIdAuth := middleware.DeviceIDAuthMiddleware() // 设备 6位hex ID 认证（无需额外Token）
	requireLogin := middleware.RequireLogin()

	// 控制器
	userCtl := controller.NewUserController(svcs.User)
	deviceCtl := controller.NewDeviceController(svcs.Device, svcs.Report)
	dataCtl := controller.NewDataController(svcs.Report, svcs.InfluxDB)
	mqttCtl := controller.NewMqttController(svcs.MQTT, svcs.MQTTLog, svcs.Report)
	downlinkCtl := controller.NewDownlinkController(svcs.Downlink, svcs.Device)

	// API路由组（TokenInterceptor 自动从 Header/Cookie/Query 提取 token 到 context）
	api := r.Group("/api")
	api.Use(userPlugin.TokenInterceptor())
	{
		// ===== Swagger 文档 =====
		api.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

		// ===== WebSocket（不受认证中间件影响） =====
		api.GET("/ws/mqtt", func(c *gin.Context) {
			wsHandler.Handle(c.Writer, c.Request)
		})
		api.GET("/ws/device", func(c *gin.Context) {
			wsHandler.HandleDevice(c.Writer, c.Request)
		})
		api.GET("/ws/user", func(c *gin.Context) {
			wsHandler.HandleUser(c.Writer, c.Request)
		})

		// ===== 用户相关路由 =====
		userGroup := api.Group("/user")
		{
			// 公开路径（无需认证）
			userGroup.POST("/register", userCtl.Register)
			userGroup.POST("/login", userCtl.Login)
			userGroup.GET("/isLogin", userCtl.IsLogin)

			// 需要登录的路径
			userGroup.POST("/logout", userAuth, userCtl.Logout)
			userGroup.PUT("", userAuth, userCtl.Update)
			userGroup.GET("/profile", userAuth, userCtl.GetProfile)
			userGroup.GET("/list", userAuth, requireLogin, userCtl.List)
			userGroup.GET("/page", userAuth, requireLogin, userCtl.Page)
			userGroup.POST("/delete", userAuth, userCtl.Delete)
		}

		// ===== 设备相关路由 =====
		deviceGroup := api.Group("/device")
		{
			// 设备注册：需要用户登录
			deviceGroup.POST("/register", userAuth, deviceCtl.Register)
			// 需要用户登录
			deviceGroup.GET("/list", userAuth, deviceCtl.List)
			deviceGroup.GET("/:deviceId/Data", userAuth, deviceCtl.GetDeviceData)
			// 设备 Token 获取（6位hex ID 认证，无需用户Token）
			deviceGroup.GET("/:deviceId/login", deviceIdAuth, deviceCtl.GetDeviceToken)
			deviceGroup.GET("/:deviceId/token", deviceIdAuth, deviceCtl.GetDeviceToken)
			deviceGroup.POST("/:deviceId/delete", userAuth, deviceCtl.Delete)
			// 下放命令
			deviceGroup.POST("/:deviceId/cmd", userAuth, downlinkCtl.PostCmd) // 用户下发
			deviceGroup.GET("/:deviceId/cmd", deviceAuth, downlinkCtl.GetCmd) // 设备拉取（UUID设备Token认证）
		}

		// ===== 数据相关路由（设备Token认证） =====
		dataGroup := api.Group("/data")
		{
			// InfluxDB连通性检查（公开）
			dataGroup.POST("/ping", dataCtl.Ping)
			// 需要设备Token认证
			dataGroup.POST("/:deviceId/Data", deviceAuth, dataCtl.ReportData)
			dataGroup.POST("/:deviceId/ping", deviceAuth, dataCtl.Heartbeat)
			dataGroup.POST("/:deviceId/heartbeat", deviceAuth, dataCtl.Heartbeat)
			// 查询数据（公开）
			dataGroup.GET("/:deviceId/Data/list", dataCtl.QueryData)
			dataGroup.GET("/list", dataCtl.ListData)
		}

		// ===== MQTT相关路由（enabled=false 时跳过注册） =====
		if config.Cfg.MQTT.Enabled {
			mqttGroup := api.Group("/mqtt")
			{
				// 设备认证路径（设备上报数据和心跳）
				mqttGroup.POST("/:deviceId/Data", deviceAuth, mqttCtl.ReportData)
				mqttGroup.POST("/:deviceId/ping", deviceAuth, mqttCtl.Heartbeat)
				mqttGroup.POST("/:deviceId/heartbeat", deviceAuth, mqttCtl.Heartbeat)

				// 用户认证路径（MQTT客户端管理）
				clientGroup := mqttGroup.Group("/client")
				clientGroup.Use(userAuth)
				{
					clientGroup.POST("/connect", mqttCtl.Connect)
					clientGroup.POST("/disconnect", mqttCtl.Disconnect)
					clientGroup.POST("/subscribe", mqttCtl.Subscribe)
					clientGroup.POST("/unsubscribe", mqttCtl.Unsubscribe)
					clientGroup.POST("/publish", mqttCtl.Publish)
					clientGroup.GET("/status", mqttCtl.Status)
					clientGroup.GET("/messages", mqttCtl.Messages)
				}
			}
		}
	}

	return r
}
