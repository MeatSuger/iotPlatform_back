package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSensorPoint_Fields(t *testing.T) {
	p := SensorPoint{
		DeviceID:   "abc123",
		SensorName: "temperature",
		Type:       "number",
		Value:      26.5,
	}
	assert.Equal(t, "abc123", p.DeviceID)
	assert.Equal(t, "temperature", p.SensorName)
	assert.Equal(t, "number", p.Type)
	assert.Equal(t, 26.5, p.Value)
	assert.True(t, p.Timestamp.IsZero())
}

func TestNewInfluxDBService(t *testing.T) {
	svc := NewInfluxDBService("http://localhost:8086", "token", "my-database", "Bearer")
	assert.NotNil(t, svc)
	assert.Equal(t, "my-database", svc.database)
	defer svc.Close()
}

func TestInfluxDBService_Close(t *testing.T) {
	svc := NewInfluxDBService("http://localhost:8086", "token", "my-database", "")
	// Closing should not panic
	assert.NotPanics(t, func() {
		svc.Close()
	})
}

func TestInfluxDBService_DoubleClose(t *testing.T) {
	svc := NewInfluxDBService("http://localhost:8086", "token", "my-database", "")
	svc.Close()
	// Double close should be safe
	assert.NotPanics(t, func() {
		svc.Close()
	})
}

func TestInfluxDBService_PingNoConnection(t *testing.T) {
	svc := NewInfluxDBService("http://localhost:9999", "token", "my-database", "")
	defer svc.Close()
	// Without a real InfluxDB, Ping should return error
	err := svc.Ping(t.Context())
	assert.Error(t, err)
}

func TestInfluxDBService_IsConnected(t *testing.T) {
	// Bad URL should still create a client (v3 validates lazily)
	svc := NewInfluxDBService("http://localhost:8086", "token", "my-database", "")
	// Client may be nil if New() fails
	_ = svc.IsConnected()
	svc.Close()
}

func TestInfluxDBService_SetCache(t *testing.T) {
	svc := NewInfluxDBService("http://localhost:8086", "token", "my-database", "")
	assert.NotPanics(t, func() {
		svc.SetCache(nil)
	})
	svc.Close()
}
