package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"

	"iot-platform.local/internal/ent"
	entity "iot-platform.local/internal/model"
	"iot-platform.local/internal/repository"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/cache"
)

// detailTestEnv 组装真实 sqlite + miniredis 的设备详情控制器依赖
type detailTestEnv struct {
	ctl    *DeviceController
	client *ent.Client
	mr     *miniredis.Miniredis
	rcache *cache.RedisCache
	sensor *service.DeviceSensorService
}

func newDetailTestEnv(t *testing.T) *detailTestEnv {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client, err := ent.Open("sqlite3", "file:"+dbPath+"?_fk=1")
	if err != nil {
		t.Fatalf("打开 sqlite 失败: %v", err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	rcache := cache.NewRedisCache(rdb)
	deviceRepo := repository.NewDeviceRepo(client)
	configRepo := repository.NewDeviceConfigRepo(client)
	sensorRepo := repository.NewDeviceSensorRepo(client)
	actuatorRepo := repository.NewDeviceActuatorRepo(client)
	cmdRepo := repository.NewDownlinkCmdRepo(client)

	deviceSvc := service.NewDeviceService(deviceRepo, rcache, sensorRepo, actuatorRepo, configRepo)
	downlinkSvc := service.NewDownlinkService(cmdRepo, deviceRepo, rdb, nil, nil)
	configSvc := service.NewDeviceConfigService(configRepo, downlinkSvc, nil)
	sensorSvc := service.NewDeviceSensorService(sensorRepo, configSvc, rcache)
	actuatorSvc := service.NewDeviceActuatorService(actuatorRepo, configSvc, rcache)

	influx := service.NewInfluxDBService(service.InfluxDBConfig{Database: "iot"})
	// URL 为空 → client 未建立，WriteSensors 走"未连接"错误分支（异步无害）
	reportSvc := service.NewDeviceReportService(deviceRepo, influx, rcache, deviceSvc)

	return &detailTestEnv{
		ctl:    NewDeviceController(deviceSvc, reportSvc, sensorSvc, actuatorSvc),
		client: client,
		mr:     mr,
		rcache: rcache,
		sensor: sensorSvc,
	}
}

func (e *detailTestEnv) seedDevice(t *testing.T, deviceID string) uint {
	t.Helper()
	now := time.Now()
	owner, err := repository.NewUserRepo(e.client).Create(context.Background(), &ent.User{
		Account: "owner_" + deviceID, Passwd: "h", Role: "user", Status: "ACTIVE",
		CreateTime: now, UpdateTime: now,
	})
	if err != nil {
		t.Fatalf("种子用户失败: %v", err)
	}
	_, err = repository.NewDeviceRepo(e.client).Create(context.Background(), &ent.Device{
		ID: deviceID, DeviceName: "测试设备", OwnerID: owner.ID, Status: "ONLINE",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("种子设备失败: %v", err)
	}
	return owner.ID
}

// doDetail 构造带 userId 的 gin.Context 并执行设备详情 handler
func doDetail(t *testing.T, deviceID string, ownerID uint, handler func(*gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/devices/"+deviceID, nil)
	c.Params = gin.Params{{Key: "deviceId", Value: deviceID}}
	c.Set("userId", ownerID)

	handler(c)
	return w
}

// TestDeviceDetail_ThingModelAggregation 验证设备详情物模型聚合（扁平视图）：
//   - data.sensors 为传感器物模型数组（定义字段 + 服务端 join 的最近遥测 latest）
//   - 未上报定义的 latest 为 null
//   - data.actuators 为执行器物模型数组
//   - 设备无定义时两数组均为 []（非 null）
func TestDeviceDetail_ThingModelAggregation(t *testing.T) {
	env := newDetailTestEnv(t)
	ownerID := env.seedDevice(t, "dev1")
	ctx := context.Background()

	// 定义：temperature（id 契约）+ humidity（中文名兜底关联）+ led1 执行器
	_, err := env.sensor.Create(ctx, "dev1", entity.SensorCreateRequest{
		ID: "temperature", Name: "温度", Type: "temperature", DataType: "float", Unit: "°C",
	})
	assert.NoError(t, err)
	_, err = env.sensor.Create(ctx, "dev1", entity.SensorCreateRequest{
		ID: "humidity", Name: "湿度", Type: "humidity", DataType: "float", Unit: "%RH",
	})
	assert.NoError(t, err)

	actuatorRepo := repository.NewDeviceActuatorRepo(env.client)
	_, err = actuatorRepo.Create(ctx, &ent.DeviceActuator{
		DeviceID: "dev1", ActuatorID: "led1", Name: "指示灯", Driver: "led",
		Enabled: true,
	})
	assert.NoError(t, err)

	// 上报遥测：temperature 按 id 命中；湿度走中文名兜底
	now := time.Now()
	recent := []entity.SensorData{
		{Name: "temperature", Type: "temperature", Value: 25.5, Timestamp: now},
		{Name: "湿度", Type: "humidity", Value: 60.0, Timestamp: now.Add(-time.Second)},
	}
	assert.NoError(t, env.rcache.CacheSensorRecent(ctx, "dev1", recent))

	w := doDetail(t, "dev1", ownerID, env.ctl.GetDeviceData)

	var resp struct {
		Code int                    `json:"code"`
		Data map[string]interface{} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 200, resp.Code)

	// 设备元信息与在线状态
	assert.Equal(t, "dev1", resp.Data["deviceId"])
	assert.Equal(t, "ONLINE", resp.Data["status"])

	// 传感器物模型数组（顶层）
	sensors, ok := resp.Data["sensors"].([]interface{})
	assert.True(t, ok, "data.sensors 应为物模型数组")
	assert.Len(t, sensors, 2)

	first := sensors[0].(map[string]interface{})
	assert.Equal(t, "temperature", first["id"])
	assert.Equal(t, "°C", first["unit"]) // 定义字段平铺
	latest := first["latest"].(map[string]interface{})
	assert.Equal(t, 25.5, latest["value"])
	assert.NotNil(t, latest["timestamp"])

	second := sensors[1].(map[string]interface{})
	assert.Equal(t, "humidity", second["id"])
	latest2 := second["latest"].(map[string]interface{})
	assert.Equal(t, 60.0, latest2["value"]) // 中文名兜底命中

	// 执行器物模型数组（顶层）
	actuators, ok := resp.Data["actuators"].([]interface{})
	assert.True(t, ok, "data.actuators 应为物模型数组")
	assert.Len(t, actuators, 1)
	act := actuators[0].(map[string]interface{})
	assert.Equal(t, "led1", act["id"])
	assert.Equal(t, "led", act["driver"])
}

// TestDeviceDetail_NoTelemetry 设备定义存在但从未上报：latest 为 null，页面可渲染"无数据"
func TestDeviceDetail_NoTelemetry(t *testing.T) {
	env := newDetailTestEnv(t)
	ownerID := env.seedDevice(t, "dev1")
	ctx := context.Background()

	_, err := env.sensor.Create(ctx, "dev1", entity.SensorCreateRequest{
		ID: "switch_1", Name: "开关", Type: "switch", DataType: "bool",
	})
	assert.NoError(t, err)

	w := doDetail(t, "dev1", ownerID, env.ctl.GetDeviceData)

	var resp struct {
		Code int                    `json:"code"`
		Data map[string]interface{} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 200, resp.Code)

	sensors := resp.Data["sensors"].([]interface{})
	assert.Len(t, sensors, 1)
	s := sensors[0].(map[string]interface{})
	assert.Equal(t, "switch_1", s["id"])
	assert.Nil(t, s["latest"])

	// 无执行器定义 → 空数组（非 null）
	assert.Equal(t, []interface{}{}, resp.Data["actuators"])
}

// TestDeviceDetail_NoThingModel 设备没有任何物模型定义：sensors/actuators 均为空数组
func TestDeviceDetail_NoThingModel(t *testing.T) {
	env := newDetailTestEnv(t)
	ownerID := env.seedDevice(t, "dev1")

	w := doDetail(t, "dev1", ownerID, env.ctl.GetDeviceData)

	var resp struct {
		Code int                    `json:"code"`
		Data map[string]interface{} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 200, resp.Code)
	assert.Equal(t, []interface{}{}, resp.Data["sensors"])
	assert.Equal(t, []interface{}{}, resp.Data["actuators"])
}

// TestDeviceDetail_Forbidden 非属主访问详情 → 403
func TestDeviceDetail_Forbidden(t *testing.T) {
	env := newDetailTestEnv(t)
	env.seedDevice(t, "dev1")

	w := doDetail(t, "dev1", 999, env.ctl.GetDeviceData)

	var resp struct {
		Code int `json:"code"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 403, resp.Code)
}
