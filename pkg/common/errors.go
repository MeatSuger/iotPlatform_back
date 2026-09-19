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
