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
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"iot-platform.local/pkg/common"
)

// Recovery 全局异常恢复中间件
// 捕获 panic：识别 *common.AppError 并按业务码响应；其余 panic 统一返回 500 业务码（HTTP 200）
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				zap.L().Error("panic recovered",
					zap.Any("error", err),
					zap.String("path", c.Request.URL.Path),
					zap.String("method", c.Request.Method),
					zap.String("stack", string(debug.Stack())),
				)

				// 识别 *common.AppError（统一业务错误类型，定义在 common/errors.go）
				if appErr, ok := err.(*common.AppError); ok {
					common.FailWithMsg(c, appErr.BizCode, appErr.Message)
					c.Abort()
					return
				}

				// 其他 panic：返回统一 500 业务码（HTTP 200）
				c.JSON(http.StatusOK, common.ApiResponse{
					Code:    common.CodeServerError,
					Message: "服务器内部错误",
					Data:    nil,
				})
				c.Abort()
			}
		}()
		c.Next()
	}
}
