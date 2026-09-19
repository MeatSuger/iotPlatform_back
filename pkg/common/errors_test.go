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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppError_Error(t *testing.T) {
	err := &AppError{BizCode: 1001, Message: "参数错误"}
	assert.Equal(t, "参数错误", err.Error())
	assert.Equal(t, 1001, err.BizCode)
}

func TestAppError_IsAppError(t *testing.T) {
	err := &AppError{BizCode: 1002, Message: "未登录"}
	var appErr *AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, "未登录", appErr.Message)
}

func TestSentinelErrors(t *testing.T) {
	assert.EqualError(t, ErrUserNotFound, "用户不存在")
	assert.EqualError(t, ErrPasswordWrong, "密码错误")
	assert.EqualError(t, ErrAccountDisabled, "账号已被禁用")
	assert.EqualError(t, ErrAccountExist, "账号已存在")
}

func TestSentinelErrors_Is(t *testing.T) {
	assert.True(t, errors.Is(ErrUserNotFound, ErrUserNotFound))
	assert.False(t, errors.Is(ErrUserNotFound, ErrPasswordWrong))
}
