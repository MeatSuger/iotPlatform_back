package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"iot-platform.local/internal/ent"
	mqttEntity "iot-platform.local/internal/model/mqtt"
	"iot-platform.local/internal/repository"
	"iot-platform.local/internal/websocket"
	"iot-platform.local/pkg/cache"
	"iot-platform.local/pkg/config"
)

// MqttClientService MQTT客户端服务
type MqttClientService struct {
	client        mqtt.Client
	status        *mqttEntity.MqttClientStatus
	messageBuffer []mqttEntity.MessageView
	bufferMu      sync.Mutex
	maxBufferSize int
	logRepo       *repository.MqttPublishLogRepo
	redisCache    *cache.RedisCache
	wsHub         *websocket.Hub
	mu            sync.RWMutex
}

// NewMqttClientService 创建MQTT客户端服务
func NewMqttClientService(
	logRepo *repository.MqttPublishLogRepo,
	redisCache *cache.RedisCache,
	wsHub *websocket.Hub,
) *MqttClientService {
	return &MqttClientService{
		status:        mqttEntity.NewDisconnectedStatus(),
		messageBuffer: make([]mqttEntity.MessageView, 0),
		maxBufferSize: 500,
		logRepo:       logRepo,
		redisCache:    redisCache,
		wsHub:         wsHub,
	}
}

// Connect 连接到MQTT Broker
func (s *MqttClientService) Connect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.status.IsConnected() {
		return fmt.Errorf("MQTT客户端已连接")
	}

	cfg := config.Cfg.MQTT
	opts := mqtt.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(cfg.ClientID).
		SetKeepAlive(time.Duration(cfg.KeepAlive) * time.Second).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetMaxReconnectInterval(30 * time.Second).
		SetOnConnectHandler(s.onConnect).
		SetConnectionLostHandler(s.onConnectionLost).
		SetDefaultPublishHandler(s.onMessageReceived)

	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}

	s.client = mqtt.NewClient(opts)
	token := s.client.Connect()
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("MQTT连接失败: %w", token.Error())
	}

	s.status = mqttEntity.NewConnectedStatus(cfg.BrokerURL, cfg.ClientID)
	zap.S().Infof("[MQTT] 已连接到 Broker: %s", cfg.BrokerURL)

	// 自动订阅配置的默认主题
	for _, topic := range cfg.Topics {
		qos := cfg.Qos
		if qos == 0 {
			qos = 1
		}
		err := s.doSubscribe(topic, qos)
		if err != nil {
			return err
		}
	}

	return nil
}

// Disconnect 断开MQTT连接
func (s *MqttClientService) Disconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client != nil && s.client.IsConnected() {
		s.client.Disconnect(250)
	}
	s.status.SetConnected(false)
	zap.S().Info("[MQTT] 已断开连接")
}

// Subscribe 订阅主题
func (s *MqttClientService) Subscribe(req mqttEntity.SubscribeRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.status.IsConnected() {
		return fmt.Errorf("MQTT客户端未连接")
	}

	return s.doSubscribe(req.Topic, req.GetQos())
}

// Unsubscribe 取消订阅主题
func (s *MqttClientService) Unsubscribe(topic string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.status.IsConnected() {
		return fmt.Errorf("MQTT客户端未连接")
	}

	token := s.client.Unsubscribe(topic)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("取消订阅失败: %w", token.Error())
	}

	s.status.RemoveSubscription(topic)
	zap.S().Infof("[MQTT] 取消订阅: %s", topic)
	return nil
}

// Publish 发布消息
func (s *MqttClientService) Publish(req mqttEntity.PublishRequest) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.status.IsConnected() {
		return fmt.Errorf("MQTT客户端未连接")
	}

	token := s.client.Publish(req.Topic, req.GetQos(), req.GetRetained(), req.Payload)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("发布消息失败: %w", token.Error())
	}

	s.status.IncrementSent()

	// 异步持久化发布日志
	go s.savePublishLog(&req)

	zap.S().Infof("[MQTT] 发布消息: topic=%s, qos=%d", req.Topic, req.GetQos())
	return nil
}

