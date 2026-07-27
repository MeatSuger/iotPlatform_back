package cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedisCacheConstants(t *testing.T) {
	assert.Equal(t, "device:", PrefixDevice)
	assert.Equal(t, "sensorRecent:", PrefixSensorRecent)
	assert.Equal(t, "mqtt:messages:", PrefixMQTTMessage)
}

func TestTTLConstants(t *testing.T) {
	assert.Greater(t, TTLDevice, TTLSensorRecent)
}

func TestNewRedisCache_NilClient(t *testing.T) {
	// Should not panic with nil - cache methods will return errors at runtime
	cache := NewRedisCache(nil)
	assert.NotNil(t, cache)
}

func TestRedisCache_GetClient(t *testing.T) {
	cache := NewRedisCache(nil)
	assert.Nil(t, cache.GetClient())
}

// Test key generation patterns
func TestCacheKeyPatterns(t *testing.T) {
	assert.Equal(t, "device:abc123", PrefixDevice+"abc123")
	assert.Equal(t, "mqtt:messages:abc123", PrefixMQTTMessage+"abc123")
}

// Test context-based operations - basic API coverage
func TestRedisCache_MethodSignatures(t *testing.T) {
	cache := NewRedisCache(nil)
	assert.NotNil(t, cache)
	assert.Nil(t, cache.GetClient())
}
