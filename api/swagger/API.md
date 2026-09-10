# IoT Platform API 参考

> **Base URL**: `https://api.meatsuger.top`（生产）· `http://localhost:8182`（开发）
>
> **版本**: 1.8.0 ｜ **更新时间**: 2026-09-10
>
> 本文档参照 Google API 改进提案（AIP）风格组织：资源导向设计（AIP-121）、标准方法（AIP-131 ~ AIP-135）、字段行为标注（AIP-203）、错误模型（AIP-193）、文档规范（AIP-192）。
>
> **平台约定**：全 API 仅使用 GET / POST 两个 HTTP 动词（嵌入式客户端兼容），见 [2.6](#26-http-动词约定)。

---

## 目录

- [1. 概述](#1-概述)
- [2. 通用约定](#2-通用约定)
- [3. 认证与授权](#3-认证与授权)
- [4. 资源模型](#4-资源模型)
- [5. 方法参考](#5-方法参考)
  - [5.1 System 资源](#51-system-资源)
  - [5.2 User 资源](#52-user-资源)
  - [5.3 Device 资源](#53-device-资源)
  - [5.4 SensorData 资源](#54-sensordata-资源)
  - [5.5 DownlinkCmd 资源](#55-downlinkcmd-资源)
  - [5.6 DeviceConfig 资源](#56-deviceconfig-资源)
  - [5.7 Sensor 资源](#57-sensor-资源)
- [6. 实时通道](#6-实时通道)
- [7. 附录](#7-附录)

---

## 1. 概述

IoT Platform API 是一套面向**设备接入与管理**的 REST API，围绕四类资源组织：

| 资源 | 资源名格式 | 说明 |
|------|-----------|------|
| `User` | `users/{userId}` | 平台用户，设备的属主 |
| `Device` | `devices/{deviceId}` | 接入平台的终端设备，`deviceId` 为 6 位十六进制串 |
| `Sensor` | `devices/{deviceId}/sensors/{sensorId}` | 设备传感器定义（物模型），`sensorId` 为设备内唯一标识符 |
| `Actuator` | `devices/{deviceId}/actuators/{actuatorId}` | 设备执行器定义（物模型），`actuatorId` 为设备内唯一标识符 |
| `SensorData` | `devices/{deviceId}/sensorData` | 设备上报的传感器时序数据（存储于 InfluxDB） |
| `DownlinkCmd` | `devices/{deviceId}/commands/{cmdId}` | 平台向设备下发的控制命令 |
| `DeviceConfig` | `devices/{deviceId}/config` | 设备配置快照（云端期望配置，版本化下发） |

方法分为三类（AIP-130）：

- **标准方法**：Get / List / Create，具有一致的语义与签名；
- **自定义后缀方法**：Update / Delete —— 受[仅 GET / POST 约定](#26-http-动词约定)限制，不使用 PUT / DELETE 动词，改用 `POST + /update`、`POST + /delete` 自定义后缀表达，语义与标准 Update / Delete 方法一致；
- **自定义方法**：如 `login`（签发 Token）、`token`（获取设备 Token）、`commands`（下发 / 拉取命令）、`config`（配置存储与下发）、传感器数据上报、心跳等，映射到适合设备接入语义的 HTTP 动词（同样仅 GET / POST）。

资源与方法总览：

```
users/{userId}                          ← 用户资源
  Create        POST   /api/users                          （标准：Create）
  Login         POST   /api/users/login                    （自定义：签发 Token）
  IsLogin       GET    /api/users/isLogin                  （自定义：会话检查）
  Logout        POST   /api/users/logout                   （自定义：吊销 Token）
  List          GET    /api/users                          （标准：List，管理员；可选分页参数）
  Get           GET    /api/users/{userId}                 （标准：Get，{userId} 支持 me）
  Update        POST   /api/users/{userId}/update          （POST 自定义后缀方法：Update，仅 GET/POST 约定）
  Delete        POST   /api/users/{userId}/delete          （POST 自定义后缀方法：Delete，仅 GET/POST 约定）

devices/{deviceId}                      ← 设备资源
  Create        POST   /api/devices                        （标准：Create）
  List          GET    /api/devices                        （标准：List）
  Get           GET    /api/devices/{deviceId}             （标准：Get）
  Update        POST   /api/devices/{deviceId}/update      （POST 自定义后缀方法：Update，增量）
  Delete        POST   /api/devices/{deviceId}/delete      （POST 自定义后缀方法：Delete）
  GetToken      GET    /api/devices/{deviceId}/token       （自定义：签发设备 Token，/login 为兼容别名）

devices/{deviceId}/sensorData           ← 传感器数据（不可变时序数据）
  Report        POST   /api/devices/{deviceId}/sensorData  （自定义：批量写入）
  List          GET    /api/devices/{deviceId}/sensorData  （标准：List）
  Heartbeat     POST   /api/devices/{deviceId}/heartbeat   （自定义：保活，/ping 为兼容别名）

devices/{deviceId}/commands             ← 下行命令
  Post          POST   /api/devices/{deviceId}/commands    （自定义：下发）
  Pull          GET    /api/devices/{deviceId}/commands    （自定义：设备拉取）

devices/{deviceId}/config               ← 设备配置快照
  Get           GET    /api/devices/{deviceId}/config      （自定义：查询期望配置，用户/设备双认证）
  Set           POST   /api/devices/{deviceId}/config      （自定义：设置并下发，版本递增）
  Report        POST   /api/devices/{deviceId}/config/report （自定义：设备回执）

devices/{deviceId}/sensors              ← 传感器定义（物模型）
  List          GET    /api/devices/{deviceId}/sensors     （标准：List）
  Get           GET    /api/devices/{deviceId}/sensors/{sensorId} （标准：Get）
  Create        POST   /api/devices/{deviceId}/sensors     （标准：Create）
  Update        POST   /api/devices/{deviceId}/sensors/{sensorId}/update （POST 自定义后缀方法：Update，增量）
  Delete        POST   /api/devices/{deviceId}/sensors/{sensorId}/delete （POST 自定义后缀方法：Delete）
  Apply         POST   /api/devices/{deviceId}/sensors/apply （自定义：编译进 config 并版本化下发）

devices/{deviceId}/actuators           ← 执行器定义（物模型）
  List          GET    /api/devices/{deviceId}/actuators     （标准：List）
  Get           GET    /api/devices/{deviceId}/actuators/{actuatorId} （标准：Get）
  Create        POST   /api/devices/{deviceId}/actuators     （标准：Create）
  Update        POST   /api/devices/{deviceId}/actuators/{actuatorId}/update （POST 自定义后缀方法：Update，增量）
  Delete        POST   /api/devices/{deviceId}/actuators/{actuatorId}/delete （POST 自定义后缀方法：Delete）
  Apply         POST   /api/devices/{deviceId}/actuators/apply （自定义：编译进 config 并版本化下发）
```

---

## 2. 通用约定

### 2.1 统一响应结构

所有 HTTP 接口（**含错误**）均返回 HTTP 200，业务结果由 `code` 字段表达（错误模型见 AIP-193 的业务码映射）：

```json
{
  "code": 200,
  "message": "success",
  "data": {}
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `code` | int | 业务状态码，见 [2.2](#22-业务状态码) |
| `message` | string | 人类可读的结果描述 |
| `data` | any | 业务数据；无数据时为 `null` |

> 例外：`GET /health` 面向容器编排探针，直接返回 HTTP 语义响应，不走统一结构。

### 2.2 业务状态码

| code | HTTP 语义 | 说明 | 典型场景 |
|------|-----------|------|----------|
| 200 | OK | 成功 | — |
| 400 | INVALID_ARGUMENT | 请求参数错误 | 缺少必填字段、JSON 格式非法、字段超长 |
| 401 | UNAUTHENTICATED | 未认证 | Token 缺失 / 无效 / 已过期 / 被踢下线 |
| 403 | PERMISSION_DENIED | 无权限 | 操作他人资源、非管理员访问管理接口、设备 Token 与路径设备不匹配 |
| 404 | NOT_FOUND | 资源不存在 | 设备 / 用户不存在 |
| 500 | INTERNAL | 服务器内部错误 | 数据库 / 缓存 / 时序库异常 |

### 2.3 时间格式

- 响应中的时间统一为 Go 参考布局 `2006-01-02T15:04:05.000-07:00`（毫秒精度，带时区偏移），如 `2026-08-31T20:30:00.000+08:00`；
- 查询参数中的时间使用 RFC 3339 格式，如 `2026-08-31T00:00:00+08:00`。

### 2.4 资源 ID

| 资源 | ID 格式 | 生成方 | 可变性 |
|------|---------|--------|--------|
| User | 自增 uint | 服务端 | 不可变 |
| Device | **6 位十六进制字符串**（如 `90431b`） | 服务端（注册时生成） | 不可变（AIP-136） |

路径参数中的 `{deviceId}` **大小写不敏感**，服务端统一归一化为小写。

### 2.5 分页（AIP-158）

用户列表 `GET /api/users` 支持**可选**页码分页（原 list / page 两个接口已合并）：

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `pageNum` | int | `0` | 页码，**从 0 开始**（仅 `pageSize > 0` 时生效） |
| `pageSize` | int | `0` | 每页数量；**大于 0 时启用分页** |
| `name` | string | — | 按用户名模糊过滤 |

**双响应形态**（`pageSize` 决定）：

- `pageSize > 0`：返回分页 envelope，`data` 结构为 `records`（当前页数据）、`total`（总数）、`size`（每页数量）、`current`（当前页码）、`pages`（总页数）；
- `pageSize` 缺省或 `<= 0`：返回**全量**用户数组（`data` 为 User 资源数组，可被 `name` 过滤）。

> 设备传感器数据查询（5.4）采用 `limit + 时间窗口` 截断式分页（结果按时间倒序）。

### 2.6 HTTP 动词约定

**仅使用 GET / POST 两个 HTTP 动词**（兼容能力受限的嵌入式客户端）：

- 所有**写操作**（创建 / 更新 / 删除 / 命令下发 / 数据上报 / 登录登出等）一律使用 `POST`；
- **更新与删除**通过 `POST + /update`、`POST + /delete` 自定义后缀表达（如 `POST /api/devices/{deviceId}/update`、`POST /api/users/{userId}/delete`），语义与标准 Update / Delete 方法一致；
- `GET` 仅用于**读取 / 幂等操作**（Get / List / 会话检查 / Token 获取 / 命令拉取）。

> 不使用 PUT / PATCH / DELETE 动词。

---

## 3. 认证与授权

### 3.1 认证方式总览

| 方式 | 凭证 | 传递方式 | 适用 |
|------|------|----------|------|
| **UserAuth** | 用户 Token（UUID） | Header `Authorization` / Cookie `Authorization` | 用户管理接口 |
| **DeviceAuth** | 设备 Token（UUID） | Header `X-Device-Token` / Cookie / Query | 数据上报、心跳、命令拉取 |
| **UserOrDeviceAuth** | 两者之一 | 上述任一方式 | 设备更新接口 |
| **DeviceIDAuth** | 无 Token | 路径参数 `{deviceId}`（6 位 hex） | 设备获取 Token |

双通道接口（如设备更新）优先识别用户 Token；用户 Token 无效时回退识别设备 Token；两者均无效返回 401。

### 3.2 用户 Token

- 获取：`POST /api/users/login`，返回 `tokenValue`；同时下发 `Authorization` Cookie（httpOnly + SameSite=Lax，release 环境启用 secure）；
- **有效期：3 天**（72 小时），响应 `tokenTimeout` 字段与 Cookie MaxAge 均为 `259200` 秒，与实际 TTL 一致；
- **多端登录**：同一账号最多 **5 个设备端**同时在线（`MaxLoginCount=5`）：
  - 登录请求可携带 `device` 字段标识设备端（推荐传稳定唯一值，如前端 localStorage 生成的 UUID）；
  - 未携带时服务端回退取 `User-Agent` 生成标识；两者皆无则所有登录视为**同一设备端**，此时多端上限不生效；
  - 第 6 个设备端登录时，**最旧设备端**的 Token 被自动登出；
- 每次登录生成**独立** Token（`IsShare=false`），不做 Token 复用；
- 被顶号 / 被逐出的 Token **立即从服务端删除**，继续使用返回 401。

### 3.3 设备 Token

- 获取：`GET /api/devices/{deviceId}/token`（仅需 6 位 hex 设备 ID，无需其他凭证；兼容别名 `GET /api/devices/{deviceId}/login`，行为完全一致）；
- **有效期：永久**（`NeverExpire`），但设备 **30 天未上线**会被定时任务清理 Token（**不删除设备**，重新获取 Token 即可）；
- **复用语义**：同一 deviceId 的有效期内重复调用，返回**同一个 Token**（不会轮换）；需要轮换时先登出（服务端调用 Logout）再重新获取才会签发新 Token；
- 旧 Token 被替换 / 被清理时**立即物理删除**，不再残留标记。

### 3.4 授权规则

| 操作 | 规则 |
|------|------|
| 更新 / 删除 / 查看设备 | 仅设备属主（UserAuth），或设备本身（DeviceAuth 且 Token 与路径设备一致） |
| 下发命令 | 仅设备属主 |
| 用户列表 / 用户状态修改 | 仅 `admin` / `super-admin` 角色 |
| 修改 / 删除用户 | 本人，或管理员 |

---

## 4. 资源模型

字段行为标注（AIP-203）：`REQUIRED` 必填 · `OPTIONAL` 可选 · `OUTPUT_ONLY` 仅由服务端输出 · `IMMUTABLE` 创建后不可变。

### 4.1 User

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `id` | uint | OUTPUT_ONLY | 用户 ID |
| `account` | string | REQUIRED（注册）· IMMUTABLE | 登录账号，3-50 字符，全平台唯一 |
| `passwd` | string | REQUIRED（注册）· OUTPUT_ONLY | 密码（bcrypt 存储，响应中不返回） |
| `name` | string | OPTIONAL | 姓名 |
| `email` | string | OPTIONAL | 邮箱 |
| `age` | int | OPTIONAL | 年龄 |
| `role` | string | OUTPUT_ONLY | 角色：`user` / `admin` / `super-admin` |
| `status` | string | OPTIONAL | 状态：`ACTIVE` / `DISABLED`（仅管理员可改） |
| `createTime` / `updateTime` | string | OUTPUT_ONLY | 创建 / 更新时间 |

### 4.2 Device

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `deviceId` | string | OUTPUT_ONLY · IMMUTABLE | 6 位 hex 设备 ID，资源标识 |
| `deviceName` | string | Create: REQUIRED · Update: OPTIONAL | 设备名称，≤100 字符 |
| `deviceType` | string | OPTIONAL | 设备类型，≤50 字符 |
| `firmwareVersion` | string | OPTIONAL | 固件版本，≤50 字符 |
| `ipAddress` | string | OPTIONAL | IP 地址，≤45 字符（兼容 IPv6） |
| `macAddress` | string | OPTIONAL | MAC 地址，≤17 字符 |
| `location` | string | OPTIONAL | 位置，≤255 字符 |
| `ownerId` | uint | OUTPUT_ONLY · IMMUTABLE | 属主用户 ID（由登录态推导，请求体不可指定） |
| `status` | string | OUTPUT_ONLY | 运行时状态：`ONLINE` / `OFFLINE` / `ACTIVE`（由心跳与离线检测维护） |
| `lastActiveTime` | string | OUTPUT_ONLY | 最后活跃时间 |
| `createdAt` / `updatedAt` | string | OUTPUT_ONLY | 创建 / 更新时间 |

> **增量更新语义**：Update 方法仅修改请求体中**出现**的字段，未出现的字段保持原值；传空串视为显式清空该字段。

### 4.3 SensorData（时序数据，不可变）

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `name` | string | REQUIRED | 传感器名称 |
| `type` | string | REQUIRED | 传感器类型（如 `temperature`） |
| `value` | any | REQUIRED | 值（数值 / 字符串 / 布尔） |
| `timestamp` | string | OPTIONAL | 采样时间，缺省为服务端接收时间 |

存储于 InfluxDB；最新一条缓存于 Redis。数据一经写入不可修改。

### 4.4 DownlinkCmd

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `id` | uint | OUTPUT_ONLY | 命令 ID（ACK 回执使用） |
| `deviceId` | string | OUTPUT_ONLY | 目标设备 |
| `type` | string | REQUIRED | 命令类型：`config` / `control` / `ota` / `message` |
| `payload` | object | REQUIRED | JSON 对象载荷（非字符串） |
| `status` | string | OUTPUT_ONLY | 状态：`pending` / `sent` / `delivered`，见[附录流转图](#命令状态流转) |
| `createdAt` | string | OUTPUT_ONLY | 创建时间 |

> 存储：命令统一持久化在 `iot_message_log`（`direction=down`，`category=config`/`cmd`），下发状态由 `status` 列承载。MQTT 网关上行的设备发布消息写入同一张表的 `direction=up` 行（`category=telemetry`/`config_report`/`other`）。

### 4.5 DeviceConfig（设备配置快照）

每个设备维护一份「当前期望配置」快照，云端每次编辑 `version` 递增并下发；设备回执回写 `reported*` 字段并置 `status=acked`。

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `deviceId` | string | OUTPUT_ONLY · IMMUTABLE | 目标设备（资源标识） |
| `version` | uint | OUTPUT_ONLY | 期望配置版本，云端每次设置递增（从 1 起） |
| `payload` | object | REQUIRED（Set） | 期望配置 JSON 对象（分 protocol 区块，见下） |
| `status` | string | OUTPUT_ONLY | 下发确认态：`pending` / `acked` |
| `reportedVersion` | uint | OUTPUT_ONLY | 设备已生效的配置版本（0=未上报） |
| `reportedPayload` | object | OUTPUT_ONLY | 设备实际生效的配置（回执回写） |
| `updatedAt` | string | OUTPUT_ONLY | 最近更新时间 |

`payload` 按设备协议分区（参考新大陆 / 联犀等平台风格），示例：

```json
{
  "network":  { "wifi": {"ssid": "", "password": ""},
                "mqtt": {"host": "", "port": 1883, "tls": false} },
  "sensor":   { "reportInterval": 60 },
  "sensors":  [ {"id": "temperature", "type": "temperature",
                 "dataType": "float", "unit": "°C",
                 "specs": {"min": -40, "max": 125, "step": 0.1,
                            "thresholds": {"min": 0, "max": 100, "alarm": true}},
                 "enabled": true} ],
  "actuators": [ {"id": "servo1", "driver": "servo", "enabled": true,
                   "specs": {"gpio": 18, "min_pulse_us": 500,
                              "max_pulse_us": 2500, "min_angle": 0,
                              "max_angle": 180}} ],
  "camera":   { "protocol": "smtp",
                "smtp": {"host": "smtp.example.com", "port": 465, "ssl": true,
                         "username": "", "password": ""},
                "snapshotInterval": 30 },
  "ota":      { "fwUrl": "", "fwVersion": "", "md5": "" }
}
```

> 分区说明：`network` 网络连接、`sensor` 传感器全局采样周期（物模型键清理后仅剩 `reportInterval`，告警阈值已收敛到每传感器 `specs.thresholds`）、`sensors` 传感器定义列表（**下发裁剪版**：仅 `id/type/dataType/unit/specs/reportInterval/enabled`，无管理字段；继承全局周期的传感器省略 `reportInterval`，见 [4.6](#46-sensor传感器定义物模型)，由 `POST /sensors/apply` 编译写入）、`actuators` 执行器定义列表（**下发裁剪版**：仅 `id/driver/specs/enabled`；`id` = 设备侧执行器名 = 控制命令 `action`，`specs` 为驱动参数，见 [4.7](#47-actuator执行器定义--物模型)）、`camera` 摄像头协议（SMTP/RTSP/ONVIF 等）、`ota` 固件升级（**预留扩展点，暂不实现升级流程**）。`payload` 为整体快照，`version` 标识其代数。

### 4.6 Sensor（传感器定义 / 物模型）

传感器定义描述「设备里有哪些传感器、如何采样、何时告警」，是设备物模型的组成部分。设计参考新大陆 NLECloud 传感器模型（`ApiTag`/`Name`/`DataType`/`TypeAttrs`）与阿里云 IoT TSL 物模型（`identifier`/`dataType`/`specs`）。

定义本身仅持久化（`iot_device_thing` 表（kind=sensor）），经 `Apply` 方法编译进 `DeviceConfig.payload.sensors` 后，复用现有 `type=config` 下行通道版本化下发；设备回执经 `POST /config/report` 回写，实现「期望 vs 实际」比对。

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `id` | string | Create: REQUIRED · IMMUTABLE | 传感器标识符：字母开头，仅含字母/数字/下划线，≤50 字符（对应新大陆 `ApiTag`），设备内唯一 |
| `name` | string | Create: REQUIRED · Update: OPTIONAL | 传感器名称，≤100 字符 |
| `type` | string | Create: REQUIRED · Update: OPTIONAL | 传感器类别，≤50 字符（如 `temperature` / `humidity` / `light` / `switch` / `custom`） |
| `dataType` | string | OPTIONAL | 值类型：`float`（默认）/ `int` / `bool` / `text` / `enum`。**上报值按此校验**：类型不符的数据点被丢弃并告警（见 6.3 设备上报） |
| `unit` | string | OPTIONAL | 单位，≤32 字符（如 `°C` / `%RH`） |
| `specs` | object | OPTIONAL | **统一定义体（强类型）**：原 `specs`/`thresholds`/`attrs` 合并为一个对象——`float/int` 用 `min/max/step`，`enum` 用 `values`，`text` 用 `maxLen`，告警用 `thresholds{min,max,…}`，其余自由键平铺透传（原 `attrs` 能力）；已知键类型错误直接拒绝；传 `{}` 显式清空 |
| `reportInterval` | int / null | OPTIONAL | 采样/上报周期（秒，>0）；**`null` = 继承设备级全局周期**（`DefaultDeviceConfig.sensor.reportInterval`）。Update 传 `null` 恢复继承 |
| `enabled` | bool | OPTIONAL | 是否启用，默认 `true`（禁用后设备应停止该传感器采样） |
| `createdAt` / `updatedAt` | string | OUTPUT_ONLY | 创建 / 更新时间 |

**完整示例**

```json
{
  "id": "temperature",
  "name": "温度",
  "type": "temperature",
  "dataType": "float",
  "unit": "°C",
  "specs": {"min": -40, "max": 125, "step": 0.1,
             "thresholds": {"min": 0, "max": 100, "alarm": true},
             "gpio": 4, "driver": "dht22"},
  "reportInterval": 60,
  "enabled": true
}
```

> **增量更新语义**：Update 方法仅修改请求体中**出现**的字段；`specs` 传 `{}` 视为显式清空，`reportInterval` 传 `null` 恢复继承全局周期。`id` 为资源标识，创建后不可变（AIP-136）。

> **键清理（2026 统一）**：`thresholds` / `attrs` 顶层字段已删除（并入 `specs`），全局 `sensor.thresholds` 已删除；存量 DB 列迁移见 `deployments/sql/init.sql` 注释。

### 4.7 Actuator（执行器定义 / 物模型）

执行器定义描述「设备上有哪些可执行动作的部件、用什么驱动、接在哪个引脚」，与 Sensor 同构：定义仅持久化（`iot_device_thing` 表（kind=actuator）），经 `Apply` 编译进 `DeviceConfig.payload.actuators` 后版本化下发；设备据此 diff 实例化/卸载执行器，运行期动作由 `type=control` 命令按 `action=id` 路由到驱动执行。

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `id` | string | Create: REQUIRED · IMMUTABLE | 执行器标识符：小写字母开头，仅含小写字母/数字/下划线，**≤11 字符**（固件 periph 设备名 / 控制命令 `action` 契约），设备内唯一 |
| `name` | string | OPTIONAL | 执行器名称，≤100 字符 |
| `driver` | string | Create: REQUIRED · Update: OPTIONAL | 驱动名：`led` / `servo` / `speaker`（与固件驱动对齐，未来可扩展） |
| `specs` | object | OPTIONAL | 驱动参数（GPIO/数量/脉宽范围等，**命名已统一**：原 `config` 改名为 `specs`，与 Sensor 定义体同名；结构见固件协议文档，平台侧不过度约束）；传 `{}` 显式清空 |
| `enabled` | bool | OPTIONAL | 是否启用，默认 `true`（禁用 = 期望设备卸载该执行器） |
| `createdAt` / `updatedAt` | string | OUTPUT_ONLY | 创建 / 更新时间 |

**完整示例**

```json
{
  "id": "servo1",
  "name": "云台舵机",
  "driver": "servo",
  "specs": {"gpio": 18, "min_pulse_us": 500, "max_pulse_us": 2500,
            "min_angle": 0, "max_angle": 180},
  "enabled": true
}
```

**方法**（全部 UserAuth）：

| 方法 | HTTP | 说明 |
|------|------|------|
| List | `GET /api/devices/{deviceId}/actuators` | 执行器定义列表 |
| Get | `GET /api/devices/{deviceId}/actuators/{actuatorId}` | 单个执行器定义 |
| Create | `POST /api/devices/{deviceId}/actuators` | 创建定义（仅持久化） |
| Update | `POST /api/devices/{deviceId}/actuators/{actuatorId}/update` | 增量更新 |
| Delete | `POST /api/devices/{deviceId}/actuators/{actuatorId}/delete` | 删除定义 |
| Apply | `POST /api/devices/{deviceId}/actuators/apply` | 编译进 `payload.actuators` 版本化下发（响应 `{deviceId, version, status, count}`） |

**请求 / 响应示例**

```json
// Create
{"id": "servo1", "name": "云台舵机", "driver": "servo",
 "specs": {"gpio": 18, "min_pulse_us": 500, "max_pulse_us": 2500},
 "enabled": true}

// Apply 响应
{"code": 200, "message": "执行器配置已下发",
 "data": {"deviceId": "90431b", "version": 3, "status": "pending", "count": 2}}
```

**错误码**：400（校验失败 / `id` 已存在）、403（非属主）、404（设备或定义不存在）。

> 下发的 `actuators` 数组即**期望列表**：设备 diff 后单向收敛（定义变化重配置、缺失或 `enabled=false` 卸载）；控制动作载荷约定见固件协议文档（`action` = `id`，`value` 随驱动而异）。

---

## 5. 方法参考

每个方法按统一模板描述：**HTTP 请求** → **路径 / 查询参数** → **请求体** → **响应体** → **示例** → **错误码**。

### 5.1 System 资源

#### Health — 健康检查

供容器编排 / 负载均衡探测，无需认证，不走统一响应结构。

**HTTP 请求**

```
GET /health
```

**响应体**

| 字段 | 类型 | 说明 |
|------|------|------|
| `status` | string | 固定 `"ok"` |
| `service` | string | 固定 `"iot-platform"` |
| `time` | string | RFC3339 当前时间 |

**示例**

```json
{"status": "ok", "service": "iot-platform", "time": "2026-08-31T20:30:00+08:00"}
```

---

### 5.2 User 资源

#### Create — 注册用户 `POST /api/users`

创建新用户账号，角色固定为 `user`。

**授权**：无需认证

**请求体**（JSON）：

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `account` | string | REQUIRED | 账号，3-50 字符，唯一 |
| `passwd` | string | REQUIRED | 密码，6-100 字符 |
| `name` | string | OPTIONAL | 姓名 |
| `email` | string | OPTIONAL | 邮箱 |

**示例**

```json
// 请求
{"account": "meatsuger", "passwd": "123456", "name": "可选姓名", "email": "可选邮箱"}

// 响应
{"code": 200, "message": "注册成功", "data": null}
```

**错误码**

| code | 场景 |
|------|------|
| 400 | 参数校验失败；`account` 已存在 |

---

#### Login — 用户登录 `POST /api/users/login`

校验账号密码，签发用户 Token。支持 JSON Body / Query / Form 三种传参方式。

**授权**：无需认证

**请求体**（JSON）：

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `account` | string | REQUIRED | 账号 |
| `passwd` | string | REQUIRED | 密码 |
| `device` | string | OPTIONAL | **设备端标识**，用于多端登录计数（见 [3.2](#32-用户-token)）；缺省回退 `User-Agent` |

**响应体** `data`（LoginResponse）：

| 字段 | 类型 | 说明 |
|------|------|------|
| `tokenName` | string | 固定 `"Authorization"` |
| `tokenValue` | string | 用户 Token，后续放入 `Authorization` Header |
| `isLogin` | bool | 固定 `true` |
| `loginId` | string | 用户 ID（字符串形式） |
| `tokenTimeout` | int64 | Token 有效期秒数（259200 = 3 天），同时为 Cookie MaxAge |
| `loginDevice` | string | 本次登录的设备端标识（归一化后） |
| `userInfo` | object | 用户资源（不含密码） |

**示例**

```json
// 请求
{"account": "meatsuger", "passwd": "123456", "device": "web-chrome-uuid-123"}

// 响应
{
  "code": 200,
  "message": "登录成功",
  "data": {
    "tokenName": "Authorization",
    "tokenValue": "uuid-token-string",
    "isLogin": true,
    "loginId": "1",
    "loginType": "login",
    "tokenTimeout": 259200,
    "sessionTimeout": 259200,
    "tokenSessionTimeout": -2,
    "tokenActivityTimeout": -1,
    "loginDevice": "web-chrome-uuid-123",
    "userInfo": {"id": 1, "account": "meatsuger", "name": "...", "role": "user", "status": "ACTIVE"}
  }
}
```

**错误码**

| code | 场景 |
|------|------|
| 400 | 账号或密码为空；账号不存在；密码错误；账号已被禁用 |

---

#### IsLogin — 检查登录状态 `GET /api/users/isLogin`

检查请求携带的 Token（Header / Cookie）是否有效。

**授权**：无需认证

**响应体** `data`：

| 字段 | 类型 | 说明 |
|------|------|------|
| `isLogin` | bool | 是否已登录 |
| `loginId` | string | 已登录时返回用户 ID |
| `loginDevice` | string | Token 所属设备端标识 |
| `user` | object | 用户资源（已登录时） |

**示例**

```json
// 已登录
{"code": 200, "message": "已登录", "data": {"isLogin": true, "loginId": "1", "loginDevice": "web-chrome-uuid-123", "user": {...}}}

// 未登录
{"code": 200, "message": "未登录", "data": {"isLogin": false}}
```

---

#### Logout — 退出登录 `POST /api/users/logout`

吊销当前请求携带的 Token（仅当前设备端，不影响其他端）。

**授权**：UserAuth

**响应体**：`data` 为 `null`

```json
{"code": 200, "message": "退出登录成功", "data": null}
```

---

#### List — 用户列表 `GET /api/users`

列表与分页合并为一个端点：分页参数全部可选，见 [2.5 分页](#25-分页aip-158)。

**授权**：UserAuth + **管理员**。

**查询参数**（全部可选）：

| 参数 | 类型 | 行为 | 默认 | 说明 |
|------|------|------|------|------|
| `pageNum` | int | OPTIONAL | `0` | 页码，**从 0 开始**（仅 `pageSize > 0` 时生效） |
| `pageSize` | int | OPTIONAL | `0` | 每页数量；**大于 0 时启用分页** |
| `name` | string | OPTIONAL | — | 按用户名模糊过滤 |

**双响应形态**：

- `pageSize > 0`：`data` 为分页 envelope（`records` / `total` / `size` / `current` / `pages`）；
- 其他：`data` 为全量 User 资源数组（可被 `name` 过滤）。

**示例**

```
GET /api/users?pageNum=0&pageSize=10&name=meat
```

```json
{"code": 200, "message": "success", "data": {"records": [{...}], "total": 1, "size": 10, "current": 0, "pages": 1}}
```

```
GET /api/users
```

```json
{"code": 200, "message": "success", "data": [{"id": 1, "account": "meatsuger", "...": "..."}, {...}]}
```

---

#### Get — 获取用户 `GET /api/users/{userId}`

**授权**：UserAuth。`{userId}` 支持 `me` 表示当前登录用户；为数字 ID 时仅本人或管理员可访问（否则 403）。

**路径参数**：

| 参数 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `userId` | string | REQUIRED | 用户 ID（数字），或 `me`（当前登录用户） |

```
GET /api/users/me
```

**响应体** `data`：User 资源（不含密码）。

**错误码**：403（查看他人且非管理员）、404（用户不存在）

---

#### Update — 更新用户 `POST /api/users/{userId}/update`

**授权**：UserAuth。`{userId}` 支持 `me`（当前登录用户）或数字 ID；普通用户只能更新 `me` / 本人 ID，管理员可更新任意用户（否则 403）。

**路径参数**：

| 参数 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `userId` | string | REQUIRED | 用户 ID（数字），或 `me`（当前登录用户） |

**请求体**（JSON，全部 OPTIONAL，仅出现的字段被更新）：

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `name` | string | OPTIONAL | 姓名 |
| `email` | string | OPTIONAL | 邮箱 |
| `age` | int | OPTIONAL | 年龄 |
| `status` | string | OPTIONAL | `ACTIVE` / `DISABLED`（仅管理员） |

**示例**

```json
// 请求
POST /api/users/me/update
{"name": "新名字", "email": "new@email.com"}

// 响应
{"code": 200, "message": "success", "data": {"id": 1, "account": "meatsuger", "name": "新名字", "...": "..."}}
```

**错误码**

| code | 场景 |
|------|------|
| 400 | 试图修改 `account` 等不可变更字段 |
| 403 | 普通用户修改 `status`；普通用户操作他人信息 |
| 404 | 目标用户不存在 |

---

#### Delete — 删除用户 `POST /api/users/{userId}/delete`

删除用户并**吊销其全部设备端的 Token**（最多 5 端同时清除）。

**授权**：UserAuth。`{userId}` 支持 `me` / 本人 ID；管理员可删除任意用户（否则 403）。

**路径参数**：

| 参数 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `userId` | string | REQUIRED | 用户 ID（数字），或 `me`（当前登录用户） |

```
POST /api/users/2/delete
```

---

### 5.3 Device 资源

#### Create — 注册设备 `POST /api/devices`

注册新设备，`deviceId` 与归属用户由服务端推导，请求体不可指定。

**授权**：UserAuth（设备归属当前登录用户）

**请求体**（JSON）：

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `deviceName` | string | REQUIRED | 设备名称 |
| `deviceType` | string | OPTIONAL | 设备类型（如 `ESP32`） |
| `firmwareVersion` | string | OPTIONAL | 固件版本 |
| `ipAddress` | string | OPTIONAL | IP 地址 |
| `macAddress` | string | OPTIONAL | MAC 地址 |
| `location` | string | OPTIONAL | 位置 |

**示例**

```json
// 请求
{
  "deviceName": "ESP32温湿度传感器",
  "deviceType": "ESP32",
  "firmwareVersion": "1.0.0",
  "ipAddress": "192.168.1.100",
  "macAddress": "AA:BB:CC:DD:EE:FF",
  "location": "客厅"
}

// 响应
{
  "code": 200,
  "message": "success",
  "data": {"deviceId": "90431b", "deviceToken": "uuid-device-token"}
}
```

> 注册成功同时下发 `X-Device-Token` Cookie。`deviceToken` 用于数据上报 / WebSocket / MQTT 接入。

---

#### List — 设备列表 `GET /api/devices`

**授权**：UserAuth。返回当前用户的全部设备（`data` 为 Device 资源数组，按 deviceId 倒序）。

---

#### Get — 获取设备详情 `GET /api/devices/{deviceId}`

**授权**：UserAuth（仅设备属主，否则 403）。

**响应体** `data`：完整 Device 资源（含在线状态）+ 物模型视图 `sensors` / `actuators`，服务端已完成 join，**一次请求即可渲染完整设备页**：

- `sensors`：传感器物模型数组，每项 = 传感器定义 + `latest`（最近一次上报值；`null` = 该定义从未上报）；
- `actuators`：执行器物模型数组；
- 设备无物模型定义时两数组均为 `[]`（非 `null`）。

> `status` 优先取 Redis 实时状态（`ONLINE` / `OFFLINE`）。

**示例**

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "deviceId": "90431b",
    "deviceName": "ESP32温湿度传感器",
    "deviceType": "ESP32",
    "status": "ONLINE",
    "ownerId": 1,
    "lastActiveTime": "2026-08-31T20:30:00+08:00",
    "sensors": [
      {"id": "temperature", "name": "温度", "type": "temperature", "dataType": "float",
       "unit": "°C", "specs": {"min": -40, "max": 125, "step": 0.1,
                               "thresholds": {"min": 0, "max": 100, "alarm": true}},
       "reportInterval": 60, "enabled": true,
       "createdAt": "2026-09-04T10:00:00.000+08:00", "updatedAt": "2026-09-04T10:00:00.000+08:00",
       "latest": {"value": 25.5, "timestamp": "2026-08-31T20:30:00.000+08:00"}},
      {"id": "switch_1", "name": "开关", "type": "switch", "dataType": "bool",
       "enabled": true, "createdAt": "2026-09-05T09:00:00.000+08:00", "updatedAt": "2026-09-05T09:00:00.000+08:00",
       "latest": null}
    ],
    "actuators": [
      {"id": "servo1", "name": "云台舵机", "driver": "servo",
       "specs": {"gpio": 18, "min_pulse_us": 500, "max_pulse_us": 2500},
       "enabled": true, "createdAt": "2026-09-05T10:00:00.000+08:00", "updatedAt": "2026-09-05T10:00:00.000+08:00"}
    ]
  }
}
```

> **关联规则**（服务端 join，前端无需自行处理）：上报遥测 `name` 优先匹配传感器定义 `id`（固件契约），其次匹配定义 `name`（存量上报兜底）。

---

#### Update — 更新设备（增量） `POST /api/devices/{deviceId}/update`

**双认证**：UserAuth（仅设备属主）**或** DeviceAuth（设备更新自身，Token 对应的 deviceId 必须与路径一致，否则 403）。

**路径参数**：

| 参数 | 类型 | 说明 |
|------|------|------|
| `deviceId` | string | 6 位 hex 设备 ID |

**请求体**（JSON，全部 OPTIONAL，**增量语义**：仅出现的字段被更新，未传字段保持原值；至少传一个字段）：

| 字段 | 类型 | 行为 |
|------|------|------|
| `deviceName` | string | OPTIONAL |
| `deviceType` | string | OPTIONAL |
| `firmwareVersion` | string | OPTIONAL |
| `ipAddress` | string | OPTIONAL |
| `macAddress` | string | OPTIONAL |
| `location` | string | OPTIONAL |

**示例**

```json
// 请求 —— 只改名称与位置，其余字段不受影响
POST /api/devices/90431b/update
{"deviceName": "客厅温湿度传感器", "location": "客厅"}

// 响应 —— 返回更新后的完整设备资源
{
  "code": 200,
  "message": "success",
  "data": {
    "deviceId": "90431b",
    "deviceName": "客厅温湿度传感器",
    "deviceType": "ESP32",
    "firmwareVersion": "1.0.0",
    "ipAddress": "192.168.1.100",
    "macAddress": "AA:BB:CC:DD:EE:FF",
    "location": "客厅",
    "ownerId": 1,
    "status": "ONLINE",
    "lastActiveTime": "2026-08-31T20:30:00+08:00",
    "createdAt": "2026-07-13T20:30:00+08:00",
    "updatedAt": "2026-08-31T20:30:00+08:00"
  }
}
```

**错误码**

| code | 场景 |
|------|------|
| 400 | 请求体为空 / 无任何可更新字段（"无更新字段"） |
| 403 | DeviceAuth 的 Token 与路径 deviceId 不一致；用户非设备属主 |
| 500 | 设备不存在 / 数据库异常 |

---

#### Delete — 删除设备 `POST /api/devices/{deviceId}/delete`

**授权**：UserAuth（仅设备属主，否则 403）。

级联清理：设备全部缓存、命令队列、MQTT 消息历史。

```json
{"code": 200, "message": "删除成功", "data": null}
```

---

#### GetToken — 获取设备 Token `GET /api/devices/{deviceId}/token`

别名端点：`GET /api/devices/{deviceId}/login`（兼容别名，行为完全一致）。

**授权**：DeviceIDAuth（仅需 6 位 hex 设备 ID）。

**Token 语义**（见 [3.3](#33-设备-token)）：

- 有效期内重复调用返回**同一个 Token**（复用，不轮换）；
- 需要轮换时先登出再重新获取；
- 设备 30 天未上线 Token 被清理，重新调用即可。

**示例**

```
GET /api/devices/90431b/token
```

```json
{"code": 200, "message": "success", "data": {"deviceId": "90431b", "deviceToken": "uuid-device-token"}}
```

---

### 5.4 SensorData 资源

#### Report — 上报传感器数据 `POST /api/devices/{deviceId}/sensorData`

**授权**：DeviceAuth（`X-Device-Token` Header）。

**路径参数**：

| 参数 | 类型 | 说明 |
|------|------|------|
| `deviceId` | string | 6 位 hex 设备 ID |

**请求体**（JSON）：

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `sensors` | array | REQUIRED | 传感器数据数组（不能为空） |
| `sensors[].name` | string | REQUIRED | 传感器名称 |
| `sensors[].type` | string | REQUIRED | 传感器类型 |
| `sensors[].value` | any | REQUIRED | 值 |
| `sensors[].timestamp` | string | OPTIONAL | 采样时间，缺省为服务端时间 |

**示例**

```json
// 请求
{
  "sensors": [
    {"name": "temperature", "type": "temperature", "value": 25.5},
    {"name": "humidity", "type": "humidity", "value": 68.2}
  ]
}

// 响应 —— data 为服务端接收时间
{"code": 200, "message": "状态上报已接收", "data": "2026-08-31T20:30:00.000+08:00"}
```

> 数据经 Redis 缓冲后异步批量写入 InfluxDB；上报同时更新设备在线状态与最新数据缓存。

**错误码**：400（`sensors` 为空 / 字段缺失）、401（Token 无效）

---

#### List — 查询传感器数据 `GET /api/devices/{deviceId}/sensorData`

**授权**：UserAuth。

**路径参数**：`deviceId`（6 位 hex）。

**查询参数**：

| 参数 | 类型 | 行为 | 默认 | 说明 |
|------|------|------|------|------|
| `limit` | int | OPTIONAL | `50` | 返回条数上限 |
| `start` | string | OPTIONAL | 3 天前 | 起始时间（RFC3339） |
| `end` | string | OPTIONAL | 当前时间 | 结束时间（RFC3339） |

```
GET /api/devices/90431b/sensorData?limit=100
```

```json
{"code": 200, "message": "success", "data": [
  {"name": "temperature", "type": "temperature", "value": 25.5, "timestamp": "2026-08-31T20:30:00.000"}
]}
```

> 结果按时间倒序；缺省时间范围（最近 3 天）可命中 Redis 查询缓存（15 秒 TTL）。

---

#### Heartbeat — 设备心跳 `POST /api/devices/{deviceId}/heartbeat`

别名端点：`POST /api/devices/{deviceId}/ping`（兼容别名，行为完全一致）。

**授权**：DeviceAuth。

更新设备在线状态与活跃时间。

```json
{"code": 200, "message": "success", "data": {"serverTime": "2026-08-31T20:30:00.000+08:00", "nextInterval": 60}}
```

> `nextInterval`：建议的下一次心跳间隔（秒）。设备离线判定阈值为其 2 倍（默认 120 秒，见 `device.offline-threshold` 配置）。

---

### 5.5 DownlinkCmd 资源

#### Post — 下发命令 `POST /api/devices/{deviceId}/commands`

**授权**：UserAuth（仅设备属主，否则 403）。

**请求体**（JSON）：

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `type` | string | REQUIRED | `config` / `control` / `ota` / `message` |
| `payload` | object | REQUIRED | JSON 对象（非字符串） |

**示例**

```json
// 请求
{"type": "control", "payload": {"led": true}}

// 响应
{"code": 200, "message": "命令已下发", "data": {"id": 42, "type": "control", "payload": {"led": true}}}
```

> 命令写入数据库（`pending`）与 Redis 队列；设备在线时经 WebSocket 实时推送，离线时等待 HTTP 轮询拉取。

---

#### Pull — 设备拉取命令 `GET /api/devices/{deviceId}/commands`

**授权**：DeviceAuth。

拉取并**消费**队列中的命令（拉取后从队列删除，状态标记 `sent`）。设备处理完成后应通过 WebSocket 发送 ACK（见 [6.1](#61-设备实时通道--getapiwsdevice)），状态转为 `delivered`。

```json
{"code": 200, "message": "success", "data": [
  {"id": 42, "type": "control", "payload": {"led": true}, "createdAt": "2026-07-13T20:30:00+08:00"}
]}
```

---

### 5.6 DeviceConfig 资源

设备配置以**整体快照**持久化，云端设置后 `version+1` 并复用下行命令通道（`type=config`）下发：设备在线时经 WebSocket 实时推送，离线时下次轮询 `/commands` 拉取。设备端也可主动经 `/config` 查询期望配置，并经 `/config/report` 回执。

#### Get — 查询配置快照 `GET /api/devices/{deviceId}/config`

**授权**：**UserOrDeviceAuth** —— 设备属主（UserAuth），或设备本人（DeviceAuth 且 Token 与路径设备一致，否则 403）。设备端可经此端点主动拉取期望配置（下行命令通道的兜底通道）。

**路径参数**：`deviceId`（6 位 hex）。

**示例**

```
GET /api/devices/90431b/config
```

```json
// 已配置
{"code": 200, "message": "success", "data": {
  "deviceId": "90431b", "version": 3, "status": "acked",
  "payload": {"sensor": {"reportInterval": 60}},
  "reportedVersion": 3, "reportedPayload": {"sensor": {"reportInterval": 60}},
  "updatedAt": "2026-09-03T10:00:00+08:00"
}}

// 尚未配置
{"code": 200, "message": "success", "data": {
  "deviceId": "90431b", "version": 0, "status": "",
  "payload": null, "reportedVersion": 0, "reportedPayload": null
}}
```

**错误码**：403（非属主）、404（设备不存在）

---

#### Set — 设置并下发配置 `POST /api/devices/{deviceId}/config`

**授权**：UserAuth（仅设备属主，否则 403）。

**请求体**（JSON）：

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `config` | object | REQUIRED | 完整配置快照（整体覆盖，结构见 [4.5](#45-deviceconfig设备配置快照)） |

**示例**

```json
// 请求
{"config": {"sensor": {"reportInterval": 120}, "camera": {"smtp": {"host": "smtp.example.com", "port": 465, "ssl": true, "username": "cam@x.com", "password": "***"}}}}

// 响应 —— version 自动递增，配置已持久化并下发（status=pending 表示已入队列）
{"code": 200, "message": "配置已保存并下发", "data": {"deviceId": "90431b", "version": 4, "status": "pending"}}
```

> 下发复用 `type=config` 命令：在线设备经 WS 实时收到 `{"type":"cmd","cmdType":"config","payload":{"version":4,"config":{...}}}`；离线设备经 `GET /commands` 轮询拉到。若下发失败（如 Redis 异常），配置仍已持久化，设备可经 `GET /config` 兜底拉取。

**错误码**：400（`config` 缺失）、403（非属主）、404（设备不存在）、500（下发失败）

---

#### Report — 设备回执 `POST /api/devices/{deviceId}/config/report`

**授权**：DeviceAuth（`X-Device-Token`）。

**请求体**（JSON）：

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `version` | uint | REQUIRED | 设备实际生效的配置版本 |
| `config` | object | REQUIRED | 设备实际生效的配置快照 |

**示例**

```json
// 请求
{"version": 4, "config": {"sensor": {"reportInterval": 120}}}

// 响应
{"code": 200, "message": "配置回执已记录", "data": null}
```

> 回执后服务端将 `status` 置为 `acked`，`reportedVersion/reportedPayload` 记录设备实际生效值（用于平台侧「期望 vs 实际」比对）。

**错误码**：400（字段缺失）

---

### 5.7 Sensor 资源

传感器定义（物模型）的 CRUD 与下发。定义结构见 [4.6](#46-sensor传感器定义物模型)；`Apply` 将全部定义编译进 `DeviceConfig.payload.sensors` 并复用 `type=config` 通道版本化下发。

#### List — 传感器定义列表 `GET /api/devices/{deviceId}/sensors`

**授权**：UserAuth（仅设备属主，否则 403）。

**示例**

```
GET /api/devices/90431b/sensors
```

```json
{"code": 200, "message": "success", "data": [
  {"id": "temperature", "name": "温度", "type": "temperature", "dataType": "float",
   "unit": "°C", "specs": {"min": -40, "max": 125, "step": 0.1,
                           "thresholds": {"min": 0, "max": 100, "alarm": true}},
   "reportInterval": 60, "enabled": true,
   "createdAt": "2026-09-04T10:00:00.000+08:00", "updatedAt": "2026-09-04T10:00:00.000+08:00"}
]}
```

---

#### Get — 查询传感器定义 `GET /api/devices/{deviceId}/sensors/{sensorId}`

**授权**：UserAuth（仅设备属主，否则 403）。

**路径参数**：

| 参数 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `sensorId` | string | REQUIRED | 传感器标识符 |

**错误码**：403（非属主）、404（设备 / 传感器不存在）

---

#### Create — 创建传感器定义 `POST /api/devices/{deviceId}/sensors`

**授权**：UserAuth（仅设备属主，否则 403）。

**请求体**（JSON，结构见 [4.6](#46-sensor传感器定义物模型)）：

| 字段 | 类型 | 行为 |
|------|------|------|
| `id` | string | REQUIRED（IMMUTABLE） |
| `name` | string | REQUIRED |
| `type` | string | REQUIRED |
| `dataType` / `unit` / `specs` / `reportInterval` / `enabled` | — | OPTIONAL |

**示例**

```json
// 请求
{"id": "temperature", "name": "温度", "type": "temperature", "dataType": "float",
 "unit": "°C", "specs": {"min": -40, "max": 125, "step": 0.1,
                         "thresholds": {"min": 0, "max": 100, "alarm": true}},
 "reportInterval": 60}

// 响应 —— data 为创建后的完整定义
{"code": 200, "message": "传感器已创建", "data": {"id": "temperature", "name": "温度", "...": "..."}}
```

> 创建仅持久化定义，**不触发下发**；下发由 `Apply` 显式触发。

**错误码**：400（校验失败 / `id` 已存在）、403（非属主）、404（设备不存在）

---

#### Update — 增量更新传感器定义 `POST /api/devices/{deviceId}/sensors/{sensorId}/update`

**授权**：UserAuth（仅设备属主，否则 403）。

**请求体**（JSON，全部 OPTIONAL，**增量语义**：仅出现的字段被更新；`specs` 传 `{}` 显式清空，`reportInterval` 传 `null` 恢复继承全局周期；至少传一个字段）：

`name` / `type` / `dataType` / `unit` / `specs` / `reportInterval` / `enabled`

**示例**

```json
// 请求 —— 只改上报周期，其余字段不受影响
POST /api/devices/90431b/sensors/temperature/update
{"reportInterval": 120}

// 响应 —— data 为更新后的完整定义
{"code": 200, "message": "success", "data": {"id": "temperature", "reportInterval": 120, "...": "..."}}
```

**错误码**：400（校验失败 / 无更新字段）、403（非属主）、404（设备 / 传感器不存在）

---

#### Delete — 删除传感器定义 `POST /api/devices/{deviceId}/sensors/{sensorId}/delete`

**授权**：UserAuth（仅设备属主，否则 403）。

```json
{"code": 200, "message": "删除成功", "data": null}
```

**错误码**：403（非属主）、404（设备 / 传感器不存在）

---

#### Apply — 下发传感器配置 `POST /api/devices/{deviceId}/sensors/apply`

**授权**：UserAuth（仅设备属主，否则 403）。

将设备**全部**传感器定义编译为 `config.payload.sensors` 数组（保留其余配置分区），经 `DeviceConfigService` 版本 +1 并复用 `type=config` 下行通道下发。无请求体。

**响应体** `data`：

| 字段 | 类型 | 说明 |
|------|------|------|
| `deviceId` | string | 设备 ID |
| `version` | uint | 下发后的配置版本 |
| `status` | string | 下发确认态（`pending`） |
| `count` | int | 本次编译的传感器定义数量 |

**示例**

```json
// 响应
{"code": 200, "message": "传感器配置已下发",
 "data": {"deviceId": "90431b", "version": 5, "status": "pending", "count": 2}}
```

> 设备侧接收路径与 `POST /config` 一致：在线设备经 WS 收到 `{"type":"cmd","cmdType":"config","payload":{"version":5,"config":{"sensors":[...], ...}}}`；离线设备经 `GET /commands` 拉取；也可经 `GET /config`（设备 Token）兜底获取。设备生效后经 `POST /config/report` 回执。

**错误码**：403（非属主）、404（设备不存在）、500（保存 / 下发失败）

---

## 6. 实时通道

### 6.1 设备实时通道 — `GET /api/ws/device`

设备建立 WebSocket 连接后，可实时接收平台下发的命令，也可发送上行消息（数据上报 / 心跳 / ACK）。

| 项目 | 说明 |
|------|------|
| **端点** | `GET /api/ws/device` |
| **认证** | 必须（Device Token） |
| **数据方向** | 双向（下发命令 + 上行数据 / 心跳 / ACK） |
| **用途** | 实时接收命令、上报数据、心跳保活 |

#### 认证方式（4 种，按优先级）

| 优先级 | 方式 | 示例 | 适用场景 |
|--------|------|------|----------|
| 1 | Header `X-Device-Token` | Go/Python 原生客户端 | 服务端 SDK |
| 2 | Header `Authorization` | ESP32 `setAuthorization()` | 嵌入式设备 |
| 3 | Cookie `X-Device-Token` | 浏览器已登录 | Web 调试 |
| 4 | Query `?X-Device-Token=xxx` | `wss://host/api/ws/device?X-Device-Token=xxx` | 浏览器 / 最简单 |

> **推荐 ESP32 使用 Query 方式，最简单可靠。**

#### 服务端 → 设备：下发命令

```json
{
  "type": "cmd",
  "id": 42,
  "cmdType": "control",
  "payload": {"led": true},
  "createdAt": "2026-07-13T20:30:00.000+08:00"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `type` | string | 固定 `"cmd"` |
| `id` | uint | 命令 ID（用于 ACK 确认） |
| `cmdType` | string | 命令类型: `config` / `control` / `ota` / `message` |
| `payload` | object | 命令载荷（JSON 对象） |
| `createdAt` | string | 命令创建时间 |

#### 设备 → 服务端：ACK 确认（两种格式）

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
| `id` / `cmdId` | uint | 命令 ID（对应 cmd 消息中的 `id`） |
| `status` | string | 仅 response: `"ok"` / `"skipped"` |

> 收到 ACK 后，服务端将命令状态从 `pending`/`sent` 更新为 `delivered`。

#### 设备 → 服务端：上行消息

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
const char *WSS_URL = "/api/ws/device";

WebSocketsClient webSocket;
String token = "your-device-token-uuid";

void setup() {
    // 方式 A: Query 参数（推荐）
    String wsUrl = String("/api/ws/device?X-Device-Token=") + token;
    webSocket.beginSSL(WSS_HOST, 443, wsUrl.c_str());

    // 方式 B: Authorization 头
    // webSocket.beginSSL(WSS_HOST, 443, WSS_URL);
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
const ws = new WebSocket(`wss://api.meatsuger.top/api/ws/device?X-Device-Token=${token}`);

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
conn, _, err := websocket.DefaultDialer.Dial("wss://api.meatsuger.top/api/ws/device", header)
```

### 6.2 用户管理端通道 — `GET /api/ws/user`

用户（管理端）建立连接后，可接收其名下所有设备的实时事件，并可直接下发命令。

| 项目 | 说明 |
|------|------|
| **端点** | `GET /api/ws/user` |
| **认证** | 必须（User Token） |
| **数据方向** | 双向（接收设备事件 + 下发命令） |
| **用途** | 管理端实时看板 / 命令下发 |

#### 认证方式（3 种，按优先级）

| 优先级 | 方式 | 说明 |
|--------|------|------|
| 1 | Header `Authorization` | 用户 Token |
| 2 | Cookie `Authorization` | 浏览器已登录 |
| 3 | Query `?token=xxx` | 兜底方式 |

#### 服务端 → 用户：推送消息

**设备上线 / 下线通知:**

```json
{"type": "deviceOnline", "deviceId": "90431b", "timestamp": "2026-08-31T20:30:00.000+08:00"}
{"type": "deviceOffline", "deviceId": "90431b", "timestamp": "2026-08-31T20:35:00.000+08:00"}
```

**命令下发回执:**

```json
{"type": "cmdAck", "deviceId": "90431b", "cmdType": "control", "cmdId": 42, "success": true}
```

失败时携带 `error` 字段（`success: false`）。

**命令送达通知（设备拉取 / 收到后）:**

```json
{
  "type": "cmdSent",
  "deviceId": "90431b",
  "cmdId": 42,
  "cmdType": "control",
  "payload": {"led": true},
  "status": "sent",
  "createdAt": "2026-07-13T20:30:00+08:00"
}
```

**心跳响应:**

```json
{"type": "pong"}
```

#### 用户 → 服务端：上行消息

**下发命令（格式对齐 HTTP `POST /api/devices/{deviceId}/commands`，多一个 `deviceId` 字段）:**

```json
{
  "type": "control",
  "deviceId": "90431b",
  "payload": {"led": true}
}
```

**心跳:**

```json
{"type": "ping"}
```

### 6.3 MQTT over WebSocket 网关 — `GET /api/ws/mqtt/broker`

设备使用**真 MQTT 协议**经 WebSocket 接入，网关透明转发到外部 MQTT Broker（Mosquitto），并在转发链路上完成 PUBLISH 日志记录与数据入库。

| 项目 | 说明 |
|------|------|
| **端点** | `GET /api/ws/mqtt/broker`（WebSocket 子协议 `mqtt`） |
| **认证** | 设备 Token（两种方式二选一，见下） |
| **协议** | MQTT 3.1.1 / 5.0，二进制帧透传 |
| **用途** | 标准 MQTT 客户端（paho / ESP32 / mqtt.js）接入 |

#### 鉴权方式（二选一）

| 方式 | 凭证位置 | 适用客户端 |
|------|----------|------------|
| 1 | HTTP 层 `X-Device-Token`（Header / Cookie / Query `?X-Device-Token=xxx`） | mqtt.js 等可拼 URL 的客户端 |
| 2 | MQTT CONNECT 包 `username=设备ID` / `password=设备Token` | 标准 MQTT 客户端（paho / ESP32） |

> 鉴权失败时返回 MQTT 标准 CONNACK `not authorized (0x05)`，而不是 HTTP 拒绝。

#### Topic 与 Payload 约定

- Topic: `iot/{deviceId}/telemetry`（`/` 分隔，第 2 段为设备 ID）
- Payload: 与 HTTP 上报接口完全相同的 JSON：

```json
{"sensors": [{"name": "temp", "type": "temperature", "value": 25.5}]}
```

> 匹配约定的 PUBLISH 消息会自动走数据入库链路（更新状态缓存 → Redis 缓冲 → InfluxDB），并写入 `iot_message_log` 表（direction=up）。

#### Topic 与 Payload 约定

| Topic | 方向 | QoS / Retained | Payload | 说明 |
|-------|------|----------------|---------|------|
| `iot/{deviceId}/telemetry` | 上行 | — | 同 HTTP `sensorData` 上报 | **已实现**：`{"sensors":[...]}` 自动入库 |
| `iot/{deviceId}/cmd` | 下行 | QoS1 | `{"id":42,"type":"control","payload":{...},"createdAt":"..."}` | **已实现**：`EnqueueCmd` 对非 config 类型实时发布（与 `GET /commands` 返回项同构；config 类型经 config retained 主题专管，不重复投递） |
| `iot/{deviceId}/config` | 下行 | QoS1 + **Retained** | `{"version": 4, "config": {...}}`（ConfigEnvelope） | **已实现**：平台每次 `POST /config` / `/sensors/apply` / `/actuators/apply` 保存后发布最新快照 |
| `iot/{deviceId}/config/report` | 上行 | QoS1 | `{"version": 4, "config": {...}}` | **已实现**：配置回执（对应 `POST /config/report`），回写 `status=acked` |
| `iot/{deviceId}/status` | 上行 | — | `{"status": "online"}` | 预留：设备状态 / 心跳（对应 `POST /heartbeat`） |

> **config 下行语义（设备“订阅即拉取”）**：平台在 `POST /api/devices/{deviceId}/config` 保存后，由 MQTT 发布器以 QoS1 + retained 发布到 `iot/{deviceId}/config`。设备只需 SUBSCRIBE 该主题即可拿到最新配置——在线时实时收到；离线/重启设备在下次订阅时由 Broker 自动补投 retained 的**最新版本**（无逐条补发，仅快照语义，与 `/config` 快照一致）。配置变更同时经 WS 实时推送 / HTTP `GET /commands` 队列轮询 / MQTT retained 三通道触达，设备任选其一，以 `version` 幂等去重。

> **config/report 上行语义**：网关收到 `iot/{deviceId}/config/report`（连接鉴权设备必须等于话题中的设备 ID，防跨设备伪造）后解析回执并复用 `DeviceConfigService.Report` 落库（回写 `reportedVersion`/`reportedPayload` 并置 `acked`），与 HTTP `POST /config/report` 完全等价。

#### 设备在线 / 离线判定

| 事件 | 触发条件 | 平台侧动作 |
|------|----------|------------|
| **上线** | 设备 SUBSCRIBE 自身配置/命令主题（`iot/{id}/config`、`iot/{id}/cmd`、`iot/{id}/#`，仅限本连接鉴权设备自身，防跨设备伪造） | Redis 状态置 `ONLINE`（同原生 WS 上线行为）+ 推送 `deviceOnline` 给 owner 管理端 |
| **下线（立即）** | 连接关闭（正常 DISCONNECT / 断网 / 被新连接顶替除外） | 立即置 `OFFLINE`（Redis+PG）+ 推送 `deviceOffline`，**不等**离线同步器周期扫描 |
| **下线（空闲超时）** | 无任何 MQTT 帧（含 PINGREQ 心跳）超过 1.5×keepalive（设备 CONNECT 携带；keepalive=0 时回退 `idle-timeout-sec`，默认 5min；夹在 15s~30min） | 强制断开双端 → 立即置 `OFFLINE` + 推送 `deviceOffline` |

> **连接存活 ≠ 离线**：离线同步器（`offline-scan-interval` 周期）会跳过「MQTT 桥接连接仍存活」的设备（`IsConnected`），即使其长时间未上报数据，避免误判。
>
> **遗嘱（Will）**：设备 CONNECT 携带的遗嘱原样随帧透传，异常断线时由外部 Broker 按遗嘱发布到遗嘱主题（透明转发天然生效）；平台离线判定不依赖遗嘱主题订阅，由网关直接检测连接死亡驱动，更快更可靠。
>
> **同设备多连接**：新连接到来会踢下线旧连接（与原生 WS 一致，同一设备只保留一个活跃桥接）。

#### 连接示例

```
wss://api.meatsuger.top/api/ws/mqtt/broker?X-Device-Token=<token>
```

或标准 MQTT 客户端配置: `host=api.meatsuger.top, port=443, path=/api/ws/mqtt/broker, username=<deviceId>, password=<deviceToken>`。

## 7. 附录

### 端点总览

| 方法 | 端点 | 认证 | 说明 |
|------|------|------|------|
| GET | `/health` | 无 | 健康检查 |
| POST | `/api/users` | 无 | 注册用户（Create） |
| POST | `/api/users/login` | 无 | 用户登录（支持 `device` 标识） |
| GET | `/api/users/isLogin` | 无 | 检查登录状态 |
| POST | `/api/users/logout` | UserAuth | 退出登录 |
| GET | `/api/users` | UserAuth + 管理员 | 用户列表（可选分页，双响应形态见 2.5） |
| GET | `/api/users/{userId}` | UserAuth | 获取用户信息（`{userId}` 支持 `me`） |
| POST | `/api/users/{userId}/update` | UserAuth | 更新用户信息 |
| POST | `/api/users/{userId}/delete` | UserAuth | 删除用户 |
| POST | `/api/devices` | UserAuth | 注册设备（Create） |
| GET | `/api/devices` | UserAuth | 设备列表 |
| GET | `/api/devices/{deviceId}` | UserAuth | 设备详情（物模型视图：设备信息 + 传感器定义与最近遥测 + 执行器定义） |
| POST | `/api/devices/{deviceId}/update` | UserAuth / DeviceAuth | 更新设备信息（增量） |
| POST | `/api/devices/{deviceId}/delete` | UserAuth | 删除设备 |
| GET | `/api/devices/{deviceId}/token` | DeviceIDAuth | 获取设备 Token |
| GET | `/api/devices/{deviceId}/login` | DeviceIDAuth | 获取设备 Token（兼容别名） |
| POST | `/api/devices/{deviceId}/sensorData` | DeviceAuth | 上报传感器数据 |
| GET | `/api/devices/{deviceId}/sensorData` | UserAuth | 查询设备传感器数据 |
| POST | `/api/devices/{deviceId}/heartbeat` | DeviceAuth | 设备心跳 |
| POST | `/api/devices/{deviceId}/ping` | DeviceAuth | 设备心跳（兼容别名） |
| POST | `/api/devices/{deviceId}/commands` | UserAuth | 下发命令 |
| GET | `/api/devices/{deviceId}/commands` | DeviceAuth | 设备拉取命令 |
| GET | `/api/devices/{deviceId}/config` | UserAuth / DeviceAuth | 查询设备配置快照（设备本人需 Token 与路径一致） |
| POST | `/api/devices/{deviceId}/config` | UserAuth | 设置并下发设备配置 |
| POST | `/api/devices/{deviceId}/config/report` | DeviceAuth | 设备配置回执 |
| GET | `/api/devices/{deviceId}/sensors` | UserAuth | 传感器定义列表 |
| GET | `/api/devices/{deviceId}/sensors/{sensorId}` | UserAuth | 查询传感器定义 |
| POST | `/api/devices/{deviceId}/sensors` | UserAuth | 创建传感器定义 |
| POST | `/api/devices/{deviceId}/sensors/{sensorId}/update` | UserAuth | 增量更新传感器定义 |
| POST | `/api/devices/{deviceId}/sensors/{sensorId}/delete` | UserAuth | 删除传感器定义 |
| POST | `/api/devices/{deviceId}/sensors/apply` | UserAuth | 下发传感器配置（编译进 config，版本递增） |
| GET | `/api/devices/{deviceId}/actuators` | UserAuth | 执行器定义列表 |
| GET | `/api/devices/{deviceId}/actuators/{actuatorId}` | UserAuth | 查询执行器定义 |
| POST | `/api/devices/{deviceId}/actuators` | UserAuth | 创建执行器定义 |
| POST | `/api/devices/{deviceId}/actuators/{actuatorId}/update` | UserAuth | 增量更新执行器定义 |
| POST | `/api/devices/{deviceId}/actuators/{actuatorId}/delete` | UserAuth | 删除执行器定义 |
| POST | `/api/devices/{deviceId}/actuators/apply` | UserAuth | 下发执行器配置（编译进 config，版本递增） |
| GET | `/api/ws/device` | DeviceAuth | 设备 WebSocket 实时通道 |
| GET | `/api/ws/user` | UserAuth | 用户管理端 WebSocket 通道 |
| GET | `/api/ws/mqtt/broker` | DeviceAuth（MQTT 层） | MQTT over WebSocket 网关 |
| GET | `/api/swagger/*` | 无 | Swagger UI（生产环境默认关闭） |

### UDP 上报通道

非 HTTP 接口：设备可通过 UDP 直接上报传感器数据（端口 `8183`，配置项 `server.udp-port`，`0` 表示禁用）。

**数据报格式（JSON）:**

```json
{
  "deviceId": "90431b",
  "token": "uuid-device-token",
  "sensors": [
    {"name": "temperature", "type": "temperature", "value": 25.5}
  ]
}
```

| 字段 | 类型 | 行为 | 说明 |
|------|------|------|------|
| `deviceId` | string | REQUIRED | 6 位 hex 设备 ID |
| `token` | string | OPTIONAL | 设备 Token（走与 HTTP 上报一致的 Token 校验） |
| `sensors` | array | REQUIRED | 传感器数据数组，格式同 [Report](#report--上报传感器数据-postapidevicesdeviceidsensordata) |

> UDP 为单向通道（无响应），入库链路与 HTTP 上报一致。单包不超过 2048 字节。

### 命令状态流转

```
pending ──→ sent ──→ delivered
   │          │
   │  (WS推送到设备 / 设备HTTP拉取)
   │                    │
   │          (设备通过WS发ACK)
```

| 状态 | 说明 |
|------|------|
| `pending` | 已下发，等待设备消费 |
| `sent` | 已推送到设备（Redis 队列被拉取 / WebSocket 已推送） |
| `delivered` | 设备通过 WebSocket 确认收到 |

### 设备数据上报通道对比

| 通道 | 端点 / Topic | 协议 | 认证 | 适用场景 |
|------|------|------|------|----------|
| HTTP | `POST /api/devices/{deviceId}/sensorData` | HTTP | Device Token | 常规上报 |
| MQTT | `iot/{deviceId}/telemetry`（经 `/api/ws/mqtt/broker`） | MQTT over WSS | Device Token | 标准 MQTT 客户端 / 高频上报 |
| WebSocket | `GET /api/ws/device` | WSS | Device Token | 上报 + 实时接收命令 |
| UDP | `udp://<host>:8183` | UDP JSON | Device Token（报文内携带） | 低功耗 / 最小开销单向上报 |

### 设备实时下发流程

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

### 数据存储（PostgreSQL 5 表）

| 表 | 用途 |
|----|------|
| `app_user` | 用户账号 |
| `iot_device` | 设备（主键 `device_id`） |
| `iot_device_config` | 设备配置快照（与设备 1:1，版本化） |
| `iot_device_thing` | 设备物模型（传感器 / 执行器统一存储） |
| `iot_message_log` | 设备消息日志（下行命令 + 上行 MQTT 发布） |

- **`iot_device_thing`**（2026-09 由 `iot_device_sensor` + `iot_device_actuator` 合并）：
  `kind` ∈ `sensor`/`actuator`，`thing_id` 即原 `sensor_id`/`actuator_id`，唯一键 `(device_id, kind, thing_id)`；
  sensor 专有字段（`type`/`dataType`/`unit`/`reportInterval`）、actuator 的 `driver` 与物模型定义体统一序列化进 `specs` JSON。
  REST 层仍以 `/sensors`、`/actuators` 两个资源对外，按 `kind` 过滤。
- **`iot_message_log`**（2026-09 由 `iot_downlink_cmd` + `mqtt_publish_log` 合并）：
  `direction=down` 存下行命令（`category=config`/`cmd`，`status=pending`/`sent`/`delivered`）；
  `direction=up` 存 MQTT 网关拦截的设备发布（`category=telemetry`/`config_report`/`other`，含 `topic`/`qos`/`retained`）。

### 变更记录

| 版本 | 日期 | 变更 |
|------|------|------|
| 1.8.0 | 2026-09-10 | 数据库表结构精简（7 → 5 表，纯内部变更，**REST/WS 契约不变**）：`iot_device_sensor` + `iot_device_actuator` 合并为 `iot_device_thing`（`kind` 判别，sensor 专有字段与物模型定义体入 `specs` JSON）；`iot_downlink_cmd` + `mqtt_publish_log` 合并为 `iot_message_log`（`direction=down/up` 判别）。**移除无鉴权的兼容端点 `GET /api/ws/mqtt`**（连同其 WebSocket 处理器），MQTT 接入统一走 `/api/ws/mqtt/broker`。新增 [数据存储（5 表）](#数据存储postgresql-5-表) 附录 |
| 1.7.0 | 2026-09-06 | 设备详情接口改造为**物模型视图**：`GET /api/devices/{deviceId}` 响应 `data.sensors` 由「最近遥测快照」升级为「传感器定义 + `latest`(最近一次上报值,`null`=从未上报)」物模型数组,`data.actuators` 为执行器定义数组(无定义时均为 `[]`);服务端完成定义↔遥测 join(上报 `name` 优先匹配定义 `id`,其次匹配定义 `name`)。物模型定义列表(整设备)新增 Redis 缓存(`cache:def_sensor:/def_actuator:`,Cache-Aside + 写路径显式失效)。**破坏性变更:详情响应不再返回裸遥测 `sensors` 与 `thingModel` 嵌套字段,前端一次请求即可渲染完整设备页** |
| 1.6.0 | 2026-09-05 | 执行器物模型落地 + MQTT 命令通道：新增 `Actuator` 资源（`devices/{deviceId}/actuators/{actuatorId}`，镜像 Sensor 模式，CRUD + `POST /actuators/apply` 编译进 `DeviceConfig.payload.actuators`，`id` ≤11 字符 = 固件 periph 设备名 = 控制命令 `action`）；下行发布器泛化（`EnqueueCmd` 对非 config 类型实时发布 `iot/{deviceId}/cmd`，与 `GET /commands` 返回项同构）；固件重构：执行器定义唯一真源 = 配置快照（diff 实例化/卸载、重启 NVS 重放），控制命令经 MQTT/HTTP 双通道按 `action` 路由执行，遥测统一 `iot/{deviceId}/telemetry`，移除旧 register/`device/{id}` 主题/应答流。**破坏性变更：`payload.actuator`（单数运行对象）移除，由 `actuators` 定义数组取代；旧 `type=register` 协议不再支持** |
| 1.5.0 | 2026-09-05 | MQTT 配置通道落地：新增平台侧 MQTT 发布器（`POST /config` 保存后以 QoS1+retained 发布 `iot/{deviceId}/config`，设备订阅即拉取，离线重连由 Broker 补投最新快照）；MQTT 网关新增 `iot/{deviceId}/config/report` 上行处理（连接鉴权设备=话题设备，复用 `DeviceConfigService.Report` 置 `acked`）；固件侧新增 appcfg 模块（NVS 持久化已应用版本/载荷/待回执标志，版本幂等去重，`sensor.reportInterval` 运行时生效，其余字段原样持久化与回执） |
| 1.4.0 | 2026-09-04 | 新增传感器物模型能力：`Sensor` 资源（`devices/{deviceId}/sensors/{sensorId}`），JSON 格式融合新大陆 NLECloud 传感器模型（`ApiTag`/`TypeAttrs`）与阿里云 TSL（`dataType`/`specs`）。新增 `List/Get/Create/Update/Delete` 标准方法与 `Apply` 自定义方法（`POST /sensors/apply` 编译进 `DeviceConfig.payload.sensors` 并版本化下发）。`GET /config` 改为 **UserOrDeviceAuth** 双认证（设备可经 Token 主动拉取期望配置）。新增 MQTT Topic 规划（`iot/{deviceId}/config` 下行等，**预留暂未实现**） |
| 1.3.0 | 2026-09-03 | 新增设备配置能力：`DeviceConfig` 资源（整体配置快照，`version` 版本化）。新增 `GET /api/devices/{deviceId}/config`（查询期望配置）、`POST /api/devices/{deviceId}/config`（设置并下发，`version` 递增）、`POST /api/devices/{deviceId}/config/report`（设备回执，回写 `reported*` 并置 `acked`）。配置复用下行命令通道下发（`type=config`）；`payload` 分区覆盖 `network`/`sensor`/`actuator`/`camera`(SMTP 等)/`ota`（OTA 预留扩展点，暂不实现升级流程） |
| 1.2.0 | 2026-09-01 | REST API 重构为资源导向路径（**仅使用 GET / POST 两个动词**，写操作一律 POST，更新 / 删除通过 `POST + /update`、`/delete` 后缀表达）。关键映射：`/api/user/*` → `/api/users/*`（`POST /api/user/register` → `POST /api/users`；`GET /api/user/profile?id=` → `GET /api/users/{userId}`，`{userId}` 支持 `me`；`PUT /api/user` → `POST /api/users/{userId}/update`；`GET /api/user/list` + `GET /api/user/page` → `GET /api/users`（可选分页，双响应形态）；`POST /api/user/delete?id=` → `POST /api/users/{userId}/delete`）；`/api/device/*` → `/api/devices/*`（`GET /api/device/{id}/Data` → `GET /api/devices/{deviceId}`；`GET /api/device/{id}/login` → `GET /api/devices/{deviceId}/token`）；数据接口并入设备子资源（`POST /api/data/{id}/Data` → `POST /api/devices/{deviceId}/sensorData`；`GET /api/data/{id}/Data/list` → `GET /api/devices/{deviceId}/sensorData`；`POST /api/data/{id}/ping` → `POST /api/devices/{deviceId}/heartbeat`）；命令接口 `POST|GET /api/device/{id}/cmd` → `POST|GET /api/devices/{deviceId}/commands`；**移除 `GET /api/data/list`**（通用数据查询扩展点）。保留兼容别名：`GET /api/devices/{id}/login`（token 别名）、`POST /api/devices/{id}/ping`（heartbeat 别名）。**破坏性变更：旧路径全部失效**，前端与设备固件需同步更新 |
| 1.1.0 | 2026-09-01 | 登录支持 `device` 设备端标识（多端上限按设备端计数）；设备 Token 改为复用语义（重复获取返回同一 Token）；设备更新接口改为增量更新并支持设备 Token 自更新；被踢 / 被顶号的 Token 立即物理删除；修正用户 Token 有效期为 3 天（Cookie MaxAge 同步） |
| 1.0.0 | 2026-08-31 | 首版完整文档 |
