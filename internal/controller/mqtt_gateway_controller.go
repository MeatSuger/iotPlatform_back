package controller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

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
//  2. 发布日志：所有 PUBLISH 帧写入 mqtt_publish_log 表
//  3. 数据入库：PUBLISH payload 解析后走 DeviceReportService（onPublish 钩子）
//
// 鉴权（框架 Sa-Token 体系，两种凭证方式二选一，均不拦截 WebSocket 升级，保证透传）：
//  1. HTTP 层：请求携带 X-Device-Token（Header/Cookie/Query），mqtt.js 等可拼 URL 参数的客户端
//  2. MQTT 层：CONNECT 包 username=设备ID / password=设备Token，标准 MQTT 客户端（paho/ESP32 等）
//
// 鉴权失败时回 MQTT 标准 CONNACK not authorized(0x05)，而不是 HTTP 拒绝。
//
// 链路：设备 --wss--> 网关(框架 Sa-Token 鉴权) --tcp--> 外部 Mosquitto(1883)
type MqttGatewayController struct {
	brokerAddr  string
	dialTimeout time.Duration
	logRepo     *repository.MqttPublishLogRepo
	reportSvc   *service.DeviceReportService // 设备数据上报服务（onPublish 入库用）
	cache       *cache.RedisCache            // MQTT 消息历史缓存（可为 nil）

	// 设备Token注册表：框架鉴权通过后记录 deviceID → token，
	// 供 onPublish 上报时使用（与 HTTP 上报共用 Sa-Token 体系）
	mu           sync.RWMutex
	deviceTokens map[string]string
}

// NewMqttGatewayController 创建 MQTT 桥接网关控制器
// reportSvc 可为 nil（此时 onPublish 只记日志不入库）；cache 可为 nil（不记录消息历史）
func NewMqttGatewayController(logRepo *repository.MqttPublishLogRepo, reportSvc *service.DeviceReportService, cache *cache.RedisCache) *MqttGatewayController {
	addr := strings.TrimPrefix(config.Cfg.MQTT.BrokerURL, "tcp://")
	addr = strings.TrimPrefix(addr, "ssl://")
	return &MqttGatewayController{
		brokerAddr:   addr,
		dialTimeout:  5 * time.Second,
		logRepo:      logRepo,
		reportSvc:    reportSvc,
		cache:        cache,
		deviceTokens: make(map[string]string),
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

	// 1. 累积 WS 消息直到拼出完整 CONNECT 包
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

	// 2. 解析 CONNECT 帧（clientID/protocolLevel 用于日志与 PUBLISH 解析）
	clientID, username, mqttToken, protocolLevel, parseOK := parseConnectCredentials(connectBytes)
	zap.S().Debugf("[MQTT网关] 设备连接: client=%s, device=%s, level=%d, parseOK=%v", clientID, username, protocolLevel, parseOK)

	// 3. 鉴权（框架 Sa-Token 体系，两种凭证方式二选一）
	var deviceID, token string
	if httpToken := middleware.GetDeviceToken(c); httpToken != "" {
		// 方式1：HTTP 层 X-Device-Token（Header/Cookie/Query），适合 mqtt.js 等可拼 URL 的客户端
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
	} else {
		// 方式2：MQTT CONNECT username=设备ID / password=设备Token（标准 MQTT 客户端）
		if !parseOK || !authenticateDevice(username, mqttToken) {
			zap.S().Warnf("[MQTT网关] CONNECT 鉴权失败, 拒绝设备: %s", username)
			_ = ws.WriteMessage(websocket.BinaryMessage, buildConnackReject(protocolLevel))
			return
		}
		deviceID = strings.ToLower(username)
		token = mqttToken
	}
	zap.S().Infof("[MQTT网关] 设备 %s 鉴权通过", deviceID)

	// 4. 连接外部 broker
	tcp, err := net.DialTimeout("tcp", g.brokerAddr, g.dialTimeout)
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

	// 5.5 记录设备 Token（onPublish 入库校验用），连接断开时清理
	g.registerDeviceToken(deviceID, token)
	defer g.unregisterDeviceToken(deviceID, token)

	// 6. 双向桥接（ws→tcp 方向累积完整帧，转发前拦截 PUBLISH 写日志）
	done := make(chan struct{}, 2)

	go func() {
		defer func() { done <- struct{}{} }()
		var buf []byte
		for {
			mt, data, err := ws.ReadMessage()
			if err != nil {
				_ = tcp.Close()
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

				g.logPublish(frame, clientID, protocolLevel)

				if _, err := tcp.Write(frame); err != nil {
					_ = tcp.Close()
					return
				}
			}
		}
	}()

	go func() {
		defer func() { done <- struct{}{} }()
		reader := &mqttFrameReader{r: bufio.NewReader(tcp)}
		for {
			frame, err := reader.ReadFrame()
			if err != nil {
				return
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				return
			}
		}
	}()

	<-done
	zap.S().Debugf("[MQTT网关] 设备 %s 连接结束", deviceID)
}

// logPublish 拦截 PUBLISH 帧：写入 mqtt_publish_log + 调用 onPublish 钩子
func (g *MqttGatewayController) logPublish(frame []byte, clientID string, protocolLevel byte) {
	if len(frame) < 2 || frame[0]&0xF0 != 0x30 {
		return
	}

	qos := (frame[0] & 0x06) >> 1
	retained := frame[0]&0x01 != 0

	pos := 1
	for pos < len(frame) && frame[pos]&0x80 != 0 {
		pos++
	}
	pos++
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
		propLen, mul := 0, 1
		for i := 0; i < 4 && pos < len(frame); i++ {
			b := frame[pos]
			propLen += int(b&0x7f) * mul
			mul *= 128
			pos++
			if b&0x80 == 0 {
				break
			}
		}
		pos += propLen
	}

	payload := bytes.Trim(frame[pos:], "\x00")

	// 写入 mqtt_publish_log
	zap.S().Debugf("[MQTT网关] PUBLISH topic=%s qos=%d", topic, qos)
	if g.logRepo != nil {
		go func() {
			if _, err := g.logRepo.Create(context.Background(), &ent.MqttPublishLog{
				Topic:      topic,
				ClientID:   clientID,
				Payload:    string(payload),
				Qos:        int(qos),
				Retained:   retained,
				BrokerURL:  config.Cfg.MQTT.BrokerURL,
				CreateTime: time.Now(),
			}); err != nil {
				zap.S().Warnf("[MQTT网关] 写入发布日志失败: %v", err)
			}
		}()
	}

	// 用户自定解析钩子
	g.onPublish(topic, payload)
}

