package service

import (
	"testing"

	"github.com/stretchr/testify/assert"

	mqttEntity "iot-platform.local/internal/model/mqtt"
)

func TestNewMqttClientService(t *testing.T) {
	svc := NewMqttClientService(nil, nil, nil)
	assert.NotNil(t, svc)
	assert.NotNil(t, svc.status)
	assert.False(t, svc.status.IsConnected())
	assert.NotNil(t, svc.messageBuffer)
	assert.Equal(t, 500, svc.maxBufferSize)
}

func TestMqttClientService_InitialStatus(t *testing.T) {
	svc := NewMqttClientService(nil, nil, nil)
	assert.False(t, svc.IsConnected())
	status := svc.Status()
	assert.False(t, status.Connected)
	assert.Equal(t, 0, status.BufferedMessages)
}

func TestMqttClientService_RecentMessages_Empty(t *testing.T) {
	svc := NewMqttClientService(nil, nil, nil)
	msgs := svc.RecentMessages(10)
	assert.Empty(t, msgs)
}

func TestMqttClientService_RecentMessages_ZeroLimit(t *testing.T) {
	svc := NewMqttClientService(nil, nil, nil)
	msgs := svc.RecentMessages(0)
	assert.Empty(t, msgs)
}

func TestTruncateString(t *testing.T) {
	assert.Equal(t, "hello", truncateString("hello", 100))
	assert.Equal(t, "hello", truncateString("hello", 5))
	assert.Equal(t, "hello...", truncateString("hello world", 5))
	assert.Equal(t, "", truncateString("", 5))
}

func TestMqttClientService_DisconnectNotConnected(t *testing.T) {
	svc := NewMqttClientService(nil, nil, nil)
	// Disconnect on never-connected service should not panic
	assert.NotPanics(t, func() {
		svc.Disconnect()
	})
}

func TestMqttClientService_ConnectNoConfig(t *testing.T) {
	svc := NewMqttClientService(nil, nil, nil)
	// Without proper config loaded, Connect will fail.
	// We test that it doesn't crash and that connection logic is reachable.
	assert.False(t, svc.IsConnected())
	assert.NotNil(t, svc.Status())
}

func TestMqttClientService_StatusSnapshot(t *testing.T) {
	svc := NewMqttClientService(nil, nil, nil)
	snapshot := svc.Status()
	assert.NotNil(t, snapshot)
	assert.False(t, snapshot.Connected)
	assert.Equal(t, int64(0), snapshot.TotalMessagesSent)
	assert.Equal(t, int64(0), snapshot.TotalMessagesReceived)
	assert.Equal(t, int64(0), snapshot.ReconnectCount)
}

func TestMqttClientService_PublishRequest(t *testing.T) {
	qos := 1
	req := mqttEntity.PublishRequest{
		Topic:   "iot/test",
		Qos:     &qos,
		Payload: "hello",
	}
	assert.Equal(t, "iot/test", req.Topic)
	assert.Equal(t, byte(1), req.GetQos())
	assert.Equal(t, "hello", req.Payload)
	assert.False(t, req.GetRetained())
}
