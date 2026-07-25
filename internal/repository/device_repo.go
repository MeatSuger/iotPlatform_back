package repository

import (
	"context"
	"time"

	"github.com/yu/iot-platform-go/internal/ent"
	entdevice "github.com/yu/iot-platform-go/internal/ent/device"
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
		SetDeviceID(device.DeviceID).
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

func (r *DeviceRepo) GetByID(ctx context.Context, id int) (*ent.Device, error) {
	return r.client.Device.Get(ctx, id)
}

func (r *DeviceRepo) GetByDeviceID(ctx context.Context, deviceID string) (*ent.Device, error) {
	return r.client.Device.Query().Where(entdevice.DeviceIDEQ(deviceID)).First(ctx)
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

func (r *DeviceRepo) Delete(ctx context.Context, id int) error {
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
		Where(entdevice.DeviceIDEQ(deviceID)).
		SetStatus(status).
		Exec(ctx)
}

func (r *DeviceRepo) UpdateLastActive(ctx context.Context, deviceID, status string) error {
	return r.client.Device.Update().
		Where(entdevice.DeviceIDEQ(deviceID)).
		SetStatus(status).
		SetLastActiveTime(time.Now()).
		Exec(ctx)
}
