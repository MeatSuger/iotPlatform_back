package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDownlinkCmdRequest_Fields(t *testing.T) {
	req := DownlinkCmdRequest{
		Type:    "control",
		Payload: []byte(`{"action":"reboot"}`),
	}
	assert.Equal(t, "control", req.Type)
	assert.Equal(t, `{"action":"reboot"}`, string(req.Payload))
}

func TestDownlinkCmdResponse_Fields(t *testing.T) {
	resp := DownlinkCmdResponse{
		ID:      1,
		Type:    "config",
		Payload: []byte(`{"interval":60}`),
	}
	assert.Equal(t, uint(1), resp.ID)
	assert.Equal(t, "config", resp.Type)
	assert.Equal(t, `{"interval":60}`, string(resp.Payload))
}

func TestCmdQueueConstants(t *testing.T) {
	assert.Equal(t, "cmd:queue:", cmdQueuePrefix)
	assert.Equal(t, 200, cmdQueueMaxLen)
}

func TestNewDownlinkService(t *testing.T) {
	svc := NewDownlinkService(nil, nil, nil, nil)
	assert.NotNil(t, svc)
	assert.Nil(t, svc.cmdRepo)
	assert.Nil(t, svc.deviceRepo)
	assert.Nil(t, svc.rdb)
	assert.Nil(t, svc.wsHub)
}

func TestDownlinkService_PublishViaMQTT(t *testing.T) {
	svc := NewDownlinkService(nil, nil, nil, nil)
	req := svc.PublishViaMQTT("abc123", `{"action":"reboot"}`)
	assert.Equal(t, "device/abc123/cmd", req.Topic)
	assert.Equal(t, `{"action":"reboot"}`, req.Payload)
	assert.Equal(t, byte(1), req.GetQos())
}
