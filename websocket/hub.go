package websocket

import (
	"sync"

	"go.uber.org/zap"

	"github.com/gorilla/websocket"
)

// Hub WebSocket连接管理中心
type Hub struct {
	clients       map[*Client]bool
	deviceClients map[string]*Client // deviceID → 活跃连接（每个设备最多一个）
	mu            sync.RWMutex
}

// Client WebSocket客户端
type Client struct {
	Conn     *websocket.Conn
	Send     chan []byte
	DeviceID string // 关联的设备ID
	Token    string // 设备 Token（用于上行消息校验）
}

// NewHub 创建WebSocket Hub
func NewHub() *Hub {
	return &Hub{
		clients:       make(map[*Client]bool),
		deviceClients: make(map[string]*Client),
	}
}

// Register 注册客户端
func (h *Hub) Register(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 同一设备旧连接踢下线
	if old, ok := h.deviceClients[client.DeviceID]; ok {
		delete(h.clients, old)
		close(old.Send)
	}
	h.clients[client] = true
	h.deviceClients[client.DeviceID] = client
	zap.S().Infof("[WebSocket] 客户端连接 [device=%s], 当前连接数: %d", client.DeviceID, len(h.clients))
}

// Unregister 注销客户端
func (h *Hub) Unregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[client]; ok {
		delete(h.clients, client)
		close(client.Send)
	}
	if h.deviceClients[client.DeviceID] == client {
		delete(h.deviceClients, client.DeviceID)
	}
	zap.S().Infof("[WebSocket] 客户端断开 [device=%s], 当前连接数: %d", client.DeviceID, len(h.clients))
}

// SendToDevice 向指定设备实时推送消息
func (h *Hub) SendToDevice(deviceID string, message []byte) {
	h.mu.RLock()
	client, ok := h.deviceClients[deviceID]
	h.mu.RUnlock()
	if !ok {
		return
	}
	select {
	case client.Send <- message:
	default:
		go h.Unregister(client)
	}
}

// Broadcast 向所有客户端广播消息
func (h *Hub) Broadcast(message string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		select {
		case client.Send <- []byte(message):
		default:
			go h.Unregister(client)
		}
	}
}

// ClientCount 获取当前连接数
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
