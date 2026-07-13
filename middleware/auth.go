package middleware

import (
	"strconv"

	"github.com/gin-gonic/gin"
	sagin "github.com/sa-tokens/sa-token-go/integrations/gin"
	"github.com/sa-tokens/sa-token-go/stputil"
	"github.com/yu/iot-platform-go/common"
)

// 设备 Manager（非全局，单独持有）
var deviceMgr *sagin.Manager

// SetDeviceManager 设置设备 Token Manager
func SetDeviceManager(mgr *sagin.Manager) {
	deviceMgr = mgr
}

// GetDeviceManager 获取设备 Token Manager
func GetDeviceManager() *sagin.Manager {
	return deviceMgr
}

// ========================================
// 认证中间件（对齐官方 sa-token-go 风格）
// Token 提取由 TokenInterceptor 完成，这里只做校验 + 上下文注入
// ========================================

// AuthMiddleware 用户认证中间件
// 前置条件：TokenInterceptor 已把 token 写入 gin.Context
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := sagin.GetTokenFromCtx(c)
		if token == "" {
			common.Fail(c, common.CodeUnauthorized)
			c.Abort()
			return
		}

		// stputil 全局函数操作默认用户 Manager
		if !stputil.IsLogin(token) {
			common.FailWithMsg(c, common.CodeUnauthorized, "Token无效或已过期")
			c.Abort()
			return
		}

		loginID, err := stputil.GetLoginID(token)
		if err != nil {
			common.FailWithMsg(c, common.CodeUnauthorized, "Token无效")
			c.Abort()
			return
		}

		userID, err := strconv.ParseUint(loginID, 10, 64)
		if err != nil {
			common.FailWithMsg(c, common.CodeUnauthorized, "Token格式无效")
			c.Abort()
			return
		}

		// 获取角色
		roles, _ := stputil.GetRoles(loginID)
		role := ""
		if len(roles) > 0 {
			role = roles[0]
		}

		// 注入上下文
		c.Set("userId", uint(userID))
		c.Set("role", role)
		c.Set("token", token)

		c.Next()
	}
}

// DeviceAuthMiddleware 设备 Sa-Token 认证中间件（用于用户获取设备Token）
// 保留用于 /device/:deviceId/login, /device/:deviceId/token 等需要 Token 的接口
func DeviceAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractDeviceToken(c)
		if token == "" {
			common.FailWithMsg(c, common.CodeUnauthorized, "缺少设备Token")
			c.Abort()
			return
		}

		loginID, err := deviceMgr.GetLoginID(token)
		if err != nil {
			common.FailWithMsg(c, common.CodeUnauthorized, "设备Token无效")
			c.Abort()
			return
		}

		c.Set("deviceId", loginID)
		c.Set("deviceToken", token)

		c.Next()
	}
}

// DeviceIDAuthMiddleware 设备 ID 认证中间件（6位hex，无需额外Token）
// 用于设备数据上报/心跳/命令拉取 — 设备只需携带自己的6位hex ID
func DeviceIDAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		deviceID := c.Param("deviceId")
		if deviceID == "" {
			common.FailWithMsg(c, common.CodeUnauthorized, "缺少设备ID")
			c.Abort()
			return
		}

		// 校验6位hex格式
		deviceID = normalizeDeviceID(deviceID)
		if !isValidDeviceID(deviceID) {
			common.FailWithMsg(c, common.CodeBadRequest, "设备ID格式无效（需6位十六进制）")
			c.Abort()
			return
		}

		c.Set("deviceId", deviceID)
		c.Next()
	}
}

func normalizeDeviceID(id string) string {
	if len(id) > 0 && len(id) <= 6 {
		return id
	}
	return id
}

func isValidDeviceID(id string) bool {
	if len(id) != 6 {
		return false
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// CheckRole 角色检查中间件
func CheckRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := getToken(c)
		if token == "" {
			common.Fail(c, common.CodeForbidden)
			c.Abort()
			return
		}
		loginID, _ := stputil.GetLoginID(token)
		for _, r := range roles {
			if stputil.HasRole(loginID, r) {
				c.Next()
				return
			}
		}
		common.FailWithMsg(c, common.CodeForbidden, "无权限执行此操作")
		c.Abort()
	}
}

// RequireLogin 要求登录
func RequireLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := getToken(c)
		if token == "" || !stputil.IsLogin(token) {
			common.Fail(c, common.CodeUnauthorized)
			c.Abort()
			return
		}
		c.Next()
	}
}

// ========================================
// Token 提取（设备 Token，不走 TokenInterceptor）
// ========================================

func extractDeviceToken(c *gin.Context) string {
	// 1. Header "X-Device-Token"
	if tok := c.GetHeader("X-Device-Token"); tok != "" {
		return tok
	}
	// 2. Cookie "X-Device-Token"（登录后浏览器自动携带）
	if tok, _ := c.Cookie("X-Device-Token"); tok != "" {
		return tok
	}
	// 3. Query
	if tok := c.Query("X-Device-Token"); tok != "" {
		return tok
	}
	return ""
}

// ========================================
// 上下文提取器
// ========================================

func GetUserID(c *gin.Context) uint {
	if uid, exists := c.Get("userId"); exists {
		return uid.(uint)
	}
	return 0
}

func GetUserRole(c *gin.Context) string {
	if role, exists := c.Get("role"); exists {
		return role.(string)
	}
	return ""
}

func GetDeviceID(c *gin.Context) string {
	if did, exists := c.Get("deviceId"); exists {
		return did.(string)
	}
	return ""
}

func GetToken(c *gin.Context) string {
	if tok := sagin.GetTokenFromCtx(c); tok != "" {
		return tok
	}
	if tok, exists := c.Get("token"); exists {
		return tok.(string)
	}
	return ""
}

func getToken(c *gin.Context) string {
	return GetToken(c)
}

func parseUint(s string) uint {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return uint(v)
}
