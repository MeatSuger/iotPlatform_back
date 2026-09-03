package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/stretchr/testify/assert"
)

// testConfig 创建测试用 InfluxDBConfig
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
	// 关闭不应 panic
	assert.NotPanics(t, func() {
		svc.Close()
	})
}

func TestInfluxDBService_DoubleClose(t *testing.T) {
	svc := NewInfluxDBService(testConfig("http://localhost:8086"))
	svc.Close()
	// 重复关闭应安全
	assert.NotPanics(t, func() {
		svc.Close()
	})
}

func TestInfluxDBService_PingNoConnection(t *testing.T) {
	svc := NewInfluxDBService(testConfig("http://localhost:9999"))
	defer svc.Close()
	// 无真实 InfluxDB 时 Ping 应返回错误
	err := svc.Ping(t.Context())
	assert.Error(t, err)
}

func TestInfluxDBService_IsConnected(t *testing.T) {
	// 无效 URL 仍能创建客户端（v3 延迟校验），New() 失败时 client 可能为 nil
	svc := NewInfluxDBService(testConfig("http://localhost:8086"))
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

// ========================================
// 错误处理/未连接/异步路径（无需真实 InfluxDB）
// ========================================

func TestInfluxDBService_WriteSensors_NotConnected(t *testing.T) {
	svc := &InfluxDBService{database: "iot"} // client nil
	err := svc.WriteSensors(context.Background(), []SensorData{{DeviceID: "d1"}})
	assert.ErrorContains(t, err, "客户端未连接")
}

func TestInfluxDBService_WriteSensors_ConnectionRefused(t *testing.T) {
	// 指向不可达地址：WriteData 快速失败（连接拒绝）
	svc := NewInfluxDBService(InfluxDBConfig{
		URL:          "http://127.0.0.1:1",
		Token:        "t",
		Database:     "iot",
		WriteTimeout: 500 * time.Millisecond,
	})
	assert.True(t, svc.IsConnected())

	// 零值 Timestamp/Measurement 在写失败路径前已被规范化（不 panic）
	data := []SensorData{{DeviceID: "d1", SensorName: "t", Value: 1.0}}
	err := svc.WriteSensors(context.Background(), data)
	assert.Error(t, err)
}

func TestInfluxDBService_WriteSensorsAsync_NotConnected(t *testing.T) {
	svc := &InfluxDBService{database: "iot"}
	svc.WriteSensorsAsync([]SensorData{{DeviceID: "d1"}}) // goroutine 内报错并退出，不 panic
	time.Sleep(30 * time.Millisecond)
}

func TestInfluxDBService_WriteSensorsBatched_NoBatcher(t *testing.T) {
	svc := &InfluxDBService{database: "iot"}
	// batcher 为 nil → 回退到异步写入（nil client 报错退出）
	svc.WriteSensorsBatched([]SensorData{{DeviceID: "d1", SensorName: "t", Value: 1}})
	time.Sleep(30 * time.Millisecond)
}

func TestInfluxDBService_FlushBatched_Safe(t *testing.T) {
	svc := &InfluxDBService{database: "iot"}
	svc.FlushBatched() // batcher nil → no-op

	svc2 := NewInfluxDBService(InfluxDBConfig{
		URL: "http://127.0.0.1:1", Token: "t", Database: "iot", BatchSize: 10,
	})
	svc2.FlushBatched() // client 已连（惰性），batcher 非空但 flush 失败仅告警
}

func TestInfluxDBService_Ping_NotConnected(t *testing.T) {
	svc := &InfluxDBService{database: "iot"}
	err := svc.Ping(context.Background())
	assert.ErrorContains(t, err, "客户端未连接")
}

func TestInfluxDBService_HandleWriteError(t *testing.T) {
	svc := &InfluxDBService{database: "iot"}

	t.Run("nil 错误", func(t *testing.T) {
		assert.NoError(t, svc.handleWriteError(nil))
	})

	t.Run("普通错误透传", func(t *testing.T) {
		err := errors.New("boom")
		assert.ErrorIs(t, svc.handleWriteError(err), err)
	})

	t.Run("PartialWriteError 分类", func(t *testing.T) {
		perr := &influxdb3.PartialWriteError{
			ServerError: influxdb3.ServerError{Message: "partial"},
			LineErrors: []influxdb3.PartialWriteLineError{
				{LineNumber: 3, ErrorMessage: "bad line", OriginalLine: "x"},
			},
		}
		out := svc.handleWriteError(perr)
		assert.ErrorContains(t, out, "partial write: 1 lines failed")
	})

	t.Run("ServerError 分类", func(t *testing.T) {
		serr := &influxdb3.ServerError{StatusCode: 429, Message: "rate limited", Code: "slow_down"}
		out := svc.handleWriteError(serr)
		assert.ErrorContains(t, out, "server error [429]: rate limited")
	})
}

func TestInfluxDBService_LogWriteError(t *testing.T) {
	svc := &InfluxDBService{database: "iot"}

	svc.logWriteError("msg", 0, nil) // no-op
	svc.logWriteError("partial", 3, &influxdb3.PartialWriteError{LineErrors: []influxdb3.PartialWriteLineError{{LineNumber: 1}}})
	svc.logWriteError("server", 3, &influxdb3.ServerError{StatusCode: 500, Message: "err"})
	svc.logWriteError("plain", 3, errors.New("boom"))
}
