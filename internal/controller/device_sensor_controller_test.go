// 物联网设备接入与物模型管理平台软件（物咸通）V1.0
// Copyright (C) 2025-2026 余昊
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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

// sensorTestEnv 组装真实 sqlite + miniredis 的传感器控制器依赖
type sensorTestEnv struct {
	ctl    *DeviceSensorController
	cfgCtl *DeviceConfigController
	client *ent.Client
	mr     *miniredis.Miniredis
}

func newSensorTestEnv(t *testing.T) *sensorTestEnv {
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

	deviceRepo := repository.NewDeviceRepo(client)
	configRepo := repository.NewDeviceConfigRepo(client)
	sensorRepo := repository.NewDeviceThingRepo(client)
	cmdRepo := repository.NewMessageLogRepo(client)

	rcache := cache.NewRedisCache(rdb)
	deviceSvc := service.NewDeviceService(deviceRepo, rcache, sensorRepo, configRepo)
	downlinkSvc := service.NewDownlinkService(cmdRepo, deviceRepo, rdb, nil, nil)
	configSvc := service.NewDeviceConfigService(configRepo, downlinkSvc, nil)
	sensorSvc := service.NewDeviceSensorService(sensorRepo, configSvc, rcache)

	return &sensorTestEnv{
		ctl:    NewDeviceSensorController(sensorSvc, deviceSvc),
		cfgCtl: NewDeviceConfigController(configSvc, deviceSvc),
		client: client,
		mr:     mr,
	}
}

func (e *sensorTestEnv) seedDevice(t *testing.T, deviceID string) uint {
	t.Helper()
	return seedOwnedDevice(t, e.client, deviceID, "设备")
}

