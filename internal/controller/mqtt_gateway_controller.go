// 物联网设备接入与物模型管理平台软件（物咸通）V1.0
// Copyright (C) 2025-2026 余昊
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package controller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	sagin "github.com/sa-tokens/sa-token-go/integrations/gin"
	"github.com/sa-tokens/sa-token-go/stputil"

	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/model"
	"iot-platform.local/internal/repository"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/cache"
	"iot-platform.local/pkg/config"
)

// MqttGatewayController MQTT over WebSocket 桥接网关（胶水层）
//
// 功能：
//  1. 透明转发：设备 --wss--> 网关 --tcp--> 外部 Mosquitto(1883)
//  2. 发布日志：所有 PUBLISH 帧写入 iot_message_log 表（direction=up）
//  3. 数据入库：PUBLISH payload 解析后走 DeviceReportService（onPublish 钩子）
//  4. 在线判定：设备 SUBSCRIBE 自身配置/命令主题（如 iot/{deviceId}/config）→ 置 ONLINE
//  5. 离线判定：连接关闭 / 空闲超时（无任何 MQTT 帧）→ 立即置 OFFLINE，不等周期扫描；
//     owner 管理端收到 deviceOffline 推送；连接存活但未上报的设备不会被离线同步器误判
//
// 鉴权（框架 Sa-Token 体系，两种凭证方式二选一，均不拦截 WebSocket 升级，保证透传）：
//  1. HTTP 层：请求携带 X-Device-Token（Header/Cookie/Query），mqtt.js 等可拼 URL 参数的客户端
//  2. MQTT 层：CONNECT 包 username=设备ID / password=设备Token，标准 MQTT 客户端（paho/ESP32 等）
//
// 鉴权失败时回 MQTT 标准 CONNACK not authorized(0x05)，而不是 HTTP 拒绝。
type MqttGatewayController struct {
	brokerAddr  string
	dialTimeout time.Duration
	msgRepo     *repository.MessageLogRepo
	reportSvc   *service.DeviceReportService // 设备数据上报服务（onPublish 入库用）
	configSvc   *service.DeviceConfigService // 设备配置服务（config/report 回执用）
	deviceSvc   *service.DeviceService       // 设备服务（订阅上线 / 断连离线状态更新用）
	cache       *cache.RedisCache            // MQTT 消息历史缓存（可为 nil）

	// 设备Token注册表：框架鉴权通过后记录 deviceID → token，
	// 供 onPublish 上报时使用（与 HTTP 上报共用 Sa-Token 体系）
	mu           sync.RWMutex
	deviceTokens map[string]string

	// —— 在线/离线状态机 ——
	// conns 登记所有活跃的 MQTT 桥接连接（deviceID → 连接生命周期状态）：
	//   - IsConnected 供离线同步器误判保护（连接存活 ≠ 离线，即使长时间未上报）
	//   - 连接关闭 → 立即置 OFFLINE（Redis+PG），不等离线同步器周期扫描
	//   - 同设备新连接顶替旧连接（与原生 WS Hub.Register 语义一致）
	idleTimeout time.Duration // 空闲看护阈值兜底（配置 idle-timeout-sec；设备 CONNECT keepalive 自适配优先）
	connMu      sync.RWMutex
	conns       map[string]*mqttConn                // deviceID(lower) → 活跃连接
	onOnline    func(deviceID string, ownerID uint) // 可选：设备上线通知（owner 管理端 WS 推送）
	onOffline   func(deviceID string, ownerID uint) // 可选：设备离线通知（owner 管理端 WS 推送）
}

// mqttConn 一条设备 MQTT 连接的生命周期状态
//
// 在线语义与原生 WS 一致但触发点不同：原生 WS 在连接建立时上线，
// MQTT 设备在订阅自身主题（配置/命令通道）时视为真正可服务 → 上线；
// 连接关闭或空闲超时（设备失联）→ 立即离线。
type mqttConn struct {
	deviceID string
	ownerID  uint          // 设备归属用户（上线/下线推送用）
	timeout  time.Duration // 空闲看护阈值（keepalive×1.5 提炼，无任何帧即判失联）

	// —— 管理端用户连接（MQTT over WSS，用户登录态）——
	// 用户连接以 "user:<loginID>" 作为 deviceID 参与登记，与设备 ID 不冲突，
	// 因此不会触发「同设备单连接」顶号，不会把 ESP32 等设备踢下线。
	// 用户连接只做透明转发 + 话题属主校验：不写设备消息日志、不做数据入库、
	// 不参与设备在线/离线判定。
	isUser bool
	userID uint
	acl    map[string]bool // 话题内 deviceId 属主校验缓存（仅泵协程访问，无需加锁）

	subOnce        sync.Once   // 订阅自身主题 → 上线标记，每连接仅一次
	unregisterOnce sync.Once   // 连接收尾（登记移除 + 离线标记）仅一次
	closeOnce      sync.Once   // 双泵任一侧退出后统一关闭双端（幂等）
	online         atomic.Bool // 是否已通过 SUBSCRIBE 标记上线（决定断连时是否置 OFFLINE）
	ws             *websocket.Conn
	tcp            net.Conn
}

// connectReadTimeout 等待设备拼出完整 CONNECT 包的最长时限（防无效连接占住协程）
const connectReadTimeout = 30 * time.Second

