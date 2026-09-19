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

	"iot-platform.local/pkg/common"
)

func init() {
	gin.SetMode(gin.TestMode)
	common.InitLogger("test", "")
	defer common.Sync()
}

func TestRecovery_NoPanic(t *testing.T) {
	r := gin.New()
	r.Use(Recovery())
	r.GET("/ok", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/ok", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok")
}

func TestRecovery_AppErrorPanic(t *testing.T) {
	r := gin.New()
	r.Use(Recovery())
	r.GET("/panic", func(c *gin.Context) {
		panic(&common.AppError{BizCode: 40001, Message: "业务异常"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/panic", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code) // AppError 统一返回 200，body 里带业务码
	assert.Contains(t, w.Body.String(), "40001")
	assert.Contains(t, w.Body.String(), "业务异常")
}

func TestRecovery_String_Panic(t *testing.T) {
	r := gin.New()
	r.Use(Recovery())
	r.GET("/panic-str", func(c *gin.Context) {
		panic("unexpected error")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/panic-str", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "服务器内部错误")
}

func TestRecovery_NilPanic(t *testing.T) {
	r := gin.New()
	r.Use(Recovery())
	r.GET("/panic-nil", func(c *gin.Context) {
		panic(nil)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/panic-nil", nil)
	assert.NotPanics(t, func() {
		r.ServeHTTP(w, req)
	})
}
