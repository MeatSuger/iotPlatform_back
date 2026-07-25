package mqtt

import (
	"sync"
	"time"
)

// MqttClientStatus MQTT客户端状态
type MqttClientStatus struct {
	Connected             bool            `json:"connected"`
	BrokerURL             string          `json:"brokerUrl"`
	ClientID              string          `json:"clientId"`
	Subscriptions         map[string]byte `json:"subscriptions"` // topic -> qos
	BufferedMessages      int             `json:"bufferedMessages"`
	LastStateChange       time.Time       `json:"lastStateChange"`
	ReconnectCount        int64           `json:"reconnectCount"`
	TotalMessagesSent     int64           `json:"totalMessagesSent"`
	TotalMessagesReceived int64           `json:"totalMessagesReceived"`
	mu                    sync.RWMutex    `json:"-"`
}

// NewDisconnectedStatus 创建断开连接的状态
func NewDisconnectedStatus() *MqttClientStatus {
	return &MqttClientStatus{
		Connected:       false,
		Subscriptions:   make(map[string]byte),
		LastStateChange: time.Now(),
	}
}

// NewConnectedStatus 创建已连接的状态
func NewConnectedStatus(brokerURL, clientID string) *MqttClientStatus {
	return &MqttClientStatus{
		Connected:       true,
		BrokerURL:       brokerURL,
		ClientID:        clientID,
		Subscriptions:   make(map[string]byte),
		LastStateChange: time.Now(),
	}
}

// SetConnected 更新连接状态
func (s *MqttClientStatus) SetConnected(connected bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Connected = connected
	s.LastStateChange = time.Now()
}

// IsConnected 线程安全地获取连接状态
func (s *MqttClientStatus) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Connected
}

// AddSubscription 添加订阅
func (s *MqttClientStatus) AddSubscription(topic string, qos byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Subscriptions[topic] = qos
}

// RemoveSubscription 移除订阅
func (s *MqttClientStatus) RemoveSubscription(topic string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Subscriptions, topic)
}

// HasSubscriptions 是否有订阅
func (s *MqttClientStatus) HasSubscriptions() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Subscriptions) > 0
}

// IncrementReconnect 增加重连计数
func (s *MqttClientStatus) IncrementReconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ReconnectCount++
}

// IncrementSent 增加发送计数
func (s *MqttClientStatus) IncrementSent() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalMessagesSent++
}

// IncrementReceived 增加接收计数
func (s *MqttClientStatus) IncrementReceived() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalMessagesReceived++
}

// Snapshot 获取状态的线程安全快照
func (s *MqttClientStatus) Snapshot() MqttClientStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	subs := make(map[string]byte, len(s.Subscriptions))
	for k, v := range s.Subscriptions {
		subs[k] = v
	}
	return MqttClientStatus{
		Connected:             s.Connected,
		BrokerURL:             s.BrokerURL,
		ClientID:              s.ClientID,
		Subscriptions:         subs,
		BufferedMessages:      s.BufferedMessages,
		LastStateChange:       s.LastStateChange,
		ReconnectCount:        s.ReconnectCount,
		TotalMessagesSent:     s.TotalMessagesSent,
		TotalMessagesReceived: s.TotalMessagesReceived,
	}
}
