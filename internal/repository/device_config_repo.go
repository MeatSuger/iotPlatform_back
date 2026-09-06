package repository

import (
	"context"
	"fmt"

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

// Upsert 原子写入设备配置快照：version 递增与 payload/status 更新在同一条
// UPDATE 中完成（AddVersion 生成 version = version + 1），避免并发 Save 时
// 「读旧版本再写回」导致的版本重复/覆盖；记录不存在则插入 version=1，
// 并发插入撞唯一约束时回退重试更新（此时能命中先插入方的行，version 继续递增）。
func (r *DeviceConfigRepo) Upsert(ctx context.Context, deviceID string, payload string) error {
	// 1. 优先原子递增更新：命中即返回，version 最终值由数据库保证连续
	n, err := r.client.DeviceConfig.Update().
		Where(entconfig.DeviceIDEQ(deviceID)).
		SetPayload(payload).
		SetStatus("pending").
		AddVersion(1).
		Save(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	// 2. 记录不存在：插入 version=1；并发下唯一约束冲突 → 回退到步骤 1 重试
	_, err = r.client.DeviceConfig.Create().
		SetDeviceID(deviceID).
		SetPayload(payload).
		SetVersion(1).
		SetStatus("pending").
		Save(ctx)
	if ent.IsConstraintError(err) {
		n, uerr := r.client.DeviceConfig.Update().
			Where(entconfig.DeviceIDEQ(deviceID)).
			SetPayload(payload).
			SetStatus("pending").
			AddVersion(1).
			Save(ctx)
		if uerr != nil {
			return uerr
		}
		if n == 0 {
			return fmt.Errorf("配置插入冲突但更新未命中: %s", deviceID)
		}
		return nil
	}
	return err
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
