# IoT Platform (Go 重构版)

> 基于原 Java Spring Boot 项目的 Go 语言重构，用于评估两种语言在后端 IoT 场景下的优劣。

---

## 项目结构

```
rebuild-go/
├── main.go                          # 应用入口
├── go.mod                           # Go模块定义
├── Makefile                         # 构建脚本
├── Dockerfile                       # 多阶段构建
├── docker-compose.yml               # 完整服务编排
├── config/
│   ├── config.go                    # 配置结构定义 + Viper加载
│   ├── config.yaml                  # 开发环境配置
│   └── config.prod.yaml             # 生产环境配置
├── common/
│   └── response.go                  # 统一API响应（ApiResponse）
├── entity/
│   ├── user.go                      # 用户实体 (app_user)
│   ├── device.go                    # 设备实体 (iot_device)
│   ├── sensor_data.go               # 传感器数据DTO
│   ├── mqtt_publish_log.go          # MQTT发布日志实体
│   └── mqtt/
│       ├── client_status.go         # MQTT客户端状态
│       ├── message_view.go          # MQTT消息视图
│       ├── publish_request.go       # 发布请求
│       ├── subscribe_request.go     # 订阅请求
│       └── topic_request.go         # 主题请求
├── middleware/
│   ├── auth.go                      # JWT认证（用户+设备双Token）
│   ├── logger.go                    # HTTP请求日志
│   └── recovery.go                  # Panic恢复
├── controller/
│   ├── user_controller.go           # 用户API (/user/*)
│   ├── device_controller.go         # 设备API (/device/*)
│   ├── data_controller.go           # 数据API (/data/*)
│   └── mqtt_controller.go           # MQTT API (/mqtt/*)
├── service/
│   ├── user_service.go              # 用户服务（注册/登录/JWT）
│   ├── device_service.go            # 设备服务（注册/Token管理）
│   ├── device_report_service.go     # 设备数据上报服务
│   ├── influxdb_service.go          # InfluxDB时序数据服务
│   ├── mqtt_client_service.go       # MQTT客户端服务
│   └── mqtt_publish_log_service.go  # MQTT日志服务
├── repository/
│   ├── user_repo.go                 # 用户数据访问
│   ├── device_repo.go               # 设备数据访问
│   └── mqtt_publish_log_repo.go     # MQTT日志数据访问
├── cache/
│   └── redis.go                     # Redis缓存抽象
├── router/
│   └── router.go                    # Gin路由注册
├── websocket/
│   ├── hub.go                       # WebSocket连接管理
│   └── handler.go                   # WebSocket处理器
└── util/
    ├── device_util.go               # 设备工具函数
    └── device_util_test.go          # 单元测试
```

---

## 技术栈对比

| 领域 | Java (原项目) | Go (本重构) |
|------|--------------|-------------|
| **语言版本** | Java 21 | Go 1.22 |
| **Web框架** | Spring Boot 3.5 | Gin 1.10 |
| **ORM** | MyBatis-Plus 3.5 | GORM 1.25 |
| **缓存** | Spring Cache + Redis | go-redis + 手写缓存抽象 |
| **认证** | Sa-Token (会话) | JWT (golang-jwt) |
| **MQTT** | Eclipse Paho (Java) | Eclipse Paho (Go) |
| **时序数据库** | InfluxDB Java Client | InfluxDB Go Client |
| **WebSocket** | Spring WebSocket | Gorilla WebSocket |
| **JSON** | Fastjson2 / Jackson | encoding/json (标准库) |
| **校验** | Bean Validation | go-playground/validator |
| **配置** | Spring YAML | Viper |
| **构建** | Maven | Go Modules + Makefile |
| **异步** | @Async + ThreadPool | Goroutine (轻量级协程) |
| **打包** | Fat JAR (~50MB) | 静态二进制 (~15MB) |

---

## 功能对比

| 功能模块 | Java版 | Go版 | 说明 |
|---------|--------|------|------|
| 用户注册/登录 | ✅ | ✅ | JWT替代Sa-Token |
| 用户CRUD | ✅ | ✅ | 含角色权限控制 |
| 设备注册 | ✅ | ✅ | 自动生成6位设备ID |
| 设备Token管理 | ✅ | ✅ | 双Token体系 |
| 设备数据上报 | ✅ | ✅ | HTTP + MQTT双通道 |
| 设备心跳 | ✅ | ✅ | |
| InfluxDB写入/查询 | ✅ | ✅ | 异步写入 |
| MQTT Broker连接 | ✅ | ✅ | 自动重连 |
| MQTT订阅/发布 | ✅ | ✅ | 含发布日志 |
| WebSocket推送 | ✅ | ✅ | MQTT消息实时广播 |
| Redis缓存 | ✅ | ✅ | 设备/状态/传感器缓存 |
| 分页查询 | ✅ | ✅ | |
| Swagger文档 | ✅ | ❌ | Go版无内置，可用swaggo补充 |
| 全局异常处理 | ✅ | ✅ | Recovery中间件 |
| CORS配置 | ✅ | ✅ | |

