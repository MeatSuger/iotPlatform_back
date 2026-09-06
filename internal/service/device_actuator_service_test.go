package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"iot-platform.local/internal/ent"
	entity "iot-platform.local/internal/model"
	"iot-platform.local/internal/repository"
	"iot-platform.local/pkg/cache"
)

// buildActuatorSvc 构造「真实 sqlite 仓库 + miniredis 命令队列」的执行器定义服务
func buildActuatorSvc(t *testing.T) (*DeviceActuatorService, *DeviceConfigService, *repository.DeviceRepo, *ent.Client) {
	t.Helper()
	client := newTestEnt(t)
	deviceRepo := repository.NewDeviceRepo(client)
	configRepo := repository.NewDeviceConfigRepo(client)
	actuatorRepo := repository.NewDeviceActuatorRepo(client)
	cmdRepo := repository.NewDownlinkCmdRepo(client)
	_, rdb := newTestRedis(t)
	rcache := cache.NewRedisCache(rdb)

	downlinkSvc := NewDownlinkService(cmdRepo, deviceRepo, rdb, nil, nil)
	configSvc := NewDeviceConfigService(configRepo, downlinkSvc, nil)
	return NewDeviceActuatorService(actuatorRepo, configSvc, rcache), configSvc, deviceRepo, client
}

// seedActuatorDevice 创建属主用户 + 测试设备（满足 owner 外键约束）
func seedActuatorDevice(t *testing.T, client *ent.Client, deviceRepo *repository.DeviceRepo) {
	t.Helper()
	owner := newOwner(t, client)
	seedDevice(t, deviceRepo, "dev1", owner, "ONLINE")
}

func validActuatorReq(id string) entity.ActuatorCreateRequest {
	return entity.ActuatorCreateRequest{
		ID:     id,
		Name:   "舵机",
		Driver: "servo",
		Config: map[string]any{"gpio": 18, "min_pulse_us": 500, "max_pulse_us": 2500},
	}
}

func TestDeviceActuatorService_CreateAndGet(t *testing.T) {
	svc, _, deviceRepo, client := buildActuatorSvc(t)
	seedActuatorDevice(t, client, deviceRepo)

	created, err := svc.Create(context.Background(), "dev1", validActuatorReq("servo1"))
	assert.NoError(t, err)
	assert.Equal(t, "servo1", created.ID)
	assert.Equal(t, "servo", created.Driver)
	assert.True(t, created.Enabled)
	assert.Equal(t, float64(18), created.Config["gpio"])

	got, err := svc.Get(context.Background(), "dev1", "servo1")
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Equal(t, "舵机", got.Name)
}

func TestDeviceActuatorService_CreateDefaults(t *testing.T) {
	svc, _, deviceRepo, client := buildActuatorSvc(t)
	seedActuatorDevice(t, client, deviceRepo)

	created, err := svc.Create(context.Background(), "dev1", entity.ActuatorCreateRequest{
		ID:     "led0",
		Name:   "灯",
		Driver: "led",
	})
	assert.NoError(t, err)
	assert.True(t, created.Enabled) // 缺省启用
	assert.Empty(t, created.Config)
}

func TestDeviceActuatorService_CreateDuplicateID(t *testing.T) {
	svc, _, deviceRepo, client := buildActuatorSvc(t)
	seedActuatorDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validActuatorReq("servo1"))
	assert.NoError(t, err)
	_, err = svc.Create(context.Background(), "dev1", validActuatorReq("servo1"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "已存在")
}

func TestDeviceActuatorService_CreateValidation(t *testing.T) {
	svc, _, deviceRepo, client := buildActuatorSvc(t)
	seedActuatorDevice(t, client, deviceRepo)

	cases := []entity.ActuatorCreateRequest{
		{ID: "Upper_Case", Driver: "led"},             // 非法标识符：大写
		{ID: "9servo", Driver: "led"},                 // 非法标识符：数字开头
		{ID: "led_very_long_name_123", Driver: "led"}, // 超长标识符（>11）
		{ID: "ok1", Driver: "relay"},                  // 非法驱动名
	}
	for _, req := range cases {
		_, err := svc.Create(context.Background(), "dev1", req)
		assert.Error(t, err, "请求 %+v 应校验失败", req)
	}
}

