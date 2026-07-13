package repository

import (
	"context"

	"github.com/yu/iot-platform-go/entity"
	"gorm.io/gorm"
)

// MqttPublishLogRepo MQTT发布日志数据访问（嵌入泛型 BaseRepo）
type MqttPublishLogRepo struct {
	BaseRepo[entity.MqttPublishLog]
}

// NewMqttPublishLogRepo 创建MQTT发布日志仓库
func NewMqttPublishLogRepo(db *gorm.DB) *MqttPublishLogRepo {
	return &MqttPublishLogRepo{BaseRepo: *NewBaseRepo[entity.MqttPublishLog](db)}
}

// List 查询日志列表（按时间倒序，特化查询 — 覆盖基类的 List 方法）
func (r *MqttPublishLogRepo) List(ctx context.Context, limit int) ([]entity.MqttPublishLog, error) {
	if limit <= 0 {
		limit = 100
	}
	return r.BaseRepo.List(ctx, func(db *gorm.DB) *gorm.DB {
		return db.Order("create_time DESC").Limit(limit)
	})
}
