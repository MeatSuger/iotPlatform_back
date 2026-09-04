package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	entity "iot-platform.local/internal/model"
	"iot-platform.local/internal/repository"

	"iot-platform.local/internal/ent"
)

// buildSensorSvc 构造「真实 sqlite 仓库 + miniredis 命令队列」的传感器定义服务
func buildSensorSvc(t *testing.T) (*DeviceSensorService, *DeviceConfigService, *repository.DeviceRepo, *ent.Client) {
	t.Helper()
	client := newTestEnt(t)
	deviceRepo := repository.NewDeviceRepo(client)
	configRepo := repository.NewDeviceConfigRepo(client)
	sensorRepo := repository.NewDeviceSensorRepo(client)
	cmdRepo := repository.NewDownlinkCmdRepo(client)
	_, rdb := newTestRedis(t)

	downlinkSvc := NewDownlinkService(cmdRepo, deviceRepo, rdb, nil)
	configSvc := NewDeviceConfigService(configRepo, downlinkSvc)
	return NewDeviceSensorService(sensorRepo, configSvc), configSvc, deviceRepo, client
}

// seedSensorDevice 创建属主用户 + 测试设备（满足 owner 外键约束）
func seedSensorDevice(t *testing.T, client *ent.Client, deviceRepo *repository.DeviceRepo) {
	t.Helper()
	owner := newOwner(t, client)
	seedDevice(t, deviceRepo, "dev1", owner, "ONLINE")
}

func validSensorReq(id string) entity.SensorCreateRequest {
	return entity.SensorCreateRequest{
		ID:             id,
		Name:           "温度",
		Type:           "temperature",
		DataType:       "float",
		Unit:           "°C",
		Specs:          map[string]any{"min": -40, "max": 125, "step": 0.1},
		ReportInterval: 60,
		Thresholds:     map[string]any{"min": 0, "max": 100, "alarm": true},
	}
}

func TestDeviceSensorService_CreateAndGet(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedSensorDevice(t, client, deviceRepo)

	created, err := svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)
	assert.Equal(t, "temperature", created.ID)
	assert.Equal(t, "float", created.DataType)
	assert.Equal(t, 60, created.ReportInterval)
	assert.True(t, created.Enabled)
	assert.Equal(t, float64(125), created.Specs["max"])

	got, err := svc.Get(context.Background(), "dev1", "temperature")
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Equal(t, "温度", got.Name)
}

func TestDeviceSensorService_CreateDefaults(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedSensorDevice(t, client, deviceRepo)

	created, err := svc.Create(context.Background(), "dev1", entity.SensorCreateRequest{
		ID:   "switch_1",
		Name: "开关",
		Type: "switch",
	})
	assert.NoError(t, err)
	assert.Equal(t, "float", created.DataType) // 缺省回退 float
	assert.True(t, created.Enabled)            // 缺省启用
	assert.Empty(t, created.Specs)
}

func TestDeviceSensorService_CreateDuplicateID(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedSensorDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)
	_, err = svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "已存在")
}

func TestDeviceSensorService_CreateValidation(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedSensorDevice(t, client, deviceRepo)

	cases := []entity.SensorCreateRequest{
		{ID: "Upper_Case", Name: "x", Type: "t"},                 // 非法标识符：大写
		{ID: "9lead", Name: "x", Type: "t"},                      // 非法标识符：数字开头
		{ID: "ok_id", Name: "n", Type: "t", DataType: "unknown"}, // 非法数据类型
		{ID: "ok_id", Name: "n", Type: "t", ReportInterval: -1},  // 非法上报周期
	}
	for _, req := range cases {
		_, err := svc.Create(context.Background(), "dev1", req)
		assert.Error(t, err, "请求 %+v 应校验失败", req)
	}
}

