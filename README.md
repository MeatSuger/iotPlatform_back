# IoT Platform (Go)

> IoT 物联网后端平台，Go 重构版。提供设备管理、数据上报、实时下放命令、MQTT/WebSocket 双向通信。

---

## 技术栈

| 组件      | 技术                      |
|-----------|---------------------------|
| 语言      | Go 1.26                   |
| Web 框架  | Gin 1.12                  |
| ORM       | Ent (entgo.io/ent v0.14)  |
| DI        | Google Wire               |
| 认证      | Sa-Token Go（Redis 会话） |
| 数据库    | PostgreSQL 16             |
| 缓存      | Redis 7                   |
| 时序库    | InfluxDB 2.7              |
| MQTT      | Eclipse Paho              |
| WebSocket | Gorilla WebSocket         |

---

## 项目结构

```
back/
├── cmd/iot-platform/        # 入口：main.go + Wire 依赖注入
│   ├── main.go              #   启动：加载配置 → 连接DB/Redis → Wire注入 → 启动服务
│   ├── wire.go              #   Wire 注入声明
│   └── wire_gen.go          #   Wire 自动生成
├── configs/                 # YAML 配置文件（dev/prod）
├── internal/
│   ├── controller/          # HTTP 控制器
│   ├── service/             # 业务逻辑层
│   ├── repository/          # 数据访问层
│   ├── model/               # 数据模型 / DTO
│   ├── middleware/           # 认证 / 日志 / 恢复 / 鉴权
│   ├── router/              # 路由注册
│   ├── server/              # UDP 服务器
│   ├── websocket/           # WebSocket Hub + Handler
│   └── ent/                 # Ent ORM 生成代码（schema / client / query）
├── pkg/
│   ├── cache/               # Redis 缓存抽象
│   ├── common/              # 统一响应 / 错误 / 日志 / 自定义时间类型
│   ├── config/              # 配置结构体 + Viper 加载
│   └── util/                # 工具函数
├── api/swagger/             # Swagger 文档 + API.md
├── deployments/             # Docker Compose / Dockerfile / SQL / MQTT 配置
│   ├── app/                 #   主项目 Dockerfile + docker-compose
│   ├── mqtt/                #   MQTT Broker (Mosquitto)
│   └── sql/                 #   数据库初始化 SQL
├── scripts/                 # 开发工具脚本
│   ├── dev.sh               #   热重载启动（air）
│   ├── tunnel.sh            #   sshuttle 隧道
│   └── k6-test.js           #   k6 性能测试
├── go.mod / go.sum          # Go 模块
├── Makefile                 # 构建 / 运行 / 打包
├── .air.toml               # air 热重载配置
└── README.md                # 本文件
```

---

## 快速开始

### 前置条件

- Go 1.26+
- PostgreSQL 16+
- Redis 7+
- InfluxDB 2.7+
- (可选) MQTT Broker (Mosquitto / EMQX)

### 本地开发

```bash
# 1. 克隆项目
git clone <repo-url> && cd iot-back-rebuild

# 2. 修改配置
cp configs/config.yaml configs/config.local.yaml
# 编辑 config.local.yaml，填入本地或远程数据库连接信息

# 3. （可选）通过隧道连接远程 Docker 服务
bash scripts/tunnel.sh on

# 4. 安装依赖
make deps

# 5. 开发运行（热重载）
make dev
# 或者直接运行
make run

# 6. 指定生产配置运行
make run-prod
```

### Docker 部署

```bash
# 编译 Linux 二进制
make build

# 构建 Docker 镜像
docker build -f deployments/app/Dockerfile -t iot-platform .

# 使用 docker-compose 一键启动
docker compose -f deployments/app/docker-compose.yml up -d

# 或使用 Makefile 一键部署到远程服务器
make deploy SSH_HOST=your-server
```

---

## 命令速查

```bash
make dev          # 启动隧道 + 热重载（air）
make build        # 编译 Linux amd64 + 格式化 + 测试
make deps         # 更新 Go 依赖
make clean        # 清理构建产物
make swagger      # 生成 Swagger 文档
make push         # 编译并上传到远程服务器
make deploy       # push + 同步 deployments/ 到远程
make gen-swagger  # 生成 Swagger 文档
```

---

## 配置文件

配置使用 Viper 加载，支持环境变量覆盖（如 `DATABASE_HOST=xxx` 覆盖 `database.host`）。

```yaml
server:
  port: 8182
  mode: debug          # debug | release

database:
  host: "127.0.0.1"
  port: 5432
  user: "postgres"
  password: ""
  dbname: "iotuser"

redis:
  host: "127.0.0.1"
  port: 6379
  password: ""

influxdb:
  url: "http://localhost:8086"
  token: ""
  org: ""
  bucket: "iot-sensor"

mqtt:
  broker-url: "tcp://mqtt:1883"
  # 留空则不自动连接

cors:
  allowed-origins:
    - "https://your-domain.com"
```

> `server.mode: debug` 时，日志会输出每条 SQL 的执行耗时（`op` / `query` / `latency`），便于排查慢查询；`release` 下无此开销。

---

## API 文档

所有接口详见 **[API.md](api/swagger/API.md)**，涵盖：

- 用户管理（注册/登录/CRUD）
- 设备管理（注册/Token/增删查）
- 数据上报（HTTP / MQTT / WebSocket / UDP）
- 下放命令（实时推送 + 轮询拉取）
- WebSocket 双向通信协议
- ESP32 完整示例代码

---

## 认证体系

| 类型 | Token 名称 | 存储 | 过期 | 说明 |
|------|-----------|------|------|------|
| 用户 | `Authorization` | Redis `Authorization:token:xxx` | 30 天 | 同一账号最多同时在线 5 端，每端独立 Token |
| 设备 | `X-Device-Token` | Redis `X-Device-Token:token:xxx` | 永不过期 | 一个 deviceId 只对应一个有效 Token（重新获取顶掉旧 Token） |

- 设备 Token 通过 `GET /api/device/:deviceId/login` 获取，使用设备 6 位 hex ID 认证（**无需用户登录**）。
- 设备若 **30 天未上线**，其 Token 会被自动清理（**不删除设备**），设备重新登录即可获取新 Token。
- 认证 Cookie 统一为 `httpOnly` + `SameSite=Lax`，生产环境（`release`）自动启用 `secure`。

---

## 数据通道

设备 ↔ 服务端之间有四条通道：

```
        ┌─────────┐
        │  设备   │
        └────┬────┘
         ┌───┼───┐
         │   │   │
    HTTP │ WS│   │ MQTT
    上报 │ 双向  │ 双向
         │   │   │
    ┌────┴───┴───┴────┐
    │   IoT Platform  │
    │  ┌────────────┐ │
    │  │ PostgreSQL │ │ ← 设备/用户/命令 元数据
    │  │ InfluxDB   │ │ ← 传感器时序数据
    │  │ Redis      │ │ ← Token 会话 + 缓存
    │  └────────────┘ │
    └─────────────────┘
```

---

## 优雅关闭

收到 `SIGINT` / `SIGTERM` 后：
1. 断开 MQTT 连接
2. HTTP Server 等待进行中请求完成（10s 超时）
3. 关闭数据库/Redis/InfluxDB 连接
