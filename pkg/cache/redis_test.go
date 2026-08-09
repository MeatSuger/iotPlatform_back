package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRedisCacheConstants(t *testing.T) {
	assert.Equal(t, "cache:device:", PrefixDevice)
	assert.Equal(t, "cache:sensor_recent:", PrefixSensorRecent)
	assert.Equal(t, "cache:mqtt_msg:", PrefixMQTTMessage)
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
	assert.Equal(t, "cache:device:abc123", PrefixDevice+"abc123")
	assert.Equal(t, "cache:mqtt_msg:abc123", PrefixMQTTMessage+"abc123")
}

// Test context-based operations - basic API coverage
func TestRedisCache_MethodSignatures(t *testing.T) {
	cache := NewRedisCache(nil)
	assert.NotNil(t, cache)
	assert.Nil(t, cache.GetClient())
}

// Test jitterTTL produces values in correct range [0.75*base, 1.25*base)
func TestJitterTTL_Range(t *testing.T) {
	base := 100 * time.Second
	min := base * 3 / 4 // 75s
	max := base * 5 / 4 // 125s
	for i := 0; i < 500; i++ {
		result := jitterTTL(base)
		assert.GreaterOrEqual(t, result, min, "jitterTTL too small: %v", result)
		assert.Less(t, result, max, "jitterTTL too large: %v", result)
	}
}

func TestJitterTTL_Zero(t *testing.T) {
	assert.Equal(t, time.Duration(0), jitterTTL(0))
}

// Test LocalCache new API
func TestLocalCache_BytesAPI(t *testing.T) {
	lc := NewLocalCache(5 * time.Second)
	defer lc.Close()

	key := "test:key"
	data := []byte(`{"hello":"world"}`)

	lc.setBytes(key, data, 10*time.Second)
	result, found, isNeg := lc.getBytes(key)
	assert.True(t, found)
	assert.False(t, isNeg)
	assert.Equal(t, data, result)
}

func TestLocalCache_NegativeCache(t *testing.T) {
	lc := NewLocalCache(5 * time.Second)
	defer lc.Close()

	key := "test:neg"
	lc.setNeg(key, 10*time.Second)

	_, found, isNeg := lc.getBytes(key)
	assert.True(t, found)
	assert.True(t, isNeg)
}

func TestLocalCache_PublicAPI_Compat(t *testing.T) {
	lc := NewLocalCache(5 * time.Second)
	defer lc.Close()

	// Set string
	lc.Set("key1", "ONLINE")
	val, ok := lc.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "ONLINE", val)

	// SetWithTTL struct
	type testStruct struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	lc.SetWithTTL("key2", testStruct{Name: "test", Age: 30}, time.Minute)
	val2, ok2 := lc.Get("key2")
	assert.True(t, ok2)
	// Struct → JSON → map (expected for public API)
	m, ok3 := val2.(map[string]interface{})
	assert.True(t, ok3)
	assert.Equal(t, "test", m["name"])
}

func TestLocalCache_Expire(t *testing.T) {
	lc := NewLocalCache(5 * time.Second)
	defer lc.Close()

	lc.setBytes("exp", []byte("data"), 50*time.Millisecond)
	_, found, _ := lc.getBytes("exp")
	assert.True(t, found)

	time.Sleep(100 * time.Millisecond)
	_, found, _ = lc.getBytes("exp")
	assert.False(t, found)
}

func TestLocalCache_Delete(t *testing.T) {
	lc := NewLocalCache(5 * time.Second)
	defer lc.Close()

	lc.setBytes("del", []byte("data"), time.Minute)
	lc.Delete("del")
	_, found, _ := lc.getBytes("del")
	assert.False(t, found)
}

func TestLocalCache_Close(t *testing.T) {
	lc := NewLocalCache(5 * time.Second)
	assert.NotPanics(t, func() {
		lc.Close()
	})
}

func TestToBytes(t *testing.T) {
	// []byte input
	b, err := toBytes([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, []byte("hello"), b)

	// string input
	b2, err := toBytes("world")
	assert.NoError(t, err)
	assert.Equal(t, []byte("world"), b2)

	// struct input
	b3, err := toBytes(map[string]int{"a": 1})
	assert.NoError(t, err)
	assert.Contains(t, string(b3), `"a":1`)
}
