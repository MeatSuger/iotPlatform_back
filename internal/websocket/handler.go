package websocket

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/sa-tokens/sa-token-go/stputil"
	"go.uber.org/zap"

	"github.com/gorilla/websocket"
	mqttEntity "github.com/yu/iot-platform-go/internal/model/mqtt"
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

// DeviceStatusUpdater 设备状态持久化回调（设备上线/下线时调用）
type DeviceStatusUpdater func(deviceID string, status string) error

// OwnerResolver 根据 deviceID 查询 ownerID 的函数签名
type OwnerResolver func(deviceID string) (ownerID uint, err error)

// UserCommandHandler 用户管理端下发命令回调（ownerID, deviceID, cmdType, payload）
// 返回 cmdID 和 error，由 service 层执行入队+推送
type UserCommandHandler func(ownerID uint, deviceID string, cmdType string, payload json.RawMessage) (cmdID uint, err error)

// WsHandler WebSocket处理器
type WsHandler struct {
	hub                 *Hub
	mqttClient          MqttPublisher
	validateDevToken    DevTokenValidator    // 可选，设备 WS 认证
	onDeviceMessage     DeviceMessageHandler // 设备上行消息回调
	resolveOwner        OwnerResolver        // 可选，设备→owner 查询
	onUserCommand       UserCommandHandler   // 可选，用户下发命令回调
	deviceStatusUpdater DeviceStatusUpdater  // 可选，设备状态持久化
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

// SetOwnerResolver 设置设备→Owner 查询器
func (h *WsHandler) SetOwnerResolver(r OwnerResolver) {
	h.resolveOwner = r
}

// SetUserCommandHandler 设置用户下发命令处理器
func (h *WsHandler) SetUserCommandHandler(handler UserCommandHandler) {
	h.onUserCommand = handler
}

// SetDeviceStatusUpdater 设置设备状态持久化回调
func (h *WsHandler) SetDeviceStatusUpdater(u DeviceStatusUpdater) {
	h.deviceStatusUpdater = u
}

// Handle 处理WebSocket连接（MQTT桥接，已不再使用，保留兼容）
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

// HandleUser 用户管理端 WebSocket（owner 连接后可接收其所有设备的实时消息）
// 认证方式：复用 sa-token stputil，与 HTTP AuthMiddleware 一致
// Token 提取：Header "Authorization" > Cookie "Authorization" > Query "token"
func (h *WsHandler) HandleUser(w http.ResponseWriter, r *http.Request) {
	// 多种方式提取用户 Token（与 sa-token TokenInterceptor 一致）
	token := r.Header.Get("Authorization")
	if token == "" {
		if c, _ := r.Cookie("Authorization"); c != nil {
			token = c.Value
		}
	}
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" {
		zap.S().Warnf("[UserWS] 用户连接缺少Token [remote=%s]", r.RemoteAddr)
		http.Error(w, "缺少用户Token", http.StatusUnauthorized)
		return
	}

	// 复用 sa-token stputil 全局管理器校验（与 AuthMiddleware 完全相同）
	if !stputil.IsLogin(token) {
		zap.S().Warnf("[UserWS] 用户Token无效或已过期 [remote=%s]", r.RemoteAddr)
		http.Error(w, "用户Token无效或已过期", http.StatusUnauthorized)
		return
	}

	loginID, err := stputil.GetLoginID(token)
	if err != nil {
		zap.S().Warnf("[UserWS] 获取用户ID失败 [remote=%s]: %v", r.RemoteAddr, err)
		http.Error(w, "Token格式无效", http.StatusUnauthorized)
		return
	}

	uid, err := strconv.ParseUint(loginID, 10, 64)
	if err != nil {
		zap.S().Warnf("[UserWS] 用户ID解析失败 [remote=%s, loginID=%s]: %v", r.RemoteAddr, loginID, err)
		http.Error(w, "Token格式无效", http.StatusUnauthorized)
		return
	}
	ownerID := uint(uid)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		zap.S().Infof("[UserWS] 升级失败 [user=%d]: %v", ownerID, err)
		return
	}

	client := &Client{
		Conn:    conn,
		Send:    make(chan []byte, 256),
		OwnerID: ownerID,
		Token:   token,
	}

	h.hub.RegisterUser(client)
	zap.S().Infof("[UserWS] 用户管理端通道建立 [user=%d]", ownerID)

	// 启动读写协程（用户端只需接收，readPump 仅处理 ping/pong/close）
	go h.writePump(client)
	go h.readUserPump(client)
}

