package repository

import (
	"context"

	"iot-platform.local/internal/ent"
	entactuator "iot-platform.local/internal/ent/deviceactuator"
)

// DeviceActuatorRepo 设备执行器定义数据访问
type DeviceActuatorRepo struct {
	client *ent.Client
}

// NewDeviceActuatorRepo 创建执行器定义仓库
func NewDeviceActuatorRepo(client *ent.Client) *DeviceActuatorRepo {
	return &DeviceActuatorRepo{client: client}
}

// ListByDeviceID 查询设备全部执行器定义（按 id 升序，保持稳定输出）
func (r *DeviceActuatorRepo) ListByDeviceID(ctx context.Context, deviceID string) ([]*ent.DeviceActuator, error) {
	return r.client.DeviceActuator.Query().
		Where(entactuator.DeviceIDEQ(deviceID)).
		Order(ent.Asc(entactuator.FieldID)).
		All(ctx)
}

// Get 查询单个执行器定义；不存在返回 ent.NotFoundError
func (r *DeviceActuatorRepo) Get(ctx context.Context, deviceID, actuatorID string) (*ent.DeviceActuator, error) {
	return r.client.DeviceActuator.Query().
		Where(
			entactuator.DeviceIDEQ(deviceID),
			entactuator.ActuatorIDEQ(actuatorID),
		).
		First(ctx)
}

// Create 创建执行器定义
func (r *DeviceActuatorRepo) Create(ctx context.Context, a *ent.DeviceActuator) (*ent.DeviceActuator, error) {
	return r.client.DeviceActuator.Create().
		SetDeviceID(a.DeviceID).
		SetActuatorID(a.ActuatorID).
		SetName(a.Name).
		SetDriver(a.Driver).
		SetSpecs(a.Specs).
		SetEnabled(a.Enabled).
		Save(ctx)
}

// ActuatorUpdateFields 执行器定义可更新字段集合（增量语义：零值指针表示不更新）
type ActuatorUpdateFields struct {
	Name    *string
	Driver  *string
	Specs   *string
	Enabled *bool
}

// Update 增量更新执行器定义，返回更新后的实体；不存在返回 ent.NotFoundError
func (r *DeviceActuatorRepo) Update(ctx context.Context, deviceID, actuatorID string, fields ActuatorUpdateFields) (*ent.DeviceActuator, error) {
	u := r.client.DeviceActuator.Update().
		Where(
			entactuator.DeviceIDEQ(deviceID),
			entactuator.ActuatorIDEQ(actuatorID),
		)

	if fields.Name != nil {
		u.SetName(*fields.Name)
	}
	if fields.Driver != nil {
		u.SetDriver(*fields.Driver)
	}
	if fields.Specs != nil {
		u.SetSpecs(*fields.Specs)
	}
	if fields.Enabled != nil {
		u.SetEnabled(*fields.Enabled)
	}

	if err := u.Exec(ctx); err != nil {
		return nil, err
	}
	return r.Get(ctx, deviceID, actuatorID)
}

// Delete 删除执行器定义，返回受影响行数
func (r *DeviceActuatorRepo) Delete(ctx context.Context, deviceID, actuatorID string) (int, error) {
	return r.client.DeviceActuator.Delete().
		Where(
			entactuator.DeviceIDEQ(deviceID),
			entactuator.ActuatorIDEQ(actuatorID),
		).
		Exec(ctx)
}

// DeleteByDeviceID 删除设备全部执行器定义（设备删除时级联入口）
func (r *DeviceActuatorRepo) DeleteByDeviceID(ctx context.Context, deviceID string) error {
	_, err := r.client.DeviceActuator.Delete().
		Where(entactuator.DeviceIDEQ(deviceID)).
		Exec(ctx)
	return err
}
