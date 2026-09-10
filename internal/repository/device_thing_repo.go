package repository

import (
	"context"

	"iot-platform.local/internal/ent"
	entthing "iot-platform.local/internal/ent/devicething"
)

// DeviceThingRepo 设备物模型组件（传感器/执行器）数据访问
type DeviceThingRepo struct {
	client *ent.Client
}

// NewDeviceThingRepo 创建物模型组件仓库
func NewDeviceThingRepo(client *ent.Client) *DeviceThingRepo {
	return &DeviceThingRepo{client: client}
}

// ListByDeviceID 查询设备指定 kind 的全部组件（按 id 升序，保持稳定输出）
func (r *DeviceThingRepo) ListByDeviceID(ctx context.Context, deviceID, kind string) ([]*ent.DeviceThing, error) {
	return r.client.DeviceThing.Query().
		Where(
			entthing.DeviceIDEQ(deviceID),
			entthing.KindEQ(kind),
		).
		Order(ent.Asc(entthing.FieldID)).
		All(ctx)
}

// Get 查询单个组件；不存在返回 ent.NotFoundError
func (r *DeviceThingRepo) Get(ctx context.Context, deviceID, kind, thingID string) (*ent.DeviceThing, error) {
	return r.client.DeviceThing.Query().
		Where(
			entthing.DeviceIDEQ(deviceID),
			entthing.KindEQ(kind),
			entthing.ThingIDEQ(thingID),
		).
		First(ctx)
}

// Create 创建组件
func (r *DeviceThingRepo) Create(ctx context.Context, t *ent.DeviceThing) (*ent.DeviceThing, error) {
	return r.client.DeviceThing.Create().
		SetDeviceID(t.DeviceID).
		SetKind(t.Kind).
		SetThingID(t.ThingID).
		SetName(t.Name).
		SetSpecs(t.Specs).
		SetEnabled(t.Enabled).
		Save(ctx)
}

// ThingUpdateFields 组件可更新字段集合（增量语义：零值指针表示不更新）
type ThingUpdateFields struct {
	Name    *string
	Specs   *string
	Enabled *bool
}

// Update 增量更新组件，返回更新后的实体；不存在返回 ent.NotFoundError
func (r *DeviceThingRepo) Update(ctx context.Context, deviceID, kind, thingID string, fields ThingUpdateFields) (*ent.DeviceThing, error) {
	u := r.client.DeviceThing.Update().
		Where(
			entthing.DeviceIDEQ(deviceID),
			entthing.KindEQ(kind),
			entthing.ThingIDEQ(thingID),
		)

	if fields.Name != nil {
		u.SetName(*fields.Name)
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
	return r.Get(ctx, deviceID, kind, thingID)
}

// Delete 删除组件，返回受影响行数
func (r *DeviceThingRepo) Delete(ctx context.Context, deviceID, kind, thingID string) (int, error) {
	return r.client.DeviceThing.Delete().
		Where(
			entthing.DeviceIDEQ(deviceID),
			entthing.KindEQ(kind),
			entthing.ThingIDEQ(thingID),
		).
		Exec(ctx)
}

// DeleteByDeviceID 删除设备全部物模型组件（设备删除时级联入口）
func (r *DeviceThingRepo) DeleteByDeviceID(ctx context.Context, deviceID string) error {
	_, err := r.client.DeviceThing.Delete().
		Where(entthing.DeviceIDEQ(deviceID)).
		Exec(ctx)
	return err
}
