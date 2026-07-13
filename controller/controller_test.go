package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	sagin "github.com/sa-tokens/sa-token-go/integrations/gin"
	"github.com/sa-tokens/sa-token-go/storage/memory"
	"github.com/sa-tokens/sa-token-go/stputil"
	"github.com/stretchr/testify/assert"

	"github.com/yu/iot-platform-go/common"
	"github.com/yu/iot-platform-go/middleware"
	"github.com/yu/iot-platform-go/service"
)

// ========================================
// 全局 sa-token-go 初始化（内存存储，无需外部依赖）
// ========================================

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)

	// 用户 Manager
	userStorage := memory.NewStorage()
	userCfg := sagin.DefaultConfig()
	userCfg.TokenName = "Authorization"
	userCfg.KeyPrefix = "Authorization:"
	userCfg.Timeout = 2592000
	userCfg.IsLog = false
	userCfg.TokenStyle = sagin.TokenStyleUUID
	userMgr := sagin.NewManager(userStorage, userCfg)
	sagin.SetManager(userMgr)

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

type testResp struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

func newJSONReq(method, path string, body interface{}, headers map[string]string) *http.Request {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func parseResp(w *httptest.ResponseRecorder) testResp {
	var r testResp
	json.Unmarshal(w.Body.Bytes(), &r)
	return r
}

// ========================================
// 1. 用户认证中间件 — 核心安全组件
// ========================================

func TestUserAuthMiddleware_RejectsUnauthenticated(t *testing.T) {
	auth := middleware.AuthMiddleware()

	tests := []struct {
		name   string
		header string
	}{
		{"无 Authorization 头", ""},
		{"无效 token", "Bearer invalid-token-xxx"},
		{"空 Bearer 头", "Bearer "},
		{"原始 token 不带 Bearer 前缀", "fake-token-without-bearer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(auth)
			r.GET("/protected", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/protected", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			r.ServeHTTP(w, req)
			resp := parseResp(w)
			assert.Equal(t, common.CodeUnauthorized, resp.Code, "应返回 %d, 实际 %d: %s", common.CodeUnauthorized, resp.Code, resp.Message)
		})
	}
}

// ========================================
// 2. 用户认证中间件 — 有效登录流程
// ========================================

func TestUserAuthMiddleware_ValidLoginFlow(t *testing.T) {
	loginID := "1"
	token, err := stputil.Login(loginID)
	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	err = stputil.SetRoles(loginID, []string{"admin"})
	assert.NoError(t, err)
	assert.True(t, stputil.HasRole(loginID, "admin"))

	// 验证 token 有效
	assert.True(t, stputil.IsLogin(token))

	// 验证 loginID 可获取
	retrievedID, err := stputil.GetLoginID(token)
	assert.NoError(t, err)
	assert.Equal(t, loginID, retrievedID)

	// 验证角色
	roles, err := stputil.GetRoles(loginID)
	assert.NoError(t, err)
	assert.Contains(t, roles, "admin")
}

// ========================================
// 3. 设备认证中间件 — 拒绝未授权请求
// ========================================

func TestDeviceAuthMiddleware_RejectsUnauthenticated(t *testing.T) {
	deviceAuth := middleware.DeviceAuthMiddleware()

	tests := []struct {
		name     string
		setupReq func(r *http.Request)
		errMsg   string
	}{
		{
			name:     "无 X-Device-Token",
			setupReq: func(r *http.Request) {},
			errMsg:   "缺少设备Token",
		},
		{
			name:     "无效设备 Token",
			setupReq: func(r *http.Request) { r.Header.Set("X-Device-Token", "bad-token") },
			errMsg:   "设备Token无效",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(deviceAuth)
			r.POST("/data", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/data", nil)
			tt.setupReq(req)
			r.ServeHTTP(w, req)
			resp := parseResp(w)
			assert.Equal(t, common.CodeUnauthorized, resp.Code)
			assert.Contains(t, resp.Message, tt.errMsg)
		})
	}
}

// ========================================
// 4. 设备认证中间件 — Cookie 提取
// ========================================

func TestDeviceAuthMiddleware_TokenFromCookie(t *testing.T) {
	deviceAuth := middleware.DeviceAuthMiddleware()

	t.Run("从 Cookie 读取 X-Device-Token", func(t *testing.T) {
		r := gin.New()
		r.Use(deviceAuth)
		r.POST("/data", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/data", nil)
		// 使用 Cookie 而非 Header（浏览器自动携带场景）
		req.AddCookie(&http.Cookie{Name: "X-Device-Token", Value: "test-cookie-token"})
		r.ServeHTTP(w, req)
		resp := parseResp(w)
		assert.Equal(t, common.CodeUnauthorized, resp.Code)
		// Cookie 中的值被提取了，但 token 无效 → 返回"设备Token无效"
		assert.Contains(t, resp.Message, "设备Token无效")
	})
}

// ========================================
// 5. 设备认证中间件 — 有效设备登录流程
// ========================================

func TestDeviceAuthMiddleware_ValidLoginFlow(t *testing.T) {
	deviceMgr := middleware.GetDeviceManager()
	assert.NotNil(t, deviceMgr)

	// 设备登录
	token, err := deviceMgr.Login("abc123", "device")
	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	// 验证 token 有效性
	assert.True(t, deviceMgr.IsLogin(token))

	// 验证 loginID
	loginID, err := deviceMgr.GetLoginID(token)
	assert.NoError(t, err)
	assert.Equal(t, "abc123", loginID)
}

// ========================================
// 6. CheckRole 角色中间件
// ========================================

func TestCheckRoleMiddleware(t *testing.T) {
	t.Run("无 token 返回 403", func(t *testing.T) {
		r := gin.New()
		r.Use(middleware.CheckRole("admin"))
		r.GET("/admin", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/admin", nil)
		r.ServeHTTP(w, req)
		resp := parseResp(w)
		assert.Equal(t, common.CodeForbidden, resp.Code)
	})
}

// ========================================
// 7. RequireLogin 中间件
// ========================================

func TestRequireLoginMiddleware(t *testing.T) {
	t.Run("无 token 返回 401", func(t *testing.T) {
		r := gin.New()
		r.Use(middleware.RequireLogin())
		r.GET("/secure", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/secure", nil)
		r.ServeHTTP(w, req)
		resp := parseResp(w)
		assert.Equal(t, common.CodeUnauthorized, resp.Code)
	})
}

// ========================================
// 8. IsLogin 控制器 — 公开接口
// ========================================

func TestIsLogin_Controller(t *testing.T) {
	t.Run("无 token 返回未登录", func(t *testing.T) {
		r := gin.New()
		ctl := &UserController{}
		r.GET("/isLogin", ctl.IsLogin)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/isLogin", nil)
		r.ServeHTTP(w, req)
		resp := parseResp(w)
		assert.Equal(t, 200, resp.Code)
		assert.Contains(t, resp.Message, "未登录")
	})
}

// ========================================
// 9. Logout 控制器 — 需要认证
// ========================================

func TestLogout_Controller(t *testing.T) {
	auth := middleware.AuthMiddleware()

	t.Run("无 token 返回 401", func(t *testing.T) {
		r := gin.New()
		ctl := &UserController{}
		r.POST("/logout", auth, ctl.Logout)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/logout", nil)
		r.ServeHTTP(w, req)
		resp := parseResp(w)
		assert.Equal(t, common.CodeUnauthorized, resp.Code)
	})

	t.Run("有效 token 登出成功", func(t *testing.T) {
		// 先登录获取 token
		token, err := stputil.Login("2")
		assert.NoError(t, err)

		// 模拟 TokenInterceptor 注入 context
		r := gin.New()
		ctl := &UserController{}
		r.Use(func(c *gin.Context) {
			c.Set("satoken_token", token)
			c.Next()
		})
		r.POST("/logout", auth, ctl.Logout)

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/logout", nil)
		r.ServeHTTP(w, req)
		resp := parseResp(w)
		assert.Equal(t, 200, resp.Code)
		assert.Contains(t, resp.Message, "退出登录成功")

		// 验证 Set-Cookie 清除 Authorization
		cookies := w.Result().Cookies()
		var found bool
		for _, c := range cookies {
			if c.Name == "Authorization" && c.MaxAge < 0 {
				found = true
			}
		}
		assert.True(t, found, "登出应清除 Authorization Cookie")
	})
}

// ========================================
// 10. 统一响应格式
// ========================================

func TestApiResponse_Format(t *testing.T) {
	t.Run("Success 响应格式", func(t *testing.T) {
		r := gin.New()
		r.GET("/test", func(c *gin.Context) { common.Success(c, gin.H{"key": "value"}) })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/test", nil))
		var resp testResp
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, common.CodeSuccess, resp.Code)
		assert.Equal(t, "success", resp.Message)
		assert.NotNil(t, resp.Data)
	})

	t.Run("Fail 带自定义消息", func(t *testing.T) {
		r := gin.New()
		r.GET("/test", func(c *gin.Context) {
			common.FailWithMsg(c, common.CodeBadRequest, "参数错误")
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/test", nil))
		var resp testResp
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, common.CodeBadRequest, resp.Code)
		assert.Equal(t, "参数错误", resp.Message)
	})

	t.Run("Fail 默认消息", func(t *testing.T) {
		r := gin.New()
		r.GET("/test", func(c *gin.Context) { common.Fail(c, common.CodeUnauthorized) })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/test", nil))
		var resp testResp
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, common.CodeUnauthorized, resp.Code)
		assert.NotEmpty(t, resp.Message)
	})
}

// ========================================
// 11. LoginResponse JSON 序列化
// ========================================

func TestLoginResponse_Serialization(t *testing.T) {
	resp := service.LoginResponse{
		TokenName:            "Authorization",
		TokenValue:           "test-token-value",
		IsLogin:              true,
		LoginID:              "1",
		LoginType:            "login",
		TokenTimeout:         2592000,
		SessionTimeout:       2592000,
		TokenSessionTimeout:  -2,
		TokenActivityTimeout: -1,
		LoginDevice:          "default-device",
	}

	b, err := json.Marshal(resp)
	assert.NoError(t, err)

	var m map[string]interface{}
	err = json.Unmarshal(b, &m)
	assert.NoError(t, err)

	assert.Equal(t, "Authorization", m["tokenName"])
	assert.Equal(t, "test-token-value", m["tokenValue"])
	assert.Equal(t, true, m["isLogin"])
	assert.Equal(t, "1", m["loginId"])
	assert.Equal(t, "login", m["loginType"])
	assert.Equal(t, float64(2592000), m["tokenTimeout"])
	assert.Equal(t, float64(2592000), m["sessionTimeout"])
	assert.Equal(t, float64(-2), m["tokenSessionTimeout"])
	assert.Equal(t, float64(-1), m["tokenActivityTimeout"])
	assert.Equal(t, "default-device", m["loginDevice"])
}

// ========================================
// 12. 数据上报请求体结构
// ========================================

func TestDataReport_RequestBody(t *testing.T) {
	body := map[string]interface{}{
		"sensors": []map[string]interface{}{
			{"name": "temperature", "type": "number", "value": 26.5},
			{"name": "humidity", "type": "number", "value": 65.0},
			{"name": "co2", "type": "CO2-SENSOR", "value": "400ppm"},
		},
	}

	b, err := json.Marshal(body)
	assert.NoError(t, err)

	// 验证反序列化
	var parsed map[string]interface{}
	err = json.Unmarshal(b, &parsed)
	assert.NoError(t, err)

	sensors, ok := parsed["sensors"].([]interface{})
	assert.True(t, ok)
	assert.Len(t, sensors, 3)

	// 验证每个传感器格式
	for _, s := range sensors {
		sensor, ok := s.(map[string]interface{})
		assert.True(t, ok)
		assert.Contains(t, sensor, "name")
		assert.Contains(t, sensor, "type")
		assert.Contains(t, sensor, "value")
	}
}

// ========================================
// 13. Register 请求验证
// ========================================

func TestRegister_RequestValidation(t *testing.T) {
	ctl := &UserController{}

	t.Run("空 body 返回 400", func(t *testing.T) {
		r := gin.New()
		r.POST("/register", ctl.Register)
		w := httptest.NewRecorder()
		req := newJSONReq("POST", "/register", map[string]string{}, nil)
		r.ServeHTTP(w, req)
		resp := parseResp(w)
		assert.Equal(t, common.CodeBadRequest, resp.Code)
		assert.Contains(t, resp.Message, "Account")
	})
}

// ========================================
// 14. Login 请求验证（空 body → 返回 400，不会 panic）
// ========================================

func TestLogin_RequestValidation(t *testing.T) {
	ctl := &UserController{}

	t.Run("空 body 返回 400", func(t *testing.T) {
		r := gin.New()
		r.POST("/login", ctl.Login)
		w := httptest.NewRecorder()
		req := newJSONReq("POST", "/login", nil, nil)
		r.ServeHTTP(w, req)
		resp := parseResp(w)
		assert.Equal(t, common.CodeBadRequest, resp.Code)
		assert.Contains(t, resp.Message, "账号和密码不能为空")
	})

	t.Run("空 JSON object 返回 400", func(t *testing.T) {
		r := gin.New()
		r.POST("/login", ctl.Login)
		w := httptest.NewRecorder()
		req := newJSONReq("POST", "/login", map[string]string{}, nil)
		r.ServeHTTP(w, req)
		resp := parseResp(w)
		assert.Equal(t, common.CodeBadRequest, resp.Code)
		assert.Contains(t, resp.Message, "账号和密码不能为空")
	})
}

// ========================================
// 15. DeviceToken 提取 — Header 优先级
// ========================================

func TestDeviceToken_ExtractionPriority(t *testing.T) {
	deviceAuth := middleware.DeviceAuthMiddleware()

	t.Run("Header 优先于 Cookie", func(t *testing.T) {
		r := gin.New()
		r.Use(deviceAuth)
		r.POST("/data", func(c *gin.Context) {
			c.String(200, "ok")
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/data", nil)
		// 同时设置 Header 和 Cookie — Header 应被使用
		req.Header.Set("X-Device-Token", "header-token")
		req.AddCookie(&http.Cookie{Name: "X-Device-Token", Value: "cookie-token"})
		r.ServeHTTP(w, req)
		resp := parseResp(w)
		assert.Equal(t, common.CodeUnauthorized, resp.Code) // 无论哪个，token 都无效
	})
}

// ========================================
// 16. 设备登录并验证完整流程
// ========================================

func TestDeviceFullFlow(t *testing.T) {
	deviceMgr := middleware.GetDeviceManager()

	// Step 1: 设备登录
	token, err := deviceMgr.Login("d001", "device")
	assert.NoError(t, err)

	// Step 2: 验证 token 有效性
	assert.True(t, deviceMgr.IsLogin(token))

	// Step 3: 获取 loginID
	loginID, err := deviceMgr.GetLoginID(token)
	assert.NoError(t, err)
	assert.Equal(t, "d001", loginID)

	// Step 4: 注销
	err = deviceMgr.LogoutByToken(token)
	assert.NoError(t, err)
	assert.False(t, deviceMgr.IsLogin(token))
}

// ========================================
// 17. 用户登录并验证完整流程
// ========================================

func TestUserFullFlow(t *testing.T) {
	// Step 1: 登录
	token, err := stputil.Login("1001")
	assert.NoError(t, err)
	assert.True(t, stputil.IsLogin(token))

	// Step 2: 设置角色
	err = stputil.SetRoles("1001", []string{"admin"})
	assert.NoError(t, err)
	assert.True(t, stputil.HasRole("1001", "admin"))

	// Step 3: 角色检查
	roles, err := stputil.GetRoles("1001")
	assert.NoError(t, err)
	assert.Contains(t, roles, "admin")

	// Step 4: 退出
	err = stputil.LogoutByToken(token)
	assert.NoError(t, err)
	assert.False(t, stputil.IsLogin(token))
}