// Status 获取MQTT客户端状态
func (s *MqttClientService) Status() *mqttEntity.MqttClientStatus {
	s.bufferMu.Lock()
	buffered := len(s.messageBuffer)
	s.bufferMu.Unlock()

	snapshot := s.status.Snapshot()
	snapshot.BufferedMessages = buffered
	return &snapshot
}

// RecentMessages 获取最近的MQTT消息
func (s *MqttClientService) RecentMessages(limit int) []mqttEntity.MessageView {
	s.bufferMu.Lock()
	defer s.bufferMu.Unlock()

	if limit <= 0 || limit > len(s.messageBuffer) {
		limit = len(s.messageBuffer)
	}

	// 返回最新的N条（buffer尾部）
	start := len(s.messageBuffer) - limit
	// copy to avoid race
	result := make([]mqttEntity.MessageView, limit)
	copy(result, s.messageBuffer[start:])

	// 反转顺序（最新的在前）
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result
}

// doSubscribe 执行订阅（调用前需获取锁）
func (s *MqttClientService) doSubscribe(topic string, qos byte) error {
	token := s.client.Subscribe(topic, qos, nil)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("订阅失败: %w", token.Error())
	}

	s.status.AddSubscription(topic, qos)
	zap.S().Infof("[MQTT] 已订阅: %s (QoS=%d)", topic, qos)
	return nil
}

// onConnect 连接成功回调
func (s *MqttClientService) onConnect(mqtt.Client) {
	zap.S().Info("[MQTT] 连接建立")

	// 重新订阅之前的主题
	cfg := config.Cfg.MQTT
	for _, topic := range cfg.Topics {
		qos := cfg.Qos
		if qos == 0 {
			qos = 1
		}
		err := s.doSubscribe(topic, qos)
		if err != nil {
			return
		}
	}
}

// onConnectionLost 连接丢失回调
func (s *MqttClientService) onConnectionLost(_ mqtt.Client, err error) {
	zap.S().Infof("[MQTT] 连接丢失: %v", err)
	s.status.SetConnected(false)
	s.status.IncrementReconnect()
}

// onMessageReceived 消息接收回调
func (s *MqttClientService) onMessageReceived(_ mqtt.Client, msg mqtt.Message) {
	view := mqttEntity.NewMessageView(
		msg.Topic(),
		string(msg.Payload()),
		int(msg.Qos()),
		msg.Retained(),
		msg.Duplicate(),
	)

	s.status.IncrementReceived()

	// 添加到内存缓冲区
	s.bufferMu.Lock()
	s.messageBuffer = append(s.messageBuffer, view)
	if len(s.messageBuffer) > s.maxBufferSize {
		s.messageBuffer = s.messageBuffer[1:] // 移除最旧的
	}
	s.bufferMu.Unlock()

	// 广播到WebSocket客户端
	go func() {
		data, _ := json.Marshal(view)
		s.wsHub.Broadcast(string(data))
	}()

	zap.S().Infof("[MQTT] 收到消息: topic=%s, payload=%s", msg.Topic(), truncateString(string(msg.Payload()), 100))
}

// savePublishLog 保存发布日志
func (s *MqttClientService) savePublishLog(req *mqttEntity.PublishRequest) {
	ctx := context.Background()
	logEntry := &ent.MqttPublishLog{
		Topic:      req.Topic,
		Payload:    req.Payload,
		Qos:        int(req.GetQos()),
		Retained:   req.GetRetained(),
		ClientID:   config.Cfg.MQTT.ClientID,
		BrokerURL:  config.Cfg.MQTT.BrokerURL,
		CreateTime: time.Now(),
	}

	if _, err := s.logRepo.Create(ctx, logEntry); err != nil {
		zap.S().Infof("[MQTT] 保存发布日志失败: %v", err)
	}
}

// IsConnected 检查是否已连接
func (s *MqttClientService) IsConnected() bool {
	return s.status.IsConnected()
}

// truncateString 截断字符串
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
