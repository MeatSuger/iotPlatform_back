package controller

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/yu/iot-platform-go/internal/middleware"
)

func TestDeviceIDAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := middleware.DeviceIDAuthMiddleware()

	t.Run("有效6位hex通过", func(t *testing.T) {
		r := gin.New()
		r.Use(auth)
		r.GET("/device/:deviceId/test", func(c *gin.Context) {
			did := middleware.GetDeviceID(c)
			assert.Equal(t, "abc123", did)
			c.JSON(200, gin.H{"ok": true})
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/device/abc123/test", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code)
	})

	t.Run("无效格式返回400", func(t *testing.T) {
		r := gin.New()
		r.Use(auth)
		r.GET("/device/:deviceId/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/device/abc/test", nil)
		r.ServeHTTP(w, req)
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, float64(400), resp["code"])
		assert.Contains(t, resp["message"], "格式无效")
	})

	t.Run("大写hex自动支持", func(t *testing.T) {
		r := gin.New()
		r.Use(auth)
		r.GET("/device/:deviceId/test", func(c *gin.Context) {
			did := middleware.GetDeviceID(c)
			c.JSON(200, gin.H{"deviceId": did})
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/device/ABCDEF/test", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code)
	})
}
