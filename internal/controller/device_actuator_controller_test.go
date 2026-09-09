package controller

import (
	"bytes"
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

// actuatorTestEnv 组装真实 sqlite + miniredis 的执行器控制器依赖
type actuatorTestEnv struct {
	ctl    *DeviceActuatorController
	client *ent.Client
	mr     *miniredis.Miniredis
}

func newActuatorTestEnv(t *testing.T) *actuatorTestEnv {
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
	actuatorRepo := repository.NewDeviceActuatorRepo(client)
	cmdRepo := repository.NewDownlinkCmdRepo(client)

	rcache := cache.NewRedisCache(rdb)
	deviceSvc := service.NewDeviceService(deviceRepo, rcache, nil, actuatorRepo, configRepo)
	downlinkSvc := service.NewDownlinkService(cmdRepo, deviceRepo, rdb, nil, nil)
	configSvc := service.NewDeviceConfigService(configRepo, downlinkSvc, nil)
	actuatorSvc := service.NewDeviceActuatorService(actuatorRepo, configSvc, rcache)

	return &actuatorTestEnv{
		ctl:    NewDeviceActuatorController(actuatorSvc, deviceSvc),
		client: client,
		mr:     mr,
	}
}

func (e *actuatorTestEnv) seedDevice(t *testing.T, deviceID string) uint {
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
		ID: deviceID, DeviceName: "设备", OwnerID: owner.ID, Status: "ONLINE",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("种子设备失败: %v", err)
	}
	return owner.ID
}

