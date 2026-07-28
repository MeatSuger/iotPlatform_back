package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"iot-platform.local/pkg/common"
)

// Recovery 全局异常恢复中间件
// 捕获 panic，识别 *common.AppError 并返回相应业务码，否则返回 500
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				zap.L().Error("panic recovered",
					zap.Any("error", err),
					zap.String("path", c.Request.URL.Path),
					zap.String("method", c.Request.Method),
				)

				// 检查是否为 AppError（统一类型，定义在 common/errors.go）
				if appErr, ok := err.(*common.AppError); ok {
					common.FailWithMsg(c, appErr.BizCode, appErr.Message)
					c.Abort()
					return
				}

				// 默认500 错误
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
