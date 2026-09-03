package repository

import (
	"context"

	"iot-platform.local/internal/ent"
	entconfig "iot-platform.local/internal/ent/deviceconfig"
)

// DeviceConfigRepo 设备配置快照数据访问
type DeviceConfigRepo struct {
	client *ent.Client
}

// NewDeviceConfigRepo 创建设备配置仓库
func NewDeviceConfigRepo(client *ent.Client) *DeviceConfigRepo {
	return &DeviceConfigRepo{client: client}
}

// GetByDeviceID 查询设备配置快照；不存在返回 ent.NotFoundError
func (r *DeviceConfigRepo) GetByDeviceID(ctx context.Context, deviceID string) (*ent.DeviceConfig, error) {
	return r.client.DeviceConfig.Query().
		Where(entconfig.DeviceIDEQ(deviceID)).
		First(ctx)
}

// Upsert 写入设备配置快照（不存在则创建，存在则覆盖期望配置与版本，并重置下发状态）
func (r *DeviceConfigRepo) Upsert(ctx context.Context, deviceID string, payload string, version uint) error {
	existing, err := r.GetByDeviceID(ctx, deviceID)
	if err != nil {
		if !ent.IsNotFound(err) {
			return err
		}
		_, err := r.client.DeviceConfig.Create().
			SetDeviceID(deviceID).
			SetPayload(payload).
			SetVersion(version).
			SetStatus("pending").
			Save(ctx)
		return err
	}

	return r.client.DeviceConfig.UpdateOneID(existing.ID).
		SetPayload(payload).
		SetVersion(version).
		SetStatus("pending").
		Exec(ctx)
}

// UpdateReported 回写设备上报的实际配置与版本，并将下发状态置为已确认
func (r *DeviceConfigRepo) UpdateReported(ctx context.Context, deviceID string, version uint, reported string) error {
	return r.client.DeviceConfig.Update().
		Where(entconfig.DeviceIDEQ(deviceID)).
		SetReportedVersion(version).
		SetReportedPayload(reported).
		SetStatus("acked").
		Exec(ctx)
}

// Delete 删除设备配置快照（通常由设备删除级联触发，这里提供显式入口）
func (r *DeviceConfigRepo) Delete(ctx context.Context, deviceID string) error {
	_, err := r.client.DeviceConfig.Delete().
		Where(entconfig.DeviceIDEQ(deviceID)).
		Exec(ctx)
	return err
}