// NewMqttGatewayController 创建 MQTT 桥接网关控制器
// reportSvc 可为 nil（此时 onPublish 只记日志不入库）；cache 可为 nil（不记录消息历史）；
// configSvc 可为 nil（此时 iot/{id}/config/report 回执不处理）；
// deviceSvc 可为 nil（此时 SUBSCRIBE 不上线标记，仅依赖上报/心跳刷新状态）
func NewMqttGatewayController(msgRepo *repository.MessageLogRepo, reportSvc *service.DeviceReportService, configSvc *service.DeviceConfigService, deviceSvc *service.DeviceService, cache *cache.RedisCache) *MqttGatewayController {
	addr := strings.TrimPrefix(config.Cfg.MQTT.BrokerURL, "tcp://")
	addr = strings.TrimPrefix(addr, "ssl://")
	return &MqttGatewayController{
		brokerAddr:   addr,
		dialTimeout:  5 * time.Second,
		msgRepo:      msgRepo,
		reportSvc:    reportSvc,
		configSvc:    configSvc,
		deviceSvc:    deviceSvc,
		cache:        cache,
		deviceTokens: make(map[string]string),
		idleTimeout:  time.Duration(config.Cfg.MqttGateway.IdleTimeoutSec) * time.Second,
		conns:        make(map[string]*mqttConn),
	}
}

// registerDeviceToken 记录设备 Token（覆盖旧连接）
func (g *MqttGatewayController) registerDeviceToken(deviceID, token string) {
	if deviceID == "" || token == "" {
		return
	}
	g.mu.Lock()
	g.deviceTokens[deviceID] = token
	g.mu.Unlock()
}

// unregisterDeviceToken 连接断开时移除 Token（仅当仍是本连接的 Token，避免误删并发连接）
func (g *MqttGatewayController) unregisterDeviceToken(deviceID, token string) {
	if deviceID == "" {
		return
	}
	g.mu.Lock()
	if g.deviceTokens[deviceID] == token {
		delete(g.deviceTokens, deviceID)
	}
	g.mu.Unlock()
}

// deviceToken 获取设备当前 Token
func (g *MqttGatewayController) deviceToken(deviceID string) string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.deviceTokens[deviceID]
}

// SetPresenceCallbacks 设置设备上线/下线通知回调（由上层注入 owner 管理端 WS 推送）
func (g *MqttGatewayController) SetPresenceCallbacks(onOnline, onOffline func(deviceID string, ownerID uint)) {
	g.onOnline = onOnline
	g.onOffline = onOffline
}

// IsConnected 设备当前是否有活跃的 MQTT 桥接连接
//
// 供离线同步器（DeviceOfflineSyncer）的 isOnlineFn 误判保护：
// MQTT 设备连接存活但长时间未上报数据时，不应被周期扫描置为离线
// （该设备的上报/心跳由本网关的 SUBSCRIBE 上线 + 空闲超时看护负责，双保险）。
func (g *MqttGatewayController) IsConnected(deviceID string) bool {
	g.connMu.RLock()
	defer g.connMu.RUnlock()
	_, ok := g.conns[strings.ToLower(deviceID)]
	return ok
}

// registerConn 登记设备连接（在线判定）。同设备新连接到来时踢下线旧连接
// （与原生 WS Hub.Register 一致：同一设备只保留一个活跃桥接，防止双连接叠加）。
func (g *MqttGatewayController) registerConn(conn *mqttConn, ws *websocket.Conn, tcp net.Conn) {
	conn.ws = ws
	conn.tcp = tcp

	g.connMu.Lock()
	if old, ok := g.conns[conn.deviceID]; ok && old != conn {
		zap.S().Infof("[MQTT网关] 设备 %s 重新连接，踢下线旧连接", conn.deviceID)
		old.close()
	}
	g.conns[conn.deviceID] = conn
	n := len(g.conns)
	g.connMu.Unlock()

	zap.S().Infof("[MQTT网关] 设备 %s 桥接登记, 当前活跃连接: %d", conn.deviceID, n)
}

// unregisterConn 连接收尾：从登记表移除；若该连接已订阅上线，
// 立即置 OFFLINE（Redis+PG）并通知 owner 管理端（不再依赖离线同步器周期扫描）。
//
// 被新连接顶替（reconnect）的旧连接由 registerConn 提前踢下线，
// 此处通过「登记表仍指向本连接」判断静默跳过状态修改，避免误把新连接置为离线。
func (g *MqttGatewayController) unregisterConn(conn *mqttConn) {
	conn.unregisterOnce.Do(func() {
		g.connMu.Lock()
		isCurrent := g.conns[conn.deviceID] == conn // 登记表仍指向本连接？（未被新连接顶替）
		if isCurrent {
			delete(g.conns, conn.deviceID)
		}
		g.connMu.Unlock()

		// 已被新连接顶替（reconnect）：本连接踢下线收尾，状态由新连接负责，静默跳过
		if !isCurrent {
			zap.S().Infof("[MQTT网关] 设备 %s 旧连接收尾（已被新连接顶替，跳过离线修改）", conn.deviceID)
			return
		}

		// 从未订阅自身主题（从未标记上线）→ 只清登记，不改业务状态
		if !conn.online.Load() {
			zap.S().Infof("[MQTT网关] 设备 %s 连接结束（未订阅自身主题，跳过离线修改）", conn.deviceID)
			return
		}

		if g.deviceSvc != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := g.deviceSvc.UpdateStatus(ctx, conn.deviceID, "OFFLINE"); err != nil {
				zap.S().Warnf("[MQTT网关] 连接关闭置离线失败 [device=%s]: %v", conn.deviceID, err)
			} else {
				zap.S().Infof("[MQTT网关] 设备 %s 离线（连接关闭，立即置 OFFLINE）", conn.deviceID)
			}
		}
		if conn.ownerID > 0 && g.onOffline != nil {
			g.onOffline(conn.deviceID, conn.ownerID)
		}
	})
}

