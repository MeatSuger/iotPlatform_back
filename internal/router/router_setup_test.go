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

package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	sagin "github.com/sa-tokens/sa-token-go/integrations/gin"
	"github.com/sa-tokens/sa-token-go/storage/memory"
	"github.com/stretchr/testify/assert"

	"iot-platform.local/internal/middleware"
	"iot-platform.local/pkg/common"
	"iot-platform.local/pkg/config"
)

// ========================================
// 全局初始化（内存存储，无需外部依赖）
// ========================================

var testUserMgr *sagin.Manager

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)

	// 初始化最小配置（路由注册所需）
	config.Cfg = &config.Config{}
	config.Cfg.Server.Mode = "test"
	config.Cfg.Server.SwaggerEnabled = false
	config.Cfg.CORS.AllowedOrigins = []string{}

	// 用户 Manager
	userStorage := memory.NewStorage()
	userCfg := sagin.DefaultConfig()
	userCfg.TokenName = "Authorization"
	userCfg.KeyPrefix = "Authorization:"
	userCfg.Timeout = 2592000
	userCfg.IsLog = false
	userCfg.TokenStyle = sagin.TokenStyleUUID
	testUserMgr = sagin.NewManager(userStorage, userCfg)
	sagin.SetManager(testUserMgr)

	// 设备 Manager
	deviceStorage := memory.NewStorage()
	deviceCfg := sagin.DefaultConfig()
	deviceCfg.TokenName = "X-Device-Token"
	deviceCfg.KeyPrefix = "X-Device-Token:"
	deviceCfg.Timeout = -1
	deviceCfg.IsLog = false
	deviceCfg.TokenStyle = sagin.TokenStyleUUID
	deviceMgr := sagin.NewManager(deviceStorage, deviceCfg)
	middleware.SetDeviceManager(deviceMgr)

	os.Exit(m.Run())
}

// ========================================
// 测试辅助
// ========================================

// newTestEngine 通过真实 Setup() 构建路由引擎（nil 服务依赖，仅验证路由注册）
func newTestEngine() *gin.Engine {
	userPlugin := sagin.NewPlugin(testUserMgr)
	svcs := &Services{}
	return Setup(svcs, nil, userPlugin, nil, nil)
}

func doReq(method, path string, body any) *httptest.ResponseRecorder {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	newTestEngine().ServeHTTP(w, req)
	return w
}

// ========================================
// 新路由表（Google AIP 风格，仅 GET/POST）— 必须全部存在
// 无 token 时预期 401/400/500 业务响应（HTTP 200 + body code），
// 只要 HTTP 状态不是 404 即证明路由存在
// ========================================

func TestNewRoutes_Exist(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		// ===== User 资源 =====
		{"Create 注册", http.MethodPost, "/api/users"},
		{"Login 登录", http.MethodPost, "/api/users/login"},
		{"IsLogin 会话检查", http.MethodGet, "/api/users/isLogin"},
		{"Logout 退出", http.MethodPost, "/api/users/logout"},
		{"List 用户列表", http.MethodGet, "/api/users"},
		{"Get 详情(me)", http.MethodGet, "/api/users/me"},
		{"Get 详情(数字ID)", http.MethodGet, "/api/users/123"},
		{"Update 更新", http.MethodPost, "/api/users/123/update"},
		{"Delete 删除", http.MethodPost, "/api/users/123/delete"},
		// ===== Device 资源 =====
		{"Register 设备注册", http.MethodPost, "/api/devices"},
		{"List 设备列表", http.MethodGet, "/api/devices"},
		{"Get 设备详情", http.MethodGet, "/api/devices/abc123"},
		{"Update 设备更新", http.MethodPost, "/api/devices/abc123/update"},
		{"Delete 设备删除", http.MethodPost, "/api/devices/abc123/delete"},
		{"GetToken 设备Token", http.MethodGet, "/api/devices/abc123/token"},
		{"GetToken login别名", http.MethodGet, "/api/devices/abc123/login"},
		// ===== SensorData 子资源 =====
		{"Report 数据上报", http.MethodPost, "/api/devices/abc123/sensorData"},
		{"Query 数据查询", http.MethodGet, "/api/devices/abc123/sensorData"},
		{"Heartbeat 心跳", http.MethodPost, "/api/devices/abc123/heartbeat"},
		{"Heartbeat ping别名", http.MethodPost, "/api/devices/abc123/ping"},
		// ===== DownlinkCmd 子资源 =====
		{"PostCmd 下发命令", http.MethodPost, "/api/devices/abc123/commands"},
		{"GetCmd 拉取命令", http.MethodGet, "/api/devices/abc123/commands"},
	}

	for _, tt := range tests {
		t.Run(tt.name+" "+tt.method+" "+tt.path, func(t *testing.T) {
			w := doReq(tt.method, tt.path, nil)
			assert.NotEqual(t, http.StatusNotFound, w.Code,
				"路由 %s %s 应存在（不应 404），实际 %d: %s", tt.method, tt.path, w.Code, w.Body.String())
		})
	}
}

// ========================================
// 旧路由必须全部失效（404）
// ========================================