---

## Java vs Go — IoT 后端场景深度对比

### 1. 开发效率

| 维度 | Java (Spring Boot) | Go (Gin) |
|------|-------------------|----------|
| **代码量** | 较多（注解+配置+XML） | 较少（显式但简洁） |
| **脚手架** | Spring Initializr 一键生成 | 需手写或使用模板 |
| **依赖管理** | Maven/Gradle (XML/DSL) | Go Modules (go.mod) |
| **热重载** | spring-devtools | air / realize |
| **IDE支持** | IntelliJ IDEA 极致 | GoLand / VS Code |
| **学习曲线** | 陡峭（Spring生态庞大） | 平缓（语言简单，标准库强大） |

**结论：** Go代码量更少（本项目Go约2500行 vs Java约3000行），但Spring的注解驱动开发在复杂业务场景下更高效。

### 2. 运行时性能

| 维度 | Java (Spring Boot) | Go |
|------|-------------------|-----|
| **启动时间** | 3~8秒（JVM预热） | <100ms（编译型） |
| **内存占用** | 200~500MB（基线） | 20~60MB（基线） |
| **CPU效率** | 优秀（JIT编译优化） | 优秀（AOT编译） |
| **并发模型** | 线程池（重量级） | Goroutine（轻量级，2KB栈） |
| **GC** | G1/ZGC（STW短但存在） | 并发标记清除（STW极短，<1ms） |
| **吞吐量** | 高 | 高（同等硬件下通常Go更优） |

**结论：** Go在IoT场景下优势明显 — **启动极快、内存极省**，适合边缘计算和容器化部署。Java需要GraalVM Native Image才能接近。

### 3. 部署和运维

| 维度 | Java | Go |
|------|------|-----|
| **编译产物** | Fat JAR (~50MB) + JRE | 静态二进制 (~15MB) |
| **Docker镜像** | ~200MB (含JRE) | ~15MB (FROM scratch) |
| **冷启动** | 慢（JVM初始化） | 瞬间 |
| **依赖** | 需要JRE | 零运行时依赖 |
| **跨平台** | 一次编译到处运行（需JVM） | 交叉编译到任何平台 |
| **K8s亲和性** | 一般（内存大、启动慢） | 极好（内存小、启动快） |

**结论：** Go在容器化和边缘部署场景下完胜。**15MB的Docker镜像 vs 200MB**，这在IoT网关设备上至关重要。

### 4. 生态系统

| 维度 | Java | Go |
|------|------|-----|
| **Web框架** | Spring Boot（统治级） | Gin/Fiber/Echo（百花齐放） |
| **ORM** | MyBatis/Hibernate/JPA | GORM/sqlx/ent |
| **MQTT客户端** | Eclipse Paho | Eclipse Paho（Go移植） |
| **数据库驱动** | JDBC（极其成熟） | database/sql（标准库，驱动按需） |
| **社区规模** | 巨大（企业级） | 快速成长（云原生为主） |
| **第三方库质量** | 高（成熟稳定） | 中高（部分库较新） |
| **安全补丁** | 快速（Oracle/Red Hat） | 快速（社区+Google） |

**结论：** Java生态在**企业级功能**（事务、安全、监控）上更成熟。Go在**云原生/基础设施**领域生态更好。

### 5. 代码质量与维护性

| 维度 | Java | Go |
|------|------|-----|
| **类型安全** | 强类型 + 泛型（复杂） | 强类型 + 泛型（Go 1.18+，简洁） |
| **错误处理** | 异常（try/catch/finally） | 显式返回值（if err != nil） |
| **代码风格** | 多样（Spring/AOP/Lombok） | 强制统一（gofmt） |
| **测试** | JUnit/Mockito/Spring Test | 标准库 testing + testify |
| **可读性** | 注解魔法多，隐式行为 | 显式、直接、无魔法 |
| **重构** | IDE重度依赖 | 简单（编译时检查+工具链） |
| **NULL安全** | Optional + @Nullable | 零值 + 指针（更简洁但需注意nil） |