// close 关闭设备侧 WSS 与 broker 侧 TCP（幂等）。
//
// 关闭后，设备异常断线（含空闲超时踢下线）时外部 Broker 会按其 CONNECT 遗嘱（Will）
// 发布遗嘱消息到遗嘱主题——透明转发使其天然生效；
// 平台侧的离线判定不依赖遗嘱主题订阅，而是直接由「连接死亡」驱动，更快更可靠。
func (c *mqttConn) close() {
	c.closeOnce.Do(func() {
		if c.ws != nil {
			_ = c.ws.Close()
		}
		if c.tcp != nil {
			_ = c.tcp.Close()
		}
	})
}

// computeIdleTimeout 推导单连接的空闲看护阈值（无任何 MQTT 帧即判定设备失联）：
//   - 设备 CONNECT 携带 keepalive → 按 MQTT 规范允许服务端 1.5×keepalive 无包断开（优先）
//   - keepalive 禁用（0）→ 使用配置 idle-timeout-sec；仍未配置 → 兜底 5min
//   - 结果夹在 [15s, 30min]，防设备端病态 keepalive 值
func (g *MqttGatewayController) computeIdleTimeout(keepAlive uint16) time.Duration {
	if keepAlive > 0 {
		t := time.Duration(keepAlive) * 1500 * time.Millisecond // = keepalive × 1.5
		if t < 15*time.Second {
			t = 15 * time.Second
		}
		if t > 30*time.Minute {
			t = 30 * time.Minute
		}
		return t
	}
	if g.idleTimeout > 0 {
		return g.idleTimeout
	}
	return 5 * time.Minute
}