func TestOldRoutes_Gone(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		// ===== 旧 user 路由 =====
		{"旧注册", http.MethodPost, "/api/user/register"},
		{"旧登录", http.MethodPost, "/api/user/login"},
		{"旧会话检查", http.MethodGet, "/api/user/isLogin"},
		{"旧退出", http.MethodPost, "/api/user/logout"},
		{"旧更新(PUT)", http.MethodPut, "/api/user"},
		{"旧详情", http.MethodGet, "/api/user/profile"},
		{"旧列表", http.MethodGet, "/api/user/list"},
		{"旧分页", http.MethodGet, "/api/user/page"},
		{"旧删除", http.MethodPost, "/api/user/delete"},
		// ===== 旧 device 路由 =====
		{"旧设备注册", http.MethodPost, "/api/device/register"},
		{"旧设备列表", http.MethodGet, "/api/device/list"},
		{"旧设备详情", http.MethodGet, "/api/device/abc123/Data"},
		{"旧设备更新", http.MethodPost, "/api/device/abc123/update"},
		{"旧设备删除", http.MethodPost, "/api/device/abc123/delete"},
		{"旧设备login", http.MethodGet, "/api/device/abc123/login"},
		{"旧设备token", http.MethodGet, "/api/device/abc123/token"},
		{"旧命令下发", http.MethodPost, "/api/device/abc123/cmd"},
		{"旧命令拉取", http.MethodGet, "/api/device/abc123/cmd"},
		// ===== 旧 data 路由 =====
		{"旧数据上报", http.MethodPost, "/api/data/abc123/Data"},
		{"旧心跳ping", http.MethodPost, "/api/data/abc123/ping"},
		{"旧心跳heartbeat", http.MethodPost, "/api/data/abc123/heartbeat"},
		{"旧数据查询", http.MethodGet, "/api/data/abc123/Data/list"},
		{"旧数据列表(死扩展点)", http.MethodGet, "/api/data/list"},
	}

	for _, tt := range tests {
		t.Run(tt.name+" "+tt.method+" "+tt.path, func(t *testing.T) {
			w := doReq(tt.method, tt.path, nil)
			assert.Equal(t, http.StatusNotFound, w.Code,
				"旧路由 %s %s 应已移除（应 404），实际 %d: %s", tt.method, tt.path, w.Code, w.Body.String())
		})
	}
}

// ========================================
// 路由语义断言
// ========================================

func TestRouteSemantics(t *testing.T) {
	t.Run("GET /api/users/isLogin 命中 IsLogin 而非 :userId 参数路由", func(t *testing.T) {
		w := doReq(http.MethodGet, "/api/users/isLogin", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "isLogin")
	})

	t.Run("GET /api/users/abc 命中 :userId 参数路由（无 token 被认证拦截）", func(t *testing.T) {
		w := doReq(http.MethodGet, "/api/users/abc", nil)
		assert.NotEqual(t, http.StatusNotFound, w.Code)
		var body struct {
			Code int `json:"code"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		assert.True(t, body.Code == common.CodeUnauthorized || body.Code == common.CodeBadRequest,
			"业务码应为 401 或 400，实际 %d: %s", body.Code, w.Body.String())
	})

	t.Run("GET /api/devices/zzzzzz 非法设备ID被 deviceIdAuth 拦截为 400", func(t *testing.T) {
		w := doReq(http.MethodGet, "/api/devices/zzzzzz", nil)
		assert.NotEqual(t, http.StatusNotFound, w.Code)
		var body struct {
			Code int `json:"code"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		assert.True(t, body.Code == common.CodeBadRequest || body.Code == common.CodeUnauthorized,
			"业务码应为 400 或 401，实际 %d: %s", body.Code, w.Body.String())
	})
}

// ========================================
// /health 依赖探测：HTTP 状态码仅表达后端是否正常
// ========================================

func buildHealthEngine(probes *HealthProbe) *gin.Engine {
	gin.SetMode(gin.TestMode)
	userPlugin := sagin.NewPlugin(testUserMgr)
	return Setup(&Services{}, nil, userPlugin, nil, probes)
}

func doHealth(t *testing.T, probes *HealthProbe) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	buildHealthEngine(probes).ServeHTTP(w, req)
	return w
}

func TestHealth_AllUp(t *testing.T) {
	probes := &HealthProbe{
		PostgreSQL: func(ctx context.Context) error { return nil },
		Redis:      func(ctx context.Context) error { return nil },
		Influx:     func(ctx context.Context) error { return nil },
	}
	w := doHealth(t, probes)
	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
	deps := body["dependencies"].(map[string]any)
	assert.Equal(t, "ok", deps["postgresql"])
	assert.Equal(t, "ok", deps["redis"])
	assert.Equal(t, "ok", deps["influxdb"])
}

func TestHealth_DependencyDown(t *testing.T) {
	probes := &HealthProbe{
		PostgreSQL: func(ctx context.Context) error { return nil },
		Redis: func(ctx context.Context) error {
			return errors.New("redis connection refused")
		},
		Influx: func(ctx context.Context) error { return nil },
	}
	w := doHealth(t, probes)
	// HTTP 非 200 仅用于表达"后端不正常"
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var body map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "error", body["status"])
	deps := body["dependencies"].(map[string]any)
	assert.Contains(t, deps["redis"].(string), "connection refused")
	assert.Equal(t, "ok", deps["postgresql"])
}

func TestHealth_NilProbes(t *testing.T) {
	// nil 探针 → 保持旧行为：恒 200，无 dependencies 字段
	w := doHealth(t, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
	_, hasDeps := body["dependencies"]
	assert.False(t, hasDeps)
}

func TestHealth_PartialProbes(t *testing.T) {
	// 仅配置 Redis 探针：其余依赖不参与探测
	probes := &HealthProbe{
		Redis: func(ctx context.Context) error { return nil },
	}
	w := doHealth(t, probes)
	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	deps := body["dependencies"].(map[string]any)
	assert.Equal(t, "ok", deps["redis"])
	_, hasPG := deps["postgresql"]
	assert.False(t, hasPG)
}