**结论：** Go的**显式哲学**使代码更容易理解和维护，但`if err != nil`的重复模式颇有争议。Java的注解虽然简洁，但"魔法"太多，调试困难。

### 6. IoT场景特定考量

| 场景 | 推荐 | 原因 |
|------|------|------|
| **边缘网关** | Go | 内存小、二进制单文件、无JRE依赖 |
| **设备管理平台（云）** | Java | 复杂业务逻辑、事务支持更好 |
| **MQTT Broker旁路** | Go | 并发处理强、资源占用低 |
| **时序数据处理** | Go | 协程天然适合流式处理 |
| **设备固件服务** | Go | 交叉编译到ARM/MIPS等架构 |
| **企业级后台管理** | Java | Spring Security/生态更完善 |

---

## 迁移注意事项

### Java -> Go 的主要差异

1. **注解 vs 显式代码**
   - Java `@Cacheable` -> Go 手动调用 `cache.Get/Set`
   - Java `@Async` -> Go `go func()`
   - Java `@Transactional` -> Go 手动管理DB事务

2. **依赖注入**
   - Java Spring IoC容器自动注入
   - Go 手动构造函数注入（`NewXxxService(repo, cache)`）

3. **ORM**
   - Java MyBatis-Plus 丰富的CRUD方法
   - Go GORM 链式调用，需要更多手动编码

4. **认证**
   - Java Sa-Token 开箱即用的会话管理
   - Go JWT 无状态，需手动处理刷新/注销

5. **JSON序列化**
   - Java Jackson自动处理驼峰/下划线
   - Go json tag 显式声明（但更可预测）

### 数据库兼容性

- 使用相同的 PostgreSQL 数据库
- 表名、字段名保持一致
- GORM AutoMigrate 会自动创建缺失的表
- InfluxDB bucket 和 measurement 完全相同

---

## 快速开始

### 前置条件
- Go 1.22+
- PostgreSQL 16+
- Redis 7+
- InfluxDB 2.7+
- (可选) Mosquitto MQTT Broker

### 本地运行

```bash
# 1. 修改配置
cp config/config.yaml config/config.local.yaml
# 编辑 config.local.yaml，填入实际的数据库/Redis/InfluxDB连接信息

# 2. 安装依赖
make deps

# 3. 运行
make run
# 或
CONFIG_PATH=config/config.local.yaml go run main.go
```

### Docker部署

```bash
# 设置环境变量
export DB_PASSWORD=your_password
export REDIS_PASSWORD=your_password
export INFLUXDB_TOKEN=your_token
export JWT_USER_SECRET=your_jwt_secret
export JWT_DEVICE_SECRET=your_device_jwt_secret

# 启动所有服务
make docker-up

# 查看日志
make docker-logs
```

### API端点

| 方法 | 路径 | 说明 | 认证 |
|------|------|------|------|
| POST | /api/user/register | 用户注册 | 无 |
| POST | /api/user/login | 用户登录 | 无 |
| GET | /api/user/isLogin | 检查登录状态 | 无 |
| POST | /api/device/register | 注册设备 | Bearer Token |
| GET | /api/device/list | 设备列表 | Bearer Token |
| GET | /api/device/:deviceId/Data | 设备详情 | Bearer Token |
| POST | /api/data/:deviceId/Data | 上报传感器数据 | X-Device-Token |
| POST | /api/data/:deviceId/ping | 设备心跳 | X-Device-Token |
| GET | /api/data/:deviceId/Data/list | 查询传感器数据 | 无 |
| GET | /api/ws/mqtt | WebSocket连接 | 无 |
| POST | /api/mqtt/client/connect | 连接MQTT | Bearer Token |
| POST | /api/mqtt/client/publish | 发布MQTT消息 | Bearer Token |
| GET | /api/mqtt/client/status | MQTT状态 | Bearer Token |

---

## 总结

| | Java (Spring Boot) | Go (Gin) |
|---|---|---|
| **最适合** | 企业级复杂后台系统 | 云原生/IoT/微服务 |
| **内存效率** | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ |
| **开发效率** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ |
| **运维简单性** | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ |
| **生态成熟度** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ |
| **编译速度** | ⭐⭐ | ⭐⭐⭐⭐⭐ |

**建议：** 对于 IoT 场景，Go 在资源效率、部署简便性和并发处理上有显著优势。如果项目需要在资源受限的设备上运行（如边缘网关），Go 是更优选择。如果优先考虑开发速度和第三方集成，Java/Spring Boot 仍然是最稳妥的选择。