// HandleWebSocket 处理设备经 Gin HTTP 入口进来的 MQTT-over-WebSocket 连接
//
// 鉴权说明：不依赖路由中间件（保证任意 MQTT 客户端可直连透传），
// 升级后在 MQTT 协议层用框架 Sa-Token 校验设备凭证。
func (g *MqttGatewayController) HandleWebSocket(c *gin.Context) {
	w := c.Writer
	r := c.Request

	upgrader := websocket.Upgrader{
		Subprotocols: []string{"mqtt"},
		CheckOrigin:  func(r *http.Request) bool { return true },
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		zap.S().Warnf("[MQTT网关] WebSocket 升级失败: %v", err)
		return
	}
	defer ws.Close()

	// 1. 累积 WS 消息直到拼出完整 CONNECT 包（限时 30s，防无效连接占住协程）
	_ = ws.SetReadDeadline(time.Now().Add(connectReadTimeout))
	var connectBytes []byte
	for {
		mt, data, err := ws.ReadMessage()
		if err != nil {
			zap.S().Debugf("[MQTT网关] 读取 CONNECT 失败: %v", err)
			return
		}
		if mt != websocket.BinaryMessage {
			continue
		}
		connectBytes = append(connectBytes, data...)
		if total, ok := mqttPacketTotalLength(connectBytes); ok && len(connectBytes) >= total {
			connectBytes = connectBytes[:total]
			break
		}
	}
	_ = ws.SetReadDeadline(time.Time{})

	// 2. 解析 CONNECT 帧（clientID/protocolLevel/keepAlive 用于日志、PUBLISH 解析与心跳看护）
	clientID, username, mqttToken, protocolLevel, keepAlive, parseOK := parseConnectCredentials(connectBytes)
	zap.S().Debugf("[MQTT网关] 设备连接: client=%s, device=%s, level=%d, parseOK=%v", clientID, username, protocolLevel, parseOK)

	// 3. 鉴权（三种凭证方式，按优先级）：
	//   1) HTTP 层 X-Device-Token（Header/Cookie/Query）——设备自身
	//   2) 管理端用户登录态（Authorization/Cookie/?token=）——浏览器经 MQTT over WSS 下发，
	//      以独立连接身份 user:<loginID> 参与，与设备 ID 不冲突 → 不会顶掉设备连接
	//   3) MQTT CONNECT username=设备ID / password=设备Token——标准 MQTT 客户端
	var (
		deviceID string
		token    string
		isUser   bool
		userID   uint
	)
	switch {
	case middleware.GetDeviceToken(c) != "":
		// 方式1：HTTP 层 X-Device-Token（Header/Cookie/Query），适合 mqtt.js 等可拼 URL 的客户端
		httpToken := middleware.GetDeviceToken(c)
		deviceMgr := middleware.GetDeviceManager()
		if deviceMgr == nil {
			zap.S().Warn("[MQTT网关] 设备 Manager 未初始化，拒绝连接")
			_ = ws.WriteMessage(websocket.BinaryMessage, buildConnackReject(protocolLevel))
			return
		}
		loginID, err := deviceMgr.GetLoginID(httpToken)
		if err != nil {
			zap.S().Warnf("[MQTT网关] HTTP Token 鉴权失败, 拒绝设备: %s", username)
			_ = ws.WriteMessage(websocket.BinaryMessage, buildConnackReject(protocolLevel))
			return
		}
		deviceID = loginID
		token = httpToken

	case userTokenFromRequest(c) != "":
		// 方式2：管理端用户登录态（用户 auth → 设备 auth：浏览器代表用户下发到其名下设备）
		uid, ok := authenticateUserToken(userTokenFromRequest(c))
		if !ok {
			zap.S().Warn("[MQTT网关] 用户 Token 鉴权失败, 拒绝连接")
			_ = ws.WriteMessage(websocket.BinaryMessage, buildConnackReject(protocolLevel))
			return
		}
		isUser = true
		userID = uid
		deviceID = fmt.Sprintf("user:%d", uid)
		token = userTokenFromRequest(c)

	default:
		// 方式3：MQTT CONNECT username=设备ID / password=设备Token（标准 MQTT 客户端）
		if !parseOK || !authenticateDevice(username, mqttToken) {
			zap.S().Warnf("[MQTT网关] CONNECT 鉴权失败, 拒绝设备: %s", username)
			_ = ws.WriteMessage(websocket.BinaryMessage, buildConnackReject(protocolLevel))
			return
		}
		deviceID = strings.ToLower(username)
		token = mqttToken
	}
	if isUser {
		zap.S().Infof("[MQTT网关] 管理端用户 %d 鉴权通过（连接身份 %s，仅转发其名下设备话题）", userID, deviceID)
	} else {
		zap.S().Infof("[MQTT网关] 设备 %s 鉴权通过", deviceID)
	}

	// 4. 连接外部 broker
	dialer := &net.Dialer{
		Timeout:   g.dialTimeout,
		KeepAlive: 30 * time.Second,
	}
	tcp, err := dialer.Dial("tcp", g.brokerAddr)
	if err != nil {
		zap.S().Warnf("[MQTT网关] 连接外部Broker失败: %s: %v", g.brokerAddr, err)
		return
	}
	defer tcp.Close()

	// 5. 转发 CONNECT
	if _, err := tcp.Write(connectBytes); err != nil {
		zap.S().Warnf("[MQTT网关] 转发 CONNECT 失败: %v", err)
		return
	}

	zap.S().Infof("[MQTT网关] 设备 %s 已桥接到外部Broker %s", deviceID, g.brokerAddr)

	// 5.5 记录设备 Token（onPublish 入库校验用），连接断开时清理。
	// 统一小写：方式1 HTTP Token 的 loginID 可能大小写混杂，
	// 而话题约定 / 设备ID 均以小写为准（onPublish/deviceToken 查找一致化）。
	// 用户连接不登记设备 Token（它不是设备，也不能用于设备上报入库）。
	deviceID = strings.ToLower(deviceID)
	if !isUser {
		g.registerDeviceToken(deviceID, token)
		defer g.unregisterDeviceToken(deviceID, token)
	}

	// 5.6 连接生命周期状态：登记 + 上线/下线判定
	conn := &mqttConn{
		deviceID: deviceID,
		timeout:  g.computeIdleTimeout(keepAlive),
		isUser:   isUser,
		userID:   userID,
	}
	if isUser {
		conn.acl = make(map[string]bool)
	} else if g.deviceSvc != nil {
		// 解析设备 Owner（owner 端上线/下线推送用），失败不阻断桥接
		if d, err := g.deviceSvc.GetByDeviceID(context.Background(), deviceID); err == nil && d.OwnerID > 0 {
			conn.ownerID = d.OwnerID
		}
	}
	g.registerConn(conn, ws, tcp)
	defer g.unregisterConn(conn)

	// 6. 双向桥接（ws→tcp 方向累积完整帧，转发前拦截 PUBLISH 写日志 + SUBSCRIBE 标记上线）
	done := make(chan struct{}, 2)

	// ws→tcp：读设备帧转发 broker。逐帧刷新读超时（空闲看护）：
	// MQTT 规范要求设备在 keepalive 内发送任意包（通常 PINGREQ），
	// 超过 1.5×keepalive 无任何帧 → 判定设备失联，强制关闭双端 →
	// unregisterConn 立即置 OFFLINE（不等离线同步器周期扫描）。
	go func() {
		defer func() { done <- struct{}{} }()
		var buf []byte
		for {
			_ = ws.SetReadDeadline(time.Now().Add(conn.timeout))
			mt, data, err := ws.ReadMessage()
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					zap.S().Warnf("[MQTT网关] 设备 %s 空闲超时（%.0fs 无任何 MQTT 帧），强制下线",
						conn.deviceID, conn.timeout.Seconds())
				}
				conn.close() // 双端关闭：踢醒对侧泵，触发 broker 遗嘱发布与平台离线判定
				return
			}
			if mt != websocket.BinaryMessage {
				continue
			}
			buf = append(buf, data...)

			for {
				total, ok := mqttPacketTotalLength(buf)
				if !ok || len(buf) < total {
					break
				}
				frame := buf[:total]
				buf = buf[total:]

				if conn.isUser {
					// 用户连接：仅透明转发，且限制在「该用户名下设备」的 iot/{deviceId}/... 话题内。
					// 不写设备消息日志、不做上报入库、不触发设备上线判定。
					if !g.userFrameAllowed(conn, frame, protocolLevel) {
						zap.S().Warnf("[MQTT网关] 用户 %d 非法/越权话题帧，已丢弃", conn.userID)
						continue
					}
				} else {
					g.logPublish(frame, clientID, protocolLevel, conn.deviceID)
					g.onSubscribe(frame, protocolLevel, conn)
				}

				if _, err := tcp.Write(frame); err != nil {
					conn.close()
					return
				}
			}
		}
	}()

	// tcp→ws：读 broker 帧转发设备（下行通道：retained 配置快照 / 实时命令 / 遗嘱）
	go func() {
		defer func() { done <- struct{}{} }()
		reader := &mqttFrameReader{r: bufio.NewReader(tcp)}
		for {
			frame, err := reader.ReadFrame()
			if err != nil {
				conn.close()
				return
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				conn.close()
				return
			}
		}
	}()

	<-done
	conn.close()
	zap.S().Debugf("[MQTT网关] 设备 %s 连接结束", conn.deviceID)
}

