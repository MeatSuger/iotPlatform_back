package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeviceRegisterRequest_Fields(t *testing.T) {
	req := DeviceRegisterRequest{
		DeviceName:      "ESP32-01",
		DeviceType:      "sensor",
		FirmwareVersion: "1.0.0",
		IPAddress:       "192.168.1.100",
		MacAddress:      "AA:BB:CC:DD:EE:FF",
		Location:        "机房A",
	}
	assert.Equal(t, "ESP32-01", req.DeviceName)
	assert.Equal(t, "sensor", req.DeviceType)
	assert.Equal(t, "1.0.0", req.FirmwareVersion)
	assert.Equal(t, "192.168.1.100", req.IPAddress)
	assert.Equal(t, "AA:BB:CC:DD:EE:FF", req.MacAddress)
	assert.Equal(t, "机房A", req.Location)
}

func TestDeviceRegisterResponse_Fields(t *testing.T) {
	resp := DeviceRegisterResponse{
		DeviceID:    "abc123",
		DeviceToken: "token-xxx",
	}
	assert.Equal(t, "abc123", resp.DeviceID)
	assert.Equal(t, "token-xxx", resp.DeviceToken)
}

func TestNewDeviceService(t *testing.T) {
	svc := NewDeviceService(nil, nil)
	assert.NotNil(t, svc)
	assert.Nil(t, svc.repo)
	assert.Nil(t, svc.cache)
}