// doSensor 构造带 userId 与路径参数的 gin.Context 并执行 handler
// sensorID 为传感器标识符（可空），op 为操作后缀（"" / "update" / "delete" / "apply"）
func doSensor(t *testing.T, deviceID, sensorID, op string, ownerID uint, body any, handler func(*gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	url := "/api/devices/" + deviceID + "/sensors"
	if sensorID != "" {
		url += "/" + sensorID
	}
	if op != "" {
		url += "/" + op
	}
	c.Request = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if body != nil {
		c.Request.Header.Set("Content-Type", "application/json")
	}
	c.Params = gin.Params{{Key: "deviceId", Value: deviceID}}
	if sensorID != "" {
		c.Params = append(c.Params, gin.Param{Key: "sensorId", Value: sensorID})
	}
	c.Set("userId", ownerID)

	handler(c)
	return w
}

// dataJSON 将 testResp.Data 重新序列化为 JSON 字节，便于二次反序列化
func dataJSON(t *testing.T, resp testResp) []byte {
	t.Helper()
	b, err := json.Marshal(resp.Data)
	assert.NoError(t, err)
	return b
}

func validCreateBody(id string) entity.SensorCreateRequest {
	return entity.SensorCreateRequest{
		ID:       id,
		Name:     "温度",
		Type:     "temperature",
		DataType: "float",
		Unit:     "°C",
		Specs: &entity.SensorSpecs{
			Min:  fptr(-40),
			Max:  fptr(125),
			Step: fptr(0.1),
			Thresholds: &entity.SpecsThresholds{
				Min: fptr(0), Max: fptr(100), Extra: map[string]any{"alarm": true},
			},
		},
		ReportInterval: iptr(60),
	}
}

// fptr / iptr 指针构造助手（模拟 JSON 请求绑定后的指针字段）
func fptr(v float64) *float64 { return &v }
func iptr(v int) *int         { return &v }

func TestSensorController_CreateGetList(t *testing.T) {
	env := newSensorTestEnv(t)
	owner := env.seedDevice(t, "abc123")

	// 创建
	w := doSensor(t, "abc123", "", "", owner, validCreateBody("temperature"), env.ctl.CreateSensor)
	resp := parseResp(w)
	assert.Equal(t, 200, resp.Code)

	var created entity.Sensor
	assert.NoError(t, json.Unmarshal(dataJSON(t, resp), &created))
	assert.Equal(t, "temperature", created.ID)
	assert.True(t, created.Enabled)

	// 查询单个
	w = doSensor(t, "abc123", "temperature", "", owner, nil, env.ctl.GetSensor)
	resp = parseResp(w)
	assert.Equal(t, 200, resp.Code)

	// 列表
	w = doSensor(t, "abc123", "", "", owner, nil, env.ctl.ListSensors)
	resp = parseResp(w)
	assert.Equal(t, 200, resp.Code)
	var list []entity.Sensor
	assert.NoError(t, json.Unmarshal(dataJSON(t, resp), &list))
	assert.Len(t, list, 1)
}

func TestSensorController_CreateBadRequest(t *testing.T) {
	env := newSensorTestEnv(t)
	owner := env.seedDevice(t, "abc123")

	// 非法标识符（数字开头）
	w := doSensor(t, "abc123", "", "", owner, validCreateBody("9Bad_ID"), env.ctl.CreateSensor)
	assert.Equal(t, 400, parseResp(w).Code)

	// 大写标识符合法（规则已放开大小写）
	w = doSensor(t, "abc123", "", "", owner, validCreateBody("Bad_ID"), env.ctl.CreateSensor)
	assert.Equal(t, 200, parseResp(w).Code)

	// 重复标识符
	w = doSensor(t, "abc123", "", "", owner, validCreateBody("temperature"), env.ctl.CreateSensor)
	assert.Equal(t, 200, parseResp(w).Code)
	w = doSensor(t, "abc123", "", "", owner, validCreateBody("temperature"), env.ctl.CreateSensor)
	assert.Equal(t, 400, parseResp(w).Code)
}

func TestSensorController_GetNotExist(t *testing.T) {
	env := newSensorTestEnv(t)
	owner := env.seedDevice(t, "abc123")

	w := doSensor(t, "abc123", "nope", "", owner, nil, env.ctl.GetSensor)
	assert.Equal(t, 404, parseResp(w).Code)
}

func TestSensorController_UpdateAndDelete(t *testing.T) {
	env := newSensorTestEnv(t)
	owner := env.seedDevice(t, "abc123")

	w := doSensor(t, "abc123", "", "", owner, validCreateBody("temperature"), env.ctl.CreateSensor)
	assert.Equal(t, 200, parseResp(w).Code)

	// 增量更新：仅改名称
	newName := "环境温度"
	w = doSensor(t, "abc123", "temperature", "update", owner, map[string]any{"name": newName}, env.ctl.UpdateSensor)
	resp := parseResp(w)
	assert.Equal(t, 200, resp.Code)
	var updated entity.Sensor
	assert.NoError(t, json.Unmarshal(dataJSON(t, resp), &updated))
	assert.Equal(t, newName, updated.Name)
	assert.Equal(t, "°C", updated.Unit) // 未传字段保持原值

	// 更新不存在的传感器
	w = doSensor(t, "abc123", "nope", "update", owner, map[string]any{"name": "x"}, env.ctl.UpdateSensor)
	assert.Equal(t, 404, parseResp(w).Code)

	// 删除
	w = doSensor(t, "abc123", "temperature", "delete", owner, nil, env.ctl.DeleteSensor)
	assert.Equal(t, 200, parseResp(w).Code)

	// 重复删除
	w = doSensor(t, "abc123", "temperature", "delete", owner, nil, env.ctl.DeleteSensor)
	assert.Equal(t, 404, parseResp(w).Code)
}

func TestSensorController_Forbidden(t *testing.T) {
	env := newSensorTestEnv(t)
	env.seedDevice(t, "abc123")
	otherOwner := env.seedDevice(t, "zzz999")

	w := doSensor(t, "abc123", "", "", otherOwner, validCreateBody("temperature"), env.ctl.CreateSensor)
	assert.Equal(t, 403, parseResp(w).Code)
}

func TestSensorController_DeviceNotFound(t *testing.T) {
	env := newSensorTestEnv(t)

	w := doSensor(t, "ghost0", "", "", 1, validCreateBody("temperature"), env.ctl.CreateSensor)
	assert.Equal(t, 404, parseResp(w).Code)
}

func TestSensorController_Apply(t *testing.T) {
	env := newSensorTestEnv(t)
	owner := env.seedDevice(t, "abc123")

	w := doSensor(t, "abc123", "", "", owner, validCreateBody("temperature"), env.ctl.CreateSensor)
	assert.Equal(t, 200, parseResp(w).Code)
	w = doSensor(t, "abc123", "", "", owner, entity.SensorCreateRequest{ID: "humidity", Name: "湿度", Type: "humidity"}, env.ctl.CreateSensor)
	assert.Equal(t, 200, parseResp(w).Code)

	// 下发
	w = doSensor(t, "abc123", "", "apply", owner, nil, env.ctl.ApplySensors)
	resp := parseResp(w)
	assert.Equal(t, 200, resp.Code)
	var applyResp entity.SensorApplyResponse
	assert.NoError(t, json.Unmarshal(dataJSON(t, resp), &applyResp))
	assert.Equal(t, uint(1), applyResp.Version)
	assert.Equal(t, 2, applyResp.Count)
	assert.Equal(t, "pending", applyResp.Status)

	// Redis 命令队列中存在携带 sensors 分区的 config 命令
	vals, err := env.mr.List(cmdQueueKey("abc123"))
	assert.NoError(t, err)
	assert.Len(t, vals, 1)
	assert.Contains(t, vals[0], `"type":"config"`)
	assert.Contains(t, vals[0], `"sensors"`)
	assert.Contains(t, vals[0], `"temperature"`)
}

// TestConfigController_GetByDeviceToken 设备本人经 GET /config 拉取期望配置（双认证）
func TestConfigController_GetByDeviceToken(t *testing.T) {
	env := newSensorTestEnv(t)
	owner := env.seedDevice(t, "abc123")

	// 先由属主保存配置
	w := doConfig(t, env.cfgCtl, http.MethodPost, "abc123", owner, entity.DeviceConfigSaveRequest{
		Config: map[string]any{"sensor": map[string]any{"reportInterval": 60}},
	}, env.cfgCtl.SaveConfig)
	assert.Equal(t, 200, parseResp(w).Code)

	gin.SetMode(gin.TestMode)

	// 设备本人：authType=device 且 Token 对应设备一致 → 200
	w = httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/devices/abc123/config", nil)
	c.Params = gin.Params{{Key: "deviceId", Value: "abc123"}}
	c.Set("authType", "device")
	c.Set("deviceId", "abc123")
	env.cfgCtl.GetConfig(c)
	assert.Equal(t, 200, parseResp(w).Code)

	// Token 与路径设备不一致 → 403
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/devices/abc123/config", nil)
	c.Params = gin.Params{{Key: "deviceId", Value: "abc123"}}
	c.Set("authType", "device")
	c.Set("deviceId", "zzz999")
	env.cfgCtl.GetConfig(c)
	assert.Equal(t, 403, parseResp(w).Code)
}
