package middleware

import (
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	sagin "github.com/sa-tokens/sa-token-go/integrations/gin"
	"github.com/sa-tokens/sa-token-go/stputil"
	"iot-platform.local/pkg/common"
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

// deviceAuthCache 本地内存缓存：Token → deviceID 映射
// 消除高频上报时对 Sa-Token Redis 存储的重复查询
var (
	deviceAuthCacheMu sync.RWMutex
	deviceAuthCache   = make(map[string]cachedAuth) // token → {deviceID, expiresAt}
)

type cachedAuth struct {
	deviceID  string
	expiresAt time.Time
}

const deviceAuthCacheTTL = 5 * time.Second

// getCachedDeviceID 从本地缓存获取已校验的 Token 对应的 deviceID
func getCachedDeviceID(token string) (string, bool) {
	deviceAuthCacheMu.RLock()
	e, ok := deviceAuthCache[token]
	deviceAuthCacheMu.RUnlock()
	if ok && time.Now().Before(e.expiresAt) {
		return e.deviceID, true
	}
	return "", false
}

// setCachedDeviceID 缓存 Token → deviceID 到本地
func setCachedDeviceID(token, deviceID string) {
	deviceAuthCacheMu.Lock()
	deviceAuthCache[token] = cachedAuth{deviceID: deviceID, expiresAt: time.Now().Add(deviceAuthCacheTTL)}
	deviceAuthCacheMu.Unlock()
}

// init 启动后台协程，每 30s 清理一次设备认证缓存中的过期条目
func init() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			deviceAuthCacheMu.Lock()
			now := time.Now()
			for k, v := range deviceAuthCache {
				if now.After(v.expiresAt) {
					delete(deviceAuthCache, k)
				}
			}
			deviceAuthCacheMu.Unlock()
		}
	}()
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

		// 一次 Redis GET 完成登录校验 + 获取 loginID（替代 IsLogin + GetLoginID 的多次往返）
		info, err := stputil.GetTokenInfo(token)
		if err != nil || info == nil {
			common.FailWithMsg(c, common.CodeUnauthorized, "Token无效或已过期")
			c.Abort()
			return
		}

		userID, err := strconv.ParseUint(info.LoginID, 10, 64)
		if err != nil {
			common.FailWithMsg(c, common.CodeUnauthorized, "Token格式无效")
			c.Abort()
			return
		}

		// 获取角色（1 次 Redis GET）
		roles, _ := stputil.GetRoles(info.LoginID)
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

// DeviceAuthMiddleware 设备 Sa-Token 认证中间件（用于数据上报/心跳/命令拉取）
// 优化：使用本地内存缓存 Token → deviceID 映射（5s TTL），避免每次 Redis 查询
func DeviceAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractDeviceToken(c)
		if token == "" {
			common.FailWithMsg(c, common.CodeUnauthorized, "缺少设备Token")
			c.Abort()
			return
		}

		// L1 本地缓存：Token → deviceID（5s TTL，覆盖高频上报场景）
		loginID, hit := getCachedDeviceID(token)
		if !hit {
			// 缓存未命中，走 Sa-Token Redis 查询（对踢出/顶号/失效 token 均返回错误）
			var err error
			loginID, err = deviceMgr.GetLoginID(token)
			if err != nil {
				common.FailWithMsg(c, common.CodeUnauthorized, "设备Token无效")
				c.Abort()
				return
			}
			setCachedDeviceID(token, loginID)
		}

		c.Set("deviceId", loginID)
		c.Set("deviceToken", token)

		c.Next()
	}
}

// UserOrDeviceAuthMiddleware 双认证中间件：优先识别用户 Token（Sa-Token 用户登录态），
// 否则回退识别设备 Token（X-Device-Token）。两者都无效则返回 401。
// 注入上下文：
//   - authType = "user"   + userId
//   - authType = "device" + deviceId / deviceToken
func UserOrDeviceAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 优先用户 Token（Header/Cookie/Query 中的 Authorization）
		if userToken := sagin.GetTokenFromCtx(c); userToken != "" {
			info, err := stputil.GetTokenInfo(userToken)
			if err == nil && info != nil {
				userID, perr := strconv.ParseUint(info.LoginID, 10, 64)
				if perr == nil {
					c.Set("authType", "user")
					c.Set("userId", uint(userID))
					c.Set("token", userToken)
					c.Next()
					return
				}
			}
		}

		// 2. 回退设备 Token
		if deviceToken := extractDeviceToken(c); deviceToken != "" {
			loginID, err := deviceMgr.GetLoginID(deviceToken)
			if err == nil {
				c.Set("authType", "device")
				c.Set("deviceId", loginID)
				c.Set("deviceToken", deviceToken)
				c.Next()
				return
			}
		}

		common.FailWithMsg(c, common.CodeUnauthorized, "Token无效或已过期")
		c.Abort()
	}
}

// GetAuthType 获取请求认证类型（"user" / "device" / ""）
func GetAuthType(c *gin.Context) string {
	if t, exists := c.Get("authType"); exists {
		return t.(string)
	}
	return ""
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

// CheckRole 角色检查中间件（基于 sa-token-go 角色体系）
// 登录时 UserService.Login 已通过 stputil.SetRoles 写入数据库中的 user.role，
// 这里直接复用 sa-token-go 的 CheckRoleOr 校验（任一角色匹配即放行）
func CheckRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := getToken(c)
		if token == "" {
			common.Fail(c, common.CodeForbidden)
			c.Abort()
			return
		}
		if err := stputil.CheckRoleOr(token, roles); err != nil {
			common.FailWithMsg(c, common.CodeForbidden, "无权限执行此操作")
			c.Abort()
			return
		}
		c.Next()
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

// GetDeviceToken 提取设备 Token（Header "X-Device-Token" → Cookie → Query），
// 与 DeviceAuthMiddleware 同一提取逻辑，供 MQTT 网关等入口复用
func GetDeviceToken(c *gin.Context) string {
	return extractDeviceToken(c)
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

// parseUint 字符串转 uint（用于从 query/param 解析 ID，失败返回 0）
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
