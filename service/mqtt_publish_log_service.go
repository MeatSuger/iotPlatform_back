package service

import (
	"context"

	"github.com/yu/iot-platform-go/entity"
	"github.com/yu/iot-platform-go/repository"
)

// MqttPublishLogService MQTT发布日志服务
type MqttPublishLogService struct {
	repo *repository.MqttPublishLogRepo
}

// NewMqttPublishLogService 创建MQTT发布日志服务
func NewMqttPublishLogService(repo *repository.MqttPublishLogRepo) *MqttPublishLogService {
	return &MqttPublishLogService{repo: repo}
}

// Create 创建日志
func (s *MqttPublishLogService) Create(ctx context.Context, log *entity.MqttPublishLog) error {
	return s.repo.Create(ctx, log)
}

// List 查询日志列表
func (s *MqttPublishLogService) List(ctx context.Context, limit int) ([]entity.MqttPublishLog, error) {
	return s.repo.List(ctx, limit)
}
