package repository

import (
	"context"
	"time"

	"iot-platform.local/internal/ent"
	entdevice "iot-platform.local/internal/ent/device"
)

// DeviceRepo 设备数据访问
type DeviceRepo struct {
	client *ent.Client
}

func NewDeviceRepo(client *ent.Client) *DeviceRepo {
	return &DeviceRepo{client: client}
}

func (r *DeviceRepo) Create(ctx context.Context, device *ent.Device) (*ent.Device, error) {
	return r.client.Device.Create().
		SetID(device.ID).
		SetDeviceName(device.DeviceName).
		SetDeviceType(device.DeviceType).
		SetFirmwareVersion(device.FirmwareVersion).
		SetIPAddress(device.IPAddress).
		SetMACAddress(device.MACAddress).
		SetLocation(device.Location).
		SetOwnerID(device.OwnerID).
		SetStatus(device.Status).
		SetCreatedAt(device.CreatedAt).
		SetUpdatedAt(device.UpdatedAt).
		Save(ctx)
}

func (r *DeviceRepo) GetByID(ctx context.Context, id string) (*ent.Device, error) {
	return r.client.Device.Get(ctx, id)
}

func (r *DeviceRepo) GetByDeviceID(ctx context.Context, deviceID string) (*ent.Device, error) {
	return r.client.Device.Query().Where(entdevice.IDEQ(deviceID)).First(ctx)
}

func (r *DeviceRepo) Update(ctx context.Context, device *ent.Device) error {
	return r.client.Device.UpdateOneID(device.ID).
		SetDeviceName(device.DeviceName).
		SetDeviceType(device.DeviceType).
		SetFirmwareVersion(device.FirmwareVersion).
		SetIPAddress(device.IPAddress).
		SetMACAddress(device.MACAddress).
		SetLocation(device.Location).
		SetStatus(device.Status).
		SetUpdatedAt(device.UpdatedAt).
		Exec(ctx)
}

func (r *DeviceRepo) Delete(ctx context.Context, id string) error {
	return r.client.Device.DeleteOneID(id).Exec(ctx)
}

func (r *DeviceRepo) ListByOwnerID(ctx context.Context, ownerID uint) ([]*ent.Device, error) {
	return r.client.Device.Query().
		Where(entdevice.OwnerIDEQ(ownerID)).
		Order(ent.Desc(entdevice.FieldID)).
		All(ctx)
}

func (r *DeviceRepo) UpdateStatus(ctx context.Context, deviceID, status string) error {
	return r.client.Device.Update().
		Where(entdevice.IDEQ(deviceID)).
		SetStatus(status).
		Exec(ctx)
}

func (r *DeviceRepo) UpdateLastActive(ctx context.Context, deviceID, status string) error {
	return r.client.Device.Update().
		Where(entdevice.IDEQ(deviceID)).
		SetStatus(status).
		SetLastActiveTime(time.Now()).
		Exec(ctx)
}

// ListByStatus 返回指定状态的所有设备（供离线检测同步器扫描 ONLINE 设备）
func (r *DeviceRepo) ListByStatus(ctx context.Context, status string) ([]*ent.Device, error) {
	return r.client.Device.Query().
		Where(entdevice.StatusEQ(status)).
		All(ctx)
}

// ListInactiveBefore 返回最后活跃时间早于 cutoff 的设备
// 含从未上线（last_active_time 为 NULL）且创建时间早于 cutoff 的设备
func (r *DeviceRepo) ListInactiveBefore(ctx context.Context, cutoff time.Time) ([]*ent.Device, error) {
	return r.client.Device.Query().
		Where(entdevice.Or(
			entdevice.LastActiveTimeLT(cutoff),
			entdevice.And(
				entdevice.LastActiveTimeIsNil(),
				entdevice.CreatedAtLT(cutoff),
			),
		)).
		All(ctx)
}
