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
)

// ApiResponse 统一API响应结构
type ApiResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// 通用响应码
const (
	CodeSuccess      = 200
	CodeBadRequest   = 400
	CodeUnauthorized = 401
	CodeForbidden    = 403
	CodeNotFound     = 404
	CodeServerError  = 500
)

var msgFlags = map[int]string{
	CodeSuccess:      "success",
	CodeBadRequest:   "请求参数错误",
	CodeUnauthorized: "未登录或Token已过期",
	CodeForbidden:    "无权限",
	CodeNotFound:     "资源不存在",
	CodeServerError:  "服务器内部错误",
}

func getMsg(code int) string {
	if msg, ok := msgFlags[code]; ok {
		return msg
	}
	return "未知错误"
}

// Success 返回成功响应
func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, ApiResponse{
		Code:    CodeSuccess,
		Message: "success",
		Data:    data,
	})
}

// SuccessWithMsg 返回自定义消息的成功响应
func SuccessWithMsg(c *gin.Context, msg string, data any) {
	c.JSON(http.StatusOK, ApiResponse{
		Code:    CodeSuccess,
		Message: msg,
		Data:    data,
	})
}

// Fail 返回失败响应
func Fail(c *gin.Context, code int) {
	c.JSON(http.StatusOK, ApiResponse{
		Code:    code,
		Message: getMsg(code),
		Data:    nil,
	})
}

// FailWithMsg 返回自定义消息的失败响应
func FailWithMsg(c *gin.Context, code int, msg string) {
	c.JSON(http.StatusOK, ApiResponse{
		Code:    code,
		Message: msg,
		Data:    nil,
	})
}
