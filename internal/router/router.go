package router

import (
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	sagin "github.com/sa-tokens/sa-token-go/integrations/gin"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "iot-platform.local/api/swagger" // Swagger 生成的文档
	"iot-platform.local/internal/controller"
	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/service"
	"iot-platform.local/internal/websocket"
	"iot-platform.local/pkg/config"
)

// Services 服务集合（用于依赖注入）
type Services struct {
	User     *service.UserService
	Device   *service.DeviceService
	Report   *service.DeviceReportService
	InfluxDB *service.InfluxDBService
	Downlink *service.DownlinkService
}

// Setup 配置路由
func Setup(svcs *Services, wsHandler *websocket.WsHandler, userPlugin *sagin.Plugin, mqttGateway *controller.MqttGatewayController) *gin.Engine {
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

	// ===== 健康检查（无需认证，供容器编排/负载均衡探测） =====
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "iot-platform",
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	// 中间件快捷变量
	userAuth := middleware.AuthMiddleware()                                         // 用户 Sa-Token 认证
	deviceAuth := middleware.DeviceAuthMiddleware()                                 // 设备 Sa-Token 认证（保留用于兼容）
	deviceIdAuth := middleware.DeviceIDAuthMiddleware()                             // 设备 6位hex ID 认证（无需额外Token）
	requireAdmin := middleware.CheckRole(service.RoleSuperAdmin, service.RoleAdmin) // 管理员角色校验（sa-token-go）

	// 控制器
	userCtl := controller.NewUserController(svcs.User)
	deviceCtl := controller.NewDeviceController(svcs.Device, svcs.Report)
	dataCtl := controller.NewDataController(svcs.Report, svcs.InfluxDB)
	downlinkCtl := controller.NewDownlinkController(svcs.Downlink, svcs.Device)

	// API路由组（TokenInterceptor 自动从 Header/Cookie/Query 提取 token 到 context）
	api := r.Group("/api")

	// 设备数据上报/心跳路由组：不需要 TokenInterceptor（使用独立的 DeviceAuth 中间件）
	// 提前注册以跳过不必要的中间件
	dataGroup := api.Group("/data")
	dataGroup.Use(deviceAuth)
	{
		dataGroup.POST("/:deviceId/Data", dataCtl.ReportData)
		dataGroup.POST("/:deviceId/ping", dataCtl.Heartbeat)
		dataGroup.POST("/:deviceId/heartbeat", dataCtl.Heartbeat)
	}

	// 其余 API 使用 TokenInterceptor
	api.Use(userPlugin.TokenInterceptor())
	{
		// ===== Swagger 文档（生产环境通过 server.swagger-enabled=false 关闭） =====
		if config.Cfg.Server.SwaggerEnabled {
			api.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
		}

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
		// ===== MQTT over WebSocket（设备真 MQTT 协议接入，复用 HTTP 入口） =====
		// 设备连接: wss://<host>/api/ws/mqtt/broker（纯透传，不挂 HTTP 中间件）
		// 鉴权在 MQTT 协议层完成（框架 Sa-Token）：
		//   - 标准 MQTT 客户端：CONNECT username=设备ID / password=设备Token
		//   - mqtt.js 等可拼 URL 的客户端：?X-Device-Token=<token>
		// 网关负责透明转发到外部 MQTT Docker + PUBLISH 日志/入库
		if mqttGateway != nil {
			api.GET("/ws/mqtt/broker", mqttGateway.HandleWebSocket)
		}

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
			userGroup.GET("/list", userAuth, requireAdmin, userCtl.List)
			userGroup.GET("/page", userAuth, requireAdmin, userCtl.Page)
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

		// ===== 数据查询路由（用户认证） =====
		dataQueryGroup := api.Group("/data")
		{
			dataQueryGroup.GET("/:deviceId/Data/list", userAuth, dataCtl.QueryData)
			dataQueryGroup.GET("/list", userAuth, dataCtl.ListData)
		}
	}

	return r
}
