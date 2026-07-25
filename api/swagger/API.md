# IoT Platform API 文档

> Base URL: `https://api.meatsuger.top` / `http://localhost:8182`

---

## 目录

- [1. 认证说明](#1-认证说明)
- [2. WebSocket 端点（重点）](#2-websocket-端点重点)
- [3. 用户接口](#3-用户接口)
- [4. 设备接口](#4-设备接口)
- [5. 数据接口](#5-数据接口)
- [6. MQTT 接口](#6-mqtt-接口)
- [A. 附录](#a-附录)

---

## 1. 认证说明

### 用户认证（UserAuth）

- Header: `Authorization` — 用户登录后获得的 Token（UUID 格式）
- 获取方式: `POST /api/user/login`

### 设备认证（DeviceAuth）

- Header: `X-Device-Token` — 设备注册后通过 login 接口获取的 Token
- 获取方式: `GET /api/device/{deviceId}/login`（使用设备 6 位 hex ID 认证）

### 设备 ID 认证（DeviceIDAuth）

- 路径参数: `{deviceId}` — 6 位十六进制设备 ID
- 用途: 设备获取 Token 时的身份验证（无需额外 Token）

---

## 2. WebSocket 端点（重点）

### 2.1 设备实时下放通道 — `/api/v2/ws/device`

设备建立 WebSocket 连接后，**仅接收**平台下发的命令，不发送数据。

| 项目 | 说明 |
|------|------|
| **端点** | `GET /api/v2/ws/device` |
| **认证** | 必须（Device Token） |
| **数据方向** | **单向**（服务端 → 设备） |
| **用途** | 实时接收平台下发的命令（config / control / ota / message） |

#### 认证方式（4 种，按优先级）

| 优先级 | 方式 | 示例 | 适用场景 |
|--------|------|------|----------|
| 1 | Header `X-Device-Token` | Go/Python 原生客户端 | 服务端 SDK |
| 2 | Header `Authorization` | ESP32 `setAuthorization()` | 嵌入式设备 |
| 3 | Cookie `X-Device-Token` | 浏览器已登录 | Web 调试 |
| 4 | Query `?X-Device-Token=xxx` | `ws://host/api/v2/ws/device?X-Device-Token=xxx` | 浏览器 / 最简单 |

> **推荐 ESP32 使用 Query 方式，最简单可靠。**

#### 设备收到下放命令的消息格式

```json
{
  "type": "cmd",
  "id": 42,
  "cmdType": "control",
  "payload": { "led": true },
  "createdAt": "2026-07-13 20:30:00 +0800"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `type` | string | 固定 `"cmd"` |
| `id` | uint | 命令 ID（用于 ACK 确认） |
| `cmdType` | string | 命令类型: `config` / `control` / `ota` / `message` |
| `payload` | object | 命令载荷（JSON 对象） |
| `createdAt` | string | 命令创建时间 |

#### 设备回应命令（ACK 确认）

设备处理完命令后，通过同一条 WebSocket 发回确认消息。支持两种格式：

**格式一（推荐 ESP32）:**
```json
{"type": "response", "id": 42, "status": "ok"}
```

**格式二（兼容）:**
```json
{"type": "ack", "cmdId": 42}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `type` | string | `"response"` 或 `"ack"` |
| `id` / `cmdId` | uint | 命令 ID（对应 cmd 消息中的 id） |
| `status` | string | 仅 response: `"ok"` / `"skipped"` |

> 收到 ACK 后，服务端将命令状态从 `pending` 更新为 `delivered`。

#### 设备可发送的上行消息

设备 WebSocket 还支持发送以下类型的上行消息：

**传感器数据上报:**
```json
{"type": "data", "sensors": [{"name": "temp", "type": "temperature", "value": 25.5}]}
```

**心跳:**
```json
{"type": "ping"}
```

#### 客户端示例

**ESP32 (Arduino WebSockets 库):**

```cpp
#include <WebSocketsClient.h>

const char *WSS_HOST = "api.meatsuger.top";
const char *WSS_URL = "/api/v2/ws/device";

WebSocketsClient webSocket;
String token = "your-device-token-uuid";

void setup() {
    // 方式 A: Query 参数（推荐）
    String wsUrl = String("/api/v2/ws/device?X-Device-Token=") + token;
    webSocket.beginSSL("api.meatsuger.top", 443, wsUrl.c_str());

    // 方式 B: Authorization 头
    // webSocket.beginSSL("api.meatsuger.top", 443, "/api/v2/ws/device");
    // webSocket.setAuthorization(token.c_str());

    webSocket.onEvent(webSocketEvent);
}

void webSocketEvent(WStype_t type, uint8_t *payload, size_t length) {
    if (type == WStype_TEXT) {
        // 解析下发命令
        // ...
        // 发送 ACK
        String ack = "{\"type\":\"response\",\"id\":" + String(cmdId) + ",\"status\":\"ok\"}";
        webSocket.sendTXT(ack);
    }
}
```

**浏览器:**
```js
const token = 'your-device-token';
const ws = new WebSocket(`wss://api.meatsuger.top/api/v2/ws/device?X-Device-Token=${token}`);

ws.onmessage = (event) => {
    const cmd = JSON.parse(event.data);
    console.log('收到命令:', cmd);
    // 处理命令后发 ACK
    ws.send(JSON.stringify({ type: 'response', id: cmd.id, status: 'ok' }));
};
```

**Go 客户端:**
```go
header := http.Header{}
header.Set("X-Device-Token", deviceToken)
conn, _, err := websocket.DefaultDialer.Dial("wss://api.meatsuger.top/api/v2/ws/device", header)
```

---

### 2.2 MQTT 转发通道 — `/api/v2/ws/mqtt`

| 项目 | 说明 |
|------|------|
| **端点** | `GET /api/v2/ws/mqtt` |
| **认证** | 无 |
| **数据方向** | **双向**（读写） |
| **用途** | WebSocket → MQTT Broker 消息转发 |

#### 客户端 → 服务端（发布）

```json
{"topic": "iot/90431b/telemetry", "qos": 1, "payload": "{\"temp\":25}"}
```

#### 服务端 → 客户端（收到 MQTT 消息时广播）

```json
{"topic": "iot/90431b/telemetry", "payload": "...", "qos": 1, "retained": false}
```

> 注意：该端点默认不启用认证，生产环境需评估安全风险。

---

## 3. 用户接口

### 3.1 注册 `POST /api/user/register`

```
Content-Type: application/json

{
  "account": "meatsuger",
  "passwd": "123456",
  "name": "可选姓名",
  "email": "可选邮箱"
}
```

**响应:**
```json
{"code": 200, "data": {"id": 1, "account": "meatsuger", ...}, "message": "success"}
```

### 3.2 登录 `POST /api/user/login`

```
Content-Type: application/json

{"account": "meatsuger", "passwd": "123456"}
```

**响应:**
```json
{
  "code": 200,
  "data": {
    "token": "uuid-token-string",
    "userInfo": { "id": 1, "account": "meatsuger", "name": "...", ... }
  },
  "message": "success"
}
```

> 后续请求将 `token` 放在 `Authorization` Header 中即可。

### 3.3 检查登录状态 `GET /api/user/isLogin`

无需认证，检查当前 Cookie/Header 中的 Token 是否有效。

### 3.4 退出 `POST /api/user/logout`

需要 `Authorization` Header。

### 3.5 更新信息 `PUT /api/user`

需要 `Authorization` Header。

```json
{"name": "新名字", "email": "new@email.com"}
```

### 3.6 获取个人信息 `GET /api/user/profile?id=1`

需要 `Authorization` Header。

### 3.7 用户列表 `GET /api/user/list`

需要 `Authorization` Header。

### 3.8 分页查询 `GET /api/user/page?page=1&size=10&name=keyword`

需要 `Authorization` Header。

### 3.9 删除 `POST /api/user/delete?id=1`

需要 `Authorization` Header。

---

## 4. 设备接口

### 4.1 注册设备 `POST /api/device/register`

需要 `Authorization` Header。

```json
{
  "deviceName": "ESP32温湿度传感器",
  "deviceType": "ESP32",
  "firmwareVersion": "1.0.0",
  "ipAddress": "192.168.1.100",
  "location": "客厅",
  "macAddress": "AA:BB:CC:DD:EE:FF"
}
```

**响应:**
```json
{
  "code": 200,
  "data": {
    "deviceId": "90431b",
    "deviceName": "ESP32温湿度传感器",
    ...
  },
  "message": "success"
}
```

> `deviceId` 是 6 位十六进制 ID，设备后续操作的核心标识。

### 4.2 获取设备 Token `GET /api/device/{deviceId}/login`

设备使用 6 位 hex ID 认证（无需其他 Token）。

**响应:**
```json
{
  "code": 200,
  "data": {
    "token": "uuid-device-token",
    "deviceId": "90431b"
  },
  "message": "success"
}
```

> 获取的 `token` 用于 WebSocket 连接和数据上报的 `X-Device-Token` Header。

### 4.3 获取设备详情 `GET /api/device/{deviceId}/Data`

需要 `Authorization` Header。

### 4.4 设备列表 `GET /api/device/list`

需要 `Authorization` Header，返回当前用户的所有设备。

### 4.5 删除设备 `POST /api/device/{deviceId}/delete`

需要 `Authorization` Header。

### 4.6 下发命令 `POST /api/device/{deviceId}/cmd`

需要 `Authorization` Header。

```json
{
  "type": "control",
  "payload": { "led": true }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `type` | string | `config` / `control` / `ota` / `message` |
| `payload` | object | JSON 对象（非字符串） |

**响应:**
```json
{
  "code": 200,
  "data": {
    "id": 42,
    "type": "control",
    "payload": { "led": true }
  },
  "message": "命令已下发"
}
```

> 命令会通过 WebSocket 实时推送给设备（如果设备在线），同时写入 Redis 队列供设备 HTTP 轮询。

### 4.7 设备拉取命令 `GET /api/device/{deviceId}/cmd`

需要设备 `X-Device-Token` Header。

**响应:**
```json
{
  "code": 200,
  "data": [
    {
      "id": 42,
      "type": "control",
      "payload": { "led": true },
      "createdAt": "2026-07-13 20:30:00"
    }
  ]
}
```

> 拉取后命令从队列中删除，数据库状态标记为 `sent`。设备处理完后应通过 WebSocket 发 ACK，状态变为 `delivered`。

---

## 5. 数据接口

### 5.1 上报传感器数据 `POST /api/data/{deviceId}/Data`

需要设备 `X-Device-Token` Header。

```json
{
  "sensors": [
    { "name": "temperature", "type": "temperature", "value": 25.5 },
    { "name": "humidity",    "type": "humidity",    "value": 68.2 }
  ]
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `sensors[].name` | string | 传感器名称 |
| `sensors[].type` | string | 传感器类型 |
| `sensors[].value` | any | 传感器值（数值/字符串/布尔） |
| `sensors[].timestamp` | string | 可选，时间戳 |

### 5.2 心跳 `POST /api/data/{deviceId}/heartbeat`

需要设备 `X-Device-Token` Header。更新设备在线状态。

### 5.3 查询传感器数据 `GET /api/data/{deviceId}/Data/list?limit=100`

### 5.4 通用数据查询 `GET /api/data/list`

### 5.5 InfluxDB 连通性检查 `POST /api/data/ping`

公开接口。

---

## 6. MQTT 接口

> **默认开发环境关闭**（`config.yaml` 中 `mqtt.enabled: false`），生产环境开启。

### 客户端管理 `/api/mqtt/client/*`

全部需要 `Authorization` Header。

| 方法 | 端点 | 说明 |
|------|------|------|
| POST | `/connect` | 连接 MQTT Broker |
| POST | `/disconnect` | 断开连接 |
| POST | `/subscribe` | 订阅主题 `{"topic":"...","qos":1}` |
| POST | `/unsubscribe` | 取消订阅 `{"topic":"..."}` |
| POST | `/publish` | 发布消息 `{"topic":"...","payload":"...","qos":1}` |
| GET | `/status` | 查看客户端状态 |
| GET | `/messages` | 获取最近消息 |

### 设备 MQTT 通道 `/api/mqtt/{deviceId}/*`

需要设备 `X-Device-Token` Header。

| 方法 | 端点 | 说明 |
|------|------|------|
| POST | `/Data` | MQTT 方式上报传感器数据 |
| POST | `/heartbeat` | MQTT 方式心跳 |

---

## A. 附录

### 命令状态流转

```
pending ──→ sent ──→ delivered
  │            │
  │   (WS推送到设备 / 设备HTTP拉取)
  │                          │
  │               (设备通过WS发ACK)
```

| 状态 | 说明 |
|------|------|
| `pending` | 已下发，等待设备消费 |
| `sent` | 已推送到设备（Redis 队列被拉取）/ WebSocket 已推送 |
| `delivered` | 设备通过 WebSocket 确认收到 |

### 设备数据上报通道对比

| 通道 | 端点 | 协议 | 认证 | 适用场景 |
|------|------|------|------|----------|
| HTTP | `/api/data/{deviceId}/Data` | HTTP POST | Device Token | 常规上报 |
| MQTT | `iot/+/telemetry` | MQTT | Broker 认证 | 高频 / 低功耗 |
| WebSocket | `/api/v2/ws/device` | WSS | Device Token | 上报 + 实时接收命令 |

### 设备实时下放流程

```
用户/平台                  IoT Backend                 ESP32 设备
    |                          |                          |
    |-- POST /cmd (JSON) ----->|                          |
    |                          |-- 存 DB (pending)        |
    |                          |-- 入 Redis 队列          |
    |                          |-- WS 推送 cmd ──────────>|
    |                          |                          |-- 执行命令
    |                          |<── WS ACK (response) ────|
    |                          |-- DB → delivered         |
    |<--- "命令已下发" --------|                          |
```
