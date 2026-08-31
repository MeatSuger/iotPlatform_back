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
// 按响应状态码分级输出，配合 pkg/common.InitLogger 的 log-level 实现：
//   - 5xx → error（生产环境可见）
//   - 4xx → warn （生产环境可见，仅必要）
//   - 2xx/3xx → debug（生产环境不输出；debug 环境显示全部请求明细）
//
// 效果：
//   - debug 环境（log-level: debug）→ 记录所有请求，便于开发排查
//   - 生产环境（log-level: info/warn）→ 只输出失败请求（4xx/5xx），成功请求静默
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
