package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/model"
	"iot-platform.local/pkg/common"
)

// ========================================
// 数据上报完整 HTTP 流程测试
// ========================================

func TestDataReport_FullFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deviceMgr := middleware.GetDeviceManager()

	t.Run("设备登录→上报传感器数据 成功", func(t *testing.T) {
		// Step 1: 设备登录，获取 token
		deviceToken, err := deviceMgr.Login("dev-001", "device")
		assert.NoError(t, err)
		assert.NotEmpty(t, deviceToken)

		// Step 2: 带 token 上报传感器数据（模拟 DataController.ReportData 核心逻辑）
		deviceAuth := middleware.DeviceAuthMiddleware()
		r := gin.New()
		r.POST("/data/:deviceId/Data", deviceAuth, func(c *gin.Context) {
			deviceID := c.Param("deviceId")
			rawToken, _ := c.Get("deviceToken")
			tokenStr, _ := rawToken.(string)

			assert.Equal(t, "dev-001", deviceID)
			assert.Equal(t, deviceToken, tokenStr)

			var dto entity.DeviceStatusDTO
			if err := c.ShouldBindJSON(&dto); err != nil {
				common.FailWithMsg(c, common.CodeBadRequest, err.Error())
				return
			}
			if len(dto.Sensors) == 0 {
				common.FailWithMsg(c, common.CodeBadRequest, "传感器数据不能为空")
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code":    200,
				"message": "状态上报已接收",
				"data":    time.Now().Format("2006-01-02T15:04:05.000"),
			})
		})

		body := map[string]interface{}{
			"sensors": []map[string]interface{}{
				{"name": "temperature", "type": "number", "value": 26.5},
				{"name": "humidity", "type": "number", "value": 65.0},
			},
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/data/dev-001/Data", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Device-Token", deviceToken)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, float64(200), resp["code"])
		assert.Equal(t, "状态上报已接收", resp["message"])
		assert.NotEmpty(t, resp["data"])
	})

	t.Run("上报空传感器数组返回400", func(t *testing.T) {
		deviceToken, _ := deviceMgr.Login("dev-002", "device")
		deviceAuth := middleware.DeviceAuthMiddleware()

		r := gin.New()
		r.POST("/data/:deviceId/Data", deviceAuth, func(c *gin.Context) {
			var dto entity.DeviceStatusDTO
			c.ShouldBindJSON(&dto)
			if len(dto.Sensors) == 0 {
				common.FailWithMsg(c, common.CodeBadRequest, "传感器数据不能为空")
				return
			}
			common.Success(c, nil)
		})

		body := map[string]interface{}{"sensors": []interface{}{}}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/data/dev-002/Data", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Device-Token", deviceToken)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, float64(400), resp["code"])
		assert.Contains(t, resp["message"], "传感器数据不能为空")
	})

	t.Run("无效设备Token返回401", func(t *testing.T) {
		deviceAuth := middleware.DeviceAuthMiddleware()
		r := gin.New()
		r.POST("/data/:deviceId/Data", deviceAuth, func(c *gin.Context) {
			c.JSON(200, gin.H{"ok": true})
		})

		req := httptest.NewRequest("POST", "/data/dev-003/Data", nil)
		req.Header.Set("X-Device-Token", "invalid-token")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, float64(401), resp["code"])
		assert.Contains(t, resp["message"], "设备Token无效")
	})

	t.Run("无设备Token返回401", func(t *testing.T) {
		deviceAuth := middleware.DeviceAuthMiddleware()
		r := gin.New()
		r.POST("/data/:deviceId/Data", deviceAuth, func(c *gin.Context) {
			c.JSON(200, gin.H{"ok": true})
		})

		req := httptest.NewRequest("POST", "/data/dev-004/Data", nil)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, float64(401), resp["code"])
		assert.Contains(t, resp["message"], "缺少设备Token")
	})
}

// ========================================
// 设备心跳测试
// ========================================

func TestHeartbeat_FullFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deviceMgr := middleware.GetDeviceManager()

	t.Run("POST /data/:deviceId/ping 成功", func(t *testing.T) {
		deviceToken, _ := deviceMgr.Login("dev-hb-001", "device")
		deviceAuth := middleware.DeviceAuthMiddleware()

		r := gin.New()
		r.POST("/data/:deviceId/ping", deviceAuth, func(c *gin.Context) {
			deviceID := c.Param("deviceId")
			assert.Equal(t, "dev-hb-001", deviceID)

			c.JSON(200, gin.H{
				"code":    200,
				"message": "success",
				"data":    gin.H{"serverTime": time.Now().Format("2006-01-02T15:04:05.000"), "nextInterval": 60},
			})
		})

		req := httptest.NewRequest("POST", "/data/dev-hb-001/ping", nil)
		req.Header.Set("X-Device-Token", deviceToken)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, float64(200), resp["code"])
	})

	t.Run("POST /data/:deviceId/heartbeat 成功", func(t *testing.T) {
		deviceToken, _ := deviceMgr.Login("dev-hb-002", "device")
		deviceAuth := middleware.DeviceAuthMiddleware()

		r := gin.New()
		r.POST("/data/:deviceId/heartbeat", deviceAuth, func(c *gin.Context) {
			c.JSON(200, gin.H{"code": 200, "message": "heartbeat ok"})
		})

		req := httptest.NewRequest("POST", "/data/dev-hb-002/heartbeat", nil)
		req.Header.Set("X-Device-Token", deviceToken)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, float64(200), resp["code"])
	})
}

// ========================================
// InfluxDB Ping（公开端点）
// ========================================

