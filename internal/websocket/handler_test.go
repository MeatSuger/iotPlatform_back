package websocket

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewWsHandler(t *testing.T) {
	hub := NewHub()
	handler := NewWsHandler(hub, nil)

	assert.NotNil(t, handler)
	assert.NotNil(t, handler.hub)
	assert.Nil(t, handler.mqttClient)
	assert.Nil(t, handler.validateDevToken)
}

func TestWsHandler_SetDeviceTokenValidator(t *testing.T) {
	handler := NewWsHandler(NewHub(), nil)
	assert.Nil(t, handler.validateDevToken)

	called := false
	handler.SetDeviceTokenValidator(func(token string) (string, error) {
		called = true
		return "dev-001", nil
	})

	assert.NotNil(t, handler.validateDevToken)
	id, err := handler.validateDevToken("test-token")
	assert.NoError(t, err)
	assert.Equal(t, "dev-001", id)
	assert.True(t, called)
}

func TestWsHandler_SetDeviceMessageHandler(t *testing.T) {
	handler := NewWsHandler(NewHub(), nil)
	assert.Nil(t, handler.onDeviceMessage)

	called := false
	handler.SetDeviceMessageHandler(func(deviceID, token string, message []byte) {
		called = true
	})

	assert.NotNil(t, handler.onDeviceMessage)
	handler.onDeviceMessage("dev-001", "token", []byte("test"))
	assert.True(t, called)
}

func TestWsHandler_SetOwnerResolver(t *testing.T) {
	handler := NewWsHandler(NewHub(), nil)
	assert.Nil(t, handler.resolveOwner)

	handler.SetOwnerResolver(func(deviceID string) (uint, error) {
		return 1001, nil
	})

	assert.NotNil(t, handler.resolveOwner)
	id, err := handler.resolveOwner("dev-001")
	assert.NoError(t, err)
	assert.Equal(t, uint(1001), id)
}

func TestWsHandler_SetUserCommandHandler(t *testing.T) {
	handler := NewWsHandler(NewHub(), nil)
	assert.Nil(t, handler.onUserCommand)

	called := false
	handler.SetUserCommandHandler(func(ownerID uint, deviceID, cmdType string, payload json.RawMessage) (uint, error) {
		called = true
		return 42, nil
	})

	assert.NotNil(t, handler.onUserCommand)
	id, err := handler.onUserCommand(1001, "dev-001", "reboot", json.RawMessage(`{"delay":5}`))
	assert.NoError(t, err)
	assert.Equal(t, uint(42), id)
	assert.True(t, called)
}

func TestWsHandler_SetDeviceStatusUpdater(t *testing.T) {
	handler := NewWsHandler(NewHub(), nil)
	assert.Nil(t, handler.deviceStatusUpdater)

	handler.SetDeviceStatusUpdater(func(deviceID, status string) error {
		return nil
	})

	assert.NotNil(t, handler.deviceStatusUpdater)
	err := handler.deviceStatusUpdater("dev-001", "ONLINE")
	assert.NoError(t, err)
}
