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

// NewDeviceRepo 创建设备仓库
func NewDeviceRepo(client *ent.Client) *DeviceRepo {
	return &DeviceRepo{client: client}
}

// Create 创建设备
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

// GetByID 按 ID 查询设备
func (r *DeviceRepo) GetByID(ctx context.Context, id string) (*ent.Device, error) {
	return r.client.Device.Get(ctx, id)
}

// GetByDeviceID 按设备编号查询设备（设备编号即主键 ID，与 GetByID 等价）
func (r *DeviceRepo) GetByDeviceID(ctx context.Context, deviceID string) (*ent.Device, error) {
	return r.client.Device.Query().Where(entdevice.IDEQ(deviceID)).First(ctx)
}

// DeviceUpdateFields 设备增量更新字段集合；字段为指针，仅非 nil 的字段会被写入，
// 未传（nil）的字段保持数据库原值，避免空串覆盖。
type DeviceUpdateFields struct {
	DeviceName      *string
	DeviceType      *string
	FirmwareVersion *string
	IPAddress       *string
	MACAddress      *string
	Location        *string
}

// IsEmpty 判断是否没有任何待更新字段
func (f DeviceUpdateFields) IsEmpty() bool {
	return f.DeviceName == nil && f.DeviceType == nil && f.FirmwareVersion == nil &&
		f.IPAddress == nil && f.MACAddress == nil && f.Location == nil
}

// Update 增量更新设备可编辑字段；仅写入 fields 中非 nil 的字段。
// 不修改 Status（运行时状态由 UpdateStatus/UpdateLastActive 维护），
// 也不修改 ID / OwnerID（ID 为 Immutable，OwnerID 由登录态决定）。
// updated_at 由 schema 的 UpdateDefault(time.Now) 在更新时自动刷新。
func (r *DeviceRepo) Update(ctx context.Context, deviceID string, fields DeviceUpdateFields) error {
	return r.client.Device.UpdateOneID(deviceID).
		SetNillableDeviceName(fields.DeviceName).
		SetNillableDeviceType(fields.DeviceType).
		SetNillableFirmwareVersion(fields.FirmwareVersion).
		SetNillableIPAddress(fields.IPAddress).
		SetNillableMACAddress(fields.MACAddress).
		SetNillableLocation(fields.Location).
		Exec(ctx)
}

// Delete 按 ID 删除设备
func (r *DeviceRepo) Delete(ctx context.Context, id string) error {
	return r.client.Device.DeleteOneID(id).Exec(ctx)
}

// ListByOwnerID 查询某 owner 的全部设备（按 ID 倒序）
func (r *DeviceRepo) ListByOwnerID(ctx context.Context, ownerID uint) ([]*ent.Device, error) {
	return r.client.Device.Query().
		Where(entdevice.OwnerIDEQ(ownerID)).
		Order(ent.Desc(entdevice.FieldID)).
		All(ctx)
}

// ListByDeviceID 按设备编号查询设备（按 ID 倒序）
func (r *DeviceRepo) ListByDeviceID(ctx context.Context, deviceID string) ([]*ent.Device, error) {
	return r.client.Device.Query().
		Where(entdevice.IDEQ(deviceID)).
		Order(ent.Desc(entdevice.FieldID)).
		All(ctx)
}

// UpdateStatus 更新设备状态
func (r *DeviceRepo) UpdateStatus(ctx context.Context, deviceID, status string) error {
	return r.client.Device.Update().
		Where(entdevice.IDEQ(deviceID)).
		SetStatus(status).
		Exec(ctx)
}

// UpdateLastActive 更新设备状态并刷新最后活跃时间
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
