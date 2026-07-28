package common

import (
	"errors"
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

// FailWithData 返回带数据的失败响应
func FailWithData(c *gin.Context, code int, msg string, data any) {
	c.JSON(http.StatusOK, ApiResponse{
		Code:    code,
		Message: msg,
		Data:    data,
	})
}

// Error 返回服务器内部错误
func Error(c *gin.Context, msg string) {
	c.JSON(http.StatusOK, ApiResponse{
		Code:    CodeServerError,
		Message: msg,
		Data:    nil,
	})
}

// Respond 统一响应辅助函数：自动识别 AppError 并提取对应的业务码
// 使用方式：common.Respond(c, data, err) 替代手动 if err != nil { ... }
func Respond(c *gin.Context, data any, err error) {
	if err != nil {
		var appErr *AppError
		if errors.As(err, &appErr) {
			c.JSON(http.StatusOK, ApiResponse{
				Code:    appErr.BizCode,
				Message: appErr.Message,
				Data:    nil,
			})
		} else {
			c.JSON(http.StatusOK, ApiResponse{
				Code:    CodeServerError,
				Message: err.Error(),
				Data:    nil,
			})
		}
		return
	}
	c.JSON(http.StatusOK, ApiResponse{
		Code:    CodeSuccess,
		Message: "success",
		Data:    data,
	})
}

// HandleServiceError 处理服务层错误 — 区分 AppError 和普通 error
// AppError → 使用其自带的 BizCode；普通 error → 返回 BadRequest
func HandleServiceError(c *gin.Context, err error) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		FailWithMsg(c, appErr.BizCode, appErr.Message)
	} else {
		FailWithMsg(c, CodeBadRequest, err.Error())
	}
}