// doActuator 构造带 userId 与路径参数的 gin.Context 并执行 handler
func doActuator(t *testing.T, deviceID, actuatorID, op string, ownerID uint, body any, handler func(*gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	url := "/api/devices/" + deviceID + "/actuators"
	if actuatorID != "" {
		url += "/" + actuatorID
	}
	if op != "" {
		url += "/" + op
	}
	c.Request = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if body != nil {
		c.Request.Header.Set("Content-Type", "application/json")
	}
	c.Params = gin.Params{{Key: "deviceId", Value: deviceID}}
	if actuatorID != "" {
		c.Params = append(c.Params, gin.Param{Key: "actuatorId", Value: actuatorID})
	}
	c.Set("userId", ownerID)

	handler(c)
	return w
}

func validActuatorBody(id string) entity.ActuatorCreateRequest {
	return entity.ActuatorCreateRequest{
		ID:     id,
		Name:   "舵机",
		Driver: "servo",
		Specs:  map[string]any{"gpio": 18, "min_pulse_us": 500, "max_pulse_us": 2500},
	}
}

func TestActuatorController_CreateGetList(t *testing.T) {
	env := newActuatorTestEnv(t)
	owner := env.seedDevice(t, "abc123")

	w := doActuator(t, "abc123", "", "", owner, validActuatorBody("servo1"), env.ctl.CreateActuator)
	resp := parseResp(w)
	assert.Equal(t, 200, resp.Code)

	var created entity.Actuator
	assert.NoError(t, json.Unmarshal(dataJSON(t, resp), &created))
	assert.Equal(t, "servo1", created.ID)
	assert.True(t, created.Enabled)
	assert.Equal(t, float64(18), created.Specs["gpio"])

	w = doActuator(t, "abc123", "servo1", "", owner, nil, env.ctl.GetActuator)
	assert.Equal(t, 200, parseResp(w).Code)

	w = doActuator(t, "abc123", "", "", owner, nil, env.ctl.ListActuators)
	resp = parseResp(w)
	assert.Equal(t, 200, resp.Code)
	var list []entity.Actuator
	assert.NoError(t, json.Unmarshal(dataJSON(t, resp), &list))
	assert.Len(t, list, 1)
}

func TestActuatorController_CreateBadRequest(t *testing.T) {
	env := newActuatorTestEnv(t)
	owner := env.seedDevice(t, "abc123")

	// 非法标识符
	w := doActuator(t, "abc123", "", "", owner, validActuatorBody("Bad_ID"), env.ctl.CreateActuator)
	assert.Equal(t, 400, parseResp(w).Code)

	// 非法驱动名
	w = doActuator(t, "abc123", "", "", owner, entity.ActuatorCreateRequest{ID: "s1", Driver: "relay"}, env.ctl.CreateActuator)
	assert.Equal(t, 400, parseResp(w).Code)

	// 重复标识符
	w = doActuator(t, "abc123", "", "", owner, validActuatorBody("servo1"), env.ctl.CreateActuator)
	assert.Equal(t, 200, parseResp(w).Code)
	w = doActuator(t, "abc123", "", "", owner, validActuatorBody("servo1"), env.ctl.CreateActuator)
	assert.Equal(t, 400, parseResp(w).Code)
}

func TestActuatorController_GetNotExist(t *testing.T) {
	env := newActuatorTestEnv(t)
	owner := env.seedDevice(t, "abc123")

	w := doActuator(t, "abc123", "nope", "", owner, nil, env.ctl.GetActuator)
	assert.Equal(t, 404, parseResp(w).Code)
}

func TestActuatorController_UpdateAndDelete(t *testing.T) {
	env := newActuatorTestEnv(t)
	owner := env.seedDevice(t, "abc123")

	w := doActuator(t, "abc123", "", "", owner, validActuatorBody("servo1"), env.ctl.CreateActuator)
	assert.Equal(t, 200, parseResp(w).Code)

	// 增量更新：仅改名称与 specs
	newName := "云台舵机"
	w = doActuator(t, "abc123", "servo1", "update", owner, map[string]any{
		"name":  newName,
		"specs": map[string]any{"gpio": 20},
	}, env.ctl.UpdateActuator)
	resp := parseResp(w)
	assert.Equal(t, 200, resp.Code)
	var updated entity.Actuator
	assert.NoError(t, json.Unmarshal(dataJSON(t, resp), &updated))
	assert.Equal(t, newName, updated.Name)
	assert.Equal(t, float64(20), updated.Specs["gpio"])
	assert.Equal(t, "servo", updated.Driver) // 未传字段保持原值

	// 更新不存在的执行器
	w = doActuator(t, "abc123", "nope", "update", owner, map[string]any{"name": "x"}, env.ctl.UpdateActuator)
	assert.Equal(t, 404, parseResp(w).Code)

	// 删除 + 重复删除
	w = doActuator(t, "abc123", "servo1", "delete", owner, nil, env.ctl.DeleteActuator)
	assert.Equal(t, 200, parseResp(w).Code)
	w = doActuator(t, "abc123", "servo1", "delete", owner, nil, env.ctl.DeleteActuator)
	assert.Equal(t, 404, parseResp(w).Code)
}

func TestActuatorController_Forbidden(t *testing.T) {
	env := newActuatorTestEnv(t)
	env.seedDevice(t, "abc123")
	otherOwner := env.seedDevice(t, "zzz999")

	w := doActuator(t, "abc123", "", "", otherOwner, validActuatorBody("servo1"), env.ctl.CreateActuator)
	assert.Equal(t, 403, parseResp(w).Code)
}

func TestActuatorController_DeviceNotFound(t *testing.T) {
	env := newActuatorTestEnv(t)

	w := doActuator(t, "ghost0", "", "", 1, validActuatorBody("servo1"), env.ctl.CreateActuator)
	assert.Equal(t, 404, parseResp(w).Code)
}

func TestActuatorController_Apply(t *testing.T) {
	env := newActuatorTestEnv(t)
	owner := env.seedDevice(t, "abc123")

	w := doActuator(t, "abc123", "", "", owner, validActuatorBody("servo1"), env.ctl.CreateActuator)
	assert.Equal(t, 200, parseResp(w).Code)
	w = doActuator(t, "abc123", "", "", owner, entity.ActuatorCreateRequest{ID: "led1", Name: "灯", Driver: "led"}, env.ctl.CreateActuator)
	assert.Equal(t, 200, parseResp(w).Code)

	// 下发
	w = doActuator(t, "abc123", "", "apply", owner, nil, env.ctl.ApplyActuators)
	resp := parseResp(w)
	assert.Equal(t, 200, resp.Code)
	var applyResp entity.ActuatorApplyResponse
	assert.NoError(t, json.Unmarshal(dataJSON(t, resp), &applyResp))
	assert.Equal(t, uint(1), applyResp.Version)
	assert.Equal(t, 2, applyResp.Count)
	assert.Equal(t, "pending", applyResp.Status)

	// Redis 命令队列中存在携带 actuators 分区的 config 命令
	vals, err := env.mr.List(cmdQueueKey("abc123"))
	assert.NoError(t, err)
	assert.Len(t, vals, 1)
	assert.Contains(t, vals[0], `"type":"config"`)
	assert.Contains(t, vals[0], `"actuators"`)
	assert.Contains(t, vals[0], `"servo1"`)
	assert.Contains(t, vals[0], `"led1"`)
}
