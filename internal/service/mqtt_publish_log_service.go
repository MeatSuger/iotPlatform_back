package service

import (
	"context"

	"github.com/yu/iot-platform-go/internal/ent"
	"github.com/yu/iot-platform-go/internal/repository"
)

// MqttPublishLogService MQTT发布日志服务
type MqttPublishLogService struct {
	repo *repository.MqttPublishLogRepo
}

func NewMqttPublishLogService(repo *repository.MqttPublishLogRepo) *MqttPublishLogService {
	return &MqttPublishLogService{repo: repo}
}

func (s *MqttPublishLogService) Create(ctx context.Context, log *ent.MqttPublishLog) error {
	_, err := s.repo.Create(ctx, log)
	return err
}

func (s *MqttPublishLogService) List(ctx context.Context, limit int) ([]*ent.MqttPublishLog, error) {
	return s.repo.List(ctx, limit)
}
