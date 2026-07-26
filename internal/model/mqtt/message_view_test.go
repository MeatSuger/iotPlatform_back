package mqtt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewMessageView(t *testing.T) {
	msg := NewMessageView("iot/test", "hello", 1, true, false)
	assert.Equal(t, "iot/test", msg.Topic)
	assert.Equal(t, "hello", msg.Payload)
	assert.Equal(t, 1, msg.Qos)
	assert.True(t, msg.Retained)
	assert.False(t, msg.Duplicate)
	assert.False(t, msg.ReceivedAt.IsZero())
}

func TestMessageView_Defaults(t *testing.T) {
	msg := NewMessageView("topic", "payload", 0, false, false)
	assert.Equal(t, 0, msg.Qos)
	assert.False(t, msg.Retained)
	assert.False(t, msg.Duplicate)
}