// logPublish 拦截 PUBLISH 帧：写入 iot_message_log + 调用 onPublish 钩子
func (g *MqttGatewayController) logPublish(frame []byte, clientID string, protocolLevel byte, connDeviceID string) {
	if len(frame) < 2 || frame[0]&0xF0 != 0x30 {
		return
	}

	qos := (frame[0] & 0x06) >> 1
	retained := frame[0]&0x01 != 0

	_, pos, valid := mqttVarint(frame, 1)
	if !valid {
		return
	}
	if pos+2 > len(frame) {
		return
	}
	topicLen := int(binary.BigEndian.Uint16(frame[pos : pos+2]))
	pos += 2
	if pos+topicLen > len(frame) {
		return
	}
	topic := string(frame[pos : pos+topicLen])
	pos += topicLen

	if qos > 0 {
		pos += 2
	}

	// MQTT 5.0: 跳过 properties
	if protocolLevel == 5 && pos < len(frame) {
		if next, valid := mqttSkipProperties(frame, pos); valid {
			pos = next
		}
	}

	payload := bytes.Trim(frame[pos:], "\x00")

	// 写入 iot_message_log（direction=up）
	category := "other"
	switch {
	case strings.HasSuffix(topic, configReportTopicSuffix):
		category = "config_report"
	case strings.HasSuffix(topic, "/telemetry"):
		category = "telemetry"
	}
	zap.S().Debugf("[MQTT网关] PUBLISH topic=%s qos=%d", topic, qos)
	if g.msgRepo != nil {
		go func() {
			if _, err := g.msgRepo.Create(context.Background(), &ent.MessageLog{
				Direction: "up",
				Category:  category,
				DeviceID:  connDeviceID,
				Topic:     topic,
				Payload:   string(payload),
				Qos:       int(qos),
				Retained:  retained,
				ClientID:  clientID,
				BrokerURL: config.Cfg.MQTT.BrokerURL,
				CreatedAt: time.Now(),
			}); err != nil {
				zap.S().Warnf("[MQTT网关] 写入消息日志失败: %v", err)
			}
		}()
	}

	// 用户自定义解析钩子
	g.onPublish(topic, payload, connDeviceID)
}

// configReportTopicSuffix iot/{deviceId}/config/report 的设备配置回执主题
const configReportTopicSuffix = "/config/report"

// onPublish PUBLISH 消息解析与入库
// 此时 frame 已写入 iot_message_log 并转发到外部 broker
//
// connDeviceID 为本连接 CONNECT 鉴权通过的设备ID（每条连接由各自 goroutine 串行调用）。
//
// Topic 约定：/前缀/设备ID/...，如 iot/{deviceId}/telemetry
// Payload 约定：与 HTTP 上报接口 /api/devices/{deviceId}/sensorData 相同的 JSON 格式：
//
//	{"sensors":[{"name":"temp","type":"temperature","value":25.5,"timestamp":"..."}]}
//
// 入库链路与 ReportData 完全一致：Token校验 → 更新设备状态缓存 →
// Redis 缓冲队列 → 后台批量写 InfluxDB + PostgreSQL 活跃时间
func (g *MqttGatewayController) onPublish(topic string, payload []byte, connDeviceID string) {
	list := strings.Split(topic, "/")
	if len(list) < 3 {
		zap.S().Debugf("[MQTT网关] PUBLISH topic=%s payload=%s (未处理)", topic, string(payload))
		return
	}
	deviceID := strings.ToLower(list[1])

	// 设备配置回执：iot/{deviceId}/config/report
	// 与 HTTP POST /api/devices/{deviceId}/config/report 同协议（DeviceConfigReport），
	// 仅接受本连接鉴权设备自身的话题，回写 status=acked
	if len(list) == 4 && strings.HasSuffix(topic, configReportTopicSuffix) {
		g.handleConfigReport(connDeviceID, deviceID, payload)
		return
	}

	if g.reportSvc == nil {
		zap.S().Debugf("[MQTT网关] PUBLISH topic=%s payload=%s (上报服务未注入，跳过入库)", topic, string(payload))
		return
	}

	// 1. 解析 JSON → DeviceStatusDTO（与 HTTP 上报同一 DTO）
	var dto entity.DeviceStatusDTO
	if err := json.Unmarshal(payload, &dto); err != nil {
		zap.S().Debugf("[MQTT网关] PUBLISH payload 解析失败 [device=%s topic=%s]: %v", deviceID, topic, err)
		return
	}
	if len(dto.Sensors) == 0 {
		zap.S().Debugf("[MQTT网关] PUBLISH payload 无传感器数据 [device=%s topic=%s]", deviceID, topic)
		return
	}

	// 2. 取该设备框架鉴权时使用的 Token（与 HTTP 上报共用 Sa-Token 校验）
	token := g.deviceToken(deviceID)
	if token == "" {
		zap.S().Warnf("[MQTT网关] PUBLISH 未找到设备Token，跳过入库 [device=%s topic=%s]", deviceID, topic)
		return
	}

	// 3. 异步入库：Token校验 → 状态缓存 → Redis缓冲 → InfluxDB（不阻塞帧转发）
	go func() {
		if err := g.reportSvc.ReportStatus(context.Background(), deviceID, token, dto); err != nil {
			zap.S().Warnf("[MQTT网关] PUBLISH 数据入库失败 [device=%s topic=%s]: %v", deviceID, topic, err)
			return
		}
		// 4. 写入设备 MQTT 消息历史（List + Lua 原子 LPush+LTrim+Expire，保留最近 499 条/24h）
		if g.cache != nil {
			if err := g.cache.LPushMQTTMessage(context.Background(), deviceID, map[string]any{
				"topic":   topic,
				"payload": string(payload),
				"ts":      time.Now().UnixMilli(),
			}); err != nil {
				zap.S().Debugf("[MQTT网关] 消息历史写入失败 [device=%s topic=%s]: %v", deviceID, topic, err)
			}
		}
		zap.S().Debugf("[MQTT网关] PUBLISH 数据已入库 [device=%s topic=%s sensors=%d]", deviceID, topic, len(dto.Sensors))
	}()
}

