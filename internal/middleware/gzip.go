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
	"bytes"
	"compress/gzip"
	"strings"

	"github.com/gin-gonic/gin"
)

// gzipMinSize 只压缩 ≥1KB 的 JSON 响应：小响应（大多数写接口）不值得付 gzip 的
// CPU 与 20 字节固定开销，大响应（历史曲线一单 ~6KB）才赚。
const gzipMinSize = 1024

// GzipJSON 压缩 JSON 响应。
//
// 为什么值得：历史曲线一单响应 ~6KB = 在家宽/移动网络上 4~5 个 TCP 段，丢一段就要
// 等一次 RTO（200ms 起，重传叠加到秒级）；压到 ~1KB 只有 1~2 段，尾延迟直接降下来。
// 实测（VUS=400）history 的 p95 里绝大部分是链路停顿而不是服务端耗时。
//
// 只处理 application/json（文件下载、WebSocket 升级都直接透传），
// 且先缓存到 gzipMinSize 再决定压不压，避免给少量数据加压缩头。
func GzipJSON() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "HEAD" || !acceptsGzip(c.GetHeader("Accept-Encoding")) {
			c.Next()
			return
		}

		gw := &gzipWriter{ResponseWriter: c.Writer}
		c.Writer = gw
		c.Next()
		gw.close()
	}
}

func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		if strings.TrimSpace(strings.SplitN(part, ";", 2)[0]) == "gzip" {
			return true
		}
	}
	return false
}

type gzipWriter struct {
	gin.ResponseWriter

	buf     bytes.Buffer
	gz      *gzip.Writer
	decided bool // 是否已经决定“压缩”或“原样”
	closed  bool
}

func (w *gzipWriter) Write(b []byte) (int, error) {
	if w.decided {
		if w.gz == nil {
			return w.ResponseWriter.Write(b)
		}
		return w.gz.Write(b)
	}

	// 非 JSON（下载、升级等）直接透传，不缓存
	if !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		w.decided = true
		w.WriteHeaderNow()
		return w.ResponseWriter.Write(b)
	}

	w.buf.Write(b)
	if w.buf.Len() < gzipMinSize {
		return len(b), nil
	}
	if err := w.startGzip(); err != nil {
		return len(b), err
	}
	return len(b), nil
}

func (w *gzipWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

// startGzip 超过阈值：补上压缩头，把已缓存的内容一次性写出去
func (w *gzipWriter) startGzip() error {
	w.decided = true
	h := w.Header()
	h.Set("Content-Encoding", "gzip")
	h.Del("Content-Length")
	h.Add("Vary", "Accept-Encoding")
	w.WriteHeaderNow()

	w.gz = gzip.NewWriter(w.ResponseWriter)
	_, err := w.gz.Write(w.buf.Bytes())
	w.buf.Reset()
	return err
}

// close 收尾：没超过阈值的按原样输出，压缩过的 flush 掉 gzip 尾部
func (w *gzipWriter) close() {
	if w.closed {
		return
	}
	w.closed = true

	if !w.decided {
		w.decided = true
		w.WriteHeaderNow()
		if w.buf.Len() > 0 {
			_, _ = w.ResponseWriter.Write(w.buf.Bytes())
			w.buf.Reset()
		}
		return
	}
	if w.gz != nil {
		_ = w.gz.Close()
	}
}

// Flush 支持流式输出（SSE 等）：先决定形态再往下刷
func (w *gzipWriter) Flush() {
	if !w.decided {
		if w.buf.Len() > 0 {
			_ = w.startGzip()
		} else {
			w.decided = true
			w.WriteHeaderNow()
		}
	}
	if w.gz != nil {
		_ = w.gz.Flush()
	}
	w.ResponseWriter.Flush()
}
