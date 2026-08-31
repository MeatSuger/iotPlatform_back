package common

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"iot-platform.local/pkg/config"
)

// SetAuthCookie 统一设置认证 Cookie：
//   - httpOnly=true：防止 XSS 通过 document.cookie 窃取 Token
//   - secure：生产环境（release）强制 HTTPS 传输，本地 debug 用 HTTP 也能携带
//   - SameSite=Lax：缓解 CSRF
func SetAuthCookie(c *gin.Context, name, value string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, value, maxAge, "/", "", isCookieSecure(), true)
}

// ClearAuthCookie 清除认证 Cookie
func ClearAuthCookie(c *gin.Context, name string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, "", -1, "/", "", isCookieSecure(), true)
}

func isCookieSecure() bool {
	return config.Cfg != nil && config.Cfg.Server.Mode == "release"
}
