// Package mqtt 提供平台侧 MQTT Broker 客户端能力。
//
// 与 mqtt_gateway_controller（设备 → Broker 的透明桥接）不同，
// 本包用于平台 → 设备的主动下行发布。
package mqtt

import (
	"encoding/json"
	"fmt"
	"time"

	mqttpaho "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"

	entity "iot-platform.local/internal/model"
	"iot-platform.local/pkg/config"
)

// 下行主题约定（见 api/swagger/API.md §6.3）：
//   - config 快照：retained，设备“订阅即拉取”最新版本
//   - cmd 命令：QoS1 非 retained，实时动作（control 等），离线由 HTTP /commands 兜底
const (
	configTopicPattern = "iot/%s/config"
	cmdTopicPattern    = "iot/%s/cmd"
)

// Publisher 平台侧 MQTT 下行发布器（单个 paho 客户端，自动重连）。
type Publisher struct {
	client mqttpaho.Client
}

// NewPublisher 创建并启动下行发布器（异步连接，自动重连）。
func NewPublisher() *Publisher {
	// 快照 broker 地址：paho 的 OnConnect 回调在独立 goroutine 异步执行，
	// 直接读全局 config.Cfg 会在并发修改配置时产生数据竞争（如测试/配置热加载）
	brokerURL := config.Cfg.MQTT.BrokerURL
	opts := mqttpaho.NewClientOptions().
		AddBroker(brokerURL).
		SetClientID("iot-platform-pub").
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetOnConnectHandler(func(c mqttpaho.Client) {
			zap.S().Infof("[MQTT] 下行发布器已连接 %s", brokerURL)
		})

	client := mqttpaho.NewClient(opts)
	client.Connect()
	return &Publisher{client: client}
}

func (p *Publisher) publish(topic string, qos byte, retained bool, payload []byte) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("MQTT 发布器未初始化")
	}
	if !p.client.IsConnected() {
		return fmt.Errorf("MQTT 未连接，发布跳过 [topic=%s]", topic)
	}

	token := p.client.Publish(topic, qos, retained, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("发布超时 [topic=%s]", topic)
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("发布失败 [topic=%s]: %w", topic, err)
	}
	zap.S().Debugf("[MQTT] 已发布 [topic=%s qos=%d retained=%v]", topic, qos, retained)
	return nil
}

// PublishConfig 发布设备最新配置快照到 retained 主题 iot/{deviceId}/config。
// retained 语义 = 设备“订阅即拉取”：在线设备实时收到；离线/重启设备
// 在下次订阅时由 Broker 自动补投最新版本，无需平台侧维护连接状态。
func (p *Publisher) PublishConfig(deviceID string, env entity.ConfigEnvelope) error {
	payload, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("序列化配置载荷失败: %w", err)
	}
	return p.publish(fmt.Sprintf(configTopicPattern, deviceID), 1, true, payload)
}

// PublishCommand 实时下行命令到 iot/{deviceId}/cmd（QoS1 非 retained）。
// payload 为命令 JSON（id/type/payload/createdAt），与 GET /commands 返回项同构。
func (p *Publisher) PublishCommand(deviceID string, payload []byte) error {
	return p.publish(fmt.Sprintf(cmdTopicPattern, deviceID), 1, false, payload)
}
