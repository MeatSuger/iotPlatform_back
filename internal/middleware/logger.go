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
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// skipPaths 不记录访问日志的路径（健康检查等高频探测）
var skipPaths = map[string]bool{
	"/health": true,
}

// sensitiveQueryKeys 不允许落日志的查询参数（设备/用户 Token、登录密码）。
var sensitiveQueryKeys = map[string]bool{
	"token":          true,
	"access_token":   true,
	"x-device-token": true,
	"authorization":  true,
	"devicetoken":    true,
	"passwd":         true,
	"password":       true,
}

// redactQuery 将敏感参数的原始值替换为 "***"，其余参数保持原样。
// 无法解析时整体替换为 "***"：错误串里可能带着任何参数（含 Token/密码），
// 原样落日志等于绕过了脱敏。
func redactQuery(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "***"
	}
	changed := false
	for key, vals := range values {
		if !sensitiveQueryKeys[strings.ToLower(key)] {
			continue
		}
		for i := range vals {
			vals[i] = "***"
		}
		changed = true
	}
	if !changed {
		return raw
	}
	return values.Encode()
}

// slowRequestThreshold 慢请求告警阈值。
//
// 2xx 在生产(info) 下不记日志，于是“接口慢”在服务端完全不可见：压测时看到的几秒延迟
// 到底是服务端还是客户端链路，只能猜。这条日志就是那个分界证据：
// 压测时 `docker logs iot-platform-go | grep 慢请求`，有就是服务端慢，没有就是链路/客户端慢。
const slowRequestThreshold = 500 * time.Millisecond

// 慢请求日志限流：饱和时"几乎每个请求都 >500ms"，逐条写日志会把磁盘和 CPU 一起打满
// （3603 VU 实测 1.7k 条/秒 JSON 写进 docker json-file，iowait 成为新的瓶颈），
// 而日志只是因为限流丢掉明细，并不丢信息：每秒汇总一行“共 N 条、代表延迟 X”。
const (
	slowLogsPerSecond = 5 // 每个时间窗最多逐条记录的条数
	slowLogWindow     = 1 * time.Second
)

var (
	slowLogWindowStart atomic.Int64 // 当前窗口起始（UnixNano）
	slowLogCount       atomic.Int64 // 当前窗口已逐条记录数
	slowLogDeferred    atomic.Int64 // 当前窗口被限流跳过的条数
)

// logSlowRequest 带限流地记录一条慢请求日志（超额的只计数）
func logSlowRequest(c *gin.Context, path string, latency time.Duration) {
	now := time.Now()
	start := slowLogWindowStart.Load()
	if now.UnixNano()-start >= int64(slowLogWindow) {
		// 换窗口：谁抢到谁负责把上一窗的汇总打出来
		if slowLogWindowStart.CompareAndSwap(start, now.UnixNano()) {
			slowLogCount.Store(0)
			if suppressed := slowLogDeferred.Swap(0); suppressed > 0 {
				zap.L().Warn("[HTTP] 慢请求汇总",
					zap.Int64("suppressed", suppressed),
					zap.Duration("window", slowLogWindow))
			}
		}
	}

	if slowLogCount.Add(1) <= slowLogsPerSecond {
		zap.L().Warn("[HTTP] 慢请求",
			zap.Duration("latency", latency),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("ip", c.ClientIP()))
		return
	}
	slowLogDeferred.Add(1)
}

// Logger 使用 Zap 的结构化 HTTP 请求日志中间件
//
// 按响应状态码分级输出（配合 pkg/common.InitLogger 的 log-level）：
//   - 5xx → error、4xx → warn、2xx/3xx → debug
//   - 任何状态码只要耗时 ≥ slowRequestThreshold → 额外一条 warn
//
// 因此 debug 环境记录全部请求便于排查；生产环境（info/warn）只输出 4xx/5xx 与慢请求，
// 且 2xx/3xx 在日志级别被过滤时走快路径提前返回，避免构造日志字段的开销。
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := redactQuery(c.Request.URL.RawQuery)

		c.Next()

		if skipPaths[path] {
			return
		}

		status := c.Writer.Status()
		latency := time.Since(start)

		// 快路径：2xx/3xx 且 Debug 被日志级别过滤时直接返回，
		// 避免每请求构造字段（c.ClientIP / UserAgent / time.Format 均有分配）；
		// 但慢请求例外，必须留一条能定位服务端耗时的日志。
		if status < 400 && !zap.L().Core().Enabled(zap.DebugLevel) {
			if latency >= slowRequestThreshold {
				logSlowRequest(c, path, latency)
			}
			return
		}

		fields := []zap.Field{
			zap.Int("status", status),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.String("ip", c.ClientIP()),
			zap.String("user-agent", c.Request.UserAgent()),
			zap.Duration("latency", latency),
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