// handleConfigReport 处理设备配置回执上行 iot/{deviceId}/config/report
//
// Payload 与 HTTP POST /api/devices/{deviceId}/config/report 一致：
//
//	{"version": N, "config": {...}}
//
// 仅接受本连接鉴权设备自身话题（connDeviceID 必须等于话题中的设备ID），
// 语义等同 HTTP 的 DeviceAuth：设备只能确认自己的配置。
func (g *MqttGatewayController) handleConfigReport(connDeviceID, deviceID string, payload []byte) {
	if g.configSvc == nil {
		zap.S().Debugf("[MQTT网关] 配置回执服务未注入，跳过 [device=%s topic=iot/%s/config/report]", deviceID, deviceID)
		return
	}
	if connDeviceID == "" || strings.ToLower(connDeviceID) != deviceID {
		zap.S().Warnf("[MQTT网关] 拒绝跨设备配置回执 [conn=%s topicDevice=%s]", connDeviceID, deviceID)
		return
	}

	var rep entity.DeviceConfigReport
	if err := json.Unmarshal(payload, &rep); err != nil {
		zap.S().Warnf("[MQTT网关] 配置回执解析失败 [device=%s]: %v", deviceID, err)
		return
	}
	if rep.Version == 0 {
		zap.S().Warnf("[MQTT网关] 配置回执缺少版本 [device=%s]", deviceID)
		return
	}

	// 异步入库（不阻塞帧转发），与 HTTP 回执共用 DeviceConfigService.Report（status=acked）
	go func() {
		if err := g.configSvc.Report(context.Background(), deviceID, rep); err != nil {
			zap.S().Warnf("[MQTT网关] 配置回执入库失败 [device=%s version=%d]: %v", deviceID, rep.Version, err)
			return
		}
		zap.S().Debugf("[MQTT网关] 配置回执已记录 [device=%s version=%d]", deviceID, rep.Version)
	}()
}

// onSubscribe 拦截设备 SUBSCRIBE 帧：设备订阅自身配置/命令主题（如 iot/{deviceId}/config）
// 视为上线信号，将 Redis 缓存设备状态置为 ONLINE（与 WSS 设备连接上线行为一致：
// DeviceService.UpdateStatus 同时刷新 Redis 状态 Hash + PG 活跃时间），并通知 owner 管理端。
//
// 仅接受设备订阅自己的主题（list[1] == 连接鉴权设备ID，与 onPublish 话题约定一致），
// 防止跨设备订阅被误用为上线标记。conn.subOnce 保证每连接只标记一次。
func (g *MqttGatewayController) onSubscribe(frame []byte, protocolLevel byte, conn *mqttConn) {
	if g.deviceSvc == nil || conn == nil || conn.deviceID == "" {
		return
	}
	deviceID := strings.ToLower(conn.deviceID)

	for _, topic := range parseSubscribeTopics(frame, protocolLevel) {
		if !isOwnSubscribeTopic(topic, deviceID) {
			continue
		}

		conn.subOnce.Do(func() {
			if err := g.deviceSvc.UpdateStatus(context.Background(), deviceID, "ONLINE"); err != nil {
				zap.S().Warnf("[MQTT网关] 订阅上线状态更新失败 [device=%s topic=%s]: %v", deviceID, topic, err)
				return
			}
			conn.online.Store(true)
			zap.S().Infof("[MQTT网关] 设备 %s 上线（订阅 %s），Redis 状态置为 ONLINE", deviceID, topic)
			if conn.ownerID > 0 && g.onOnline != nil {
				g.onOnline(deviceID, conn.ownerID)
			}
		})
		return
	}
}

// isOwnSubscribeTopic 判断订阅主题是否为设备自身的配置/命令通道
// 遵循 onPublish 话题约定：/前缀/设备ID/...，例如 iot/{deviceId}/config、iot/{deviceId}/cmd、iot/{deviceId}/#
func isOwnSubscribeTopic(topic, deviceID string) bool {
	list := strings.Split(topic, "/")
	if len(list) < 3 {
		return false
	}
	if strings.ToLower(list[1]) != deviceID {
		return false
	}
	switch list[2] {
	case "config", "cmd", "#":
		return true
	default:
		return false
	}
}