func TestDeviceActuatorService_List(t *testing.T) {
	svc, _, deviceRepo, client := buildActuatorSvc(t)
	seedActuatorDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validActuatorReq("servo1"))
	assert.NoError(t, err)
	_, err = svc.Create(context.Background(), "dev1", entity.ActuatorCreateRequest{ID: "led1", Name: "灯", Driver: "led"})
	assert.NoError(t, err)

	list, err := svc.List(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	assert.Equal(t, "servo1", list[0].ID) // 按创建顺序稳定输出
	assert.Equal(t, "led1", list[1].ID)
}

func TestDeviceActuatorService_GetNotExist(t *testing.T) {
	svc, _, deviceRepo, client := buildActuatorSvc(t)
	seedActuatorDevice(t, client, deviceRepo)

	got, err := svc.Get(context.Background(), "dev1", "nope")
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestDeviceActuatorService_UpdateIncremental(t *testing.T) {
	svc, _, deviceRepo, client := buildActuatorSvc(t)
	seedActuatorDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validActuatorReq("servo1"))
	assert.NoError(t, err)

	// 仅更新名称与 driver，其余字段保持原值
	newName := "云台舵机"
	newDriver := "speaker"
	got, err := svc.Update(context.Background(), "dev1", "servo1", entity.ActuatorUpdateRequest{
		Name:   &newName,
		Driver: &newDriver,
	})
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Equal(t, newName, got.Name)
	assert.Equal(t, "speaker", got.Driver)
	assert.Equal(t, float64(18), got.Config["gpio"]) // 未传字段保持原值
}

func TestDeviceActuatorService_UpdateClearConfig(t *testing.T) {
	svc, _, deviceRepo, client := buildActuatorSvc(t)
	seedActuatorDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validActuatorReq("servo1"))
	assert.NoError(t, err)

	// config 传 {} 显式清空
	raw := json.RawMessage(`{}`)
	got, err := svc.Update(context.Background(), "dev1", "servo1", entity.ActuatorUpdateRequest{Config: &raw})
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got.Config)
}

func TestDeviceActuatorService_UpdateEmptyRequest(t *testing.T) {
	svc, _, deviceRepo, client := buildActuatorSvc(t)
	seedActuatorDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validActuatorReq("servo1"))
	assert.NoError(t, err)

	got, err := svc.Update(context.Background(), "dev1", "servo1", entity.ActuatorUpdateRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "无更新字段")
	assert.Nil(t, got)
}

func TestDeviceActuatorService_UpdateNotExist(t *testing.T) {
	svc, _, deviceRepo, client := buildActuatorSvc(t)
	seedActuatorDevice(t, client, deviceRepo)

	name := "x"
	got, err := svc.Update(context.Background(), "dev1", "nope", entity.ActuatorUpdateRequest{Name: &name})
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestDeviceActuatorService_Delete(t *testing.T) {
	svc, _, deviceRepo, client := buildActuatorSvc(t)
	seedActuatorDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validActuatorReq("servo1"))
	assert.NoError(t, err)

	assert.NoError(t, svc.Delete(context.Background(), "dev1", "servo1"))

	err = svc.Delete(context.Background(), "dev1", "servo1")
	assert.ErrorIs(t, err, ErrActuatorNotFound)
}

