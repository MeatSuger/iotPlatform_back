package repository

import (
	"context"

	"github.com/yu/iot-platform-go/internal/ent"
)

// MqttPublishLogRepo MQTT发布日志数据访问
type MqttPublishLogRepo struct {
	client *ent.Client
}

func NewMqttPublishLogRepo(client *ent.Client) *MqttPublishLogRepo {
	return &MqttPublishLogRepo{client: client}
}

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

func (r *MqttPublishLogRepo) List(ctx context.Context, limit int) ([]*ent.MqttPublishLog, error) {
	if limit <= 0 {
		limit = 100
	}
	return r.client.MqttPublishLog.Query().
		Order(ent.Desc("create_time")).
		Limit(limit).
		All(ctx)
}
