package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/gorilla/websocket"

	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/repository"
	"iot-platform.local/pkg/config"
)

// MqttWsGateway MQTT over WebSocket 鉴权网关（胶水层）
//
// 功能：
//  1. 鉴权：设备 CONNECT 时校验 username=设备ID / password=设备Token
//  2. 透明转发：鉴权通过后双向桥接到外部 Mosquitto
//  3. 发布日志：所有 PUBLISH 帧写入 mqtt_publish_log 表
//
// 链路：设备 --wss--> 网关(鉴权) --tcp--> 外部 Mosquitto(1883)
type MqttWsGateway struct {
	brokerAddr  string
	dialTimeout time.Duration
	logRepo     *repository.MqttPublishLogRepo
}

// NewMqttWsGateway 创建 MQTT 鉴权网关
func NewMqttWsGateway(logRepo *repository.MqttPublishLogRepo) *MqttWsGateway {
	addr := strings.TrimPrefix(config.Cfg.MQTT.BrokerURL, "tcp://")
	addr = strings.TrimPrefix(addr, "ssl://")
	return &MqttWsGateway{brokerAddr: addr, dialTimeout: 5 * time.Second, logRepo: logRepo}
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

	// 2. 解析 CONNECT 凭证
	clientID, deviceID, token, protocolLevel, parseOK := parseConnectCredentials(connectBytes)
	zap.S().Debugf("[MQTT网关] 设备连接: client=%s, device=%s, level=%d, parseOK=%v", clientID, deviceID, protocolLevel, parseOK)

	// 3. 鉴权
	if config.Cfg.MqttGateway.RequireAuth {
		if !parseOK || !authenticateDevice(deviceID, token) {
			zap.S().Warnf("[MQTT网关] 鉴权失败, 拒绝设备: %s", deviceID)
			_ = ws.WriteMessage(websocket.BinaryMessage, buildConnackReject(protocolLevel))
			return
		}
		zap.S().Infof("[MQTT网关] 设备 %s 鉴权通过", deviceID)
	}

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
func (g *MqttWsGateway) logPublish(frame []byte, clientID string, protocolLevel byte) {
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

// onPublish 预留：用户自行编写 PUBLISH 消息的解析与入库逻辑
// 此时 frame 已写入 mqtt_publish_log 并转发到外部 broker
func (g *MqttWsGateway) onPublish(topic string, payload []byte) {
	// TODO: 用户自行编写
}

// ============ CONNECT 解析 & 鉴权 ============

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

func buildConnackReject(protocolLevel byte) []byte {
	if protocolLevel == 5 {
		return []byte{0x20, 0x03, 0x00, 0x87, 0x00}
	}
	return []byte{0x20, 0x02, 0x00, 0x05}
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
