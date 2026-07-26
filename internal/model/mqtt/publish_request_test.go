package mqtt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPublishRequest_GetQos(t *testing.T) {
	// Default
	req := PublishRequest{}
	assert.Equal(t, byte(0), req.GetQos())

	// Set to 1
	qos := 1
	req.Qos = &qos
	assert.Equal(t, byte(1), req.GetQos())

	// Set to 2
	qos = 2
	req.Qos = &qos
	assert.Equal(t, byte(2), req.GetQos())
}

func TestPublishRequest_GetRetained(t *testing.T) {
	// Default (nil pointer -> false)
	req := PublishRequest{}
	assert.False(t, req.GetRetained())

	// Set to true
	r := true
	req.Retained = &r
	assert.True(t, req.GetRetained())

	// Set to false
	r = false
	req.Retained = &r
	assert.False(t, req.GetRetained())
}

func TestPublishRequest_JSON(t *testing.T) {
	jsonBody := `{"topic":"iot/test","qos":1,"payload":"hello","retained":true}`
	var req PublishRequest
	err := json.Unmarshal([]byte(jsonBody), &req)
	assert.NoError(t, err)
	assert.Equal(t, "iot/test", req.Topic)
	assert.Equal(t, byte(1), req.GetQos())
	assert.Equal(t, "hello", req.Payload)
	assert.True(t, req.GetRetained())
}

func TestPublishRequest_JSONDefaults(t *testing.T) {
	jsonBody := `{"topic":"iot/test","payload":"hello"}`
	var req PublishRequest
	err := json.Unmarshal([]byte(jsonBody), &req)
	assert.NoError(t, err)
	assert.Equal(t, "iot/test", req.Topic)
	assert.Equal(t, byte(0), req.GetQos()) // nil pointer → default 0
	assert.Equal(t, "hello", req.Payload)
	assert.False(t, req.GetRetained())
}