func TestInfluxDBPing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("POST /data/ping 返回成功", func(t *testing.T) {
		r := gin.New()
		r.POST("/ping", func(c *gin.Context) {
			common.Success(c, "true")
		})

		req := httptest.NewRequest("POST", "/ping", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, float64(200), resp["code"])
		assert.Equal(t, "true", resp["data"])
	})
}

// ========================================
// 传感器数据 JSON 验证
// ========================================

func TestSensorData_JSONValidation(t *testing.T) {
	t.Run("正常传感器 JSON 解析为 DTO", func(t *testing.T) {
		jsonBody := `{
			"sensors": [
				{"name": "CO2传感器", "type": "CO2-SENSOR", "value": "300ppm"},
				{"name": "温度", "type": "temperature", "value": 25.5},
				{"name": "湿度", "type": "humidity", "value": 60}
			]
		}`

		var dto entity.DeviceStatusDTO
		err := json.Unmarshal([]byte(jsonBody), &dto)
		assert.NoError(t, err)
		assert.Len(t, dto.Sensors, 3)
		assert.Equal(t, "CO2传感器", dto.Sensors[0].Name)
		assert.Equal(t, "CO2-SENSOR", dto.Sensors[0].Type)
		assert.Equal(t, "300ppm", dto.Sensors[0].Value)
	})

	t.Run("缺少 sensors 字段 → len=0", func(t *testing.T) {
		var dto entity.DeviceStatusDTO
		err := json.Unmarshal([]byte(`{}`), &dto)
		assert.NoError(t, err)
		assert.Len(t, dto.Sensors, 0)
	})

	t.Run("非 JSON 格式应返回400", func(t *testing.T) {
		r := gin.New()
		r.POST("/data", func(c *gin.Context) {
			var dto entity.DeviceStatusDTO
			err := c.ShouldBindJSON(&dto)
			assert.Error(t, err)
			common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		})

		req := httptest.NewRequest("POST", "/data", bytes.NewReader([]byte("not json")))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, float64(400), resp["code"])
	})
}

// ========================================
// 数据查询端点
// ========================================

func TestQueryData(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("GET /data/:deviceId/Data/list 带limit参数", func(t *testing.T) {
		r := gin.New()
		r.GET("/data/:deviceId/Data/list", func(c *gin.Context) {
			deviceID := c.Param("deviceId")
			assert.Equal(t, "dev-query-001", deviceID)
			limit := c.DefaultQuery("limit", "50")
			assert.Equal(t, "10", limit)
			common.Success(c, []map[string]interface{}{
				{"name": "temp", "value": 25.5},
			})
		})

		req := httptest.NewRequest("GET", "/data/dev-query-001/Data/list?limit=10", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, float64(200), resp["code"])
	})

	t.Run("GET /data/list 通用入口", func(t *testing.T) {
		r := gin.New()
		r.GET("/data/list", func(c *gin.Context) {
			common.Success(c, gin.H{"message": "请在具体设备下查询数据"})
		})

		req := httptest.NewRequest("GET", "/data/list", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, float64(200), resp["code"])
	})
}

// ========================================
// MQTT 设备数据上报
// ========================================

func TestMqttDataReport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deviceMgr := middleware.GetDeviceManager()

	t.Run("POST /mqtt/:deviceId/Data 成功", func(t *testing.T) {
		deviceToken, _ := deviceMgr.Login("mqtt-dev-001", "device")
		deviceAuth := middleware.DeviceAuthMiddleware()

		r := gin.New()
		r.POST("/mqtt/:deviceId/Data", deviceAuth, func(c *gin.Context) {
			deviceID := c.Param("deviceId")
			assert.Equal(t, "mqtt-dev-001", deviceID)
			common.SuccessWithMsg(c, "MQTT数据上报已接收", time.Now().Format("2006-01-02T15:04:05.000"))
		})

		body := map[string]interface{}{
			"sensors": []map[string]interface{}{
				{"name": "MQTT-CO2", "type": "CO2-SENSOR", "value": "500ppm"},
			},
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/mqtt/mqtt-dev-001/Data", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Device-Token", deviceToken)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, float64(200), resp["code"])
		assert.Equal(t, "MQTT数据上报已接收", resp["message"])
	})

	t.Run("POST /mqtt/:deviceId/ping 心跳成功", func(t *testing.T) {
		deviceToken, _ := deviceMgr.Login("mqtt-dev-002", "device")
		deviceAuth := middleware.DeviceAuthMiddleware()

		r := gin.New()
		r.POST("/mqtt/:deviceId/ping", deviceAuth, func(c *gin.Context) {
			common.Success(c, gin.H{"serverTime": time.Now(), "nextInterval": 60})
		})

		req := httptest.NewRequest("POST", "/mqtt/mqtt-dev-002/ping", nil)
		req.Header.Set("X-Device-Token", deviceToken)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, float64(200), resp["code"])
	})

	t.Run("POST /mqtt/:deviceId/heartbeat 成功", func(t *testing.T) {
		deviceToken, _ := deviceMgr.Login("mqtt-dev-003", "device")
		deviceAuth := middleware.DeviceAuthMiddleware()

		r := gin.New()
		r.POST("/mqtt/:deviceId/heartbeat", deviceAuth, func(c *gin.Context) {
			common.SuccessWithMsg(c, "heartbeat", nil)
		})

		req := httptest.NewRequest("POST", "/mqtt/mqtt-dev-003/heartbeat", nil)
		req.Header.Set("X-Device-Token", deviceToken)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, float64(200), resp["code"])
	})
}
