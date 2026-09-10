package repository

import (
	"context"

	"iot-platform.local/internal/ent"
	entmsg "iot-platform.local/internal/ent/messagelog"
)

// MessageLogRepo 设备消息日志数据访问（下行命令 + 上行 MQTT 发布）
type MessageLogRepo struct {
	client *ent.Client
}

// NewMessageLogRepo 创建消息日志仓库
func NewMessageLogRepo(client *ent.Client) *MessageLogRepo {
	return &MessageLogRepo{client: client}
}

// Create 写入一条消息日志
func (r *MessageLogRepo) Create(ctx context.Context, m *ent.MessageLog) (*ent.MessageLog, error) {
	return r.client.MessageLog.Create().
		SetDirection(m.Direction).
		SetCategory(m.Category).
		SetDeviceID(m.DeviceID).
		SetType(m.Type).
		SetTopic(m.Topic).
		SetPayload(m.Payload).
		SetQos(m.Qos).
		SetRetained(m.Retained).
		SetClientID(m.ClientID).
		SetBrokerURL(m.BrokerURL).
		SetStatus(m.Status).
		SetCreatedAt(m.CreatedAt).
		Save(ctx)
}

// MarkSent 将一批下行命令标记为已发送（sent）
func (r *MessageLogRepo) MarkSent(ctx context.Context, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.client.MessageLog.Update().
		Where(
			entmsg.IDIn(ids...),
			entmsg.DirectionEQ("down"),
		).
		SetStatus("sent").
		Exec(ctx)
}

// MarkDelivered 将下行命令标记为已送达（delivered）
func (r *MessageLogRepo) MarkDelivered(ctx context.Context, id uint) error {
	return r.client.MessageLog.UpdateOneID(id).
		SetStatus("delivered").
		Exec(ctx)
}
