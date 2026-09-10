package router

import (
	"context"
	"net/http"
	"sync"
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

// HealthProbe 健康检查依赖探针。
// 任一探针失败时 /health 返回 HTTP 503（HTTP 状态码仅表达后端是否正常），
// 全部为 nil 时 /health 恒返回 200（保持向后兼容）。
type HealthProbe struct {
	PostgreSQL func(ctx context.Context) error
	Redis      func(ctx context.Context) error
	Influx     func(ctx context.Context) error
}

// probeTimeout 单次健康探测总超时，防止依赖无响应拖垮健康检查
const probeTimeout = 2 * time.Second

// healthCheck 健康检查（供容器编排/负载均衡探测，依赖失联返回 503）
// @Summary      健康检查
// @Description  供容器编排/负载均衡探测，不走统一响应结构；
// @Description  探测 PostgreSQL/Redis/InfluxDB，任一失联时返回 HTTP 503
// @Tags         system
// @Produce      json
// @Success      200  {object}  map[string]string
// @Failure      503  {object}  map[string]string
// @Router       /health [get]
// healthCheck 健康检查 (GET /health)
func healthCheck(probes *HealthProbe) gin.HandlerFunc {
	return func(c *gin.Context) {
		status := "ok"
		httpCode := http.StatusOK
		deps := map[string]string{}

		if probes != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), probeTimeout)
			defer cancel()

			var wg sync.WaitGroup
			var mu sync.Mutex
			check := func(name string, fn func(ctx context.Context) error) {
				if fn == nil {
					return
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					err := fn(ctx)
					mu.Lock()
					defer mu.Unlock()
					if err != nil {
						deps[name] = "error: " + err.Error()
						status = "error"
						httpCode = http.StatusServiceUnavailable
					} else {
						deps[name] = "ok"
					}
				}()
			}

			check("postgresql", probes.PostgreSQL)
			check("redis", probes.Redis)
			check("influxdb", probes.Influx)
			wg.Wait()
		}

		body := gin.H{
			"status":       status,
			"service":      "iot-platform",
			"time":         time.Now().Format(time.RFC3339),
			"dependencies": deps,
		}
		if probes == nil {
			delete(body, "dependencies")
		}
		c.JSON(httpCode, body)
	}
}

// Services 服务集合（用于依赖注入）
type Services struct {
	User      *service.UserService
	Device    *service.DeviceService
	Report    *service.DeviceReportService
	InfluxDB  *service.InfluxDBService
	Downlink  *service.DownlinkService
	Config    *service.DeviceConfigService
	Sensors   *service.DeviceSensorService
	Actuators *service.DeviceActuatorService
}

