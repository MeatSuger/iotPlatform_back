package common

import "errors"

// AppError 统一业务错误类型
// middleware/recovery.go 与 service/device_report_service.go 共用此类型
type AppError struct {
	BizCode int    // 业务状态码
	Message string // 错误消息
}

// Error 实现 error 接口
func (e *AppError) Error() string {
	return e.Message
}

// ========================================
// 业务错误哨兵
// ========================================
var (
	ErrUserNotFound    = errors.New("用户不存在")
	ErrPasswordWrong   = errors.New("密码错误")
	ErrAccountDisabled = errors.New("账号已被禁用")
	ErrAccountExist    = errors.New("账号已存在")
)
