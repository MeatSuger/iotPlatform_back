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
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// skipPaths 不记录访问日志的路径（健康检查等高频探测）
var skipPaths = map[string]bool{
	"/health": true,
}

// Logger 使用 Zap 的结构化 HTTP 请求日志中间件
//
// 按响应状态码分级输出（配合 pkg/common.InitLogger 的 log-level）：
//   - 5xx → error、4xx → warn、2xx/3xx → debug
//
// 因此 debug 环境记录全部请求便于排查；生产环境（info/warn）只输出 4xx/5xx，
// 且 2xx/3xx 在日志级别被过滤时走快路径提前返回，避免构造日志字段的开销。
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		if skipPaths[path] {
			return
		}

		status := c.Writer.Status()

		// 快路径：2xx/3xx 且 Debug 被日志级别过滤时直接返回，
		// 避免每请求构造字段（c.ClientIP / UserAgent / time.Format 均有分配）。
		if status < 400 && !zap.L().Core().Enabled(zap.DebugLevel) {
			return
		}

		fields := []zap.Field{
			zap.Int("status", status),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.String("ip", c.ClientIP()),
			zap.String("user-agent", c.Request.UserAgent()),
			zap.Duration("latency", time.Since(start)),
			zap.String("time", time.Now().Format(time.RFC3339)),
		}
		if len(c.Errors) > 0 {
			fields = append(fields, zap.Strings("errors", c.Errors.Errors()))
		}

		switch {
		case status >= 500:
			zap.L().Error("[HTTP]", fields...)
		case status >= 400:
			zap.L().Warn("[HTTP]", fields...)
		default:
			zap.L().Debug("[HTTP]", fields...)
		}
	}
}