// parseSubscribeTopics 解析 SUBSCRIBE 帧中的主题过滤器列表（支持 MQTT 3.1.1 与 5.0）
// 非 SUBSCRIBE 帧或帧不完整时返回 nil。
func parseSubscribeTopics(frame []byte, protocolLevel byte) []string {
	if len(frame) < 2 || frame[0]&0xF0 != 0x80 { // SUBSCRIBE 类型 = 8（高 4 位 0x80）
		return nil
	}

	// 跳过剩余长度（可变字节整数）
	_, pos, valid := mqttVarint(frame, 1)
	if !valid {
		return nil
	}

	// 可变头：packet identifier（2 字节）
	if pos+2 > len(frame) {
		return nil
	}
	pos += 2

	// MQTT 5.0: 跳过 properties（可变字节整数长度 + 属性字节）
	if protocolLevel == 5 {
		next, valid := mqttSkipProperties(frame, pos)
		if !valid {
			return nil
		}
		pos = next
	}

	// Payload：多个 (topic filter, QoS) 对
	var topics []string
	for pos+2 <= len(frame) {
		topicLen := int(binary.BigEndian.Uint16(frame[pos : pos+2]))
		pos += 2
		if pos+topicLen > len(frame) {
			return nil
		}
		topics = append(topics, string(frame[pos:pos+topicLen]))
		pos += topicLen + 1 // 主题 + 1 字节 QoS
		if pos > len(frame) {
			return nil
		}
	}
	return topics
}

// ============ 管理端用户接入（用户 auth → 设备 auth）============

// userTokenFromRequest 提取管理端用户登录 Token（Authorization Header/Cookie → Query token），
// 与用户通道 /api/ws/user 的提取方式一致。浏览器 WebSocket 无法自定义 Header，
// 因此额外支持 ?token=xxx（mqtt.js 可直接拼进 URL）。
func userTokenFromRequest(c *gin.Context) string {
	if tok := sagin.GetTokenFromCtx(c); tok != "" {
		return tok
	}
	return c.Query("token")
}

// authenticateUserToken 校验管理端用户 Token，返回用户 ID。
func authenticateUserToken(token string) (uint, bool) {
	info, err := stputil.GetTokenInfo(token)
	if err != nil || info == nil {
		return 0, false
	}
	uid, perr := strconv.ParseUint(info.LoginID, 10, 64)
	if perr != nil || uid == 0 {
		return 0, false
	}
	return uint(uid), true
}

// userFrameAllowed 校验用户连接的一帧是否可转发到 Broker：
//   - 非 PUBLISH / SUBSCRIBE 帧（PINGREQ 等）直接放行；
//   - PUBLISH / SUBSCRIBE 的话题必须形如 iot/{deviceId}/...，且该设备归属此用户。
//
// 结果按 deviceId 缓存到连接上（仅本连接泵协程访问，无需加锁）。
func (g *MqttGatewayController) userFrameAllowed(conn *mqttConn, frame []byte, protocolLevel byte) bool {
	if len(frame) < 2 {
		return true
	}

	var topics []string
	switch frame[0] & 0xF0 {
	case 0x30: // PUBLISH
		if t, ok := parsePublishTopic(frame); ok {
			topics = append(topics, t)
		}
	case 0x80: // SUBSCRIBE
		topics = parseSubscribeTopics(frame, protocolLevel)
	default:
		return true
	}

	for _, topic := range topics {
		list := strings.Split(topic, "/")
		if len(list) < 3 || list[0] != "iot" {
			return false
		}
		deviceID := strings.ToLower(list[1])
		allowed, hit := conn.acl[deviceID]
		if !hit {
			allowed = g.userOwnsDevice(conn.userID, deviceID)
			conn.acl[deviceID] = allowed
		}
		if !allowed {
			return false
		}
	}
	return true
}

// userOwnsDevice 设备是否归属指定用户。
func (g *MqttGatewayController) userOwnsDevice(userID uint, deviceID string) bool {
	if g.deviceSvc == nil || userID == 0 {
		return false
	}
	d, err := g.deviceSvc.GetByDeviceID(context.Background(), deviceID)
	return err == nil && d != nil && d.OwnerID == userID
}

// parsePublishTopic 解析 PUBLISH 帧的 topic（跳过可变长度与 topic 长度字段），失败返回 ok=false。
func parsePublishTopic(frame []byte) (string, bool) {
	if len(frame) < 2 || frame[0]&0xF0 != 0x30 {
		return "", false
	}
	_, pos, valid := mqttVarint(frame, 1)
	if !valid || pos+2 > len(frame) {
		return "", false
	}
	topicLen := int(binary.BigEndian.Uint16(frame[pos : pos+2]))
	pos += 2
	if topicLen <= 0 || pos+topicLen > len(frame) {
		return "", false
	}
	return string(frame[pos : pos+topicLen]), true
}

// ============ CONNECT 解析 & 鉴权 ============

// authenticateDevice 用框架 Sa-Token 校验 CONNECT 凭证：username=设备ID, password=设备Token
func authenticateDevice(deviceID, token string) bool {
	if deviceID == "" || token == "" {
		return false
	}
	deviceMgr := middleware.GetDeviceManager()
	if deviceMgr == nil {
		zap.S().Warn("[MQTT网关] 设备 Manager 未初始化，拒绝连接")
		return false
	}
	loginID, err := deviceMgr.GetLoginID(token)
	return err == nil && loginID == deviceID
}

