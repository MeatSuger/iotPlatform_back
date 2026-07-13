package websocket

import (
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/gorilla/websocket"
	mqttEntity "github.com/yu/iot-platform-go/entity/mqtt"
)

// MqttPublisher WebSocket需要的MQTT发布能力（接口解耦，避免循环依赖）
type MqttPublisher interface {
	Publish(req mqttEntity.PublishRequest) error
}

// 升级器配置
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有来源（生产环境应限制）
	},
}

const (
	// 写入超时
	writeWait = 10 * time.Second

	// 读取Pong的超时
	pongWait = 60 * time.Second

	// 发送Ping的间隔（必须小于pongWait）
	pingPeriod = (pongWait * 9) / 10

	// 最大消息大小
	maxMessageSize = 1024 * 1024 // 1MB
)

// DevTokenValidator 设备 Token 校验函数签名
type DevTokenValidator func(token string) (deviceID string, err error)

// DeviceMessageHandler 设备上行消息处理回调（传感器数据 / ACK / 心跳）
type DeviceMessageHandler func(deviceID, token string, message []byte)

// WsHandler WebSocket处理器
type WsHandler struct {
	hub              *Hub
	mqttClient       MqttPublisher
	validateDevToken DevTokenValidator    // 可选，设备 WS 认证
	onDeviceMessage  DeviceMessageHandler // 设备上行消息回调
}

// NewWsHandler 创建WebSocket处理器
func NewWsHandler(hub *Hub, mqttClient MqttPublisher) *WsHandler {
	return &WsHandler{
		hub:        hub,
		mqttClient: mqttClient,
	}
}

// SetDeviceTokenValidator 设置设备 Token 校验器（用于设备 WebSocket 端点）
func (h *WsHandler) SetDeviceTokenValidator(v DevTokenValidator) {
	h.validateDevToken = v
}

// SetDeviceMessageHandler 设置设备上行消息处理器
func (h *WsHandler) SetDeviceMessageHandler(handler DeviceMessageHandler) {
	h.onDeviceMessage = handler
}

// Handle 处理WebSocket连接
func (h *WsHandler) Handle(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		zap.S().Infof("[WebSocket] 升级失败: %v", err)
		return
	}

	client := &Client{
		Conn: conn,
		Send: make(chan []byte, 256),
	}

	h.hub.Register(client)

	// 启动读写协程
	go h.writePump(client)
	go h.readPump(client)
}

// readPump 从WebSocket读取消息并转发到MQTT
func (h *WsHandler) readPump(client *Client) {
	defer func() {
		h.hub.Unregister(client)
		client.Conn.Close()
	}()

	client.Conn.SetReadLimit(maxMessageSize)
	client.Conn.SetReadDeadline(time.Now().Add(pongWait))
	client.Conn.SetPongHandler(func(string) error {
		client.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := client.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				zap.S().Infof("[WebSocket] 读取错误: %v", err)
			}
			break
		}

		// 解析并转发到MQTT
		var req mqttEntity.PublishRequest
		if err := json.Unmarshal(message, &req); err != nil {
			zap.S().Infof("[WebSocket] JSON解析失败: %v", err)
			continue
		}

		if err := h.mqttClient.Publish(req); err != nil {
			zap.S().Infof("[WebSocket] MQTT发布失败: %v", err)
		}
	}
}

// HandleDevice 设备 WebSocket（实时下放通道）— 通过 query token 认证
func (h *WsHandler) HandleDevice(w http.ResponseWriter, r *http.Request) {
	// 校验设备 Token
	if h.validateDevToken == nil {
		http.Error(w, "设备WebSocket未启用", http.StatusServiceUnavailable)
		return
	}
	// 多种方式提取 Token：X-Device-Token > Authorization > Cookie > Query
	token := r.Header.Get("X-Device-Token")
	if token == "" {
		token = r.Header.Get("Authorization") // ESP32 setAuthorization()
	}
	if token == "" {
		if c, _ := r.Cookie("X-Device-Token"); c != nil {
			token = c.Value
		}
	}
	if token == "" {
		token = r.URL.Query().Get("X-Device-Token")
	}
	if token == "" {
		zap.S().Warnf("[WebSocket] 设备连接缺少Token [remote=%s]", r.RemoteAddr)
		http.Error(w, "缺少设备Token", http.StatusUnauthorized)
		return
	}

	// 打印 token 前后各4位用于排查
	tokenPreview := token
	if len(token) > 12 {
		tokenPreview = token[:4] + "..." + token[len(token)-4:]
	}
	zap.S().Infof("[WebSocket] 设备Token校验 [token=%s]", tokenPreview)

	deviceID, err := h.validateDevToken(token)
	if err != nil {
		zap.S().Warnf("[WebSocket] 设备Token无效 [token=%s]: %v", tokenPreview, err)
		http.Error(w, "设备Token无效", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		zap.S().Infof("[WebSocket] 设备升级失败 [device=%s]: %v", deviceID, err)
		return
	}

	client := &Client{
		Conn:     conn,
		Send:     make(chan []byte, 256),
		DeviceID: deviceID,
		Token:    token,
	}

	h.hub.Register(client)
	zap.S().Infof("[WebSocket] 设备实时通道建立 [device=%s]", deviceID)

	go h.writePump(client)
	go h.readDevicePump(client)
}

// readDevicePump 从设备 WebSocket 读取上行消息（传感器数据 / ACK / 心跳）
func (h *WsHandler) readDevicePump(client *Client) {
	defer func() {
		h.hub.Unregister(client)
		client.Conn.Close()
	}()

	client.Conn.SetReadLimit(maxMessageSize)
	client.Conn.SetReadDeadline(time.Now().Add(pongWait))
	client.Conn.SetPongHandler(func(string) error {
		client.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := client.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				zap.S().Infof("[WebSocket] 设备读取错误 [device=%s]: %v", client.DeviceID, err)
			}
			break
		}

		// 尝试解析消息类型，用于日志
		var envelope struct {
			Type string `json:"type"`
		}
		msgType := "unknown"
		if json.Unmarshal(message, &envelope) == nil && envelope.Type != "" {
			msgType = envelope.Type
		}

		zap.S().Infof("[WebSocket] 设备上行消息 [device=%s, type=%s, size=%d]", client.DeviceID, msgType, len(message))

		// 回调上层业务处理
		if h.onDeviceMessage != nil {
			h.onDeviceMessage(client.DeviceID, client.Token, message)
		}
	}
}

// writePump 将消息从channel写入WebSocket
func (h *WsHandler) writePump(client *Client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		client.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.Send:
			client.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub关闭了channel
				client.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := client.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// 批量发送队列中的消息
			n := len(client.Send)
			for i := 0; i < n; i++ {
				w.Write([]byte("\n"))
				w.Write(<-client.Send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			client.Conn.SetWriteDeadline(time.Now().Add(pingPeriod))
			if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
