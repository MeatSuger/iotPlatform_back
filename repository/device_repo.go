package repository

import (
	"context"
	"time"

	"github.com/yu/iot-platform-go/entity"
	"gorm.io/gorm"
)

// DeviceRepo 设备数据访问（嵌入泛型 BaseRepo）
type DeviceRepo struct {
	BaseRepo[entity.Device]
}

// NewDeviceRepo 创建设备仓库
func NewDeviceRepo(db *gorm.DB) *DeviceRepo {
	return &DeviceRepo{BaseRepo: *NewBaseRepo[entity.Device](db)}
}

// GetByDeviceID 按设备唯一标识查询（特化查询）
func (r *DeviceRepo) GetByDeviceID(ctx context.Context, deviceID string) (*entity.Device, error) {
	var device entity.Device
	err := r.DB.WithContext(ctx).Where("device_id = ?", deviceID).First(&device).Error
	if err != nil {
		return nil, err
	}
	return &device, nil
}

// ListByOwnerID 按所有者查询设备列表（特化查询）
func (r *DeviceRepo) ListByOwnerID(ctx context.Context, ownerID uint) ([]entity.Device, error) {
	return r.BaseRepo.List(ctx, func(db *gorm.DB) *gorm.DB {
		return db.Where("owner_id = ?", ownerID).Order("id DESC")
	})
}

// UpdateStatus 更新设备状态（特化更新）
func (r *DeviceRepo) UpdateStatus(ctx context.Context, deviceID, status string) error {
	return r.DB.WithContext(ctx).Model(&entity.Device{}).
		Where("device_id = ?", deviceID).
		Update("status", status).Error
}

// UpdateLastActive 更新设备最后活跃时间
func (r *DeviceRepo) UpdateLastActive(ctx context.Context, deviceID, status string) error {
	return r.DB.WithContext(ctx).Model(&entity.Device{}).
		Where("device_id = ?", deviceID).
		Updates(map[string]interface{}{
			"status":           status,
			"last_active_time": time.Now(),
		}).Error
}
