package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeviceReportError_Values(t *testing.T) {
	assert.Equal(t, 401, ErrInvalidDeviceToken.BizCode)
	assert.Equal(t, "设备Token无效", ErrInvalidDeviceToken.Message)

	assert.Equal(t, 401, ErrDeviceTokenMismatch.BizCode)
	assert.Equal(t, "设备Token不匹配", ErrDeviceTokenMismatch.Message)

	assert.Equal(t, 404, ErrDeviceNotFound.BizCode)
	assert.Equal(t, "设备不存在", ErrDeviceNotFound.Message)

	assert.Equal(t, 401, ErrDeviceTokenNotFound.BizCode)
	assert.Equal(t, "缺少设备Token", ErrDeviceTokenNotFound.Message)
}

func TestNewDeviceReportService(t *testing.T) {
	svc := NewDeviceReportService(nil, nil, nil, nil)
	assert.NotNil(t, svc)
	assert.Nil(t, svc.deviceRepo)
	assert.Nil(t, svc.influxSvc)
	assert.Nil(t, svc.cache)
	assert.Nil(t, svc.deviceSvc)
}
