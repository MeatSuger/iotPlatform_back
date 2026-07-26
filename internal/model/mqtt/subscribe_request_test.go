package mqtt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubscribeRequest_GetQos(t *testing.T) {
	// Default
	req := SubscribeRequest{}
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