// readUserPump 用户管理端读取协程 — 支持下发给指定设备的命令
func (h *WsHandler) readUserPump(client *Client) {
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
				zap.S().Infof("[UserWS] 读取错误 [user=%d]: %v", client.OwnerID, err)
			}
			break
		}

		// 解析消息：带 deviceId + payload 的是命令（与 HTTP POST /device/:deviceId/cmd body 一致，仅多 deviceId）
		// 仅 type="ping" 的是心跳
		var envelope struct {
			Type     string          `json:"type"`
			DeviceID string          `json:"deviceId"`
			Payload  json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(message, &envelope); err != nil {
			zap.S().Warnf("[UserWS] 消息解析失败 [user=%d]: %v", client.OwnerID, err)
			continue
		}

		// 有 deviceId + type + payload → 下发命令（格式对齐 HTTP DownlinkCmdRequest + deviceId）
		if envelope.DeviceID != "" && envelope.Type != "" && envelope.Payload != nil {
			if h.onUserCommand == nil {
				h.replyOwner(client.OwnerID, "error", map[string]interface{}{"deviceId": envelope.DeviceID}, "命令下发未启用")
				continue
			}

			cmdID, err := h.onUserCommand(client.OwnerID, envelope.DeviceID, envelope.Type, envelope.Payload)
			if err != nil {
				zap.S().Warnf("[UserWS] 命令下发失败 [user=%d, device=%s, type=%s]: %v",
					client.OwnerID, envelope.DeviceID, envelope.Type, err)
				h.replyOwner(client.OwnerID, "cmdAck", map[string]interface{}{
					"deviceId": envelope.DeviceID,
					"cmdType":  envelope.Type,
					"success":  false,
					"error":    err.Error(),
				}, "")
				continue
			}

			zap.S().Infof("[UserWS] 命令已下发 [user=%d, device=%s, type=%s, cmdID=%d]",
				client.OwnerID, envelope.DeviceID, envelope.Type, cmdID)
			h.replyOwner(client.OwnerID, "cmdAck", map[string]interface{}{
				"deviceId": envelope.DeviceID,
				"cmdType":  envelope.Type,
				"cmdId":    cmdID,
				"success":  true,
			}, "")
			continue
		}

		// 心跳
		if envelope.Type == "ping" {
			h.replyOwner(client.OwnerID, "pong", nil, "")
			continue
		}

		zap.S().Infof("[UserWS] 未知消息 [user=%d, type=%s]", client.OwnerID, envelope.Type)
	}
}

// replyOwner 向 owner 所有管理端广播消息
func (h *WsHandler) replyOwner(ownerID uint, msgType string, data map[string]interface{}, errMsg string) {
	resp := map[string]interface{}{"type": msgType}
	for k, v := range data {
		resp[k] = v
	}
	if errMsg != "" {
		resp["error"] = errMsg
	}
	msg, _ := json.Marshal(resp)
	h.hub.SendToOwner(ownerID, msg)
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

	// 解析设备 Owner（用于上线通知和消息转发）
	var ownerID uint
	if h.resolveOwner != nil {
		if oid, err := h.resolveOwner(deviceID); err == nil {
			ownerID = oid
		}
	}

	client := &Client{
		Conn:     conn,
		Send:     make(chan []byte, 256),
		DeviceID: deviceID,
		OwnerID:  ownerID,
		Token:    token,
	}

	h.hub.Register(client)
	zap.S().Infof("[WebSocket] 设备实时通道建立 [device=%s, owner=%d]", deviceID, ownerID)

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
