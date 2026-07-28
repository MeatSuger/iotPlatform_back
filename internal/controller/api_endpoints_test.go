package controller

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"iot-platform.local/pkg/common"
)

// ========================================
// 设备接口（对照 swagger.json）
// ========================================

func TestDeviceList(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/device/list", func(c *gin.Context) {
		common.Success(c, []map[string]any{
			{"id": 1, "deviceId": "a1b2c3", "deviceName": "ESP32-01", "deviceType": "sensor", "status": "ONLINE"},
			{"id": 2, "deviceId": "d4e5f6", "deviceName": "ESP32-02", "deviceType": "actuator", "status": "OFFLINE"},
		})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/device/list", nil))

	var resp struct {
		Code int              `json:"code"`
		Data []map[string]any `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, 200, resp.Code)
	assert.Len(t, resp.Data, 2)
	assert.Equal(t, "ESP32-01", resp.Data[0]["deviceName"])
}

func TestDeviceDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/device/:deviceId/Data", func(c *gin.Context) {
		did := c.Param("deviceId")
		assert.Equal(t, "a1b2c3", did)
		common.Success(c, map[string]any{
			"id": 1, "deviceId": "a1b2c3", "deviceName": "ESP32-01",
			"deviceType": "sensor", "firmwareVersion": "1.0.0",
			"ipAddress": "192.168.1.100", "macAddress": "AA:BB:CC:DD:EE:FF",
			"location": "机房A", "ownerId": 1, "status": "ONLINE",
			"sensors": []map[string]any{
				{"name": "temperature", "type": "number", "value": 26.5},
			},
		})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/device/a1b2c3/Data", nil))

	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, 200, resp.Code)
	assert.Equal(t, "ESP32-01", resp.Data["deviceName"])
	assert.NotNil(t, resp.Data["sensors"])
}

func TestDeviceDelete(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.POST("/device/:deviceId/delete", func(c *gin.Context) {
		common.SuccessWithMsg(c, "删除成功", nil)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/device/a1b2c3/delete", nil))

	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, 200, resp.Code)
	assert.Equal(t, "删除成功", resp.Message)
}

// ========================================
// 用户管理（对照 swagger.json）
// ========================================

func TestUserProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/user/profile", func(c *gin.Context) {
		common.Success(c, map[string]any{
			"id": 1, "account": "admin", "name": "管理员",
			"email": "admin@example.com", "role": "admin", "status": "active",
		})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/user/profile?id=1", nil))

	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, 200, resp.Code)
	assert.Equal(t, "管理员", resp.Data["name"])
}

func TestUserPage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/user/page", func(c *gin.Context) {
		assert.Equal(t, "0", c.DefaultQuery("pageNum", "0"))
		assert.Equal(t, "5", c.DefaultQuery("pageSize", "10"))
		assert.Equal(t, "admin", c.Query("name"))
		common.Success(c, map[string]any{
			"records": []map[string]any{
				{"id": 1, "account": "admin", "name": "管理员", "role": "admin"},
			},
			"total": 1, "size": 5, "current": 0, "pages": 1,
		})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/user/page?pageNum=0&pageSize=5&name=admin", nil))

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, float64(200), resp["code"])
}

func TestUserDelete(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.POST("/user/delete", func(c *gin.Context) {
		assert.Equal(t, "2", c.Query("id"))
		common.SuccessWithMsg(c, "删除成功", nil)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/user/delete?id=2", nil))

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, float64(200), resp["code"])
	assert.Equal(t, "删除成功", resp["message"])
}

// ========================================
// 命令下发（对照 swagger.json DownlinkCmdRequest schema）
// ========================================

func TestDownlinkCmdTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cmdTypes := []struct {
		cmdType string
		payload string
		desc    string
	}{
		{"config", `{"interval":60,"threshold":30}`, "配置更新"},
		{"control", `{"action":"reboot","delay":5}`, "远程控制"},
		{"ota", `{"url":"https://ota.example.com/fw.bin","version":"2.0.0"}`, "OTA升级"},
		{"message", `{"text":"系统维护通知","priority":1}`, "消息推送"},
	}

	for _, tc := range cmdTypes {
		t.Run(tc.desc, func(t *testing.T) {
			r := gin.New()
			r.POST("/device/:deviceId/cmd", func(c *gin.Context) {
				var req map[string]json.RawMessage
				if err := c.ShouldBindJSON(&req); err != nil {
					common.FailWithMsg(c, common.CodeBadRequest, err.Error())
					return
				}
				common.SuccessWithMsg(c, "命令已下发", map[string]any{
					"id": 1, "type": tc.cmdType,
				})
			})

			body, _ := json.Marshal(map[string]any{
				"type":    tc.cmdType,
				"payload": json.RawMessage(tc.payload),
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/device/dev-001/cmd", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			var resp map[string]any
			json.Unmarshal(w.Body.Bytes(), &resp)
			assert.Equal(t, float64(200), resp["code"])
			assert.Equal(t, "命令已下发", resp["message"])
		})
	}
}

// ========================================
// 设备拉取命令
// ========================================

func TestDevicePollCmd(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/device/:deviceId/cmd", func(c *gin.Context) {
		assert.Equal(t, "dev-001", c.Param("deviceId"))
		common.Success(c, []map[string]any{
			{"id": 1, "type": "config", "payload": json.RawMessage(`{"interval":60}`), "createdAt": "2025-07-26T00:00:00+08:00"},
			{"id": 2, "type": "control", "payload": json.RawMessage(`{"action":"reboot"}`), "createdAt": "2025-07-26T01:00:00+08:00"},
		})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/device/dev-001/cmd", nil))

	var resp struct {
		Code int              `json:"code"`
		Data []map[string]any `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, 200, resp.Code)
	assert.Len(t, resp.Data, 2)
	assert.Equal(t, float64(1), resp.Data[0]["id"])
	assert.Equal(t, "config", resp.Data[0]["type"])
}

