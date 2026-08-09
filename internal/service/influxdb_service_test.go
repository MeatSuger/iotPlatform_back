package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// helper: 创建测试用 InfluxDBConfig
func testConfig(url string) InfluxDBConfig {
	return InfluxDBConfig{
		URL:                   url,
		Token:                 "token",
		Database:              "my-database",
		AuthScheme:            "Bearer",
		WriteTimeout:          10 * time.Second,
		QueryTimeout:          2 * time.Minute,
		IdleConnectionTimeout: 90 * time.Second,
		MaxIdleConnections:    10,
	}
}

func TestSensorData_Fields(t *testing.T) {
	d := SensorData{
		DeviceID:   "abc123",
		SensorName: "temperature",
		Type:       "number",
		Value:      26.5,
	}
	assert.Equal(t, "abc123", d.DeviceID)
	assert.Equal(t, "temperature", d.SensorName)
	assert.Equal(t, "number", d.Type)
	assert.Equal(t, 26.5, d.Value)
	assert.True(t, d.Timestamp.IsZero())
}

func TestNewInfluxDBService(t *testing.T) {
	svc := NewInfluxDBService(testConfig("http://localhost:8086"))
	assert.NotNil(t, svc)
	assert.Equal(t, "my-database", svc.database)
	defer svc.Close()
}

func TestInfluxDBService_Close(t *testing.T) {
	svc := NewInfluxDBService(testConfig("http://localhost:8086"))
	// Closing should not panic
	assert.NotPanics(t, func() {
		svc.Close()
	})
}

func TestInfluxDBService_DoubleClose(t *testing.T) {
	svc := NewInfluxDBService(testConfig("http://localhost:8086"))
	svc.Close()
	// Double close should be safe
	assert.NotPanics(t, func() {
		svc.Close()
	})
}

func TestInfluxDBService_PingNoConnection(t *testing.T) {
	svc := NewInfluxDBService(testConfig("http://localhost:9999"))
	defer svc.Close()
	// Without a real InfluxDB, Ping should return error
	err := svc.Ping(t.Context())
	assert.Error(t, err)
}

func TestInfluxDBService_IsConnected(t *testing.T) {
	// Bad URL should still create a client (v3 validates lazily)
	svc := NewInfluxDBService(testConfig("http://localhost:8086"))
	// Client may be nil if New() fails
	_ = svc.IsConnected()
	svc.Close()
}

func TestInfluxDBService_SetCache(t *testing.T) {
	svc := NewInfluxDBService(testConfig("http://localhost:8086"))
	assert.NotPanics(t, func() {
		svc.SetCache(nil)
	})
	svc.Close()
}
