package service

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/gorilla/websocket"

	"iot-platform.local/internal/middleware"
	"iot-platform.local/pkg/config"
)

// MqttWsGateway MQTT over WebSocket 鉴权网关（胶水层）
//
// 定位：只做两件事
//  1. 鉴权：设备连 /api/ws/mqtt/broker，网关解析 CONNECT 包中的
//     username=设备ID / password=设备Token，校验设备身份
//  2. 透明转发：校验通过后，建立到外部 MQTT Docker 的 TCP 连接，
//     将整个 MQTT 会话按包边界双向桥接（不解析业务协议、不维护会话状态）
//
// 链路：设备 --wss--> 网关(鉴权) --tcp--> 外部 Mosquitto(1883)
type MqttWsGateway struct {
	brokerAddr  string // 外部 MQTT Docker 地址（host:port）
	dialTimeout time.Duration
}

// NewMqttWsGateway 创建 MQTT 鉴权网关，broker 地址取自配置 mqtt.broker-url
func NewMqttWsGateway() *MqttWsGateway {
	addr := strings.TrimPrefix(config.Cfg.MQTT.BrokerURL, "tcp://")
	addr = strings.TrimPrefix(addr, "ssl://")
	return &MqttWsGateway{brokerAddr: addr, dialTimeout: 5 * time.Second}
}

// HandleWebSocket 处理设备经 Gin HTTP 入口进来的 MQTT-over-WebSocket 连接
func (g *MqttWsGateway) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
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

	// 1. 累积 WS 消息直到拼出完整 CONNECT 包（mqtt.js 可能分片发送：固定头一个消息、body 一个消息）
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
			connectBytes = connectBytes[:total] // 完整 CONNECT
			break
		}
	}

	// 2. 解析 CONNECT 提取设备凭证
	deviceID, token, protocolLevel, parseOK := parseConnectCredentials(connectBytes)
	zap.S().Debugf("[MQTT网关] 设备连接: device=%s, level=%d, parseOK=%v", deviceID, protocolLevel, parseOK)

	// 3. 鉴权
	if config.Cfg.MqttGateway.RequireAuth {
		if !parseOK || !authenticateDevice(deviceID, token) {
			zap.S().Warnf("[MQTT网关] 鉴权失败, 拒绝设备: %s", deviceID)
			_ = ws.WriteMessage(websocket.BinaryMessage, buildConnackReject(protocolLevel))
			return
		}
		zap.S().Infof("[MQTT网关] 设备 %s 鉴权通过", deviceID)
	}

	// 4. 建立到外部 MQTT Docker 的 TCP 连接
	tcp, err := net.DialTimeout("tcp", g.brokerAddr, g.dialTimeout)
	if err != nil {
		zap.S().Warnf("[MQTT网关] 连接外部Broker失败: %s: %v", g.brokerAddr, err)
		return
	}
	defer tcp.Close()

	// 5. 把完整 CONNECT 转发给外部 broker
	if _, err := tcp.Write(connectBytes); err != nil {
		zap.S().Warnf("[MQTT网关] 转发 CONNECT 失败: %v", err)
		return
	}

	zap.S().Infof("[MQTT网关] 设备 %s 已桥接到外部Broker %s", deviceID, g.brokerAddr)

	// 6. 双向桥接（按 MQTT 包边界转发）
	done := make(chan struct{}, 2)
	// 设备 WS -> 外部 broker TCP
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			mt, data, err := ws.ReadMessage()
			if err != nil {
				_ = tcp.Close()
				return
			}
			if mt != websocket.BinaryMessage {
				continue
			}
			if _, err := tcp.Write(data); err != nil {
				_ = tcp.Close()
				return
			}
		}
	}()
	// 外部 broker TCP -> 设备 WS
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

	<-done // 任一端关闭即结束
	zap.S().Debugf("[MQTT网关] 设备 %s 连接结束", deviceID)
}

// authenticateDevice 校验设备凭证：username=设备ID, password=设备Token（Sa-Token）
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
	if err != nil || loginID != deviceID {
		return false
	}
	return true
}