// Setup 配置路由
func Setup(svcs *Services, wsHandler *websocket.WsHandler, userPlugin *sagin.Plugin, mqttGateway *controller.MqttGatewayController, probes *HealthProbe) *gin.Engine {
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

	// ===== 健康检查（无需认证，供容器编排/负载均衡探测；依赖失联返回 503） =====
	r.GET("/health", healthCheck(probes))

	// 中间件快捷变量
	userAuth := middleware.AuthMiddleware()                                         // 用户 Sa-Token 认证
	userOrDeviceAuth := middleware.UserOrDeviceAuthMiddleware()                     // 双认证：用户 Token 或设备 Token
	deviceAuth := middleware.DeviceAuthMiddleware()                                 // 设备 Sa-Token 认证（数据上报/命令拉取）
	deviceIdAuth := middleware.DeviceIDAuthMiddleware()                             // 设备 6位hex ID 认证（无需额外Token）
	requireAdmin := middleware.CheckRole(service.RoleSuperAdmin, service.RoleAdmin) // 管理员角色校验（sa-token-go）

	// 控制器
	userCtl := controller.NewUserController(svcs.User)
	deviceCtl := controller.NewDeviceController(svcs.Device, svcs.Report, svcs.Sensors, svcs.Actuators)
	dataCtl := controller.NewDataController(svcs.Report, svcs.InfluxDB)
	downlinkCtl := controller.NewDownlinkController(svcs.Downlink, svcs.Device)
	configCtl := controller.NewDeviceConfigController(svcs.Config, svcs.Device)
	sensorCtl := controller.NewDeviceSensorController(svcs.Sensors, svcs.Device)
	actuatorCtl := controller.NewDeviceActuatorController(svcs.Actuators, svcs.Device)

	// API 路由组（TokenInterceptor 在下方对整组应用，自动从 Header/Cookie/Query 提取 token 到 context）
	api := r.Group("/api")

	// 设备数据上报/心跳路由组：在 TokenInterceptor 应用前注册，走独立的 DeviceAuth 中间件
	// （已取消：TokenInterceptor 仅提取 token 不拦截，全部 REST 路由统一注册在其之后）

	// 其余 API 使用 TokenInterceptor
	api.Use(userPlugin.TokenInterceptor())
	{
		// ===== Swagger 文档（生产环境通过 server.swagger-enabled=false 关闭） =====
		if config.Cfg.Server.SwaggerEnabled {
			api.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
		}

		// ===== WebSocket（不受认证中间件影响） =====
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

		// ====================================================================
		// REST 资源路由（Google AIP 风格，仅使用 GET / POST 两个动词，
		// 兼容嵌入式客户端；更新/删除通过 POST + /update /delete 后缀表达）
		// ====================================================================

		// ===== User 资源 /api/users =====
		api.POST("/users", userCtl.Create)                          // Create：注册用户（无需认证）
		api.POST("/users/login", userCtl.Login)                     // Login：登录签发 Token（无需认证）
		api.GET("/users/isLogin", userCtl.IsLogin)                  // IsLogin：会话检查（无需认证）
		api.POST("/users/logout", userAuth, userCtl.Logout)         // Logout：吊销当前 Token
		api.GET("/users", userAuth, requireAdmin, userCtl.List)     // List：用户列表（可选分页）
		api.GET("/users/:userId", userAuth, userCtl.Get)            // Get：详情（me=当前用户）
		api.POST("/users/:userId/update", userAuth, userCtl.Update) // Update：本人或管理员
		api.POST("/users/:userId/delete", userAuth, userCtl.Delete) // Delete：本人或管理员

		// ===== Device 资源 /api/devices =====
		api.POST("/devices", userAuth, deviceCtl.Register)
		api.GET("/devices", userAuth, deviceCtl.List)
		api.GET("/devices/:deviceId", userAuth, deviceCtl.GetDeviceData)
		api.POST("/devices/:deviceId/update", userOrDeviceAuth, deviceCtl.UpdateDevice)
		api.POST("/devices/:deviceId/delete", userAuth, deviceCtl.Delete)
		api.GET("/devices/:deviceId/token", deviceIdAuth, deviceCtl.GetDeviceToken)
		api.GET("/devices/:deviceId/login", deviceIdAuth, deviceCtl.GetDeviceToken) // 兼容别名

		// ===== SensorData 子资源 =====
		api.POST("/devices/:deviceId/sensorData", deviceAuth, dataCtl.ReportData)
		api.GET("/devices/:deviceId/sensorData", userAuth, dataCtl.QueryData)
		api.POST("/devices/:deviceId/heartbeat", deviceAuth, dataCtl.Heartbeat)
		api.POST("/devices/:deviceId/ping", deviceAuth, dataCtl.Heartbeat) // 兼容别名

		// ===== 下行命令子资源 =====
		api.POST("/devices/:deviceId/commands", userAuth, downlinkCtl.PostCmd)
		api.GET("/devices/:deviceId/commands", deviceAuth, downlinkCtl.GetCmd)

		// ===== DeviceConfig 子资源 =====
		api.GET("/devices/:deviceId/config", userOrDeviceAuth, configCtl.GetConfig) // Get：设备属主或设备本人
		api.POST("/devices/:deviceId/config", userAuth, configCtl.SaveConfig)
		api.POST("/devices/:deviceId/config/report", deviceAuth, configCtl.ReportConfig)

		// ===== Sensor 子资源（传感器定义 / 物模型） =====
		// 注意：POST .../sensors/apply 是静态段，gin 静态匹配优先于 :sensorId 参数段；
		// 而 GET .../sensors/apply 会命中 GetSensor（sensorId=apply）——本组路由不提供 GET apply，
		// 如未来新增，请改用独立子路径（如 .../sensors/apply/all），避免语义混淆。
		api.GET("/devices/:deviceId/sensors", userAuth, sensorCtl.ListSensors)
		api.GET("/devices/:deviceId/sensors/:sensorId", userAuth, sensorCtl.GetSensor)
		api.POST("/devices/:deviceId/sensors", userAuth, sensorCtl.CreateSensor)
		api.POST("/devices/:deviceId/sensors/apply", userAuth, sensorCtl.ApplySensors)
		api.POST("/devices/:deviceId/sensors/:sensorId/update", userAuth, sensorCtl.UpdateSensor)
		api.POST("/devices/:deviceId/sensors/:sensorId/delete", userAuth, sensorCtl.DeleteSensor)

		// ===== Actuator 子资源（执行器定义 / 物模型） =====
		api.GET("/devices/:deviceId/actuators", userAuth, actuatorCtl.ListActuators)
		api.GET("/devices/:deviceId/actuators/:actuatorId", userAuth, actuatorCtl.GetActuator)
		api.POST("/devices/:deviceId/actuators", userAuth, actuatorCtl.CreateActuator)
		api.POST("/devices/:deviceId/actuators/apply", userAuth, actuatorCtl.ApplyActuators)
		api.POST("/devices/:deviceId/actuators/:actuatorId/update", userAuth, actuatorCtl.UpdateActuator)
		api.POST("/devices/:deviceId/actuators/:actuatorId/delete", userAuth, actuatorCtl.DeleteActuator)
	}

	return r
}
