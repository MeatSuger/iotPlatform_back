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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// withObserverLogger 将全局 zap.L() 替换为观测器 logger，测试结束后还原原 logger
func withObserverLogger(t *testing.T, fn func(obs *observer.ObservedLogs)) {
	core, obs := observer.New(zapcore.DebugLevel)
	restore := zap.ReplaceGlobals(zap.New(core)) // 返回原 logger 的恢复函数
	t.Cleanup(restore)
	fn(obs)
}

func TestLogger_NotNil(t *testing.T) {
	handler := Logger()
	assert.NotNil(t, handler)
}

// TestLogger_LevelsByStatus 验证访问日志按状态码分级：
// 2xx/3xx→debug, 4xx→warn, 5xx→error
func TestLogger_LevelsByStatus(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		expectCore zapcore.Level
	}{
		{name: "success", status: http.StatusOK, expectCore: zapcore.DebugLevel},
		{name: "redirect", status: http.StatusFound, expectCore: zapcore.DebugLevel},
		{name: "client_error", status: http.StatusBadRequest, expectCore: zapcore.WarnLevel},
		{name: "server_error", status: http.StatusInternalServerError, expectCore: zapcore.ErrorLevel},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withObserverLogger(t, func(obs *observer.ObservedLogs) {
				r := gin.New()
				r.Use(Logger())
				r.GET("/test", func(c *gin.Context) { c.Status(tc.status) })

				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, "/test", nil)
				r.ServeHTTP(w, req)

				logs := obs.TakeAll()
				assert.Len(t, logs, 1)
				assert.Equal(t, tc.expectCore, logs[0].Level)
				assert.Equal(t, "/test", logs[0].ContextMap()["path"])
			})
		})
	}
}

// TestLogger_SkipHealth 验证 /health 路径不产生访问日志
func TestLogger_SkipHealth(t *testing.T) {
	withObserverLogger(t, func(obs *observer.ObservedLogs) {
		r := gin.New()
		r.Use(Logger())
		r.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, obs.TakeAll())
	})
}
