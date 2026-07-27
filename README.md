# IoT Platform (Go)

> IoT 物联网后端平台，Go 重构版。提供设备管理、数据上报、实时下放命令、MQTT/WebSocket 双向通信。

---

## 技术栈

| 组件      | 技术                      |
|-----------|---------------------------|
| 语言      | Go 1.22                   |
| Web 框架  | Gin 1.12                  |
| ORM       | GORM                      |
| 认证      | Sa-Token Go（Redis 会话） |
| 数据库    | PostgreSQL 16             |
| 缓存      | Redis 7                   |
| 时序库    | InfluxDB 2.7              |
| MQTT      | Eclipse Paho              |
| WebSocket | Gorilla WebSocket         |

---

## 项目结构

```
iot-back-rebuild/
├── main.go              # 入口：加载配置 → 连接DB/Redis → Wire注入 → 启动服务
├── config/              # 配置结构 + YAML（dev/prod）
├── entity/              # 数据库实体 + DTO
├── controller/          # HTTP 控制器
├── service/             # 业务逻辑层
├── repository/          # 数据访问层
├── middleware/           # 认证 / 日志 / 恢复 / 鉴权
├── router/              # 路由注册
├── websocket/           # WebSocket Hub + Handler
├── cache/               # Redis 缓存抽象
├── common/              # 统一响应 / 错误 / 日志 / 自定义时间类型
├── util/                # 工具函数
├── deployments/         # Docker Compose / Dockerfile / SQL / MQTT 配置
│   ├── app/             #   主项目 Dockerfile + docker-compose
│   ├── mqtt/            #   MQTT Broker
│   └── sql/             #   数据库初始化 SQL
├── go.mod / go.sum      # Go 模块
├── Makefile             # 构建 / 运行 / 打包
├── tunnel.sh            # sshuttle 隧道（本地开发连接远程 Docker 服务）
├── API.md               # 完整 API 文档
└── README.md            # 本文件
```

---

## 快速开始

### 前置条件

- Go 1.22+
- PostgreSQL 16+
- Redis 7+
- InfluxDB 2.7+
- (可选) MQTT Broker (Mosquitto / EMQX)

### 本地开发

```bash
# 1. 克隆项目
git clone <repo-url> && cd iot-back-rebuild

# 2. 修改配置
cp config/config.yaml config/config.local.yaml
# 编辑 config.local.yaml，填入本地或远程数据库连接信息

# 3. （可选）通过隧道连接远程 Docker 服务
bash tunnel.sh on

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

---

## API 文档

所有接口详见 **[API.md](./API.md)**，涵盖：

- 用户管理（注册/登录/CRUD）
- 设备管理（注册/Token/增删查）
- 数据上报（HTTP / MQTT / WebSocket / UDP）
- 下放命令（实时推送 + 轮询拉取）
- WebSocket 双向通信协议
- ESP32 完整示例代码

---

## 认证体系

| 类型 | Token 名称 | 存储 | 过期 |
|------|-----------|------|------|
| 用户 | `Authorization` | Redis `Authorization:token:xxx` | 30 天 |
| 设备 | `X-Device-Token` | Redis `X-Device-Token:token:xxx` | 永不过期 |

设备 Token 通过 `GET /device/:deviceId/login` 获取（需要用户登录）。

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