// onPublish PUBLISH 消息解析与入库
// 此时 frame 已写入 mqtt_publish_log 并转发到外部 broker
//
// Topic 约定：/前缀/设备ID/...，如 iot/{deviceId}/telemetry
// Payload 约定：与 HTTP 上报接口 /api/data/{deviceId}/Data 相同的 JSON 格式：
//
//	{"sensors":[{"name":"temp","type":"temperature","value":25.5,"timestamp":"..."}]}
//
// 入库链路与 ReportData 完全一致：Token校验 → 更新设备状态缓存 →
// Redis 缓冲队列 → 后台批量写 InfluxDB + PostgreSQL 活跃时间
func (g *MqttGatewayController) onPublish(topic string, payload []byte) {
	list := strings.Split(topic, "/")
	if len(list) < 3 {
		zap.S().Debugf("[MQTT网关] PUBLISH topic=%s payload=%s (未处理)", topic, string(payload))
		return
	}
	deviceID := strings.ToLower(list[1])

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
		zap.S().Infof("[MQTT网关] PUBLISH 数据已入库 [device=%s topic=%s sensors=%d]", deviceID, topic, len(dto.Sensors))
	}()
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

func parseConnectCredentials(packet []byte) (clientID, username, password string, protocolLevel byte, ok bool) {
	if len(packet) < 2 || packet[0] != 0x10 {
		return "", "", "", 0, false
	}

	pos := 1
	for pos < len(packet) && packet[pos]&0x80 != 0 {
		pos++
	}
	pos++

	if pos+2 > len(packet) {
		return "", "", "", 0, false
	}
	nameLen := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
	pos += 2 + nameLen
	if pos+4 > len(packet) {
		return "", "", "", 0, false
	}

	protocolLevel = packet[pos]
	flags := packet[pos+1]
	pos += 4

	if protocolLevel == 5 {
		if pos >= len(packet) {
			return "", "", "", 0, false
		}
		propLen, multiplier := 0, 1
		for i := 0; i < 4; i++ {
			if pos >= len(packet) {
				return "", "", "", 0, false
			}
			b := packet[pos]
			propLen += int(b&0x7f) * multiplier
			multiplier *= 128
			pos++
			if b&0x80 == 0 {
				break
			}
		}
		pos += propLen
		if pos > len(packet) {
			return "", "", "", 0, false
		}
	}

	if pos+2 > len(packet) {
		return "", "", "", 0, false
	}
	cidLen := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
	clientID = string(packet[pos+2 : pos+2+cidLen])
	pos += 2 + cidLen

	if flags&0x04 != 0 {
		if protocolLevel == 5 {
			if pos >= len(packet) {
				return "", "", "", 0, false
			}
			wpLen := int(packet[pos])
			pos += 1 + wpLen
		}
		if pos+2 > len(packet) {
			return "", "", "", 0, false
		}
		wtLen := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
		pos += 2 + wtLen
		if pos+2 > len(packet) {
			return "", "", "", 0, false
		}
		wpLen := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
		pos += 2 + wpLen
	}

	if flags&0x80 != 0 {
		if pos+2 > len(packet) {
			return "", "", "", 0, false
		}
		ul := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
		pos += 2
		if pos+ul > len(packet) {
			return "", "", "", 0, false
		}
		username = string(packet[pos : pos+ul])
		pos += ul
	}

	if flags&0x40 != 0 {
		if pos+2 > len(packet) {
			return "", "", "", 0, false
		}
		pl := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
		pos += 2
		if pos+pl > len(packet) {
			return "", "", "", 0, false
		}
		password = string(packet[pos : pos+pl])
	}

	return clientID, username, password, protocolLevel, true
}

// ============ MQTT 帧工具 ============

func mqttPacketTotalLength(packet []byte) (int, bool) {
	if len(packet) < 2 || packet[0]&0xF0 == 0 {
		return 0, false
	}
	multiplier := 1
	rl := 0
	pos := 1
	for i := 0; i < 4; i++ {
		if pos >= len(packet) {
			return 0, false
		}
		b := packet[pos]
		rl += int(b&0x7f) * multiplier
		multiplier *= 128
		pos++
		if b&0x80 == 0 {
			return pos + rl, true
		}
	}
	return 0, false
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
