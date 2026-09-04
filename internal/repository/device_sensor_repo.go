package repository

import (
	"context"

	"iot-platform.local/internal/ent"
	entsensor "iot-platform.local/internal/ent/devicesensor"
)

// DeviceSensorRepo 设备传感器定义数据访问
type DeviceSensorRepo struct {
	client *ent.Client
}

// NewDeviceSensorRepo 创建传感器定义仓库
func NewDeviceSensorRepo(client *ent.Client) *DeviceSensorRepo {
	return &DeviceSensorRepo{client: client}
}

// ListByDeviceID 查询设备全部传感器定义（按创建时间升序，保持稳定输出）
func (r *DeviceSensorRepo) ListByDeviceID(ctx context.Context, deviceID string) ([]*ent.DeviceSensor, error) {
	return r.client.DeviceSensor.Query().
		Where(entsensor.DeviceIDEQ(deviceID)).
		Order(ent.Asc(entsensor.FieldID)).
		All(ctx)
}

// Get 查询单个传感器定义；不存在返回 ent.NotFoundError
func (r *DeviceSensorRepo) Get(ctx context.Context, deviceID, sensorID string) (*ent.DeviceSensor, error) {
	return r.client.DeviceSensor.Query().
		Where(
			entsensor.DeviceIDEQ(deviceID),
			entsensor.SensorIDEQ(sensorID),
		).
		First(ctx)
}

// Create 创建传感器定义
func (r *DeviceSensorRepo) Create(ctx context.Context, s *ent.DeviceSensor) (*ent.DeviceSensor, error) {
	return r.client.DeviceSensor.Create().
		SetDeviceID(s.DeviceID).
		SetSensorID(s.SensorID).
		SetName(s.Name).
		SetType(s.Type).
		SetDataType(s.DataType).
		SetUnit(s.Unit).
		SetSpecs(s.Specs).
		SetReportInterval(s.ReportInterval).
		SetThresholds(s.Thresholds).
		SetAttrs(s.Attrs).
		SetEnabled(s.Enabled).
		Save(ctx)
}

// UpdateFields 传感器定义可更新字段集合（增量语义：零值指针表示不更新）
type SensorUpdateFields struct {
	Name           *string
	Type           *string
	DataType       *string
	Unit           *string
	Specs          *string
	ReportInterval *int
	Thresholds     *string
	Attrs          *string
	Enabled        *bool
}

// Update 增量更新传感器定义，返回更新后的实体；不存在返回 ent.NotFoundError
func (r *DeviceSensorRepo) Update(ctx context.Context, deviceID, sensorID string, fields SensorUpdateFields) (*ent.DeviceSensor, error) {
	u := r.client.DeviceSensor.Update().
		Where(
			entsensor.DeviceIDEQ(deviceID),
			entsensor.SensorIDEQ(sensorID),
		)

	if fields.Name != nil {
		u.SetName(*fields.Name)
	}
	if fields.Type != nil {
		u.SetType(*fields.Type)
	}
	if fields.DataType != nil {
		u.SetDataType(*fields.DataType)
	}
	if fields.Unit != nil {
		u.SetUnit(*fields.Unit)
	}
	if fields.Specs != nil {
		u.SetSpecs(*fields.Specs)
	}
	if fields.ReportInterval != nil {
		u.SetReportInterval(*fields.ReportInterval)
	}
	if fields.Thresholds != nil {
		u.SetThresholds(*fields.Thresholds)
	}
	if fields.Attrs != nil {
		u.SetAttrs(*fields.Attrs)
	}
	if fields.Enabled != nil {
		u.SetEnabled(*fields.Enabled)
	}

	if err := u.Exec(ctx); err != nil {
		return nil, err
	}
	return r.Get(ctx, deviceID, sensorID)
}

// Delete 删除传感器定义，返回受影响行数
func (r *DeviceSensorRepo) Delete(ctx context.Context, deviceID, sensorID string) (int, error) {
	return r.client.DeviceSensor.Delete().
		Where(
			entsensor.DeviceIDEQ(deviceID),
			entsensor.SensorIDEQ(sensorID),
		).
		Exec(ctx)
}

// DeleteByDeviceID 删除设备全部传感器定义（设备删除时级联入口）
func (r *DeviceSensorRepo) DeleteByDeviceID(ctx context.Context, deviceID string) error {
	_, err := r.client.DeviceSensor.Delete().
		Where(entsensor.DeviceIDEQ(deviceID)).
		Exec(ctx)
	return err
}
