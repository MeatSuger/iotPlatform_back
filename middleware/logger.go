package middleware

import (
	"time"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Logger 使用 ginzap 的结构化 HTTP 请求日志中间件
// 使用全局 zap.L() logger（由 common.InitLogger 初始化）
func Logger() gin.HandlerFunc {
	return ginzap.GinzapWithConfig(zap.L(), &ginzap.Config{
		TimeFormat: time.RFC3339,
		UTC:        true,
		SkipPaths:  []string{"/health"},
	})
}

// RecoveryWithZap 使用 Zap 的 panic 恢复中间件
// 自动记录 panic 堆栈到 Zap（对应 common.InitLogger 初始化的全局 logger）
func RecoveryWithZap() gin.HandlerFunc {
	return ginzap.RecoveryWithZap(zap.L(), true)
}