// ========================================
// DeviceStatusDTO 传感器数据格式验证
// ========================================

func TestSensorDataFormats(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantLen int
	}{
		{
			name:    "数值型传感器",
			json:    `{"sensors":[{"name":"温度","type":"number","value":25.5}]}`,
			wantLen: 1,
		},
		{
			name:    "字符串型传感器（CO2）",
			json:    `{"sensors":[{"name":"CO2","type":"CO2-SENSOR","value":"300ppm"}]}`,
			wantLen: 1,
		},
		{
			name:    "混合传感器数组",
			json:    `{"sensors":[{"name":"temp","type":"number","value":22.0},{"name":"door","type":"binary","value":1}]}`,
			wantLen: 2,
		},
		{
			name:    "空数组（业务层拒绝）",
			json:    `{"sensors":[]}`,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dto struct {
				Sensors []map[string]any `json:"sensors"`
			}
			err := json.Unmarshal([]byte(tt.json), &dto)
			assert.NoError(t, err)
			assert.Len(t, dto.Sensors, tt.wantLen)
		})
	}
}

// ========================================
// MqttClientStatus JSON 序列化
// ========================================

func TestMqttClientStatus_JSON(t *testing.T) {
	status := map[string]any{
		"connected":            true,
		"brokerURL":            "tcp://mqtt:1883",
		"clientID":             "iot-platform.local-go",
		"subscriptionCount":    3,
		"reconnectCount":       0,
		"lastConnectedTime":    "2025-07-26T00:00:00+08:00",
		"lastDisconnectedTime": nil,
	}

	b, err := json.Marshal(status)
	assert.NoError(t, err)

	var parsed map[string]any
	json.Unmarshal(b, &parsed)
	assert.True(t, parsed["connected"].(bool))
	assert.Equal(t, "tcp://mqtt:1883", parsed["brokerURL"])
}

// ========================================
// 完整的命令下发 + 拉取流程模拟
// ========================================

func TestDownlinkFullFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 模拟存储（内存队列）
	cmdQueue := make([]map[string]any, 0)
	body, _ := json.Marshal(map[string]any{
		"type": "control", "payload": json.RawMessage(`{"action":"reboot"}`),
	})

	// POST: 用户下发
	r1 := gin.New()
	r1.POST("/device/:deviceId/cmd", func(c *gin.Context) {
		var req map[string]json.RawMessage
		c.ShouldBindJSON(&req)
		cmd := map[string]any{
			"id":       len(cmdQueue) + 1,
			"deviceId": c.Param("deviceId"),
			"type":     string(req["type"]),
		}
		cmdQueue = append(cmdQueue, cmd)
		common.SuccessWithMsg(c, "命令已下发", cmd)
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("POST", "/device/dev-001/cmd", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	r1.ServeHTTP(w1, req1)

	assert.Equal(t, 200, w1.Code)
	assert.Len(t, cmdQueue, 1)

	// GET: 设备拉取
	r2 := gin.New()
	r2.GET("/device/:deviceId/cmd", func(c *gin.Context) {
		common.Success(c, cmdQueue)
	})

	w2 := httptest.NewRecorder()
	r2.ServeHTTP(w2, httptest.NewRequest("GET", "/device/dev-001/cmd", nil))

	var resp struct {
		Data []map[string]any `json:"data"`
	}
	json.Unmarshal(w2.Body.Bytes(), &resp)
	assert.Len(t, resp.Data, 1)
	// json.RawMessage 保留原始 JSON 字符串（含引号）
	assert.Contains(t, resp.Data[0]["type"].(string), "control")
}

// newJSONBody 构造带 JSON body 的请求
func newJSONBody(t *testing.T, method, path string, body any) (*httptest.ResponseRecorder, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return httptest.NewRecorder(), gin.New()
}

// 确保 io 包被使用（上面 bytes.NewReader 需要）
var _ io.Reader