// parseConnectCredentials 解析 MQTT CONNECT 包，提取 username(设备ID) / password(设备Token) 和协议版本
func parseConnectCredentials(packet []byte) (username, password string, protocolLevel byte, ok bool) {
	if len(packet) < 2 || packet[0] != 0x10 { // 必须是 CONNECT 包
		return "", "", 0, false
	}

	// 跳过 fixed header + remaining length（varint）
	pos := 1
	for pos < len(packet) && packet[pos]&0x80 != 0 {
		pos++
	}
	pos++ // 最后一个 remaining length 字节

	// protocol name: 2B 长度 + 名称
	if pos+2 > len(packet) {
		return "", "", 0, false
	}
	nameLen := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
	pos += 2 + nameLen
	if pos+4 > len(packet) {
		return "", "", 0, false
	}

	protocolLevel = packet[pos]
	flags := packet[pos+1]
	pos += 4 // protocol level(1) + connect flags(1) + keepalive(2)

	// MQTT 5.0: connect properties（varint 长度 + 内容）——在 clientId 之前
	if protocolLevel == 5 {
		if pos >= len(packet) {
			return "", "", 0, false
		}
		propLen := 0
		multiplier := 1
		for i := 0; i < 4; i++ {
			if pos >= len(packet) {
				return "", "", 0, false
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
			return "", "", 0, false
		}
	}

	// client identifier
	if pos+2 > len(packet) {
		return "", "", 0, false
	}
	cidLen := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
	pos += 2 + cidLen

	// will（如果有）
	if flags&0x04 != 0 {
		if protocolLevel == 5 {
			// 5.0 的 will properties: 1B 长度 + 内容
			if pos >= len(packet) {
				return "", "", 0, false
			}
			wpLen := int(packet[pos])
			pos += 1 + wpLen
		}
		if pos+2 > len(packet) {
			return "", "", 0, false
		}
		willTopicLen := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
		pos += 2 + willTopicLen
		if pos+2 > len(packet) {
			return "", "", 0, false
		}
		willPayloadLen := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
		pos += 2 + willPayloadLen
	}

	// username（bit7）
	if flags&0x80 != 0 {
		if pos+2 > len(packet) {
			return "", "", 0, false
		}
		ul := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
		pos += 2
		if pos+ul > len(packet) {
			return "", "", 0, false
		}
		username = string(packet[pos : pos+ul])
		pos += ul
	}

	// password（bit6）
	if flags&0x40 != 0 {
		if pos+2 > len(packet) {
			return "", "", 0, false
		}
		pl := int(binary.BigEndian.Uint16(packet[pos : pos+2]))
		pos += 2
		if pos+pl > len(packet) {
			return "", "", 0, false
		}
		password = string(packet[pos : pos+pl])
	}

	return username, password, protocolLevel, true
}

// buildConnackReject 构造拒绝连接的 CONNACK（bad user name or password）
func buildConnackReject(protocolLevel byte) []byte {
	if protocolLevel == 5 {
		// MQTT 5.0: session present(0) + reason(0x87 not authorized) + properties length(0)
		return []byte{0x20, 0x03, 0x00, 0x87, 0x00}
	}
	// MQTT 3.1.1: session present(0) + return code(0x05 not authorized)
	return []byte{0x20, 0x02, 0x00, 0x05}
}

// mqttPacketTotalLength 计算完整 MQTT 包预期大小（fixed header + remaining length + body）
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

// mqttFrameReader 从 TCP 流按 MQTT 包边界读取完整帧（fixed header + remaining length + body）
type mqttFrameReader struct {
	r *bufio.Reader
}

// ReadFrame 读取一个完整 MQTT 包
func (fr *mqttFrameReader) ReadFrame() ([]byte, error) {
	first, err := fr.r.ReadByte()
	if err != nil {
		return nil, err
	}

	// remaining length（varint，最多 4 字节）
	multiplier := 1
	remaining := 0
	header := []byte{first}
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
