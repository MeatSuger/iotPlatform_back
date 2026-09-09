package websocket

import (
	"sync"

	"go.uber.org/zap"

	"github.com/gorilla/websocket"
)

// Hub WebSocket连接管理中心
type Hub struct {
	clients       map[*Client]bool
	deviceClients map[string]*Client        // deviceID → 活跃连接（每个设备最多一个）
	ownerClients  map[uint]map[*Client]bool // ownerID → 用户管理端连接集合
	mu            sync.RWMutex

	// 可选回调：设备连接/断开时通知
	OnDeviceOnline  func(deviceID string, ownerID uint)
	OnDeviceOffline func(deviceID string, ownerID uint)
}

// Client WebSocket客户端
type Client struct {
	Conn     *websocket.Conn
	Send     chan []byte
	DeviceID string // 关联的设备ID（设备客户端）
	OwnerID  uint   // 拥有者ID（用户客户端，0 表示是设备客户端）
	Token    string // 认证 Token
}

// NewHub 创建WebSocket Hub
func NewHub() *Hub {
	return &Hub{
		clients:       make(map[*Client]bool),
		deviceClients: make(map[string]*Client),
		ownerClients:  make(map[uint]map[*Client]bool),
	}
}

// Register 注册设备客户端（同一设备只保留一个连接，旧连接踢下线）
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
	zap.S().Infof("[WebSocket] 设备连接 [device=%s], 当前连接数: %d", client.DeviceID, len(h.clients))

	// 通知 owner 设备上线
	if h.OnDeviceOnline != nil && client.OwnerID > 0 {
		go h.OnDeviceOnline(client.DeviceID, client.OwnerID)
	}
}

// RegisterUser 注册用户（owner）管理端客户端（同一用户允许多个连接）
func (h *Hub) RegisterUser(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.clients[client] = true
	if h.ownerClients[client.OwnerID] == nil {
		h.ownerClients[client.OwnerID] = make(map[*Client]bool)
	}
	h.ownerClients[client.OwnerID][client] = true
	zap.S().Infof("[WebSocket] 用户管理端连接 [owner=%d], 当前连接数: %d", client.OwnerID, len(h.clients))
}

// Unregister 注销客户端（同时处理设备和用户客户端）
func (h *Hub) Unregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[client]; !ok {
		return
	}
	delete(h.clients, client)
	close(client.Send)

	// 设备客户端
	if client.DeviceID != "" && h.deviceClients[client.DeviceID] == client {
		delete(h.deviceClients, client.DeviceID)
		zap.S().Infof("[WebSocket] 设备断开 [device=%s], 当前连接数: %d", client.DeviceID, len(h.clients))

		// 通知 owner 设备下线
		if h.OnDeviceOffline != nil && client.OwnerID > 0 {
			go h.OnDeviceOffline(client.DeviceID, client.OwnerID)
		}
	}

	// 用户客户端
	if client.OwnerID > 0 && client.DeviceID == "" {
		if set, ok := h.ownerClients[client.OwnerID]; ok {
			delete(set, client)
			if len(set) == 0 {
				delete(h.ownerClients, client.OwnerID)
			}
		}
		zap.S().Infof("[WebSocket] 用户管理端断开 [owner=%d], 当前连接数: %d", client.OwnerID, len(h.clients))
	}
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

// SendToOwner 向指定 owner 的所有管理端连接推送消息
func (h *Hub) SendToOwner(ownerID uint, message []byte) {
	h.mu.RLock()
	clients := h.ownerClients[ownerID]
	h.mu.RUnlock()

	for client := range clients {
		select {
		case client.Send <- message:
		default:
			go h.Unregister(client)
		}
	}
}

// SendToDeviceOwner 向指定设备的 owner 管理端推送消息（从内存取 ownerID，零查库）
// 典型用途：DownlinkService 发送命令后，通知 owner 管理端「命令已下发」
func (h *Hub) SendToDeviceOwner(deviceID string, message []byte) {
	h.mu.RLock()
	deviceClient, ok := h.deviceClients[deviceID]
	h.mu.RUnlock()
	if !ok || deviceClient.OwnerID == 0 {
		return
	}
	h.SendToOwner(deviceClient.OwnerID, message)
}

// IsDeviceOnline 判断设备是否在线
func (h *Hub) IsDeviceOnline(deviceID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.deviceClients[deviceID]
	return ok
}