// buildConnackReject 构造 CONNACK not authorized（0x05）拒绝包
func buildConnackReject(protocolLevel byte) []byte {
	if protocolLevel == 5 {
		return []byte{0x20, 0x03, 0x00, 0x87, 0x00}
	}
	return []byte{0x20, 0x02, 0x00, 0x05}
}

// ============ MQTT 帧工具 ============

// mqttVarint 读取 MQTT 可变字节整数（remaining length 与 property length 同编码）。
// 从 buf[start] 开始，返回 (value, nextPos, ok)；最多 4 字节，越界/超长返回 ok=false。
func mqttVarint(buf []byte, start int) (int, int, bool) {
	value, mul, pos := 0, 1, start
	for i := 0; i < 4; i++ {
		if pos >= len(buf) {
			return 0, pos, false
		}
		b := buf[pos]
		value += int(b&0x7f) * mul
		mul *= 128
		pos++
		if b&0x80 == 0 {
			return value, pos, true
		}
	}
	return 0, pos, false
}

// mqttSkipProperties 跳过 MQTT 5.0 属性块，返回跳过后的位置与是否有效。
func mqttSkipProperties(buf []byte, pos int) (int, bool) {
	propLen, next, ok := mqttVarint(buf, pos)
	if !ok {
		return pos, false
	}
	next += propLen
	if next > len(buf) {
		return pos, false
	}
	return next, true
}

func parseConnectCredentials(packet []byte) (clientID, username, password string, protocolLevel byte, keepAlive uint16, ok bool) {
	if len(packet) < 2 || packet[0] != 0x10 {
		return "", "", "", 0, 0, false
	}

	pos, valid := 0, false
	if _, pos, valid = mqttVarint(packet, 1); !valid {
		return "", "", "", 0, 0, false
	}

	if pos+2 > len(packet) {
		return "", "", "", 0, 0, false
	}
	nameLen := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
	pos += 2 + nameLen
	if pos+4 > len(packet) {
		return "", "", "", 0, 0, false
	}

	protocolLevel = packet[pos]
	flags := packet[pos+1]
	keepAlive = binary.BigEndian.Uint16(packet[pos+2 : pos+4]) // 心跳间隔（秒），0=禁用
	pos += 4

	if protocolLevel == 5 {
		next, valid := mqttSkipProperties(packet, pos)
		if !valid {
			return "", "", "", 0, 0, false
		}
		pos = next
	}

	// Payload 顺序：clientID → [will properties/topic/payload] → username → password
	if pos+2 > len(packet) {
		return "", "", "", 0, 0, false
	}
	cidLen := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
	clientID = string(packet[pos+2 : pos+2+cidLen])
	pos += 2 + cidLen

	// 跳过 Will（遗嘱）topic/payload：设备遗嘱仍随 CONNECT 原样透传给外部 Broker，
	// 设备异常断线时由 Broker 发布到遗嘱主题；平台离线判定不依赖订阅遗嘱主题，
	// 而是直接由「连接死亡」驱动（unregisterConn），效果等同且更快。
	if flags&0x04 != 0 {
		if protocolLevel == 5 {
			if pos >= len(packet) {
				return "", "", "", 0, 0, false
			}
			wpLen := int(packet[pos]) // will properties 长度（1B）
			pos += 1 + wpLen
		}
		if pos+2 > len(packet) {
			return "", "", "", 0, 0, false
		}
		wtLen := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
		pos += 2 + wtLen
		if pos+2 > len(packet) {
			return "", "", "", 0, 0, false
		}
		wpLen := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
		pos += 2 + wpLen
	}

	if flags&0x80 != 0 {
		if pos+2 > len(packet) {
			return "", "", "", 0, 0, false
		}
		ul := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
		pos += 2
		if pos+ul > len(packet) {
			return "", "", "", 0, 0, false
		}
		username = string(packet[pos : pos+ul])
		pos += ul
	}

	if flags&0x40 != 0 {
		if pos+2 > len(packet) {
			return "", "", "", 0, 0, false
		}
		pl := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
		pos += 2
		if pos+pl > len(packet) {
			return "", "", "", 0, 0, false
		}
		password = string(packet[pos : pos+pl])
	}

	return clientID, username, password, protocolLevel, keepAlive, true
}

func mqttPacketTotalLength(packet []byte) (int, bool) {
	if len(packet) < 2 || packet[0]&0xF0 == 0 {
		return 0, false
	}
	rl, pos, ok := mqttVarint(packet, 1)
	if !ok {
		return 0, false
	}
	return pos + rl, true
}

type mqttFrameReader struct {
	r *bufio.Reader
}

func (fr *mqttFrameReader) ReadFrame() ([]byte, error) {
	first, err := fr.r.ReadByte()
	if err != nil {
		return nil, err
	}

	header := []byte{first}
	multiplier := 1
	remaining := 0
	for i := 0; i < 4; i++ {
		b, err := fr.r.ReadByte()
		if err != nil {
			return nil, err
		}
		header = append(header, b)
		remaining += int(b&0x7f) * multiplier
		multiplier *= 128
		if b&0x80 == 0 {
			break
		}
		if i == 3 {
			return nil, errors.New("mqtt frame: invalid remaining length")
		}
	}

	body := make([]byte, remaining)
	if _, err := io.ReadFull(fr.r, body); err != nil {
		return nil, err
	}
	return append(header, body...), nil
}
