package repository

import (
	"context"

	"iot-platform.local/internal/ent"
)

// MqttPublishLogRepo MQTT 发布日志仓库
type MqttPublishLogRepo struct {
	client *ent.Client
}

// NewMqttPublishLogRepo 创建仓库
func NewMqttPublishLogRepo(client *ent.Client) *MqttPublishLogRepo {
	return &MqttPublishLogRepo{client: client}
}

// Create 记录一条发布日志
func (r *MqttPublishLogRepo) Create(ctx context.Context, log *ent.MqttPublishLog) (*ent.MqttPublishLog, error) {
	return r.client.MqttPublishLog.Create().
		SetTopic(log.Topic).
		SetPayload(log.Payload).
		SetQos(log.Qos).
		SetRetained(log.Retained).
		SetClientID(log.ClientID).
		SetBrokerURL(log.BrokerURL).
		SetCreateTime(log.CreateTime).
		Save(ctx)
}
