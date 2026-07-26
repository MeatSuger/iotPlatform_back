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
	svc := NewInfluxDBService("http://localhost:8086", "token", "org", "bucket")
	assert.NotNil(t, svc)
	assert.Equal(t, "org", svc.org)
	assert.Equal(t, "bucket", svc.bucket)
	assert.NotNil(t, svc.client)
	defer svc.Close()
}

func TestInfluxDBService_Close(t *testing.T) {
	svc := NewInfluxDBService("http://localhost:8086", "token", "org", "bucket")
	// Closing should not panic
	assert.NotPanics(t, func() {
		svc.Close()
	})
}

func TestInfluxDBService_DoubleClose(t *testing.T) {
	svc := NewInfluxDBService("http://localhost:8086", "token", "org", "bucket")
	svc.Close()
	// Double close should be safe
	assert.NotPanics(t, func() {
		svc.Close()
	})
}

func TestInfluxDBService_PingNoConnection(t *testing.T) {
	svc := NewInfluxDBService("http://localhost:9999", "token", "org", "bucket")
	defer svc.Close()
	// Without a real InfluxDB, Ping should return error
	err := svc.Ping(t.Context())
	assert.Error(t, err)
}
