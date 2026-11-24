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
	"time"

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

func TestRedactQuery_MasksTokens(t *testing.T) {
	got := redactQuery("token=abc.def&deviceId=90431b&X-Device-Token=secret")
	assert.NotContains(t, got, "abc.def")
	assert.NotContains(t, got, "secret")
	assert.Contains(t, got, "deviceId=90431b")
	assert.Contains(t, got, "%2A%2A%2A")
}

func TestRedactQuery_KeepsOtherParams(t *testing.T) {
	raw := "limit=50&start=2026-01-01T00:00:00Z"
	assert.Equal(t, raw, redactQuery(raw))
}

// TestRedactQuery_MasksPassword 登录接口支持 Query 传凭据，passwd/password 不得落日志。
func TestRedactQuery_MasksPassword(t *testing.T) {
	got := redactQuery("account=alice&passwd=Secret123&password=p%40ss")
	assert.NotContains(t, got, "Secret123")
	assert.NotContains(t, got, "p@ss")
	assert.Contains(t, got, "account=alice")
	assert.Contains(t, got, "%2A%2A%2A")
}

// TestRedactQuery_MalformedDoesNotLeak 解析失败时不得原样返回 query。
func TestRedactQuery_MalformedDoesNotLeak(t *testing.T) {
	cases := []string{
		"token=abc.def;x=1", // 非法分号分隔
		"token=%zz",         // 非法百分号转义
		"passwd=Secret;bad",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			got := redactQuery(raw)
			assert.NotContains(t, got, "abc.def")
			assert.NotContains(t, got, "Secret")
			assert.NotContains(t, got, "%zz")
		})
	}
}

// TestLogger_SlowRequestWarn 2xx 慢请求必须在 info 级别下留一条 warn（否则服务端耗时完全不可见）
func TestLogger_SlowRequestWarn(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel) // 生产级别：debug 被过滤，走快路径
	logger := zap.New(core)

	r := gin.New()
	r.Use(Logger())
	r.GET("/slow", func(c *gin.Context) {
		time.Sleep(600 * time.Millisecond)
		c.JSON(200, gin.H{"ok": true})
	})
	r.GET("/fast", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	prev := zap.L()
	zap.ReplaceGlobals(logger)
	t.Cleanup(func() { zap.ReplaceGlobals(prev) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/slow", nil))
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/fast", nil))

	var slow, fast int
	for _, e := range logs.All() {
		if e.Level != zap.WarnLevel || e.Message != "[HTTP] 慢请求" {
			continue
		}
		if e.ContextMap()["path"] == "/slow" {
			slow++
		}
		if e.ContextMap()["path"] == "/fast" {
			fast++
		}
	}
	assert.Equal(t, 1, slow, "慢请求应记一条 warn")
	assert.Equal(t, 0, fast, "快请求不应记 warn")
}

// TestLogger_SlowRequestRateLimited 慢请求日志必须限流：饱和时每个请求都慢，
// 逐条写日志会把磁盘打满（实测 iowait 成为瓶颈），超额的只计数并汇总。
func TestLogger_SlowRequestRateLimited(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	prev := zap.L()
	zap.ReplaceGlobals(zap.New(core))
	t.Cleanup(func() { zap.ReplaceGlobals(prev) })

	r := gin.New()
	r.Use(Logger())
	r.GET("/slow", func(c *gin.Context) {
		time.Sleep(2 * time.Millisecond)
		c.JSON(200, gin.H{"ok": true})
	})

	// 直接打限流函数，避免依赖真实 500ms 阈值
	slowLogWindowStart.Store(0)
	slowLogCount.Store(0)
	slowLogDeferred.Store(0)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/slow", nil)

	const total = 100
	for range total {
		logSlowRequest(c, "/slow", 600*time.Millisecond)
	}

	detailed := 0
	for _, e := range logs.All() {
		if e.Message == "[HTTP] 慢请求" {
			detailed++
		}
	}
	assert.LessOrEqual(t, detailed, slowLogsPerSecond, "同一时间窗内逐条日志必须 ≤ 上限")
	assert.Equal(t, int64(total-slowLogsPerSecond), slowLogDeferred.Load(), "其余必须记为被限流")
}
