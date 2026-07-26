package common

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppError_Error(t *testing.T) {
	err := NewAppError(400, 1001, "参数错误")
	assert.Equal(t, "参数错误", err.Error())
	assert.Equal(t, 400, err.HTTPCode)
	assert.Equal(t, 1001, err.BizCode)
}

func TestAppError_IsAppError(t *testing.T) {
	err := NewAppError(401, 1002, "未登录")
	var appErr *AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, "未登录", appErr.Message)
}

func TestSentinelErrors(t *testing.T) {
	assert.EqualError(t, ErrUserNotFound, "用户不存在")
	assert.EqualError(t, ErrPasswordWrong, "密码错误")
	assert.EqualError(t, ErrAccountDisabled, "账号已被禁用")
	assert.EqualError(t, ErrAccountExist, "账号已存在")
	assert.EqualError(t, ErrMultiLoginLimited, "多端登录数量已达上限")
}

func TestSentinelErrors_Is(t *testing.T) {
	assert.True(t, errors.Is(ErrUserNotFound, ErrUserNotFound))
	assert.False(t, errors.Is(ErrUserNotFound, ErrPasswordWrong))
}
