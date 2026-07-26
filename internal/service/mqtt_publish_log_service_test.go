package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewMqttPublishLogService(t *testing.T) {
	svc := NewMqttPublishLogService(nil)
	assert.NotNil(t, svc)
	assert.Nil(t, svc.repo)
}
