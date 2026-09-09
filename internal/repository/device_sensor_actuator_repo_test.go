package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"iot-platform.local/internal/ent"
)

// newDeviceOwner 创建测试用户 + 设备（返回 ownerID）
func newDeviceOwner(t *testing.T, client *ent.Client, deviceID string) uint {
	t.Helper()
	owner, err := NewUserRepo(client).Create(context.Background(), makeUser("owner-"+deviceID))
	require.NoError(t, err)
	d, err := NewDeviceRepo(client).Create(context.Background(), makeDevice(deviceID, owner.ID))
	require.NoError(t, err)
	require.Equal(t, deviceID, d.ID)
	return owner.ID
}

func TestNewDeviceSensorRepo(t *testing.T) {
	repo := NewDeviceSensorRepo(nil)
	assert.NotNil(t, repo)
	assert.Nil(t, repo.client)
}

func TestDeviceSensorRepo_CRUD(t *testing.T) {
	client := newRepoEnt(t)
	newDeviceOwner(t, client, "sensordev")
	repo := NewDeviceSensorRepo(client)
	ctx := context.Background()

	// Create
	s, err := repo.Create(ctx, &ent.DeviceSensor{
		DeviceID: "sensordev", SensorID: "temp", Name: "温度", Type: "temperature",
		DataType: "float", Unit: "°C", Specs: `{}`, Enabled: true,
	})
	require.NoError(t, err)
	require.NotZero(t, s.ID)

	// Get 命中 / 未命中（NotFoundError）
	got, err := repo.Get(ctx, "sensordev", "temp")
	require.NoError(t, err)
	assert.Equal(t, "温度", got.Name)
	assert.Nil(t, got.ReportInterval) // 未设置 = NULL（继承全局周期）
	_, err = repo.Get(ctx, "sensordev", "ghost")
	require.Error(t, err)
	assert.True(t, ent.IsNotFound(err))

	// ListByDeviceID（按 id 升序；第二个传感器在后）
	_, err = repo.Create(ctx, &ent.DeviceSensor{
		DeviceID: "sensordev", SensorID: "humi", Name: "湿度", Type: "humidity",
		DataType: "float", Enabled: true,
	})
	require.NoError(t, err)
	all, err := repo.ListByDeviceID(ctx, "sensordev")
	require.NoError(t, err)
	assert.Len(t, all, 2)
	assert.Equal(t, "temp", all[0].SensorID)

	// Update 全字段（覆盖每个增量分支）
	name, tp, dt, un := "温度传感", "temperature2", "int", ""
	sp := `{"min":0}`
	interval, enabled := 30, false
	got2, err := repo.Update(ctx, "sensordev", "humi", SensorUpdateFields{
		Name: &name, Type: &tp, DataType: &dt, Unit: &un, Specs: &sp,
		ReportInterval: &interval, Enabled: &enabled,
	})
	require.NoError(t, err)
	assert.Equal(t, "温度传感", got2.Name)
	assert.Equal(t, "temperature2", got2.Type)
	assert.Equal(t, 30, *got2.ReportInterval)
	assert.False(t, got2.Enabled)

	// Update reportInterval 清空（ClearReportInterval → 恢复继承全局周期）
	got3, err := repo.Update(ctx, "sensordev", "humi", SensorUpdateFields{
		ClearReportInterval: true,
	})
	require.NoError(t, err)
	assert.Nil(t, got3.ReportInterval)

	// Update 未命中 → NotFoundError
	_, err = repo.Update(ctx, "sensordev", "ghost", SensorUpdateFields{Name: &name})
	require.Error(t, err)
	assert.True(t, ent.IsNotFound(err))

	// Delete（命中 1 / 重复删除 0）
	n, err := repo.Delete(ctx, "sensordev", "temp")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	_, err = repo.Get(ctx, "sensordev", "temp")
	assert.True(t, ent.IsNotFound(err))
	n2, err := repo.Delete(ctx, "sensordev", "temp")
	require.NoError(t, err)
	assert.Equal(t, 0, n2)

	// DeleteByDeviceID 级联删除
	require.NoError(t, repo.DeleteByDeviceID(ctx, "sensordev"))
	all2, err := repo.ListByDeviceID(ctx, "sensordev")
	require.NoError(t, err)
	assert.Empty(t, all2)

	// 不存在设备 → 空列表
	all3, err := repo.ListByDeviceID(ctx, "ghost-dev")
	require.NoError(t, err)
	assert.Empty(t, all3)
}

func TestNewDeviceActuatorRepo(t *testing.T) {
	repo := NewDeviceActuatorRepo(nil)
	assert.NotNil(t, repo)
	assert.Nil(t, repo.client)
}

func TestDeviceActuatorRepo_CRUD(t *testing.T) {
	client := newRepoEnt(t)
	newDeviceOwner(t, client, "actdev")
	repo := NewDeviceActuatorRepo(client)
	ctx := context.Background()

	// Create
	a, err := repo.Create(ctx, &ent.DeviceActuator{
		DeviceID: "actdev", ActuatorID: "led", Name: "指示灯", Driver: "led",
		Specs: `{"gpio":2}`, Enabled: true,
	})
	require.NoError(t, err)
	require.NotZero(t, a.ID)

	// Get 命中 / 未命中
	got, err := repo.Get(ctx, "actdev", "led")
	require.NoError(t, err)
	assert.Equal(t, "led", got.Driver)
	_, err = repo.Get(ctx, "actdev", "ghost")
	require.Error(t, err)
	assert.True(t, ent.IsNotFound(err))

	// ListByDeviceID（第二个执行器在后）
	_, err = repo.Create(ctx, &ent.DeviceActuator{
		DeviceID: "actdev", ActuatorID: "servo", Name: "舵机", Driver: "servo",
		Specs: `{}`, Enabled: false,
	})
	require.NoError(t, err)
	all, err := repo.ListByDeviceID(ctx, "actdev")
	require.NoError(t, err)
	assert.Len(t, all, 2)
	assert.Equal(t, "led", all[0].ActuatorID)

	// Update 全字段（覆盖每个增量分支）
	name, driver, specs := "LED灯", "led2", `{"gpio":5}`
	enabled := false
	got2, err := repo.Update(ctx, "actdev", "led", ActuatorUpdateFields{
		Name: &name, Driver: &driver, Specs: &specs, Enabled: &enabled,
	})
	require.NoError(t, err)
	assert.Equal(t, "LED灯", got2.Name)
	assert.Equal(t, "led2", got2.Driver)
	assert.Equal(t, `{"gpio":5}`, got2.Specs)
	assert.False(t, got2.Enabled)

	// Update 未命中 → NotFoundError
	_, err = repo.Update(ctx, "actdev", "ghost", ActuatorUpdateFields{Name: &name})
	require.Error(t, err)
	assert.True(t, ent.IsNotFound(err))

	// Delete / 重复删除
	n, err := repo.Delete(ctx, "actdev", "led")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	n2, err := repo.Delete(ctx, "actdev", "led")
	require.NoError(t, err)
	assert.Equal(t, 0, n2)

	// DeleteByDeviceID
	require.NoError(t, repo.DeleteByDeviceID(ctx, "actdev"))
	all2, err := repo.ListByDeviceID(ctx, "actdev")
	require.NoError(t, err)
	assert.Empty(t, all2)
}
