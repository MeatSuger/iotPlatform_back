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

// configTestEnv 组装真实 sqlite + miniredis 的配置控制器依赖
type configTestEnv struct {
	ctl    *DeviceConfigController
	client *ent.Client
	mr     *miniredis.Miniredis
}

func newConfigTestEnv(t *testing.T) *configTestEnv {
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
	cmdRepo := repository.NewMessageLogRepo(client)

	rcache := cache.NewRedisCache(rdb)
	deviceSvc := service.NewDeviceService(deviceRepo, rcache, nil, configRepo)
	downlinkSvc := service.NewDownlinkService(cmdRepo, deviceRepo, rdb, nil, nil)
	configSvc := service.NewDeviceConfigService(configRepo, downlinkSvc, nil)

	return &configTestEnv{
		ctl:    NewDeviceConfigController(configSvc, deviceSvc),
		client: client,
		mr:     mr,
	}
}

func (e *configTestEnv) seedDevice(t *testing.T, deviceID string) uint {
	t.Helper()
	return seedOwnedDevice(t, e.client, deviceID, "设备")
}

// doConfig 构造带 userId 与路径参数的 gin.Context 并执行 handler
func doConfig(t *testing.T, ctl *DeviceConfigController, method, deviceID string, ownerID uint, body any, handler func(*gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	c.Request = httptest.NewRequest(method, "/api/devices/"+deviceID+"/config", bytes.NewReader(b))
	if body != nil {
		c.Request.Header.Set("Content-Type", "application/json")
	}
	c.Params = gin.Params{{Key: "deviceId", Value: deviceID}}
	c.Set("userId", ownerID)

	handler(c)
	return w
}

func TestConfigController_GetNotConfigured(t *testing.T) {
	env := newConfigTestEnv(t)
	owner := env.seedDevice(t, "abc123")

	w := doConfig(t, env.ctl, http.MethodGet, "abc123", owner, nil, env.ctl.GetConfig)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Version uint `json:"version"`
		} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 200, resp.Code)
	assert.Equal(t, uint(0), resp.Data.Version)
}

func TestConfigController_SaveAndGet(t *testing.T) {
	env := newConfigTestEnv(t)
	owner := env.seedDevice(t, "abc123")

	// 保存配置
	w := doConfig(t, env.ctl, http.MethodPost, "abc123", owner, entity.DeviceConfigSaveRequest{
		Config: map[string]any{"sensor": map[string]any{"reportInterval": 60}},
	}, env.ctl.SaveConfig)
	assert.Equal(t, http.StatusOK, w.Code)

	var saveResp struct {
		Code int `json:"code"`
		Data struct {
			Version uint   `json:"version"`
			Status  string `json:"status"`
		} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &saveResp))
	assert.Equal(t, 200, saveResp.Code)
	assert.Equal(t, uint(1), saveResp.Data.Version)
	assert.Equal(t, "pending", saveResp.Data.Status)

	// 队列中已下发 config 命令
	vals, err := env.mr.List(cmdQueueKey("abc123"))
	assert.NoError(t, err)
	assert.Len(t, vals, 1)
	assert.Contains(t, vals[0], `"type":"config"`)

	// 查询配置
	w2 := doConfig(t, env.ctl, http.MethodGet, "abc123", owner, nil, env.ctl.GetConfig)
	var getResp struct {
		Code int `json:"code"`
		Data struct {
			Version uint   `json:"version"`
			Status  string `json:"status"`
		} `json:"data"`
	}
	assert.NoError(t, json.Unmarshal(w2.Body.Bytes(), &getResp))
	assert.Equal(t, uint(1), getResp.Data.Version)
	assert.Equal(t, "pending", getResp.Data.Status)
}

func TestConfigController_SaveForbidden(t *testing.T) {
	env := newConfigTestEnv(t)
	env.seedDevice(t, "abc123")
	// 非 owner
	otherOwner := env.seedDevice(t, "zzz999")

	w := doConfig(t, env.ctl, http.MethodPost, "abc123", otherOwner, entity.DeviceConfigSaveRequest{
		Config: map[string]any{"a": 1},
	}, env.ctl.SaveConfig)

	var resp struct {
		Code int `json:"code"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 403, resp.Code)
}

func TestConfigController_SaveDeviceNotFound(t *testing.T) {
	env := newConfigTestEnv(t)
	// 设备不存在（owner 任意）
	w := doConfig(t, env.ctl, http.MethodPost, "ghost0", 1, entity.DeviceConfigSaveRequest{
		Config: map[string]any{"a": 1},
	}, env.ctl.SaveConfig)

	var resp struct {
		Code int `json:"code"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 404, resp.Code)
}

func TestConfigController_Report(t *testing.T) {
	env := newConfigTestEnv(t)
	owner := env.seedDevice(t, "abc123")

	// 先保存
	doConfig(t, env.ctl, http.MethodPost, "abc123", owner, entity.DeviceConfigSaveRequest{
		Config: map[string]any{"actuator": map[string]any{"mode": "auto"}},
	}, env.ctl.SaveConfig)

	// 设备回执
	w := doConfig(t, env.ctl, http.MethodPost, "abc123", 0, entity.DeviceConfigReport{
		Version: 1,
		Config:  map[string]any{"actuator": map[string]any{"mode": "auto"}},
	}, env.ctl.ReportConfig)

	var resp struct {
		Code int `json:"code"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 200, resp.Code)

	// 状态已回写 acked
	got, err := repository.NewDeviceConfigRepo(env.client).GetByDeviceID(context.Background(), "abc123")
	assert.NoError(t, err)
	assert.Equal(t, "acked", got.Status)
	assert.Equal(t, uint(1), got.ReportedVersion)
}

// cmdQueueKey 复用 service 包不可见的命令队列前缀
func cmdQueueKey(deviceID string) string {
	return "cmd:queue:" + deviceID
}