func TestDeviceActuatorService_Apply(t *testing.T) {
	svc, configSvc, deviceRepo, client := buildActuatorSvc(t)
	seedActuatorDevice(t, client, deviceRepo)

	// 预置一份含其他分区的配置
	_, err := configSvc.Save(context.Background(), "dev1", map[string]any{
		"sensor": map[string]any{"reportInterval": 60},
	})
	assert.NoError(t, err)

	_, err = svc.Create(context.Background(), "dev1", validActuatorReq("servo1"))
	assert.NoError(t, err)
	_, err = svc.Create(context.Background(), "dev1", entity.ActuatorCreateRequest{ID: "led1", Name: "灯", Driver: "led"})
	assert.NoError(t, err)

	resp, err := svc.Apply(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Equal(t, "dev1", resp.DeviceID)
	assert.Equal(t, uint(2), resp.Version) // 在预置配置基础上递增
	assert.Equal(t, "pending", resp.Status)
	assert.Equal(t, 2, resp.Count)

	// 配置快照包含 actuators 分区且保留原有 sensor 分区
	got, err := configSvc.Get(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Contains(t, got.Payload, `"actuators"`)
	assert.Contains(t, got.Payload, `"servo1"`)
	assert.Contains(t, got.Payload, `"led1"`)
	assert.Contains(t, got.Payload, `"reportInterval"`)
}

func TestDeviceActuatorService_ApplyEmpty(t *testing.T) {
	svc, configSvc, deviceRepo, client := buildActuatorSvc(t)
	seedActuatorDevice(t, client, deviceRepo)

	// 无执行器定义时 Apply：下发空 actuators 数组
	resp, err := svc.Apply(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Equal(t, uint(1), resp.Version)
	assert.Equal(t, 0, resp.Count)

	got, err := configSvc.Get(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Contains(t, got.Payload, `"actuators":[]`)
}

// TestDeviceActuatorService_ListCacheConsistency 验证定义列表缓存的写路径失效：
// Create/Update/Delete 后 List 必须立即可见新状态（缓存陈旧会导致断言失败）。
func TestDeviceActuatorService_ListCacheConsistency(t *testing.T) {
	svc, _, deviceRepo, client := buildActuatorSvc(t)
	seedActuatorDevice(t, client, deviceRepo)
	ctx := context.Background()

	// 预置定义并预热缓存
	_, err := svc.Create(ctx, "dev1", entity.ActuatorCreateRequest{ID: "led1", Name: "指示灯", Driver: "led"})
	assert.NoError(t, err)
	list, err := svc.List(ctx, "dev1")
	assert.NoError(t, err)
	assert.Len(t, list, 1)

	// Create 后缓存失效 → List 立即可见
	_, err = svc.Create(ctx, "dev1", entity.ActuatorCreateRequest{ID: "servo1", Name: "云台舵机", Driver: "servo"})
	assert.NoError(t, err)
	list, err = svc.List(ctx, "dev1")
	assert.NoError(t, err)
	ids := make([]string, len(list))
	for i, a := range list {
		ids[i] = a.ID
	}
	assert.Equal(t, []string{"led1", "servo1"}, ids)

	// Update 后缓存失效 → List 反映新驱动参数
	newName := "主灯"
	_, err = svc.Update(ctx, "dev1", "led1", entity.ActuatorUpdateRequest{Name: &newName})
	assert.NoError(t, err)
	list, err = svc.List(ctx, "dev1")
	assert.NoError(t, err)
	for _, a := range list {
		if a.ID == "led1" {
			assert.Equal(t, "主灯", a.Name)
		}
	}

	// Delete 后缓存失效 → 列表收缩
	assert.NoError(t, svc.Delete(ctx, "dev1", "servo1"))
	list, err = svc.List(ctx, "dev1")
	assert.NoError(t, err)
	assert.Len(t, list, 1)

	// 连续 List 稳定（命中缓存路径）
	list, err = svc.List(ctx, "dev1")
	assert.NoError(t, err)
	assert.Len(t, list, 1)
}

// TestDeviceActuatorService_ListEmptyDevice 设备无定义时 List 返回空切片且可重复调用
func TestDeviceActuatorService_ListEmptyDevice(t *testing.T) {
	svc, _, deviceRepo, client := buildActuatorSvc(t)
	seedActuatorDevice(t, client, deviceRepo)

	list, err := svc.List(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.NotNil(t, list)
	assert.Len(t, list, 0)

	list, err = svc.List(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Len(t, list, 0)
}