func TestDeviceSensorService_List(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedSensorDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)
	_, err = svc.Create(context.Background(), "dev1", entity.SensorCreateRequest{ID: "humidity", Name: "湿度", Type: "humidity"})
	assert.NoError(t, err)

	list, err := svc.List(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	assert.Equal(t, "temperature", list[0].ID) // 按创建顺序稳定输出
	assert.Equal(t, "humidity", list[1].ID)
}

func TestDeviceSensorService_GetNotExist(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedSensorDevice(t, client, deviceRepo)

	got, err := svc.Get(context.Background(), "dev1", "nope")
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestDeviceSensorService_UpdateIncremental(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedSensorDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)

	// 仅更新名称与上报周期，其余字段保持原值
	newName := "环境温度"
	newInterval := 120
	got, err := svc.Update(context.Background(), "dev1", "temperature", entity.SensorUpdateRequest{
		Name:           &newName,
		ReportInterval: &newInterval,
	})
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Equal(t, newName, got.Name)
	assert.Equal(t, 120, got.ReportInterval)
	assert.Equal(t, "°C", got.Unit)                 // 未传字段保持原值
	assert.Equal(t, float64(125), got.Specs["max"]) // specs 未受影响
}

func TestDeviceSensorService_UpdateClearSpecs(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedSensorDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)

	// specs 传 {} 显式清空
	raw := json.RawMessage(`{}`)
	got, err := svc.Update(context.Background(), "dev1", "temperature", entity.SensorUpdateRequest{Specs: &raw})
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got.Specs)
}

func TestDeviceSensorService_UpdateEmptyRequest(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedSensorDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)

	got, err := svc.Update(context.Background(), "dev1", "temperature", entity.SensorUpdateRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "无更新字段")
	assert.Nil(t, got)
}

func TestDeviceSensorService_UpdateNotExist(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedSensorDevice(t, client, deviceRepo)

	name := "x"
	got, err := svc.Update(context.Background(), "dev1", "nope", entity.SensorUpdateRequest{Name: &name})
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestDeviceSensorService_Delete(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedSensorDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)

	assert.NoError(t, svc.Delete(context.Background(), "dev1", "temperature"))

	err = svc.Delete(context.Background(), "dev1", "temperature")
	assert.ErrorIs(t, err, ErrSensorNotFound)
}

func TestDeviceSensorService_Apply(t *testing.T) {
	svc, configSvc, deviceRepo, client := buildSensorSvc(t)
	seedSensorDevice(t, client, deviceRepo)

	// 预置一份含其他分区的配置
	_, err := configSvc.Save(context.Background(), "dev1", map[string]any{
		"sensor": map[string]any{"reportInterval": 60},
	})
	assert.NoError(t, err)

	_, err = svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)
	_, err = svc.Create(context.Background(), "dev1", entity.SensorCreateRequest{ID: "humidity", Name: "湿度", Type: "humidity"})
	assert.NoError(t, err)

	resp, err := svc.Apply(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Equal(t, "dev1", resp.DeviceID)
	assert.Equal(t, uint(2), resp.Version) // 在预置配置基础上递增
	assert.Equal(t, "pending", resp.Status)
	assert.Equal(t, 2, resp.Count)

	// 配置快照包含 sensors 分区且保留原有 sensor 分区
	got, err := configSvc.Get(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Contains(t, got.Payload, `"sensors"`)
	assert.Contains(t, got.Payload, `"temperature"`)
	assert.Contains(t, got.Payload, `"humidity"`)
	assert.Contains(t, got.Payload, `"reportInterval"`)
}

func TestDeviceSensorService_ApplyEmpty(t *testing.T) {
	svc, configSvc, deviceRepo, client := buildSensorSvc(t)
	seedSensorDevice(t, client, deviceRepo)

	// 无传感器定义时 Apply：下发空 sensors 数组
	resp, err := svc.Apply(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Equal(t, uint(1), resp.Version)
	assert.Equal(t, 0, resp.Count)

	got, err := configSvc.Get(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Contains(t, got.Payload, `"sensors":[]`)
}
