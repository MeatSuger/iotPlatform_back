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

package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGzipJSON 大 JSON 响应压缩，小 JSON 与非 JSON 原样透传
func TestGzipJSON(t *testing.T) {
	big := strings.Repeat("a", 4096)

	newRouter := func() *gin.Engine {
		r := gin.New()
		r.Use(GzipJSON())
		r.GET("/big", func(c *gin.Context) { c.JSON(200, gin.H{"data": big}) })
		r.GET("/small", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
		r.GET("/plain", func(c *gin.Context) {
			c.Header("Content-Type", "text/plain")
			c.String(200, big)
		})
		return r
	}

	t.Run("大 JSON 压缩", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/big", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		newRouter().ServeHTTP(w, req)

		assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
		zr, err := gzip.NewReader(w.Body)
		require.NoError(t, err)
		body, err := io.ReadAll(zr)
		require.NoError(t, err)
		assert.Contains(t, string(body), big)
		assert.Less(t, w.Body.Len(), 1024, "压缩后应远小于原文")
	})

	t.Run("小 JSON 不压缩", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/small", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		newRouter().ServeHTTP(w, req)

		assert.Empty(t, w.Header().Get("Content-Encoding"))
		assert.Contains(t, w.Body.String(), `"ok":true`)
	})

	t.Run("非 JSON 透传", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/plain", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		newRouter().ServeHTTP(w, req)

		assert.Empty(t, w.Header().Get("Content-Encoding"))
		assert.Equal(t, big, w.Body.String())
	})

	t.Run("客户端不支持 gzip", func(t *testing.T) {
		w := httptest.NewRecorder()
		newRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/big", nil))

		assert.Empty(t, w.Header().Get("Content-Encoding"))
		assert.Contains(t, w.Body.String(), big)
	})
}
