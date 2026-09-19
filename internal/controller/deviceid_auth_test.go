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
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"iot-platform.local/internal/middleware"
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
		var resp map[string]any
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
