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
